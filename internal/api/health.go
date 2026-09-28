// Package api — 健康检查端点。
//
// /health  — 存活探针（Liveness），只要进程在运行就返回 200。
// /ready   — 就绪探针（Readiness），节点已初始化完成且 raft 已启动才返回 200。
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleHealth 存活探针 — 进程存活即返回 200。
func (srv *AppServer) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"node_id":   srv.selfID,
		"timestamp": 0, // 前端可自行填充
	})
}

// handleReady 就绪探针 — 检查节点是否已完成初始化并可对外服务。
func (srv *AppServer) handleReady(c *gin.Context) {
	// 简单判断：只要节点角色已确定（非初始状态），即认为就绪
	role := srv.node.GetRole()
	if role == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "not_ready",
			"node_id": srv.selfID,
			"reason":  "raft node not initialized",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ready",
		"node_id": srv.selfID,
		"role":    role,
	})
}
