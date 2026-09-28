// cmd/node/main.go — MedTrust-Raft 节点启动入口
//
// 启动顺序：
//  1. 解析 --config 标志，读取 YAML 配置
//  2. 打开 BoltDB 存储
//  3. 从 DB 重建内存区块链（Chain）
//  4. 构造 Raft 节点并启动
//  5. 注册 RPC 服务（net/rpc over HTTP），开始监听
//  6. 启动 Committer goroutine（CommitCh → Block → DB）
//  7. 启动 Gin HTTP API 服务
//  8. 等待 OS 信号，优雅退出
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"medtrust-raft/internal/api"
	"medtrust-raft/internal/raft"
	"medtrust-raft/internal/store"
	"medtrust-raft/internal/transport"

	"gopkg.in/yaml.v3"
)

// -----------------------------------------------------------------------
// § 配置结构体（对应 configs/nodeN.yaml）
// -----------------------------------------------------------------------

type PeerConfig struct {
	ID   string `yaml:"id"`
	Addr string `yaml:"addr"` // RPC 地址，如 "127.0.0.1:7002"
}

type RaftConfig struct {
	HeartbeatMs        int `yaml:"heartbeat_ms"`
	ElectionTimeoutMin int `yaml:"election_timeout_min_ms"`
	ElectionTimeoutMax int `yaml:"election_timeout_max_ms"`
	RPCTimeoutMs       int `yaml:"rpc_timeout_ms"`
}

type StorageConfig struct {
	DBPath string `yaml:"db_path"`
}

type NodeConfig struct {
	NodeID  string        `yaml:"node_id"`
	RPCAddr string        `yaml:"rpc_addr"`
	APIAddr string        `yaml:"api_addr"`
	APIKey  string        `yaml:"api_key"` // 可选：API 认证密钥
	Peers   []PeerConfig  `yaml:"peers"`
	Raft    RaftConfig    `yaml:"raft"`
	Storage StorageConfig `yaml:"storage"`
}

// -----------------------------------------------------------------------
// § main
// -----------------------------------------------------------------------

func main() {
	cfgPath := flag.String("config", "configs/node1.yaml", "path to node config YAML file")
	flag.Parse()

	// 1. 读取并解析配置
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("failed to load config %s: %v", *cfgPath, err)
	}
	log.Printf("=== MedTrust-Raft  node=%s  rpc=%s  api=%s ===",
		cfg.NodeID, cfg.RPCAddr, cfg.APIAddr)

	// 2. 打开 BoltDB
	boltStore, err := store.Open(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("failed to open store: %v", err)
	}
	log.Printf("store opened: %s", cfg.Storage.DBPath)

	// 3. 重建内存链
	chain, err := api.LoadChain(boltStore)
	if err != nil {
		log.Fatalf("failed to load chain: %v", err)
	}
	log.Printf("chain loaded: %d business blocks", chain.Len())

	// 4. 构造 Raft 节点
	var peers []transport.Peer
	for _, p := range cfg.Peers {
		peers = append(peers, transport.NewTCPPeer(transport.NodeID(p.ID), p.Addr))
	}

	// 应用 RPC 超时配置
	if cfg.Raft.RPCTimeoutMs > 0 {
		for _, p := range peers {
			p.SetTimeout(cfg.Raft.RPCTimeoutMs)
		}
	}

		raftCfg := raft.Config{
			ID:            transport.NodeID(cfg.NodeID),
			Peers:         peers,
			HeartbeatMs:   cfg.Raft.HeartbeatMs,
			ElectionMinMs: cfg.Raft.ElectionTimeoutMin,
			ElectionMaxMs: cfg.Raft.ElectionTimeoutMax,
			RPCTimeoutMs:  cfg.Raft.RPCTimeoutMs,
			Persister:     boltStore,     // Raft 硬状态持久化
			LogPersister:  boltStore,     // Raft 日志持久化
		}
	node := raft.NewNode(raftCfg)

	// 恢复 Raft 日志状态与区块链对齐（防止重启后日志索引从 1 开始，导致 Committer 跳过）
	chainLast := chain.Last()
	if chainLast.Index > 0 {
		node.RecoverLog(chainLast.Index, chainLast.Term)
	}

	node.Start()

	// 5. 注册 RPC 服务并监听
	svc := raft.NewRaftService(node)
	if err := rpc.Register(svc); err != nil {
		log.Fatalf("rpc register: %v", err)
	}
	rpc.HandleHTTP()
	rpcLn, err := net.Listen("tcp", cfg.RPCAddr)
	if err != nil {
		log.Fatalf("rpc listen %s: %v", cfg.RPCAddr, err)
	}
	go func() {
		log.Printf("RPC server listening on %s", cfg.RPCAddr)
		if err := http.Serve(rpcLn, nil); err != nil {
			log.Printf("rpc serve stopped: %v", err)
		}
	}()

	// 6. 创建 SSE 事件广播器
	broadcaster := api.NewEventBroadcaster()

	// 7. 构造 peerAPIAddrs（api_port = rpc_port + 1000）
	peerAPIAddrs := make(map[transport.NodeID]string)
	for _, p := range cfg.Peers {
		apiAddr, err := deriveAPIAddr(p.Addr)
		if err != nil {
			log.Fatalf("cannot derive api addr from rpc addr %s: %v", p.Addr, err)
		}
		peerAPIAddrs[transport.NodeID(p.ID)] = "http://" + apiAddr
	}
	selfAPIHTTP := "http://" + cfg.APIAddr

	// 8. 启动 Gin API 服务（必须先创建 AppServer，才能传给 Committer）
	appSrv := api.NewAppServer(node, boltStore, chain, cfg.NodeID, selfAPIHTTP, peerAPIAddrs, broadcaster, cfg.APIKey)
	go func() {
		log.Printf("API server listening on %s", cfg.APIAddr)
		if err := appSrv.Run(cfg.APIAddr); err != nil {
			log.Printf("api serve stopped: %v", err)
		}
	}()

	// 9. 启动 Committer（传入 broadcaster 以实现 SSE 推送）
	api.StartCommitter(node, boltStore, chain, broadcaster)

	// 10. 等待 OS 信号，优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %s, shutting down node %s ...", sig, cfg.NodeID)

	node.Stop()
	if err := boltStore.Close(); err != nil {
		log.Printf("store close: %v", err)
	}
	// 关闭所有 Peer 连接
	for _, p := range peers {
		p.Close()
	}
	log.Printf("node %s exited cleanly.", cfg.NodeID)
}

// -----------------------------------------------------------------------
// § 辅助函数
// -----------------------------------------------------------------------

// loadConfig 读取并解析 YAML 配置文件。
func loadConfig(path string) (*NodeConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	var cfg NodeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return &cfg, nil
}

// deriveAPIAddr 从 RPC 地址推导出 API 地址：api_port = rpc_port + 1000。
// 例：127.0.0.1:7002 → 127.0.0.1:8002
func deriveAPIAddr(rpcAddr string) (string, error) {
	parts := strings.LastIndex(rpcAddr, ":")
	if parts < 0 {
		return "", fmt.Errorf("invalid addr: %s", rpcAddr)
	}
	host := rpcAddr[:parts]
	portStr := rpcAddr[parts+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("parse port %s: %w", portStr, err)
	}
	return fmt.Sprintf("%s:%d", host, port+1000), nil
}
