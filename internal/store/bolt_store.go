// Package store 提供基于 BoltDB 的区块持久化层。
package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"medtrust-raft/internal/blockchain"
	"medtrust-raft/internal/raft"
	"medtrust-raft/internal/transport"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketBlocks       = []byte("blocks")
	bucketMeta         = []byte("meta")
	bucketRaftLog      = []byte("raft_log")
	bucketIndexPatient = []byte("idx_patient") // patient_id -> []blockIndex
	bucketIndexTime    = []byte("idx_time")    // YYYY-MM-DD -> []blockIndex
	bucketUsers        = []byte("portal_users")
	bucketConsents     = []byte("portal_consents")
	bucketAttachments  = []byte("portal_attachments")
	bucketRecordDrafts = []byte("portal_record_drafts")
	keyLastIndex       = []byte("last_index")
	keyRaftTerm        = []byte("raft_term")
	keyRaftVotedFor    = []byte("raft_voted_for")
	keyPortalCrypto    = []byte("portal_crypto_key")
)

// BoltStore 封装 BoltDB，提供区块的读写与查询。
// 并发安全：bbolt 内部以事务模型保证隔离，无需额外加锁。
type BoltStore struct {
	db *bolt.DB
}

func (s *BoltStore) PortalCryptoKey(newKey []byte) ([]byte, error) {
	var key []byte
	err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketMeta)
		if existing := bucket.Get(keyPortalCrypto); existing != nil {
			key = append([]byte(nil), existing...)
			return nil
		}
		key = append([]byte(nil), newKey...)
		return bucket.Put(keyPortalCrypto, key)
	})
	return key, err
}

// Open 打开（或创建）指定路径的 BoltDB 数据库，并初始化所需 Bucket。
// path 的父目录不存在时会自动创建。
func Open(path string) (*BoltStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", filepath.Dir(path), err)
	}
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// 确保所有 Bucket 存在
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{
			bucketBlocks, bucketMeta, bucketRaftLog,
			bucketIndexPatient, bucketIndexTime, bucketUsers, bucketConsents, bucketAttachments, bucketRecordDrafts,
		} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init buckets: %w", err)
	}
	return &BoltStore{db: db}, nil
}

// PortalUser is stored outside the immutable medical ledger.
type PortalUser struct {
	Username     string `json:"username"`
	Role         string `json:"role"`
	PasswordHash string `json:"password_hash"`
	RealName     string `json:"real_name,omitempty"`
	LicenseNo    string `json:"license_no,omitempty"`
	Hospital     string `json:"hospital,omitempty"`
	Department   string `json:"department,omitempty"`
	Verified     bool   `json:"verified"`
	VerifiedAt   string `json:"verified_at,omitempty"`
}

func (s *BoltStore) SavePortalUser(user PortalUser) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketUsers).Put([]byte(user.Username), data)
	})
}

type Consent struct {
	Patient   string `json:"patient"`
	Doctor    string `json:"doctor"`
	GrantedAt string `json:"granted_at"`
	ExpiresAt string `json:"expires_at"`
	Active    bool   `json:"active"`
}

func consentKey(patient, doctor string) []byte { return []byte(patient + "\x00" + doctor) }

func (s *BoltStore) SaveConsent(consent Consent) error {
	data, err := json.Marshal(consent)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConsents).Put(consentKey(consent.Patient, consent.Doctor), data)
	})
}

func (s *BoltStore) LoadConsents() ([]Consent, error) {
	items := make([]Consent, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketConsents).ForEach(func(_, value []byte) error {
			var item Consent
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			items = append(items, item)
			return nil
		})
	})
	return items, err
}

func (s *BoltStore) GetConsent(patient, doctor string) (*Consent, error) {
	var consent Consent
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketConsents).Get(consentKey(patient, doctor))
		if value == nil {
			return fmt.Errorf("consent not found")
		}
		return json.Unmarshal(value, &consent)
	})
	if err != nil {
		return nil, err
	}
	return &consent, nil
}

