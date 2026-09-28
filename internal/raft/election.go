// Package raft — 主事件循环 + 选举逻辑
package raft

import (
	"math/rand"
	"sync"
	"time"

	"medtrust-raft/internal/transport"
)

// -----------------------------------------------------------------------
// § 主循环：run()
// 节点整个生命周期在此循环中驱动，根据当前角色分派到对应行为。
// -----------------------------------------------------------------------

func (n *Node) run() {
	for {
		select {
		case <-n.stopCh:
			return
		default:
		}

		n.mu.Lock()
		role := n.role
		n.mu.Unlock()

		switch role {
		case Follower:
			n.runFollower()
		case Candidate:
			n.runCandidate()
		case Leader:
			n.runLeader()
		}
	}
}

// -----------------------------------------------------------------------
// § Follower 行为
// 等待选举超时；若超时前未收到心跳/合法投票，则切换为 Candidate。
// -----------------------------------------------------------------------

func (n *Node) runFollower() {
	timeout := randomElectionTimeout(n.cfg.ElectionMinMs, n.cfg.ElectionMaxMs)
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-n.stopCh:
			return

		case <-n.resetElectionCh:
			// 收到心跳或投票，重置计时器
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(randomElectionTimeout(n.cfg.ElectionMinMs, n.cfg.ElectionMaxMs))

		case <-timer.C:
			// 超时，升级为候选人
			n.mu.Lock()
			n.setRole(Candidate)
			n.mu.Unlock()
			return
		}
	}
}

// -----------------------------------------------------------------------
// § Candidate 行为
// 自增任期、为自己投票、并行向所有 Peer 发送 RequestVote。
// -----------------------------------------------------------------------

func (n *Node) runCandidate() {
	n.mu.Lock()
	n.currentTerm++
	n.votedFor = n.cfg.ID
	n.persistStateLocked()
	term := n.currentTerm
	lastIndex := n.log.LastIndex()
	lastTerm := n.log.LastTerm()
	peers := n.cfg.Peers
	id := n.cfg.ID
	n.mu.Unlock()

	raftLog("ELECTION", string(id), "started election for Term %d", term)

	votes := 1 // 自己的一票
	majority := len(peers)/2 + 1 // 需要的总票数（含自身），peers 不含自身
	if votes >= majority {
		n.mu.Lock()
		n.setRole(Leader)
		n.mu.Unlock()
		n.initLeaderState()
		raftLog("LEADER", string(id), "is elected as Leader (Term %d)", term)
		return
	}

	// 选举超时（若本轮选举未在此时间内结束，则重新发起）
	electionDeadline := time.After(
		randomElectionTimeout(n.cfg.ElectionMinMs, n.cfg.ElectionMaxMs),
	)

	args := &transport.RequestVoteArgs{
		Term:         term,
		CandidateID:  id,
		LastLogIndex: lastIndex,
		LastLogTerm:  lastTerm,
	}

	// 并行向每个 Peer 发送 RequestVote
	type voteResult struct {
		reply *transport.RequestVoteReply
		err   error
	}
	resultCh := make(chan voteResult, len(peers))

	for _, p := range peers {
		go func(peer transport.Peer) {
			reply, err := peer.SendRequestVote(args)
			resultCh <- voteResult{reply, err}
		}(p)
	}

	for i := 0; i < len(peers); i++ {
		select {
		case <-n.stopCh:
			return
		case <-electionDeadline:
			// 选举超时，重新发起（回到 Candidate 循环）
			raftLog("ELECTION", string(id), "election timeout, restarting (Term %d)", term)
			return
		case res := <-resultCh:
			if res.err != nil {
				continue
			}
			n.mu.Lock()
			// 发现更大任期，立刻退回 Follower
			if res.reply.Term > n.currentTerm {
				n.stepDown(res.reply.Term)
				n.mu.Unlock()
				return
			}
			if res.reply.VoteGranted {
				votes++
				raftLog("VOTE", string(id), "received vote (%d/%d) in Term %d", votes, majority, term)
				if votes >= majority {
					// 赢得选举
					n.setRole(Leader)
					n.mu.Unlock()
					n.initLeaderState()
					raftLog("LEADER", string(id), "is elected as Leader (Term %d)", term)
					return
				}
			}
			n.mu.Unlock()
		}
	}

	select {
	case <-n.stopCh:
		return
	case <-electionDeadline:
		raftLog("ELECTION", string(id), "election timeout, restarting (Term %d)", term)
		return
	}
}

// -----------------------------------------------------------------------
// § RequestVote RPC Handler（被远端调用）
// -----------------------------------------------------------------------

// HandleRequestVote 处理来自候选人的拉票请求。
func (n *Node) HandleRequestVote(
	args *transport.RequestVoteArgs,
	reply *transport.RequestVoteReply,
) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 发现更高任期，先退回 Follower
	if args.Term > n.currentTerm {
		n.stepDown(args.Term)
	}

	reply.Term = n.currentTerm
	reply.VoteGranted = false

	if args.Term < n.currentTerm {
		return nil // 过时请求，拒绝
	}

	// 判断是否已投票给别人
	alreadyVoted := n.votedFor != "" && n.votedFor != args.CandidateID
	if alreadyVoted {
		return nil
	}

	// Raft 选举安全性：候选人的日志必须至少和本节点一样新
	if !n.logIsUpToDate(args.LastLogIndex, args.LastLogTerm) {
		return nil
	}

	// 投票
	n.votedFor = args.CandidateID
	n.persistStateLocked()
	reply.VoteGranted = true
	n.triggerElectionReset() // 投票后重置自己的选举超时
	raftLog("VOTE", string(n.cfg.ID), "voted for %s in Term %d", args.CandidateID, args.Term)
	return nil
}

// logIsUpToDate 返回候选人的日志是否至少与本节点一样新（必须在持锁下调用）。
func (n *Node) logIsUpToDate(candLastIndex transport.LogIndex, candLastTerm transport.Term) bool {
	myLastTerm := n.log.LastTerm()
	myLastIndex := n.log.LastIndex()
	if candLastTerm != myLastTerm {
		return candLastTerm > myLastTerm
	}
	return candLastIndex >= myLastIndex
}

// -----------------------------------------------------------------------
// § 工具函数
// -----------------------------------------------------------------------

// triggerElectionReset 非阻塞地发送重置信号（必须在持锁下调用）。
func (n *Node) triggerElectionReset() {
	select {
	case n.resetElectionCh <- struct{}{}:
	default: // channel 已有信号，无需重复发送
	}
}

// randomElectionTimeout 返回 [min, max) 毫秒范围内的随机 Duration。
func randomElectionTimeout(minMs, maxMs int) time.Duration {
	if maxMs <= minMs {
		maxMs = minMs + 1
	}
	ms := minMs + rand.Intn(maxMs-minMs)
	return time.Duration(ms) * time.Millisecond
}

// majority 返回集群中构成多数所需的节点数（含自身）。
func majority(peerCount int) int {
	total := peerCount + 1 // peers 不含自身
	return total/2 + 1
}

// quorumReached 判断 matchCount（已确认复制的节点数，含自身 Leader）是否达到多数派。
func quorumReached(matched, total int) bool {
	return matched >= total/2+1
}

// noopMu 仅用于 initLeaderState 中对 nextIndex/matchIndex 的初始化（已持锁）。
var noopMu sync.Mutex
