// Package raft — Node 核心结构体与角色状态机
package raft

import (
	"sync"
	"sync/atomic"
	"time"

	"medtrust-raft/internal/transport"
)

// Role 枚举节点的三种角色。
type Role int32

const (
	Follower  Role = iota // 0 — 默认角色，服从 Leader
	Candidate             // 1 — 发起选举中
	Leader                // 2 — 集群唯一领导者
)

func (r Role) String() string {
	return [...]string{"Follower", "Candidate", "Leader"}[r]
}

// CommitNotify 是向上层（区块链层）传递"已提交条目"的通知通道元素。
type CommitNotify struct {
	Entry transport.LogEntry
}

// Config 是创建 Node 时必须传入的完整配置。
type Config struct {
	ID               transport.NodeID
	Peers            []transport.Peer // 其他所有节点的 Peer 代理
	HeartbeatMs      int              // 心跳间隔（毫秒）
	ElectionMinMs    int              // 选举超时最小值（毫秒）
	ElectionMaxMs    int              // 选举超时最大值（毫秒）
	RPCTimeoutMs     int              // 节点间 RPC 调用超时（毫秒），默认 300
	Persister        Persister        // Raft 硬状态持久化器（可选但强烈建议）
	LogPersister     LogPersister     // Raft 日志持久化器（可选但强烈建议）
	SnapshotPolicy   SnapshotPolicy   // 快照压缩策略（零值禁用自动压缩）
	SnapshotInterval time.Duration    // 自动压缩检查间隔（默认 5 分钟）
}

// Node 是 Raft 状态机的核心载体。
//
// 并发模型：所有对 Node 字段的读写均须持有 mu，
// 唯独 commitCh 是无锁的 channel，供外部消费。
type Node struct {
	mu  sync.Mutex
	cfg Config

	// 持久化状态（真实系统需落盘；Demo 中存内存）
	currentTerm transport.Term   // 见过的最大任期
	votedFor    transport.NodeID // 本任期内投票给谁（""表示未投）
	log         *RaftLog

	// 易失状态
	role        Role
	commitIndex transport.LogIndex // 已知被提交的最高日志序号
	lastApplied transport.LogIndex // 已应用到状态机的最高序号

	// Leader 专属：跟踪每个 Follower 的复制进度
	nextIndex  map[transport.NodeID]transport.LogIndex // 下次发给 Follower 的 Index
	matchIndex map[transport.NodeID]transport.LogIndex // 已确认 Follower 复制的最高 Index

	// 选举超时控制
	resetElectionCh chan struct{} // 收到心跳/投票后向此 channel 发信号，重置计时器

	// 停止信号
	stopCh chan struct{}

	// 提交通知（上层区块链模块监听此 channel，将条目持久化为区块）
	CommitCh chan CommitNotify

	// 原子读当前角色（供外部无锁查询，如 API 层判断是否为 Leader）
	atomicRole int32

	// leaderID 记录本节点最近一次收到心跳的 Leader ID（由 HandleAppendEntries 更新）
	leaderID transport.NodeID
	// roleSince 记录当前角色从何时开始，供可视化展示选举/故障转移状态。
	roleSince time.Time
	// lastHeartbeatAt 记录最近一次收到合法 Leader 心跳或日志复制的时间。
	lastHeartbeatAt time.Time

	// 待处理的成员变更请求（单节点变更策略）
	pendingChange *MembershipChangeRequest
}