type AttachmentMeta struct {
	ID          string `json:"id"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Patient     string `json:"patient"`
	Doctor      string `json:"doctor"`
	StoragePath string `json:"storage_path"`
	CreatedAt   string `json:"created_at"`
}

// RecordDraft is a mutable, encrypted staging record kept outside the ledger
// until the patient approves the exact content hash.
type RecordDraft struct {
	ID                  string `json:"id"`
	Patient             string `json:"patient"`
	Doctor              string `json:"doctor"`
	Ciphertext          string `json:"ciphertext"`
	RecordHash          string `json:"record_hash"`
	Status              string `json:"status"`
	CreatedAt           string `json:"created_at"`
	ReviewedAt          string `json:"reviewed_at,omitempty"`
	ApprovedAt          string `json:"approved_at,omitempty"`
	RejectedAt          string `json:"rejected_at,omitempty"`
	RejectionReason     string `json:"rejection_reason,omitempty"`
	ConsentID           string `json:"consent_id,omitempty"`
	SubmittedAt         string `json:"submitted_at,omitempty"`
	SubmittedBlockIndex uint64 `json:"submitted_block_index,omitempty"`
	CertificateID       string `json:"certificate_id,omitempty"`
}

func (s *BoltStore) SaveRecordDraft(draft RecordDraft) error {
	data, err := json.Marshal(draft)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRecordDrafts).Put([]byte(draft.ID), data)
	})
}

func (s *BoltStore) GetRecordDraft(id string) (*RecordDraft, error) {
	var draft RecordDraft
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketRecordDrafts).Get([]byte(id))
		if value == nil {
			return fmt.Errorf("record draft not found")
		}
		return json.Unmarshal(value, &draft)
	})
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (s *BoltStore) LoadRecordDrafts() ([]RecordDraft, error) {
	items := make([]RecordDraft, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRecordDrafts).ForEach(func(_, value []byte) error {
			var item RecordDraft
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			items = append(items, item)
			return nil
		})
	})
	return items, err
}

func (s *BoltStore) SaveAttachment(meta AttachmentMeta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucketAttachments).Put([]byte(meta.ID), data) })
}

func (s *BoltStore) GetAttachment(id string) (*AttachmentMeta, error) {
	var meta AttachmentMeta
	err := s.db.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(bucketAttachments).Get([]byte(id))
		if value == nil {
			return fmt.Errorf("attachment not found")
		}
		return json.Unmarshal(value, &meta)
	})
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *BoltStore) LoadPortalUsers() ([]PortalUser, error) {
	users := make([]PortalUser, 0)
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketUsers).ForEach(func(_, value []byte) error {
			var user PortalUser
			if err := json.Unmarshal(value, &user); err != nil {
				return err
			}
			users = append(users, user)
			return nil
		})
	})
	return users, err
}

// AppendBlock 将区块写入 "blocks" Bucket，并更新索引和 meta。
// 同一事务内完成，保证原子性。
func (s *BoltStore) AppendBlock(b *blockchain.Block) error {
	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("store: marshal block %d: %w", b.Index, err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		// 保存区块
		if err := tx.Bucket(bucketBlocks).Put(encodeIndex(b.Index), data); err != nil {
			return err
		}
		// 更新 last_index
		if err := tx.Bucket(bucketMeta).Put(keyLastIndex, encodeIndex(b.Index)); err != nil {
			return err
		}
		// 更新索引
		return s.updateIndices(tx, b)
	})
}

// updateIndices 更新 patient_id 和 time 索引。
func (s *BoltStore) updateIndices(tx *bolt.Tx, b *blockchain.Block) error {
	// 尝试解析 Payload 为结构化医疗记录
	var record struct {
		PatientID string `json:"patient_id"`
		Record    struct {
			PatientID string `json:"patient_id"`
		} `json:"record"`
	}
	_ = json.Unmarshal([]byte(b.Payload), &record)
	patientID := record.PatientID
	if patientID == "" {
		patientID = record.Record.PatientID
	}

	// patient_id 索引
	if patientID != "" {
		bkt := tx.Bucket(bucketIndexPatient)
		var indices []uint64
		if v := bkt.Get([]byte(patientID)); v != nil {
			_ = json.Unmarshal(v, &indices)
		}
		indices = append(indices, b.Index)
		data, _ := json.Marshal(indices)
		if err := bkt.Put([]byte(patientID), data); err != nil {
			return err
		}
	}

	// time 索引（按天）
	if b.Timestamp > 0 {
		day := time.Unix(0, b.Timestamp).UTC().Format("2006-01-02")
		bkt := tx.Bucket(bucketIndexTime)
		var indices []uint64
		if v := bkt.Get([]byte(day)); v != nil {
			_ = json.Unmarshal(v, &indices)
		}
		indices = append(indices, b.Index)
		data, _ := json.Marshal(indices)
		if err := bkt.Put([]byte(day), data); err != nil {
			return err
		}
	}

	return nil
}

// GetBlock 按区块高度（Index）查询单个区块，不存在时返回 nil, nil。
func (s *BoltStore) GetBlock(index uint64) (*blockchain.Block, error) {
	var b blockchain.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketBlocks).Get(encodeIndex(index))
		if v == nil {
			return fmt.Errorf("store: block %d not found", index)
		}
		return json.Unmarshal(v, &b)
	})
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// LastIndex 返回已持久化的最高区块高度。数据库为空时返回 0。
func (s *BoltStore) LastIndex() (uint64, error) {
	var idx uint64
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketMeta).Get(keyLastIndex)
		if v == nil {
			idx = 0
			return nil
		}
		idx = binary.BigEndian.Uint64(v)
		return nil
	})
	return idx, err
}

// AllBlocks 按 Index 升序返回数据库中的全部区块。
// 若 limit > 0，则最多返回 limit 条；若 offset > 0，则跳过前 offset 条。
func (s *BoltStore) AllBlocks(offset, limit int) ([]*blockchain.Block, error) {
	var blocks []*blockchain.Block
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketBlocks).ForEach(func(_, v []byte) error {
			if offset > 0 {
				offset--
				return nil
			}
			if limit > 0 && len(blocks) >= limit {
				return nil
			}
			var b blockchain.Block
			if err := json.Unmarshal(v, &b); err != nil {
				return err
			}
			blocks = append(blocks, &b)
			return nil
		})
	})
	return blocks, err
}

// Close 关闭数据库，释放文件锁。
func (s *BoltStore) Close() error {
	return s.db.Close()
}

// -----------------------------------------------------------------------
// § Raft 硬状态持久化
// -----------------------------------------------------------------------

// SaveRaftState 将当前 Raft 任期和投票目标原子写入 meta bucket。
func (s *BoltStore) SaveRaftState(term transport.Term, votedFor transport.NodeID) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		buf := make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(term))
		if err := b.Put(keyRaftTerm, buf); err != nil {
			return err
		}
		return b.Put(keyRaftVotedFor, []byte(votedFor))
	})
}

// LoadRaftState 从 meta bucket 恢复 Raft 硬状态。
// 若不存在，返回零值和 nil 错误。
func (s *BoltStore) LoadRaftState() (transport.Term, transport.NodeID, error) {
	var term uint64
	var votedFor string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketMeta)
		if v := b.Get(keyRaftTerm); v != nil && len(v) == 8 {
			term = binary.BigEndian.Uint64(v)
		}
		if v := b.Get(keyRaftVotedFor); v != nil {
			votedFor = string(v)
		}
		return nil
	})
	return transport.Term(term), transport.NodeID(votedFor), err
}

// -----------------------------------------------------------------------
// § Raft 日志持久化（实现 raft.LogPersister 接口）
// -----------------------------------------------------------------------

// SaveLogEntry 将单条 Raft 日志条目持久化到 raft_log bucket。
func (s *BoltStore) SaveLogEntry(entry transport.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("store: marshal log entry %d: %w", entry.Index, err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRaftLog).Put(encodeIndex(uint64(entry.Index)), data)
	})
}

// LoadAllLogEntries 按 Index 升序返回所有已持久化的 Raft 日志条目。
func (s *BoltStore) LoadAllLogEntries() ([]transport.LogEntry, error) {
	var entries []transport.LogEntry
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRaftLog).ForEach(func(_, v []byte) error {
			var e transport.LogEntry
			if err := json.Unmarshal(v, &e); err != nil {
				return err
			}
			entries = append(entries, e)
			return nil
		})
	})
	return entries, err
}

// ClearLogEntries 清空所有已持久化日志（快照后调用）。
func (s *BoltStore) ClearLogEntries() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bucketRaftLog); err != nil {
			return err
		}
		_, err := tx.CreateBucket(bucketRaftLog)
		return err
	})
}

// ClearLogEntriesBefore 删除指定索引及之前的所有日志条目。
func (s *BoltStore) ClearLogEntriesBefore(index transport.LogIndex) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRaftLog)
		return b.ForEach(func(k, v []byte) error {
			idx := binary.BigEndian.Uint64(k)
			if idx <= uint64(index) {
				return b.Delete(k)
			}
			return nil
		})
	})
}

// -----------------------------------------------------------------------
// § 快照持久化（实现 raft.SnapshotStore 接口）
// -----------------------------------------------------------------------

var (
	bucketSnapshots = []byte("snapshots")
	keySnapshotMeta = []byte("snapshot_meta")
)

// SaveSnapshot 持久化快照元数据和数据。
func (s *BoltStore) SaveSnapshot(snap *raft.Snapshot) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketSnapshots)
		if err != nil {
			return err
		}
		meta := struct {
			LastIndex uint64 `json:"last_index"`
			LastTerm  uint64 `json:"last_term"`
			Size      int    `json:"size"`
		}{
			LastIndex: uint64(snap.LastIndex),
			LastTerm:  uint64(snap.LastTerm),
			Size:      len(snap.Data),
		}
		metaData, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if err := b.Put(keySnapshotMeta, metaData); err != nil {
			return err
		}
		return b.Put([]byte("data"), snap.Data)
	})
}

// LoadSnapshot 加载最新快照。
func (s *BoltStore) LoadSnapshot() (*raft.Snapshot, error) {
	var snap raft.Snapshot
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSnapshots)
		if b == nil {
			return fmt.Errorf("no snapshot bucket")
		}
		metaData := b.Get(keySnapshotMeta)
		if metaData == nil {
			return fmt.Errorf("no snapshot meta")
		}
		var meta struct {
			LastIndex uint64 `json:"last_index"`
			LastTerm  uint64 `json:"last_term"`
			Size      int    `json:"size"`
		}
		if err := json.Unmarshal(metaData, &meta); err != nil {
			return err
		}
		snap.LastIndex = transport.LogIndex(meta.LastIndex)
		snap.LastTerm = transport.Term(meta.LastTerm)
		snap.Data = make([]byte, meta.Size)
		copy(snap.Data, b.Get([]byte("data")))
		return nil
	})
	if err != nil {
		return nil, nil // 无快照不报错
	}
	return &snap, nil
}

// -----------------------------------------------------------------------
// § 索引查询
// -----------------------------------------------------------------------

// QueryByPatientID 按患者 ID 查询相关区块。
func (s *BoltStore) QueryByPatientID(patientID string) ([]*blockchain.Block, error) {
	var indices []uint64
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketIndexPatient).Get([]byte(patientID))
		if v == nil {
			return nil
		}
		return json.Unmarshal(v, &indices)
	})
	if err != nil {
		return nil, err
	}
	return s.getBlocksByIndices(indices)
}

// QueryByTimeRange 按时间范围查询区块（start, end 为 Unix 纳秒时间戳）。
func (s *BoltStore) QueryByTimeRange(start, end int64) ([]*blockchain.Block, error) {
	var allIndices []uint64
	startDay := time.Unix(0, start).UTC().Format("2006-01-02")
	endDay := time.Unix(0, end).UTC().Format("2006-01-02")

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketIndexTime)
		return b.ForEach(func(k, v []byte) error {
			day := string(k)
			if day < startDay || day > endDay {
				return nil
			}
			var indices []uint64
			if err := json.Unmarshal(v, &indices); err != nil {
				return err
			}
			allIndices = append(allIndices, indices...)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return s.getBlocksByIndices(allIndices)
}

// getBlocksByIndices 按索引列表批量查询区块。
func (s *BoltStore) getBlocksByIndices(indices []uint64) ([]*blockchain.Block, error) {
	blocks := make([]*blockchain.Block, 0, len(indices))
	for _, idx := range indices {
		b, err := s.GetBlock(idx)
		if err != nil {
			continue
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// encodeIndex 将 uint64 编码为 8 字节大端序，作为 BoltDB 的 Key。
// 大端序保证 ForEach 遍历时按 Index 升序排列。
func encodeIndex(idx uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, idx)
	return buf
}
