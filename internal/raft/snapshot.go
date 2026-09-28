// Package raft — 日志快照与压缩机制
//
// 快照原理：当 RaftLog 中已提交的条目积累到一定数量时，
// 将旧日志截断并生成快照。快照包含截至某一索引的完整状态，
// 后续节点恢复或 Follower 落后太多时，可通过 InstallSnapshot
// 快速同步，而无需逐条发送日志。
package raft

import (
	"fmt"
	"sync"

	"medtrust-raft/internal/transport"
)

// Snapshot 代表某一时刻 Raft 状态机的完整快照。
type Snapshot struct {
	LastIndex transport.LogIndex // 快照包含的最后一条日志索引
	LastTerm  transport.Term     // 快照最后一条日志的任期
	Data      []byte             // 快照数据（由上层状态机序列化）
}

// SnapshotStore 定义快照的持久化接口。
type SnapshotStore interface {
	// Save 持久化一个快照，返回其元数据。
	Save(snap *Snapshot) error

	// Load 加载最新的快照；若无快照返回 nil, nil。
	Load() (*Snapshot, error)

	// List 返回所有快照的元数据列表（按 LastIndex 升序）。
	List() ([]SnapshotMeta, error)

	// Delete 删除指定索引及之前的快照。
	Delete(throughIndex transport.LogIndex) error
}

// SnapshotMeta 是快照的元数据（不含 Data，轻量查询用）。
type SnapshotMeta struct {
	LastIndex transport.LogIndex
	LastTerm  transport.Term
	Size      int64
}

// -----------------------------------------------------------------------
// § 内存快照管理器（演示用；生产环境应替换为文件系统实现）
// -----------------------------------------------------------------------

// MemorySnapshotStore 是内存中的快照存储，适用于测试和演示。
type MemorySnapshotStore struct {
	mu       sync.RWMutex
	snapshot *Snapshot
}

// NewMemorySnapshotStore 创建内存快照存储。
func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{}
}

// Save 保存快照，直接替换旧快照。
func (m *MemorySnapshotStore) Save(snap *Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot = snap
	return nil
}

// Load 加载最新快照。
func (m *MemorySnapshotStore) Load() (*Snapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.snapshot == nil {
		return nil, nil
	}
	// 返回副本，防止外部修改
	snap := *m.snapshot
	snap.Data = make([]byte, len(m.snapshot.Data))
	copy(snap.Data, m.snapshot.Data)
	return &snap, nil
}

// List 返回快照列表（内存版最多一个）。
func (m *MemorySnapshotStore) List() ([]SnapshotMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.snapshot == nil {
		return nil, nil
	}
	return []SnapshotMeta{{
		LastIndex: m.snapshot.LastIndex,
		LastTerm:  m.snapshot.LastTerm,
		Size:      int64(len(m.snapshot.Data)),
	}}, nil
}

// Delete 删除指定索引及之前的快照（内存版只有一份，若匹配则清空）。
func (m *MemorySnapshotStore) Delete(throughIndex transport.LogIndex) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.snapshot != nil && m.snapshot.LastIndex <= throughIndex {
		m.snapshot = nil
	}
	return nil
}

// -----------------------------------------------------------------------
// § RaftLog 快照相关扩展
// -----------------------------------------------------------------------

// SnapshotPolicy 定义快照触发策略。
type SnapshotPolicy struct {
	// MaxEntries 内存中允许保留的最大日志条目数（不含哨兵）。
	// 超过此值时触发快照压缩。
	MaxEntries int

	// MinEntriesAfterSnapshot 快照后保留的最小日志条目数，
	// 用于应对网络分区期间 Leader 需要向 Follower 发送旧日志。
	MinEntriesAfterSnapshot int
}

// DefaultSnapshotPolicy 返回默认的快照策略。
func DefaultSnapshotPolicy() SnapshotPolicy {
	return SnapshotPolicy{
		MaxEntries:              10000,
		MinEntriesAfterSnapshot: 1000,
	}
}

