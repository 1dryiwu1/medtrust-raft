// Package api — Chain 内存结构 + Committer goroutine
//
// Committer 是 Raft 共识层与区块链存储层之间的桥梁：
// 它监听 Node.CommitCh，将每条已被多数派确认的 LogEntry
// 封装为 Block，写入 BoltDB，并维护内存链用于哈希链接。
package api

import (
	"fmt"
	"log"
	"sync"
	"time"

	"medtrust-raft/internal/blockchain"
	"medtrust-raft/internal/raft"
	"medtrust-raft/internal/store"
)

// Chain 在内存中保存完整的区块列表，提供快速的哈希链接查询。
// 节点启动时从 BoltDB 加载，之后由 Committer 实时追加。
type Chain struct {
	mu     sync.RWMutex
	blocks []*blockchain.Block
}

// LoadChain 从 BoltStore 加载所有已持久化区块，重建内存链。
// 若数据库为空，链中仅含创世块（不写入 DB，仅作哈希锚点）。
func LoadChain(s *store.BoltStore) (*Chain, error) {
	blocks, err := s.AllBlocks(0, 0)
	if err != nil {
		return nil, fmt.Errorf("load chain: %w", err)
	}
	c := &Chain{}
	if len(blocks) == 0 {
		// 空库：内存中放一个创世块作为哈希锚点，不写 DB
		c.blocks = []*blockchain.Block{blockchain.GenesisBlock()}
	} else {
		c.blocks = blocks
	}
	return c, nil
}

// Last 返回链中最后一个区块（至少是创世块，永远不为 nil）。
func (c *Chain) Last() *blockchain.Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.blocks[len(c.blocks)-1]
}

// Append 向内存链末尾追加一个新区块（由 Committer 调用，已持写锁）。
func (c *Chain) Append(b *blockchain.Block) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blocks = append(c.blocks, b)
}

// Len 返回链中真实业务区块的数量（不含创世块）。
func (c *Chain) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// blocks[0] 是创世块（Index=0），真实业务块从 Index=1 开始
	n := len(c.blocks)
	if n <= 1 {
		return 0
	}
	return n - 1
}

// StartCommitter 启动后台 goroutine，持续消费 Node.CommitCh。
// 每收到一条 CommitNotify，即将对应 LogEntry 封装为 Block 并持久化。
// 若 broadcaster 非 nil，新区块上链后会通过 SSE 推送给所有前端。
func StartCommitter(node *raft.Node, s *store.BoltStore, chain *Chain, broadcaster *EventBroadcaster) {
	go func() {
		for notify := range node.CommitCh {
			entry := notify.Entry

			// 防重复：若链中已有此高度的区块，跳过
			if uint64(entry.Index) <= uint64(chain.Last().Index) {
				continue
			}

			block := blockchain.NewBlock(chain.Last(), entry)

			if err := s.AppendBlock(block); err != nil {
				log.Printf("%s \033[31m[STORE   ]\033[0m Node         failed to persist block %d: %v",
					time.Now().Format("15:04:05.000"), block.Index, err)
				continue
			}

			chain.Append(block)

			log.Printf("%s \033[32m[BLOCK   ]\033[0m              block #%d persisted  payload=%q  hash=%.12s...",
				time.Now().Format("15:04:05.000"),
				block.Index,
				block.Payload,
				block.Hash,
			)

			// Prometheus 指标
			RecordBlock("")

			// SSE 实时推送新区块事件
			if broadcaster != nil {
				broadcaster.Broadcast(Event{
					Type: "block",
					Data: block,
				})
			}
		}
	}()
}
