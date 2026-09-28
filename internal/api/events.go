// Package api — Server-Sent Events (SSE) 实时推送模块。
//
// 当以下事件发生时，会向所有已连接的 SSE 客户端广播：
//   - 新区块上链（block）
//   - 节点角色/任期变化（status）
//
// 前端通过 EventSource('/api/events/stream') 建立长连接接收事件，
// 替代部分高频轮询，降低延迟和带宽。
package api

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Event 是 SSE 推送的标准事件结构。
type Event struct {
	Type string      `json:"type"` // "block" | "status" | "election"
	Data interface{} `json:"data"`
}

// EventBroadcaster 管理所有 SSE 订阅者，负责事件广播。
type EventBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan Event]struct{}
}

// NewEventBroadcaster 创建广播器。
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		clients: make(map[chan Event]struct{}),
	}
}

// Subscribe 注册一个新的事件接收通道。
func (eb *EventBroadcaster) Subscribe() chan Event {
	ch := make(chan Event, 16)
	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()
	return ch
}

// Unsubscribe 注销事件通道并释放资源。
func (eb *EventBroadcaster) Unsubscribe(ch chan Event) {
	eb.mu.Lock()
	delete(eb.clients, ch)
	eb.mu.Unlock()
	close(ch)
}

// Broadcast 向所有活跃订阅者发送事件（非阻塞，满通道则丢弃）。
func (eb *EventBroadcaster) Broadcast(ev Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

// handleEvents 处理 SSE 长连接请求。
func (srv *AppServer) handleEvents(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	ch := srv.broadcaster.Subscribe()
	defer srv.broadcaster.Unsubscribe(ch)

	// 发送一个初始心跳，确认连接建立
	c.SSEvent("heartbeat", map[string]string{"status": "connected"})
	c.Writer.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev := <-ch:
			data, _ := json.Marshal(ev)
			c.SSEvent("message", string(data))
			c.Writer.Flush()

		case <-ticker.C:
			c.SSEvent("heartbeat", map[string]string{"ts": time.Now().Format(time.RFC3339)})
			c.Writer.Flush()

		case <-c.Request.Context().Done():
			return
		}
	}
}
