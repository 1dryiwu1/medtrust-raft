// Package raft — Raft 日志持久化接口。
//
// 日志持久化保证节点重启后不会丢失已提交的日志条目，
// 从而能正确参与后续的 Leader 选举和日志复制。
package raft

import "medtrust-raft/internal/transport"

// LogPersister 定义 Raft 日志条目的持久化契约。
type LogPersister interface {
	// SaveLogEntry 将单条日志条目持久化。
	SaveLogEntry(entry transport.LogEntry) error

	// LoadAllLogEntries 按 Index 升序返回所有已持久化的日志条目。
	// 若数据库为空，返回 nil, nil。
	LoadAllLogEntries() ([]transport.LogEntry, error)

	// ClearLogEntries 清空所有已持久化日志（通常在快照后调用）。
	ClearLogEntries() error
}