// ShouldSnapshot 判断当前日志是否需要触发快照。
// 必须在持有 RaftLog 写锁（或已确保互斥）时调用。
func (l *RaftLog) ShouldSnapshot(policy SnapshotPolicy) bool {
	// entries[0] 是哨兵，真实条目数为 len-1
	return len(l.entries)-1 > policy.MaxEntries
}

// CreateSnapshot 创建截至 lastIncludedIndex 的快照，并截断之前的日志。
// 返回创建的快照和截断后的第一条日志索引。
func (l *RaftLog) CreateSnapshot(lastIncludedIndex transport.LogIndex, lastIncludedTerm transport.Term, data []byte) (*Snapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if lastIncludedIndex <= 0 || int(lastIncludedIndex) >= len(l.entries) {
		return nil, fmt.Errorf("snapshot index %d out of range [1,%d]", lastIncludedIndex, len(l.entries)-1)
	}

	// 验证任期是否匹配
	if l.entries[lastIncludedIndex].Term != lastIncludedTerm {
		return nil, fmt.Errorf("snapshot term mismatch: expected %d, got %d at index %d",
			l.entries[lastIncludedIndex].Term, lastIncludedTerm, lastIncludedIndex)
	}

	snap := &Snapshot{
		LastIndex: lastIncludedIndex,
		LastTerm:  lastIncludedTerm,
		Data:      make([]byte, len(data)),
	}
	copy(snap.Data, data)

	// 截断日志：保留哨兵 + 保留部分（若配置了 persister，先持久化快照再截断）
	if l.persister != nil {
		if sp, ok := l.persister.(SnapshotStore); ok {
			_ = sp.Save(snap)
		}
		// 同时清理持久化的日志条目
		if lp, ok := l.persister.(interface{ ClearLogEntries() error }); ok {
			_ = lp.ClearLogEntries()
		}
	}

	// 内存截断：保留哨兵和 lastIncludedIndex 之后的条目
	newEntries := make([]transport.LogEntry, 0, len(l.entries)-int(lastIncludedIndex)+1)
	newEntries = append(newEntries, transport.LogEntry{Index: 0, Term: 0, Payload: ""}) // 哨兵
	for i := lastIncludedIndex + 1; i < transport.LogIndex(len(l.entries)); i++ {
		newEntries = append(newEntries, l.entries[i])
	}
	l.entries = newEntries

	return snap, nil
}

// InstallSnapshot 安装一个从 Leader 传来的快照，替换本地日志。
// 通常在 Follower 日志严重落后时被调用。
func (l *RaftLog) InstallSnapshot(snap *Snapshot) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 若本地已有更新日志，拒绝旧快照
	if len(l.entries) > 1 {
		lastIdx := transport.LogIndex(len(l.entries) - 1)
		if lastIdx > snap.LastIndex {
			return fmt.Errorf("local log newer than snapshot: local=%d snapshot=%d", lastIdx, snap.LastIndex)
		}
	}

	// 清空日志，仅保留哨兵
	l.entries = []transport.LogEntry{
		{Index: 0, Term: 0, Payload: ""},
	}

	// 持久化快照
	if l.persister != nil {
		if sp, ok := l.persister.(SnapshotStore); ok {
			_ = sp.Save(snap)
		}
		if lp, ok := l.persister.(interface{ ClearLogEntries() error }); ok {
			_ = lp.ClearLogEntries()
		}
	}

	return nil
}

// FirstIndex 返回当前日志中的第一条有效索引（不含哨兵）。
// 若日志已清空（仅哨兵），返回 snap.LastIndex+1 或 1。
func (l *RaftLog) FirstIndex() transport.LogIndex {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if len(l.entries) > 1 {
		return l.entries[1].Index
	}
	return 1 // 仅哨兵时，下一条从 1 开始
}
