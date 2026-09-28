// Package raft — Raft 状态持久化接口定义。
//
// 真实生产环境中，currentTerm 和 votedFor 必须落盘，否则节点重启后
// 可能重复投票或接受过期任期，导致集群脑裂。
package raft

import "medtrust-raft/internal/transport"

// Persister 定义 Raft 硬状态（Hard State）的持久化契约。
// 实现者只需保证 Save 的原子性即可，Raft 核心负责在正确时机调用。
type Persister interface {
	// SaveRaftState 持久化当前任期和投票目标。
	// term 和 votedFor 必须在同一事务/同一文件中落盘。
	SaveRaftState(term transport.Term, votedFor transport.NodeID) error

	// LoadRaftState 从存储中恢复任期和投票目标。
	// 若从未持久化过，返回 term=0, votedFor="", nil。
	LoadRaftState() (term transport.Term, votedFor transport.NodeID, err error)
}
