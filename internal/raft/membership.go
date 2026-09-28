// Package raft — 动态成员变更实现。
//
// 采用单节点变更（One-node-at-a-time）策略：
//   - 每次只添加或移除一个节点
//   - 变更先作为特殊日志条目提交，达成共识后生效
//   - 避免联合共识（Joint Consensus）的复杂性，同时保证安全性
//
// 安全性保证：
//   - 只有 Leader 可发起成员变更
//   - 变更日志需经多数派确认后才生效
//   - 防止脑裂：旧配置和新配置的多数派必有交集
package raft

import (
	"fmt"

	"medtrust-raft/internal/transport"
)

// MembershipChangeRequest 是上层 API 传入的成员变更请求。
type MembershipChangeRequest struct {
	Type     transport.ChangeType
	NodeID   transport.NodeID
	NodeAddr string // 添加节点时必填
}

// HandleMembershipChange 处理来自 Leader 的成员变更请求（RPC handler）。
// Follower 收到后直接应用配置变更。
func (n *Node) HandleMembershipChange(
	args *transport.MembershipChangeArgs,
	reply *transport.MembershipChangeReply,
) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.Success = false

	// 发现更大任期，退回 Follower
	if args.Term > n.currentTerm {
		n.stepDown(args.Term)
		return nil
	}

	// 拒绝过时 Leader 的请求
	if args.Term < n.currentTerm {
		return nil
	}

	// 重置选举超时（成员变更消息也视为有效心跳）
	n.triggerElectionReset()
	n.leaderID = args.LeaderID

	// 应用配置变更
	if err := n.applyMembershipChangeLocked(args.Type, args.NodeID, args.NodeAddr); err != nil {
		raftLog("MEMBER", string(n.cfg.ID), "failed to apply membership change: %v", err)
		return nil
	}

	reply.Success = true
	raftLog("MEMBER", string(n.cfg.ID), "applied membership change: %s %s", changeTypeStr(args.Type), args.NodeID)
	return nil
}

// ProposeMembershipChange 由 Leader 调用，向集群提议成员变更。
// 非 Leader 调用将返回错误。
func (n *Node) ProposeMembershipChange(req MembershipChangeRequest) error {
	if !n.IsLeader() {
		return ErrNotLeader
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if n.role != Leader {
		return ErrNotLeader
	}

	// 单节点变更：检查是否已有待处理的变更
	if n.pendingChange != nil {
		return fmt.Errorf("pending membership change in progress")
	}

	// 验证请求
	if req.Type == transport.AddNode && req.NodeAddr == "" {
		return fmt.Errorf("node address required for add operation")
	}

	// 构造成员变更日志条目
	payload := fmt.Sprintf("MEMBER_CHANGE|%d|%s|%s", req.Type, req.NodeID, req.NodeAddr)
	idx := n.log.Append(n.currentTerm, payload)

	n.pendingChange = &req
	raftLog("MEMBER", string(n.cfg.ID), "proposed membership change: %s %s at index %d", changeTypeStr(req.Type), req.NodeID, idx)

	// 立即触发日志复制（不等待心跳）
	n.broadcastAppendEntriesLocked()

	return nil
}

// applyMembershipChangeLocked 在持锁状态下应用成员变更。
func (n *Node) applyMembershipChangeLocked(changeType transport.ChangeType, nodeID transport.NodeID, nodeAddr string) error {
	switch changeType {
	case transport.AddNode:
		// 检查是否已存在
		for _, p := range n.cfg.Peers {
			if p.ID() == nodeID {
				return fmt.Errorf("node %s already exists", nodeID)
			}
		}
		// 创建新 Peer 并添加到列表
		newPeer := transport.NewTCPPeer(nodeID, nodeAddr)
		newPeer.SetTimeout(n.cfg.RPCTimeoutMs)
		n.cfg.Peers = append(n.cfg.Peers, newPeer)

		// Leader 初始化新节点的复制状态
		if n.role == Leader {
			n.nextIndex[nodeID] = n.log.LastIndex() + 1
			n.matchIndex[nodeID] = 0
		}

	case transport.RemoveNode:
		// 不能移除自己
		if nodeID == n.cfg.ID {
			return fmt.Errorf("cannot remove self")
		}
		// 从 Peers 列表中移除
		filtered := make([]transport.Peer, 0, len(n.cfg.Peers))
		for _, p := range n.cfg.Peers {
			if p.ID() != nodeID {
				filtered = append(filtered, p)
			} else {
				p.Close() // 关闭连接
			}
		}
		n.cfg.Peers = filtered

		// Leader 清理复制状态
		if n.role == Leader {
			delete(n.nextIndex, nodeID)
			delete(n.matchIndex, nodeID)
		}

	default:
		return fmt.Errorf("unknown change type: %d", changeType)
	}

	return nil
}

// commitMembershipChange 在日志条目被提交后调用，应用待处理的成员变更。
// 由 Leader 在 updateCommitIndexLocked 中触发。
func (n *Node) commitMembershipChangeLocked(index transport.LogIndex) {
	if n.pendingChange == nil {
		return
	}

	req := n.pendingChange
	n.pendingChange = nil

	// 广播成员变更到所有节点（包括新加入的节点）
	args := &transport.MembershipChangeArgs{
		Term:     n.currentTerm,
		LeaderID: n.cfg.ID,
		Type:     req.Type,
		NodeID:   req.NodeID,
		NodeAddr: req.NodeAddr,
	}

	for _, p := range n.cfg.Peers {
		go func(peer transport.Peer) {
			_, err := peer.SendMembershipChange(args)
			if err != nil {
				raftLog("MEMBER", string(n.cfg.ID), "failed to notify %s: %v", peer.ID(), err)
			}
		}(p)
	}

	raftLog("MEMBER", string(n.cfg.ID), "membership change committed at index %d: %s %s", index, changeTypeStr(req.Type), req.NodeID)
}

// GetPeers 返回当前 Peers 列表的副本（供 API 层查询）。
func (n *Node) GetPeers() []transport.NodeID {
	n.mu.Lock()
	defer n.mu.Unlock()
	ids := make([]transport.NodeID, 0, len(n.cfg.Peers))
	for _, p := range n.cfg.Peers {
		ids = append(ids, p.ID())
	}
	return ids
}

// changeTypeStr 返回变更类型的可读字符串。
func changeTypeStr(t transport.ChangeType) string {
	if t == transport.AddNode {
		return "ADD"
	}
	return "REMOVE"
}
