// Package raft — Leader 心跳 + 日志复制逻辑
package raft

import (
	"time"

	"medtrust-raft/internal/transport"
)

// -----------------------------------------------------------------------
// § Leader 初始化
// -----------------------------------------------------------------------

// initLeaderState 在赢得选举后立即调用，初始化 Leader 专属的追踪表。
func (n *Node) initLeaderState() {
	n.mu.Lock()
	defer n.mu.Unlock()
	nextIdx := n.log.LastIndex() + 1
	for _, p := range n.cfg.Peers {
		n.nextIndex[p.ID()] = nextIdx
		n.matchIndex[p.ID()] = 0
	}
}

// -----------------------------------------------------------------------
// § Leader 主循环
// -----------------------------------------------------------------------

// runLeader 驱动 Leader 的心跳与复制。
// 每隔 HeartbeatMs 向所有 Follower 广播 AppendEntries（心跳或携带日志）。
func (n *Node) runLeader() {
	ticker := time.NewTicker(time.Duration(n.cfg.HeartbeatMs) * time.Millisecond)
	defer ticker.Stop()

	// 立即发送一轮心跳，宣告自己当选
	n.broadcastAppendEntries()

	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.mu.Lock()
			if n.role != Leader {
				n.mu.Unlock()
				return
			}
			n.mu.Unlock()
			n.broadcastAppendEntries()
		}
	}
}

// -----------------------------------------------------------------------
// § AppendEntries 广播
// -----------------------------------------------------------------------

// broadcastAppendEntries 并行向所有 Peer 发送 AppendEntries（心跳/复制）。
func (n *Node) broadcastAppendEntries() {
	n.mu.Lock()
	if n.role != Leader {
		n.mu.Unlock()
		return
	}
	peers := n.cfg.Peers
	term := n.currentTerm
	leaderID := n.cfg.ID
	commitIndex := n.commitIndex
	n.mu.Unlock()

	for _, p := range peers {
		go func(peer transport.Peer) {
			n.sendAppendEntriesToPeer(peer, term, leaderID, commitIndex)
		}(p)
	}
}

// sendAppendEntriesToPeer 向单个 Follower 发送 AppendEntries，并处理应答。
func (n *Node) sendAppendEntriesToPeer(
	peer transport.Peer,
	term transport.Term,
	leaderID transport.NodeID,
	leaderCommit transport.LogIndex,
) {
	n.mu.Lock()
	if n.role != Leader || n.currentTerm != term {
		n.mu.Unlock()
		return
	}
	nextIdx := n.nextIndex[peer.ID()]
	prevLogIndex := nextIdx - 1
	prevLogTerm := n.log.TermAt(prevLogIndex)
	entries := n.log.Slice(nextIdx)
	n.mu.Unlock()

	args := &transport.AppendEntriesArgs{
		Term:         term,
		LeaderID:     leaderID,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: leaderCommit,
	}

	if len(entries) > 0 {
		raftLog("REPLICATE", string(leaderID),
			"→ %s  entries [%d..%d] (Term %d)",
			peer.ID(), nextIdx, nextIdx+transport.LogIndex(len(entries))-1, term)
	} else {
		raftLog("HEARTBEAT", string(leaderID), "→ %s (Term %d)", peer.ID(), term)
	}

	reply, err := peer.SendAppendEntries(args)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	// 发现更大任期，退回 Follower
	if reply.Term > n.currentTerm {
		n.stepDown(reply.Term)
		return
	}

	// 任期已变，忽略过时应答
	if n.role != Leader || n.currentTerm != term {
		return
	}

	if reply.Success {
		// 更新 matchIndex / nextIndex
		if len(entries) > 0 {
			newMatch := entries[len(entries)-1].Index
			if newMatch > n.matchIndex[peer.ID()] {
				n.matchIndex[peer.ID()] = newMatch
				n.nextIndex[peer.ID()] = newMatch + 1
			}
		}
		// 尝试推进 commitIndex
		n.maybeAdvanceCommit(term)
	} else {
		// 一致性检查失败：快速回退
		n.nextIndex[peer.ID()] = n.fastBacktrack(peer.ID(), reply)
	}
}

// -----------------------------------------------------------------------
// § 多数派提交
// -----------------------------------------------------------------------

// maybeAdvanceCommit 检查是否有新的日志条目被多数派复制，若有则提交并通知上层。
// 必须在持锁状态下调用。
func (n *Node) maybeAdvanceCommit(term transport.Term) {
	lastIdx := n.log.LastIndex()

	for idx := lastIdx; idx > n.commitIndex; idx-- {
		// Raft 安全性约束：只能提交当前任期的日志条目
		if n.log.TermAt(idx) != term {
			break
		}
		// 统计已复制此条目的节点数（Leader 自身算 1）
		count := 1
		for _, p := range n.cfg.Peers {
			if n.matchIndex[p.ID()] >= idx {
				count++
			}
		}
		total := len(n.cfg.Peers) + 1
		if quorumReached(count, total) {
			// 逐条提交 (commitIndex+1 .. idx)
			for i := n.commitIndex + 1; i <= idx; i++ {
				entry, ok := n.log.At(i)
				if !ok {
					continue
				}
				n.CommitCh <- CommitNotify{Entry: entry}
				raftLog("CONSENSUS", string(n.cfg.ID),
					"data %q committed on majority nodes (Index %d, Term %d)",
					entry.Payload, entry.Index, entry.Term)
				// 检测并应用成员变更
				if isMembershipChange(entry.Payload) {
					n.commitMembershipChangeLocked(i)
				}
			}
			n.commitIndex = idx
			break
		}
	}
}

