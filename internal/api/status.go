package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// nodeStatusResponse is GET /api/node/status response body.
// It keeps the original fields and adds detailed Raft runtime fields for the
// visual dashboard: election state, log progress, heartbeat age, and leader
// replication progress for peers.
type nodeStatusResponse struct {
	NodeID              string                `json:"node_id"`
	Role                string                `json:"role"`
	Term                uint64                `json:"term"`
	CommitIndex         uint64                `json:"commit_index"`
	LastApplied         uint64                `json:"last_applied"`
	LastLogIndex        uint64                `json:"last_log_index"`
	LastLogTerm         uint64                `json:"last_log_term"`
	BlockCount          int                   `json:"block_count"`
	IsLeader            bool                  `json:"is_leader"`
	LeaderID            string                `json:"leader_id"`
	LeaderAddr          string                `json:"leader_addr"`
	ClusterSize         int                   `json:"cluster_size"`
	PeerCount           int                   `json:"peer_count"`
	Quorum              int                   `json:"quorum"`
	HeartbeatMs         int                   `json:"heartbeat_ms"`
	ElectionTimeoutMin  int                   `json:"election_timeout_min_ms"`
	ElectionTimeoutMax  int                   `json:"election_timeout_max_ms"`
	RoleSinceUnixMs     int64                 `json:"role_since_unix_ms"`
	RoleDurationMs      int64                 `json:"role_duration_ms"`
	LastHeartbeatUnixMs int64                 `json:"last_heartbeat_unix_ms"`
	LastHeartbeatAgeMs  int64                 `json:"last_heartbeat_age_ms"`
	ElectionState       string                `json:"election_state"`
	FaultState          string                `json:"fault_state"`
	Peers               []peerReplicationInfo `json:"peers"`
}

type peerReplicationInfo struct {
	ID         string `json:"id"`
	MatchIndex uint64 `json:"match_index"`
	NextIndex  uint64 `json:"next_index"`
	Lag        uint64 `json:"lag"`
	State      string `json:"state"`
}

func (srv *AppServer) handleStatus(c *gin.Context) {
	snap := srv.node.StatusSnapshot()
	peers := make([]peerReplicationInfo, 0, len(snap.Peers))
	for _, p := range snap.Peers {
		peers = append(peers, peerReplicationInfo{
			ID:         p.ID,
			MatchIndex: p.MatchIndex,
			NextIndex:  p.NextIndex,
			Lag:        p.Lag,
			State:      p.State,
		})
	}

	resp := nodeStatusResponse{
		NodeID:              snap.NodeID,
		Role:                snap.Role,
		Term:                snap.Term,
		CommitIndex:         snap.CommitIndex,
		LastApplied:         snap.LastApplied,
		LastLogIndex:        snap.LastLogIndex,
		LastLogTerm:         snap.LastLogTerm,
		BlockCount:          srv.chain.Len(),
		IsLeader:            snap.IsLeader,
		LeaderID:            snap.LeaderID,
		LeaderAddr:          srv.leaderAddr(),
		ClusterSize:         snap.ClusterSize,
		PeerCount:           snap.PeerCount,
		Quorum:              snap.Quorum,
		HeartbeatMs:         snap.HeartbeatMs,
		ElectionTimeoutMin:  snap.ElectionTimeoutMin,
		ElectionTimeoutMax:  snap.ElectionTimeoutMax,
		RoleSinceUnixMs:     snap.RoleSinceUnixMs,
		RoleDurationMs:      snap.RoleDurationMs,
		LastHeartbeatUnixMs: snap.LastHeartbeatUnixMs,
		LastHeartbeatAgeMs:  snap.LastHeartbeatAgeMs,
		ElectionState:       snap.ElectionState,
		FaultState:          faultState(snap.ElectionState, snap.LastHeartbeatAgeMs, snap.ElectionTimeoutMax),
		Peers:               peers,
	}
	c.JSON(http.StatusOK, resp)
}

func faultState(electionState string, heartbeatAgeMs int64, electionMaxMs int) string {
	if electionState == "leading" {
		return "leader_active"
	}
	if electionState == "campaigning" {
		return "election_in_progress"
	}
	if heartbeatAgeMs < 0 {
		return "leader_unknown"
	}
	if electionMaxMs > 0 && heartbeatAgeMs > int64(electionMaxMs) {
		return "heartbeat_overdue"
	}
	return "healthy"
}

type clusterNodeInfo struct {
	ID      string `json:"id"`
	APIAddr string `json:"api_addr"`
}

func (srv *AppServer) handleClusterNodes(c *gin.Context) {
	var nodes []clusterNodeInfo
	nodes = append(nodes, clusterNodeInfo{
		ID:      srv.selfID,
		APIAddr: srv.selfAPIAddr,
	})
	for id, addr := range srv.peerAPIAddrs {
		nodes = append(nodes, clusterNodeInfo{
			ID:      string(id),
			APIAddr: addr,
		})
	}
	c.JSON(http.StatusOK, nodes)
}
