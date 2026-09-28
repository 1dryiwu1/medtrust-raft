// Package api — Prometheus 指标暴露。
//
// 注册以下指标：
//   - raft_elections_total          — Leader 选举总次数（按节点和结果标记）
//   - raft_commit_latency_seconds   — 日志提交延迟（从 propose 到 commit）
//   - raft_committed_entries_total  — 已提交的日志条目总数
//   - api_requests_total            — HTTP API 请求总数（按方法和路径标记）
//   - api_request_duration_seconds  — HTTP API 请求处理耗时
//   - blocks_total                  — 已持久化的区块总数
package api

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	electionCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "raft_elections_total",
		Help: "Total number of leader elections.",
	}, []string{"node", "result"})

	commitLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "raft_commit_latency_seconds",
		Help:    "Latency from log propose to commit.",
		Buckets: prometheus.DefBuckets,
	}, []string{"node"})

	committedEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "raft_committed_entries_total",
		Help: "Total number of committed log entries.",
	}, []string{"node"})

	apiRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "api_requests_total",
		Help: "Total number of HTTP API requests.",
	}, []string{"method", "path", "status"})

	apiDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "api_request_duration_seconds",
		Help:    "HTTP API request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	blockCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "blocks_total",
		Help: "Total number of persisted blocks.",
	}, []string{"node"})
)

func init() {
	prometheus.MustRegister(electionCounter, commitLatency, committedEntries,
		apiRequests, apiDuration, blockCounter)
}

// promMiddleware 是 Gin 中间件，自动记录请求数和耗时。
func promMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		apiRequests.WithLabelValues(c.Request.Method, path, status).Inc()
		apiDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

// handleMetrics 暴露 Prometheus 指标端点。
func handleMetrics() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// -----------------------------------------------------------------------
// 以下函数供业务代码调用以记录具体业务指标
// -----------------------------------------------------------------------

// RecordElection 记录一次选举事件。
func RecordElection(nodeID, result string) {
	electionCounter.WithLabelValues(nodeID, result).Inc()
}

// RecordCommitLatency 记录日志提交延迟。
func RecordCommitLatency(nodeID string, latency time.Duration) {
	commitLatency.WithLabelValues(nodeID).Observe(latency.Seconds())
}

// RecordCommittedEntry 记录一条已提交的日志。
func RecordCommittedEntry(nodeID string) {
	committedEntries.WithLabelValues(nodeID).Inc()
}

// RecordBlock 记录一个已持久化的区块。
func RecordBlock(nodeID string) {
	blockCounter.WithLabelValues(nodeID).Inc()
}
