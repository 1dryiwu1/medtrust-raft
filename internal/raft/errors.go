// Package raft — 对外暴露的错误类型
package raft

import "errors"

// ErrNotLeader 当非 Leader 节点收到写请求时返回。
// API 层应将此错误转换为 HTTP 307 Redirect，引导客户端访问 Leader。
var ErrNotLeader = errors.New("not leader: please redirect to the current leader")
