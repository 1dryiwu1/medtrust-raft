// Package raft 实现 Raft 共识算法的核心状态机。
package raft

import (
	"fmt"
	"sync"

	"medtrust-raft/internal/transport"
)

// RaftLog 管理 Raft 的内存日志条目。
//
// 索引约定（1-based）：
//   - entries[0] 是哨兵，Index=0 Term=0，简化所有边界判断
//   - 真实条目从 entries[1] 开始
type RaftLog struct {
	mu        sync.RWMutex
	entries   []transport.LogEntry
	persister LogPersister // 可选的日志持久化器
}

func newRaftLog(lp LogPersister) *RaftLog {
	l := &RaftLog{
		entries: []transport.LogEntry{
			{Index: 0, Term: 0, Payload: ""}, // 哨兵
		},
	}
	// 从持久化存储恢复日志
	if lp != nil {
		entries, err := lp.LoadAllLogEntries()
		if err == nil {
			for _, e := range entries {
				if e.Index > 0 {
					l.entries = append(l.entries, e)
				}
			}
		}
	}
	return l
}

// LastIndex 返回最后一条日志的序号。
func (l *RaftLog) LastIndex() transport.LogIndex {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return transport.LogIndex(len(l.entries) - 1)
}

// LastTerm 返回最后一条日志的任期。
func (l *RaftLog) LastTerm() transport.Term {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.entries[len(l.entries)-1].Term
}

// TermAt 返回指定 index 处条目的任期；越界返回 0。
func (l *RaftLog) TermAt(index transport.LogIndex) transport.Term {
	l.mu.RLock()
	defer l.mu.RUnlock()
	i := int(index)
	if i <= 0 || i >= len(l.entries) {
		return 0
	}
	return l.entries[i].Term
}

// At 返回指定 index 的条目，若不存在返回 false。
func (l *RaftLog) At(index transport.LogIndex) (transport.LogEntry, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	i := int(index)
	if i <= 0 || i >= len(l.entries) {
		return transport.LogEntry{}, false
	}
	return l.entries[i], true
}

// Append 由 Leader 调用，追加一条新条目，返回其 Index。
// 若配置了 LogPersister，条目会同步持久化到磁盘。
func (l *RaftLog) Append(term transport.Term, payload string) transport.LogIndex {
	l.mu.Lock()
	defer l.mu.Unlock()
	idx := transport.LogIndex(len(l.entries))
	entry := transport.LogEntry{
		Index:   idx,
		Term:    term,
		Payload: payload,
	}
	l.entries = append(l.entries, entry)
	if l.persister != nil {
		_ = l.persister.SaveLogEntry(entry)
	}
	return idx
}

// AppendAfter 由 Follower 调用，将 entries 追加到日志中。
// 若遇到 Index 相同但 Term 不同的冲突条目，截断后重写。
func (l *RaftLog) AppendAfter(entries []transport.LogEntry) {
	if len(entries) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range entries {
		i := int(e.Index)
		if i < len(l.entries) {
			if l.entries[i].Term != e.Term {
				l.entries = append(l.entries[:i], e) // 截断冲突并写入
				if l.persister != nil {
					_ = l.persister.SaveLogEntry(e)
				}
			}
			// term 相同：已有此条目，幂等跳过
		} else {
			l.entries = append(l.entries, e)
			if l.persister != nil {
				_ = l.persister.SaveLogEntry(e)
			}
		}
	}
}

// Slice 返回 [from, end] 的条目副本（含 from），若越界返回 nil。
func (l *RaftLog) Slice(from transport.LogIndex) []transport.LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	i := int(from)
	if i <= 0 || i >= len(l.entries) {
		return nil
	}
	result := make([]transport.LogEntry, len(l.entries)-i)
	copy(result, l.entries[i:])
	return result
}

// RecoverFromChain 根据区块链最后一个区块恢复 Raft 日志状态。
// 节点重启后若区块链已有数据但 Raft 日志被清空，调用此方法对齐状态，
// 防止新日志从 #1 开始导致 Committer 跳过已存在的索引。
func (l *RaftLog) RecoverFromChain(lastIndex transport.LogIndex, lastTerm transport.Term) {
	l.mu.Lock()
	defer l.mu.Unlock()

	current := transport.LogIndex(len(l.entries) - 1)
	if current >= lastIndex {
		return // 已有足够日志，无需恢复
	}

	raftLog("RECOVER", "raft-log", "recovering from chain: current=%d target=%d term=%d", current, lastIndex, lastTerm)

	// 填充虚拟条目直到索引对齐
	for transport.LogIndex(len(l.entries)-1) < lastIndex {
		idx := transport.LogIndex(len(l.entries))
		entry := transport.LogEntry{
			Index:   idx,
			Term:    lastTerm,
			Payload: "", // 空 payload，仅作占位
		}
		l.entries = append(l.entries, entry)
		if l.persister != nil {
			_ = l.persister.SaveLogEntry(entry)
		}
	}

	recoveredLastIndex := transport.LogIndex(len(l.entries) - 1)
	recoveredLastTerm := l.entries[len(l.entries)-1].Term
	raftLog("RECOVER", "raft-log", "recovered: lastIndex=%d lastTerm=%d", recoveredLastIndex, recoveredLastTerm)
}

// Compact 根据策略自动触发日志压缩。
// 若日志条目数超过 MaxEntries，则截取到保留 MinEntriesAfterSnapshot 条。
// 返回是否执行了压缩，以及错误（如有）。
func (l *RaftLog) Compact(policy SnapshotPolicy) (bool, error) {
	if !l.ShouldSnapshot(policy) {
		return false, nil
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 双重检查（锁内再次确认）
	if len(l.entries)-1 <= policy.MaxEntries {
		return false, nil
	}

	// 计算截断点：保留最近 MinEntriesAfterSnapshot 条
	cutoff := transport.LogIndex(len(l.entries) - 1 - policy.MinEntriesAfterSnapshot)
	if cutoff <= 0 {
		return false, nil
	}

	lastIncluded := l.entries[cutoff]
	data := []byte(fmt.Sprintf("snapshot-up-to-index-%d", lastIncluded.Index))

	snap := &Snapshot{
		LastIndex: lastIncluded.Index,
		LastTerm:  lastIncluded.Term,
		Data:      data,
	}

	// 持久化快照并截断内存日志
	if l.persister != nil {
		if sp, ok := l.persister.(SnapshotStore); ok {
			if err := sp.Save(snap); err != nil {
				return false, fmt.Errorf("save snapshot failed: %w", err)
			}
		}
		if lp, ok := l.persister.(interface{ ClearLogEntriesBefore(index transport.LogIndex) error }); ok {
			_ = lp.ClearLogEntriesBefore(lastIncluded.Index)
		}
	}

	// 内存截断：保留哨兵 + cutoff 之后的条目
	newEntries := make([]transport.LogEntry, 0, len(l.entries)-int(cutoff))
	newEntries = append(newEntries, transport.LogEntry{Index: 0, Term: 0, Payload: ""})
	for i := cutoff + 1; i < transport.LogIndex(len(l.entries)); i++ {
		newEntries = append(newEntries, l.entries[i])
	}
	l.entries = newEntries

	return true, nil
}
