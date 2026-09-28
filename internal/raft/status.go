package raft

import (
	"time"

	"medtrust-raft/internal/transport"
)

// PeerReplicationStatus describes one follower's replication progress from
// the current node's point of view. Values are meaningful when this node is
// Leader; followers still expose peer IDs with zero progress for UI stability.
type PeerReplicationStatus struct {
	ID         string `json:"id"`
	MatchIndex uint64 `json:"match_index"`
	NextIndex  uint64 `json:"next_index"`
	Lag        uint64 `json:"lag"`
	State      string `json:"state"` // synced | lagging | probing
}

// StatusSnapshot is a lock-safe read model for API/dashboard consumers.
type StatusSnapshot struct {
	NodeID              string                  `json:"node_id"`
	Role                string                  `json:"role"`
	Term                uint64                  `json:"term"`
	IsLeader            bool                    `json:"is_leader"`
	LeaderID            string                  `json:"leader_id"`
	CommitIndex         uint64                  `json:"commit_index"`
	LastApplied         uint64                  `json:"last_applied"`
	LastLogIndex        uint64                  `json:"last_log_index"`
	LastLogTerm         uint64                  `json:"last_log_term"`
	ClusterSize         int                     `json:"cluster_size"`
	PeerCount           int                     `json:"peer_count"`
	Quorum              int                     `json:"quorum"`
	HeartbeatMs         int                     `json:"heartbeat_ms"`
	ElectionTimeoutMin  int                     `json:"election_timeout_min_ms"`
	ElectionTimeoutMax  int                     `json:"election_timeout_max_ms"`
	RoleSinceUnixMs     int64                   `json:"role_since_unix_ms"`
	RoleDurationMs      int64                   `json:"role_duration_ms"`
	LastHeartbeatUnixMs int64                   `json:"last_heartbeat_unix_ms"`
	LastHeartbeatAgeMs  int64                   `json:"last_heartbeat_age_ms"`
	ElectionState       string                  `json:"election_state"` // leading | following | campaigning | waiting_leader
	Peers               []PeerReplicationStatus `json:"peers"`
}

// StatusSnapshot returns the current Raft runtime state for dashboards.
func (n *Node) StatusSnapshot() StatusSnapshot {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()
	role := n.role
	lastLogIndex := n.log.LastIndex()
	lastLogTerm := n.log.LastTerm()
	clusterSize := len(n.cfg.Peers) + 1
	quorum := clusterSize/2 + 1

	leaderID := string(n.leaderID)
	if role == Leader {
		leaderID = string(n.cfg.ID)
	}

	lastHeartbeatUnixMs := int64(0)
	lastHeartbeatAgeMs := int64(-1)
	if !n.lastHeartbeatAt.IsZero() {
		lastHeartbeatUnixMs = n.lastHeartbeatAt.UnixMilli()
		lastHeartbeatAgeMs = now.Sub(n.lastHeartbeatAt).Milliseconds()
	}

	roleSinceUnixMs := int64(0)
	roleDurationMs := int64(0)
	if !n.roleSince.IsZero() {
		roleSinceUnixMs = n.roleSince.UnixMilli()
		roleDurationMs = now.Sub(n.roleSince).Milliseconds()
	}

	peers := make([]PeerReplicationStatus, 0, len(n.cfg.Peers))
	for _, p := range n.cfg.Peers {
		id := p.ID()
		match := n.matchIndex[id]
		next := n.nextIndex[id]
		lag := transport.LogIndex(0)
		if lastLogIndex > match {
			lag = lastLogIndex - match
		}
		state := "synced"
		if role != Leader {
			state = "probing"
		} else if match == 0 && lastLogIndex > 0 {
			state = "probing"
		} else if lag > 0 {
			state = "lagging"
		}
		peers = append(peers, PeerReplicationStatus{
			ID:         string(id),
			MatchIndex: uint64(match),
			NextIndex:  uint64(next),
			Lag:        uint64(lag),
			State:      state,
		})
	}

	electionState := "waiting_leader"
	switch role {
	case Leader:
		electionState = "leading"
	case Candidate:
		electionState = "campaigning"
	case Follower:
		if leaderID != "" {
			electionState = "following"
		}
	}

	return StatusSnapshot{
		NodeID:              string(n.cfg.ID),
		Role:                role.String(),
		Term:                uint64(n.currentTerm),
		IsLeader:            role == Leader,
		LeaderID:            leaderID,
		CommitIndex:         uint64(n.commitIndex),
		LastApplied:         uint64(n.lastApplied),
		LastLogIndex:        uint64(lastLogIndex),
		LastLogTerm:         uint64(lastLogTerm),
		ClusterSize:         clusterSize,
		PeerCount:           len(n.cfg.Peers),
		Quorum:              quorum,
		HeartbeatMs:         n.cfg.HeartbeatMs,
		ElectionTimeoutMin:  n.cfg.ElectionMinMs,
		ElectionTimeoutMax:  n.cfg.ElectionMaxMs,
		RoleSinceUnixMs:     roleSinceUnixMs,
		RoleDurationMs:      roleDurationMs,
		LastHeartbeatUnixMs: lastHeartbeatUnixMs,
		LastHeartbeatAgeMs:  lastHeartbeatAgeMs,
		ElectionState:       electionState,
		Peers:               peers,
	}
}