// -----------------------------------------------------------------------
// § AppendEntries RPC Handler（被远端调用）
// -----------------------------------------------------------------------

// HandleAppendEntries 处理来自 Leader 的心跳或日志复制请求。
func (n *Node) HandleAppendEntries(
	args *transport.AppendEntriesArgs,
	reply *transport.AppendEntriesReply,
) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.Success = false

	// 过时任期，拒绝
	if args.Term < n.currentTerm {
		return nil
	}

	// 发现更大任期或收到合法心跳，退回/保持 Follower
	if args.Term > n.currentTerm {
		n.stepDown(args.Term)
	} else if n.role == Candidate {
		// 同任期内收到 Leader 心跳，放弃选举
		n.setRole(Follower)
		raftLog("FOLLOWER", string(n.cfg.ID),
			"reverted to Follower (Term %d, Leader %s)", args.Term, args.LeaderID)
	}

	reply.Term = n.currentTerm
	n.triggerElectionReset()
	n.lastHeartbeatAt = time.Now()

	// 一致性检查
	if args.PrevLogIndex > 0 {
		prevTerm := n.log.TermAt(args.PrevLogIndex)
		if prevTerm == 0 {
			// 本地日志在 PrevLogIndex 处不存在
			reply.ConflictIndex = n.log.LastIndex() + 1
			reply.ConflictTerm = 0
			return nil
		}
		if prevTerm != args.PrevLogTerm {
			// 任期冲突：找到冲突任期的第一条日志，帮助 Leader 快速回退
			reply.ConflictTerm = prevTerm
			reply.ConflictIndex = n.firstIndexOfTerm(prevTerm)
			return nil
		}
	}

	// 追加日志
	if len(args.Entries) > 0 {
		n.log.AppendAfter(args.Entries)
		raftLog("REPLICATE", string(n.cfg.ID),
			"← %s  appended %d entries (Index %d..%d)",
			args.LeaderID,
			len(args.Entries),
			args.Entries[0].Index,
			args.Entries[len(args.Entries)-1].Index,
		)
	}

	// 推进本地 commitIndex
	if args.LeaderCommit > n.commitIndex {
		newCommit := args.LeaderCommit
		if last := n.log.LastIndex(); last < newCommit {
			newCommit = last
		}
		for i := n.lastApplied + 1; i <= newCommit; i++ {
			entry, ok := n.log.At(i)
			if !ok {
				continue
			}
			n.CommitCh <- CommitNotify{Entry: entry}
			n.lastApplied = i
		}
		n.commitIndex = newCommit
	}

	reply.Success = true
	n.leaderID = args.LeaderID
	return nil
}

// -----------------------------------------------------------------------
// § Propose：Leader 接受外部写请求
// -----------------------------------------------------------------------

// Propose 由 API 层调用，将一条医疗 Payload 写入 Leader 日志。
// 返回写入的 LogIndex，或 error（非 Leader 时）。
func (n *Node) Propose(payload string) (transport.LogIndex, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.role != Leader {
		return 0, ErrNotLeader
	}
	idx := n.log.Append(n.currentTerm, payload)
	raftLog("REPLICATE", string(n.cfg.ID),
		"new entry proposed (Index %d, Payload %q)", idx, payload)
	return idx, nil
}

// -----------------------------------------------------------------------
// § 辅助函数
// -----------------------------------------------------------------------

// fastBacktrack 根据 Follower 返回的冲突信息，计算 Leader 应回退到的 nextIndex。
// 必须在持锁状态下调用。
func (n *Node) fastBacktrack(
	peerID transport.NodeID,
	reply *transport.AppendEntriesReply,
) transport.LogIndex {
	if reply.ConflictTerm == 0 {
		return reply.ConflictIndex
	}
	// 在 Leader 自己的日志中寻找 ConflictTerm 的最后一条
	for i := n.log.LastIndex(); i >= 1; i-- {
		if n.log.TermAt(i) == reply.ConflictTerm {
			return i + 1
		}
	}
	return reply.ConflictIndex
}

// firstIndexOfTerm 返回本节点日志中 term 第一次出现的 Index（必须在持锁下调用）。
func (n *Node) firstIndexOfTerm(term transport.Term) transport.LogIndex {
	for i := transport.LogIndex(1); i <= n.log.LastIndex(); i++ {
		if n.log.TermAt(i) == term {
			return i
		}
	}
	return n.log.LastIndex() + 1
}

// isMembershipChange 检测 payload 是否为成员变更日志。
func isMembershipChange(payload string) bool {
	return len(payload) > 14 && payload[:14] == "MEMBER_CHANGE|"
}

// broadcastAppendEntriesLocked 在已持锁状态下广播 AppendEntries。
func (n *Node) broadcastAppendEntriesLocked() {
	peers := n.cfg.Peers
	term := n.currentTerm
	leaderID := n.cfg.ID
	commitIndex := n.commitIndex
	for _, p := range peers {
		go func(peer transport.Peer) {
			n.sendAppendEntriesToPeer(peer, term, leaderID, commitIndex)
		}(p)
	}
}
