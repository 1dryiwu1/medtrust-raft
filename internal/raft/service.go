// Package raft — RaftService 是将 Node 的 RPC handler 注册到 net/rpc 的适配层。
package raft

import (
	"medtrust-raft/internal/transport"
)

// RaftService 实现 transport.RaftRPC 接口，作为 net/rpc 的注册对象。
// net/rpc 要求方法签名为 func(args *T, reply *R) error，此结构体满足此要求。
type RaftService struct {
	node *Node
}

// NewRaftService 创建 RaftService 适配器。
func NewRaftService(n *Node) *RaftService {
	return &RaftService{node: n}
}

// RequestVote 实现 transport.RaftRPC 接口，被远端 Candidate 调用。
func (s *RaftService) RequestVote(
	args *transport.RequestVoteArgs,
	reply *transport.RequestVoteReply,
) error {
	return s.node.HandleRequestVote(args, reply)
}

// AppendEntries 实现 transport.RaftRPC 接口，被远端 Leader 调用。
func (s *RaftService) AppendEntries(
	args *transport.AppendEntriesArgs,
	reply *transport.AppendEntriesReply,
) error {
	return s.node.HandleAppendEntries(args, reply)
}

// MembershipChange 实现动态成员变更 RPC，被远端 Leader 调用。
func (s *RaftService) MembershipChange(
	args *transport.MembershipChangeArgs,
	reply *transport.MembershipChangeReply,
) error {
	return s.node.HandleMembershipChange(args, reply)
}
