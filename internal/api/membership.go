// Package api — 动态成员变更 HTTP Handler
package api

import (
	"net/http"

	"medtrust-raft/internal/raft"
	"medtrust-raft/internal/transport"

	"github.com/gin-gonic/gin"
)

// addMemberRequest 是 POST /api/cluster/members 的请求体。
type addMemberRequest struct {
	NodeID   string `json:"node_id" binding:"required"`
	RPCAddr  string `json:"rpc_addr" binding:"required"`
	APIAddr  string `json:"api_addr" binding:"required"`
}

// handleAddMember 处理添加节点请求。
// 仅 Leader 可接受此请求。
func (srv *AppServer) handleAddMember(c *gin.Context) {
	if !srv.node.IsLeader() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":       "not leader: only leader can modify cluster membership",
			"leader_addr": srv.leaderAddr(),
		})
		return
	}

	var req addMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// 更新本地 Peer API 地址映射
	srv.peerAPIAddrs[transport.NodeID(req.NodeID)] = req.APIAddr

	// 向 Raft 层提议成员变更
	err := srv.node.ProposeMembershipChange(raft.MembershipChangeRequest{
		Type:     transport.AddNode,
		NodeID:   transport.NodeID(req.NodeID),
		NodeAddr: req.RPCAddr,
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":  "pending",
		"message": "membership change proposed, will be committed once majority confirms",
		"node_id": req.NodeID,
		"action":  "add",
	})
}

// handleRemoveMember 处理移除节点请求。
// 仅 Leader 可接受此请求。
func (srv *AppServer) handleRemoveMember(c *gin.Context) {
	if !srv.node.IsLeader() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":       "not leader: only leader can modify cluster membership",
			"leader_addr": srv.leaderAddr(),
		})
		return
	}

	nodeID := transport.NodeID(c.Param("id"))
	if nodeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "node id required"})
		return
	}

	// 从 API 地址映射中移除
	delete(srv.peerAPIAddrs, nodeID)

	// 向 Raft 层提议成员变更
	err := srv.node.ProposeMembershipChange(raft.MembershipChangeRequest{
		Type:   transport.RemoveNode,
		NodeID: nodeID,
	})
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":  "pending",
		"message": "membership change proposed, will be committed once majority confirms",
		"node_id": nodeID,
		"action":  "remove",
	})
}

// handleListMembers 返回当前集群成员列表。
func (srv *AppServer) handleListMembers(c *gin.Context) {
	peers := srv.node.GetPeers()
	members := make([]gin.H, 0, len(peers)+1)

	// 添加自身
	members = append(members, gin.H{
		"id":       srv.selfID,
		"api_addr": srv.selfAPIAddr,
		"self":     true,
	})

	// 添加其他节点
	for _, id := range peers {
		addr := ""
		if a, ok := srv.peerAPIAddrs[id]; ok {
			addr = a
		}
		members = append(members, gin.H{
			"id":       string(id),
			"api_addr": addr,
			"self":     false,
		})
	}

	c.JSON(http.StatusOK, members)
}