// NewNode 创建并返回一个初始化为 Follower 的 Raft 节点（尚未启动）。
// 若 Config 中提供了 Persister，启动前会从磁盘恢复 term 和 votedFor。
func NewNode(cfg Config) *Node {
	n := &Node{
		cfg:             cfg,
		currentTerm:     0,
		votedFor:        "",
		log:             newRaftLog(cfg.LogPersister),
		role:            Follower,
		commitIndex:     0,
		lastApplied:     0,
		nextIndex:       make(map[transport.NodeID]transport.LogIndex),
		matchIndex:      make(map[transport.NodeID]transport.LogIndex),
		resetElectionCh: make(chan struct{}, 1),
		stopCh:          make(chan struct{}),
		CommitCh:        make(chan CommitNotify, 256),
		roleSince:       time.Now(),
	}

	// 从持久化存储恢复硬状态
	if cfg.Persister != nil {
		term, votedFor, err := cfg.Persister.LoadRaftState()
		if err != nil {
			// 持久化层故障是致命错误，记录并继续以零值启动
			raftLog("PERSIST", string(cfg.ID), "failed to load raft state: %v (starting fresh)", err)
		} else {
			n.currentTerm = term
			n.votedFor = votedFor
			raftLog("PERSIST", string(cfg.ID), "restored raft state: term=%d votedFor=%s", term, votedFor)
		}
	}

	atomic.StoreInt32(&n.atomicRole, int32(Follower))
	return n
}

// Start 启动节点的主循环（后台 goroutine）。
func (n *Node) Start() {
	raftLog("FOLLOWER", string(n.cfg.ID), "started, entering Follower state (Term %d)", n.currentTerm)
	go n.run()
	// 启动日志压缩后台任务
	go n.compactionLoop()
}

// compactionLoop 定期执行日志压缩，防止内存无限增长。
func (n *Node) compactionLoop() {
	// 若策略未启用（MaxEntries <= 0），直接退出
	if n.cfg.SnapshotPolicy.MaxEntries <= 0 {
		return
	}
	interval := n.cfg.SnapshotInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			if compacted, err := n.log.Compact(n.cfg.SnapshotPolicy); err != nil {
				raftLog("COMPACT", string(n.cfg.ID), "log compaction failed: %v", err)
			} else if compacted {
				raftLog("COMPACT", string(n.cfg.ID), "log compacted successfully")
			}
		}
	}
}

// Stop 优雅停止节点。
func (n *Node) Stop() {
	close(n.stopCh)
}

// IsLeader 供外部（API 层）无锁判断本节点当前是否为 Leader。
func (n *Node) IsLeader() bool {
	return Role(atomic.LoadInt32(&n.atomicRole)) == Leader
}

// CurrentTerm 返回当前任期（加锁）。
func (n *Node) CurrentTerm() transport.Term {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.currentTerm
}

// GetLeaderID 返回本节点已知的 Leader ID（加锁读）。
func (n *Node) GetLeaderID() transport.NodeID {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.leaderID
}

// GetRole 返回当前角色字符串，供 /api/node/status 使用。
func (n *Node) GetRole() string {
	return Role(atomic.LoadInt32(&n.atomicRole)).String()
}

// CommitIndex 返回当前已提交的最高日志序号（加锁读）。
func (n *Node) CommitIndex() transport.LogIndex {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.commitIndex
}

// RecoverLog 根据区块链最后一个区块恢复 Raft 日志状态。
// 必须在 Start() 之前调用，用于节点重启后对齐日志和区块链。
func (n *Node) RecoverLog(lastBlockIndex uint64, lastBlockTerm uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.log.RecoverFromChain(
		transport.LogIndex(lastBlockIndex),
		transport.Term(lastBlockTerm),
	)
}

// raft 内部：切换角色并更新原子变量（必须在持锁状态下调用）。
func (n *Node) setRole(r Role) {
	if n.role != r {
		n.roleSince = time.Now()
	}
	n.role = r
	atomic.StoreInt32(&n.atomicRole, int32(r))
}

// raft 内部：当发现更大的任期时，无条件退回 Follower（必须在持锁状态下调用）。
func (n *Node) stepDown(term transport.Term) {
	n.currentTerm = term
	n.setRole(Follower)
	n.votedFor = ""
	n.persistStateLocked()
	raftLog("FOLLOWER", string(n.cfg.ID), "stepped down to Follower (Term %d)", term)
}

// persistStateLocked 将当前 term 和 votedFor 持久化（必须在持锁状态下调用）。
func (n *Node) persistStateLocked() {
	if n.cfg.Persister == nil {
		return
	}
	if err := n.cfg.Persister.SaveRaftState(n.currentTerm, n.votedFor); err != nil {
		raftLog("PERSIST", string(n.cfg.ID), "failed to save raft state: %v", err)
	}
}
