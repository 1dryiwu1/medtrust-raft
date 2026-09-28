// Package blockchain 定义区块结构及哈希计算。
// 在 MedTrust-Raft 中，每条被 Raft 多数派确认的 LogEntry 都映射为一个 Block，
// 通过哈希链保证数据不可篡改。
package blockchain

import (
	"crypto/sha256"
	"fmt"
	"time"

	"medtrust-raft/internal/transport"
)

// Block 是区块链的基本存储单元。
// 每个 Block 与一条 Raft LogEntry 一一对应。
type Block struct {
	Index     uint64 `json:"index"`      // 区块高度（等同于 LogEntry.Index）
	PrevHash  string `json:"prev_hash"`  // 上一个区块的哈希，保证链式完整性
	Hash      string `json:"hash"`       // 本区块的 SHA-256 指纹
	Payload   string `json:"payload"`    // 医疗数据摘要（加密字符串，透传自 LogEntry）
	Timestamp int64  `json:"timestamp"`  // 区块生成时的 Unix 纳秒时间戳
	Term      uint64 `json:"term"`       // 生成时的 Raft 任期，用于审计追溯
}

// genesisHash 是创世区块使用的哨兵哈希，固定值，与任何真实区块不同。
const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// GenesisBlock 返回链的起始区块（Index=0），不承载任何业务数据。
// 每次调用返回新实例，但字段值固定。
func GenesisBlock() *Block {
	b := &Block{
		Index:     0,
		PrevHash:  genesisHash,
		Payload:   "genesis",
		Timestamp: 0,
		Term:      0,
	}
	b.Hash = b.computeHash()
	return b
}

// NewBlock 以 prev 区块为前驱，将一条已提交的 LogEntry 封装为新 Block。
func NewBlock(prev *Block, entry transport.LogEntry) *Block {
	b := &Block{
		Index:     uint64(entry.Index),
		PrevHash:  prev.Hash,
		Payload:   entry.Payload,
		Timestamp: time.Now().UnixNano(),
		Term:      uint64(entry.Term),
	}
	b.Hash = b.computeHash()
	return b
}

// computeHash 计算区块的 SHA-256 指纹。
// 参与哈希的字段：Index | PrevHash | Payload | Timestamp | Term
func (b *Block) computeHash() string {
	raw := fmt.Sprintf("%d|%s|%s|%d|%d",
		b.Index, b.PrevHash, b.Payload, b.Timestamp, b.Term)
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", sum)
}

// IsValid 验证 b 是否合法地接在 prev 之后：
//  1. 索引连续
//  2. PrevHash 正确引用 prev.Hash
//  3. 自身 Hash 可复现
func (b *Block) IsValid(prev *Block) bool {
	if b.Index != prev.Index+1 {
		return false
	}
	if b.PrevHash != prev.Hash {
		return false
	}
	if b.Hash != b.computeHash() {
		return false
	}
	return true
}
