// Package api handles medical record writes and blockchain queries.
package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"medtrust-raft/internal/blockchain"

	"github.com/gin-gonic/gin"
)

// uploadRequest is the request body for POST /api/record/upload.
// Payload can be a JSON object or a string. The server wraps it in an
// evidence envelope before submitting it to Raft.
type uploadRequest struct {
	Payload interface{} `json:"payload" binding:"required"`
}

type evidenceEnvelope struct {
	CertificateID string      `json:"certificate_id"`
	DataHash      string      `json:"data_hash"`
	HashAlgorithm string      `json:"hash_algorithm"`
	SubmittedAt   string      `json:"submitted_at"`
	Record        interface{} `json:"record"`
}

// handleUpload accepts writes only on the Leader. Followers return leader_addr
// so the frontend can retry against the current Leader.
func (srv *AppServer) handleUpload(c *gin.Context) {
	var req uploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	if !srv.node.IsLeader() {
		leaderAddr := srv.leaderAddr()
		log.Printf("%s \033[36m[API     ]\033[0m Node %-8s upload rejected: not leader, leader=%s",
			time.Now().Format("15:04:05.000"), srv.selfID, leaderAddr)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":       "not leader: this node cannot accept writes",
			"leader_addr": leaderAddr,
		})
		return
	}

	rawPayload, err := normalizePayload(req.Payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to serialize payload: " + err.Error()})
		return
	}

	dataHashBytes := sha256.Sum256([]byte(rawPayload))
	dataHash := fmt.Sprintf("%x", dataHashBytes)
	certificateID := fmt.Sprintf(
		"MED-%s-%s",
		time.Now().Format("20060102-150405"),
		strings.ToUpper(dataHash[:8]),
	)

	envelope := evidenceEnvelope{
		CertificateID: certificateID,
		DataHash:      dataHash,
		HashAlgorithm: "SHA-256",
		SubmittedAt:   time.Now().Format(time.RFC3339),
		Record:        req.Payload,
	}
	payloadBytes, err := json.Marshal(envelope)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to serialize evidence envelope: " + err.Error()})
		return
	}
	payloadStr := string(payloadBytes)

	idx, err := srv.node.Propose(payloadStr)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	log.Printf("%s \033[36m[API     ]\033[0m Node %-8s upload accepted  index=%d  certificate=%s  hash=%.12s...",
		time.Now().Format("15:04:05.000"), srv.selfID, idx, certificateID, dataHash)

	c.JSON(http.StatusAccepted, gin.H{
		"index":          idx,
		"certificate_id": certificateID,
		"data_hash":      dataHash,
		"hash_algorithm": "SHA-256",
		"status":         "pending consensus",
		"message":        "entry submitted to Raft log, will be committed once majority confirms",
	})
}

func normalizePayload(payload interface{}) (string, error) {
	switch v := payload.(type) {
	case string:
		return v, nil
	default:
		b, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

// handleGetAll returns persisted blocks in ascending index order.
func (srv *AppServer) handleGetAll(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}

	blocks, err := srv.store.AllBlocks(offset, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, blocks)
}

// handleGetOne returns a single block by block height.
func (srv *AppServer) handleGetOne(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a non-negative integer"})
		return
	}

	block, err := srv.store.GetBlock(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "block not found", "index": id})
		return
	}
	c.JSON(http.StatusOK, block)
}

// handleValidateChain verifies every persisted block against the genesis block.
func (srv *AppServer) handleValidateChain(c *gin.Context) {
	blocks, err := srv.store.AllBlocks(0, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Index < blocks[j].Index
	})

	prev := blockchain.GenesisBlock()
	for _, block := range blocks {
		if !block.IsValid(prev) {
			c.JSON(http.StatusOK, gin.H{
				"valid":         false,
				"block_count":   len(blocks),
				"invalid_index": block.Index,
				"message":       fmt.Sprintf("block #%d failed hash-chain validation", block.Index),
				"checked_at":    time.Now().Format(time.RFC3339),
			})
			return
		}
		prev = block
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":       true,
		"block_count": len(blocks),
		"latest_hash": prev.Hash,
		"message":     "blockchain integrity validation passed",
		"checked_at":  time.Now().Format(time.RFC3339),
	})
}
