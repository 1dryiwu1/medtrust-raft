// Package api — Gin HTTP 服务器，对外暴露三类接口：
//   - 医疗数据写入（仅 Leader）
//   - 区块查询（任意节点可读）
//   - 节点健康状态（供前端图形化展示）
package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"sync"

	"medtrust-raft/internal/raft"
	"medtrust-raft/internal/store"
	"medtrust-raft/internal/transport"

	"github.com/gin-gonic/gin"
)

// AppServer 持有所有运行时依赖，并负责 Gin 路由注册。
type AppServer struct {
	node         *raft.Node
	store        *store.BoltStore
	chain        *Chain
	selfID       string
	selfAPIAddr  string                      // "http://host:port"（含 scheme）
	peerAPIAddrs map[transport.NodeID]string // nodeID → "http://host:port"
	broadcaster  *EventBroadcaster           // SSE 事件广播器
	apiKey       string                      // 可选的 API 认证密钥
	authMu       sync.Mutex
	draftMu      sync.Mutex
	users        map[string]authUser
	sessions     map[string]authUser
	portalKey    []byte
}

// NewAppServer 构造 AppServer。
// selfAPIAddr 格式示例："http://127.0.0.1:8001"
// peerAPIAddrs 格式示例：{"node-2": "http://127.0.0.1:8002", ...}
func NewAppServer(
	node *raft.Node,
	s *store.BoltStore,
	chain *Chain,
	selfID, selfAPIAddr string,
	peerAPIAddrs map[transport.NodeID]string,
	broadcaster *EventBroadcaster,
	apiKey string,
) *AppServer {
	srv := &AppServer{
		node:         node,
		store:        s,
		chain:        chain,
		selfID:       selfID,
		selfAPIAddr:  selfAPIAddr,
		peerAPIAddrs: peerAPIAddrs,
		broadcaster:  broadcaster,
		apiKey:       apiKey,
		users:        make(map[string]authUser),
		sessions:     make(map[string]authUser),
	}
	srv.loadPortalUsers()
	srv.loadPortalKey()
	return srv
}

func (srv *AppServer) loadPortalKey() {
	candidate := make([]byte, 32)
	if _, err := rand.Read(candidate); err != nil {
		panic("cannot initialize portal encryption key")
	}
	key, err := srv.store.PortalCryptoKey(candidate)
	if err != nil {
		panic("cannot persist portal encryption key")
	}
	srv.portalKey = key
}

func (srv *AppServer) loadPortalUsers() {
	users, _ := srv.store.LoadPortalUsers()
	for _, user := range users {
		srv.users[user.Username] = authUser{Username: user.Username, Role: user.Role, PasswordHash: user.PasswordHash, RealName: user.RealName, LicenseNo: user.LicenseNo, Hospital: user.Hospital, Department: user.Department, Verified: user.Verified, VerifiedAt: user.VerifiedAt}
	}
	if _, exists := srv.users["admin"]; exists {
		return
	}
	initialPassword := os.Getenv("MEDTRUST_ADMIN_PASSWORD")
	if !passwordIsAcceptable(initialPassword) {
		panic("MEDTRUST_ADMIN_PASSWORD must be set to 12-128 characters before initializing a new database")
	}
	hash, err := hashPassword(initialPassword)
	if err != nil {
		panic("cannot secure initial administrator password")
	}
	admin := authUser{Username: "admin", Role: "admin", PasswordHash: hash}
	srv.users[admin.Username] = admin
	if err := srv.store.SavePortalUser(portalUserForStore(admin)); err != nil {
		panic("cannot persist initial administrator")
	}
}

// leaderAddr 返回当前已知 Leader 的 HTTP 地址。
// 若本节点就是 Leader，返回 selfAPIAddr；否则查表。
func (srv *AppServer) leaderAddr() string {
	if srv.node.IsLeader() {
		return srv.selfAPIAddr
	}
	lid := srv.node.GetLeaderID()
	if addr, ok := srv.peerAPIAddrs[lid]; ok {
		return addr
	}
	return "" // Leader 尚未选出
}

func (srv *AppServer) approvedPortalWriteRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil || srv.apiKey == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "patient-approved portal submission is required"})
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		expected := srv.portalWriteSignature(body)
		provided, err := hex.DecodeString(c.GetHeader("X-Portal-Approval-Signature"))
		if err != nil || !hmac.Equal(provided, expected) {
			c.JSON(http.StatusForbidden, gin.H{"error": "patient-approved portal submission is required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (srv *AppServer) portalWriteSignature(body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(srv.apiKey))
	_, _ = mac.Write(body)
	return mac.Sum(nil)
}

// Run 注册所有路由并启动 Gin HTTP 服务（阻塞）。
func (srv *AppServer) Run(addr string) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(promMiddleware())

	// CORS — 允许浏览器跨域查询三个节点
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,X-API-Key,Authorization,X-Portal-Approval-Signature")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// 前端静态页面
	r.StaticFile("/", "./web/dist/index.html")
	r.Static("/assets", "./web/dist/assets")

	// 可选的 API Key 认证中间件（仅影响写入操作）
	authMiddleware := func(c *gin.Context) {
		if srv.apiKey == "" {
			c.Next()
			return
		}
		key := c.GetHeader("X-API-Key")
		if key != srv.apiKey {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing API key"})
			c.Abort()
			return
		}
		c.Next()
	}

	api := r.Group("/api")
	{
		srv.authRoutes(api)
		srv.portalRoutes(api)
		api.POST("/record/upload", authMiddleware, srv.approvedPortalWriteRequired(), srv.handleUpload)
		api.GET("/records", srv.handleGetAll)
		api.GET("/record/:id", srv.handleGetOne)
		api.GET("/chain/validate", srv.handleValidateChain)
		api.GET("/node/status", srv.handleStatus)
		api.GET("/cluster/nodes", srv.handleClusterNodes)
		api.GET("/events/stream", srv.handleEvents)

		// 索引查询
		api.GET("/records/search", srv.handleSearchRecords)

		// 动态成员变更（仅 Leader，需认证）
		api.POST("/cluster/members", authMiddleware, srv.handleAddMember)
		api.DELETE("/cluster/members/:id", authMiddleware, srv.handleRemoveMember)
		api.GET("/cluster/members", srv.handleListMembers)
	}

	// 健康检查端点（无需认证）
	r.GET("/health", srv.handleHealth)
	r.GET("/ready", srv.handleReady)

	// Prometheus 指标端点
	r.GET("/metrics", handleMetrics())

	return r.Run(addr)
}
