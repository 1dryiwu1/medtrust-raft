package transport

import (
	"fmt"
	"net/rpc"
	"sync"
	"time"
)

const defaultRPCTimeout = 300 * time.Millisecond

// TCPPeer 是 Peer 接口的真实网络实现，基于 Go 标准库 net/rpc（JSON编解码）。
// 每个 TCPPeer 实例代表本节点视角下的一个远端邻居节点。
type TCPPeer struct {
	id      NodeID
	addr    string // "host:port"
	mu      sync.Mutex
	client  *rpc.Client // 惰性连接，首次调用时建立
	timeout time.Duration
}

// NewTCPPeer 创建一个指向 addr 的远端节点代理，尚不建立连接。
func NewTCPPeer(id NodeID, addr string) *TCPPeer {
	return &TCPPeer{
		id:      id,
		addr:    addr,
		timeout: defaultRPCTimeout,
	}
}

// ID 实现 Peer 接口。
func (p *TCPPeer) ID() NodeID { return p.id }

// connect 在锁保护下惰性建立 RPC 连接（若已有连接则直接复用）。
func (p *TCPPeer) connect() error {
	if p.client != nil {
		return nil
	}
	c, err := rpc.DialHTTP("tcp", p.addr)
	if err != nil {
		return fmt.Errorf("peer %s dial %s: %w", p.id, p.addr, err)
	}
	p.client = c
	return nil
}

// call 执行一次带超时的 RPC 调用；若连接断开则自动重连一次。
func (p *TCPPeer) call(method string, args, reply any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.connect(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- p.client.Call(method, args, reply) }()

	select {
	case err := <-done:
		if err != nil {
			// 连接可能已失效，重置以便下次重连
			p.client.Close()
			p.client = nil
		}
		return err
	case <-time.After(p.timeout):
		return fmt.Errorf("peer %s: rpc %s timeout", p.id, method)
	}
}

// SendRequestVote 实现 Peer 接口。
func (p *TCPPeer) SendRequestVote(args *RequestVoteArgs) (*RequestVoteReply, error) {
	reply := &RequestVoteReply{}
	err := p.call("RaftService.RequestVote", args, reply)
	return reply, err
}

// SendAppendEntries 实现 Peer 接口。
func (p *TCPPeer) SendAppendEntries(args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	reply := &AppendEntriesReply{}
	err := p.call("RaftService.AppendEntries", args, reply)
	return reply, err
}

// SendMembershipChange 实现 Peer 接口。
func (p *TCPPeer) SendMembershipChange(args *MembershipChangeArgs) (*MembershipChangeReply, error) {
	reply := &MembershipChangeReply{}
	err := p.call("RaftService.MembershipChange", args, reply)
	return reply, err
}

// SetTimeout 实现 Peer 接口，动态调整 RPC 超时。
func (p *TCPPeer) SetTimeout(timeoutMs int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if timeoutMs < 100 {
		timeoutMs = 100
	}
	p.timeout = time.Duration(timeoutMs) * time.Millisecond
}

// Close 实现 Peer 接口，释放底层连接。
func (p *TCPPeer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client != nil {
		err := p.client.Close()
		p.client = nil
		return err
	}
	return nil
}
