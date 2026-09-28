// Package transport 定义节点间 Raft RPC 通信的所有数据结构与接口。
// 这是整个集群通信的契约文件，所有节点必须严格遵守。
package transport

// -----------------------------------------------------------------------
// § 1. 通用类型
// -----------------------------------------------------------------------

// NodeID 是集群中每个节点的唯一标识（如 "node-1", "hospital-A:7001"）。
type NodeID string

// Term 表示 Raft 的任期编号，单调递增，是判断消息新旧的核心依据。
type Term uint64

// LogIndex 表示日志/区块在链中的序号，从 1 开始。
type LogIndex uint64

// -----------------------------------------------------------------------
// § 2. 日志条目 (Log Entry)
// 在本项目中，一条日志 = 一个候选区块的序列化摘要
// -----------------------------------------------------------------------

// LogEntry 是 Raft 日志的最小单元，也是区块在共识层的载体。
type LogEntry struct {
	Index   LogIndex `json:"index"`    // 日志序号（等同于区块高度）
	Term    Term     `json:"term"`     // 写入时的任期，用于冲突检测
	Payload string   `json:"payload"`  // 医疗数据摘要（加密字符串），业务层写入，共识层透传
}

// -----------------------------------------------------------------------
// § 3. RequestVote RPC — 领导者选举
// -----------------------------------------------------------------------

// RequestVoteArgs 由候选人（Candidate）广播给所有其他节点，请求选票。
type RequestVoteArgs struct {
	Term         Term     `json:"term"`           // 候选人当前任期
	CandidateID  NodeID   `json:"candidate_id"`   // 候选人节点 ID
	LastLogIndex LogIndex `json:"last_log_index"` // 候选人最后一条日志的序号
	LastLogTerm  Term     `json:"last_log_term"`  // 候选人最后一条日志的任期
}

// RequestVoteReply 是收到选票请求的节点返回的应答。
type RequestVoteReply struct {
	Term        Term `json:"term"`         // 应答者当前任期（若更大，候选人需退回 Follower）
	VoteGranted bool `json:"vote_granted"` // true = 投票给候选人；false = 拒绝
}

// -----------------------------------------------------------------------
// § 4. AppendEntries RPC — 心跳 & 日志复制
// -----------------------------------------------------------------------

// AppendEntriesArgs 由 Leader 发送给所有 Follower，承担两个职责：
//   - 当 Entries 为空时：纯心跳，重置 Follower 的选举超时计时器
//   - 当 Entries 非空时：携带新的日志条目，要求 Follower 追加到本地日志
type AppendEntriesArgs struct {
	Term     Term   `json:"term"`      // Leader 当前任期
	LeaderID NodeID `json:"leader_id"` // Leader 节点 ID（Follower 可据此转发客户端请求）

	// 一致性检查字段：
	// Follower 必须确认其 PrevLogIndex 处的日志 Term 与 PrevLogTerm 一致，
	// 才允许追加 Entries，防止日志出现空洞或错位。
	PrevLogIndex LogIndex `json:"prev_log_index"` // 新条目前一条日志的序号
	PrevLogTerm  Term     `json:"prev_log_term"`  // 新条目前一条日志的任期

	Entries []LogEntry `json:"entries"` // 待复制的日志条目（心跳时为空切片）

	LeaderCommit LogIndex `json:"leader_commit"` // Leader 已提交的最高日志序号
}

// AppendEntriesReply 是 Follower 对 AppendEntries 的应答。
type AppendEntriesReply struct {
	Term    Term `json:"term"`    // 应答者当前任期（若更大，Leader 需退回 Follower）
	Success bool `json:"success"` // true = 一致性检查通过并成功追加

	// 加速冲突回退（优化项）：
	// 当 Success=false 时，Follower 返回冲突信息，
	// 帮助 Leader 快速定位需要回退的位置，避免逐条重试。
	ConflictIndex LogIndex `json:"conflict_index"` // 冲突起始序号
	ConflictTerm  Term     `json:"conflict_term"`  // 冲突位置的任期
}

// -----------------------------------------------------------------------
// § 5. RPC 服务接口
// -----------------------------------------------------------------------

// RaftRPC 是每个节点必须暴露的 Raft 服务接口。
// 实现此接口即可被其他节点通过 net/rpc 远程调用。
type RaftRPC interface {
	// RequestVote 处理来自候选人的拉票请求。
	RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) error

	// AppendEntries 处理来自 Leader 的心跳或日志复制请求。
	AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) error
}

// -----------------------------------------------------------------------
// § 5.1 MembershipChange RPC — 动态成员变更
// -----------------------------------------------------------------------

// ChangeType 表示成员变更的类型。
type ChangeType int

const (
	AddNode    ChangeType = iota // 添加新节点
	RemoveNode                   // 移除现有节点
)

// MembershipChangeArgs 由 Leader 广播给所有节点，请求变更集群成员。
type MembershipChangeArgs struct {
	Term     Term       `json:"term"`      // Leader 当前任期
	LeaderID NodeID     `json:"leader_id"` // Leader 节点 ID
	Type     ChangeType `json:"type"`      // 变更类型：添加或移除
	NodeID   NodeID     `json:"node_id"`   // 被变更的节点 ID
	NodeAddr string     `json:"node_addr"` // 被变更节点的 RPC 地址（添加时必填）
}

// MembershipChangeReply 是节点对成员变更请求的应答。
type MembershipChangeReply struct {
	Term    Term `json:"term"`     // 应答者当前任期
	Success bool `json:"success"`  // true = 接受变更
}

// -----------------------------------------------------------------------
// § 6. 节点间传输层接口
// -----------------------------------------------------------------------

// Peer 封装了对单个远端节点的 RPC 调用能力。
// 通过接口隔离，方便在测试时替换为内存 Mock，无需真实网络。
type Peer interface {
	// ID 返回对端节点的唯一标识。
	ID() NodeID

	// SendRequestVote 向对端发起拉票请求，超时后返回 error。
	SendRequestVote(args *RequestVoteArgs) (*RequestVoteReply, error)

	// SendAppendEntries 向对端发起日志复制（或心跳）请求，超时后返回 error。
	SendAppendEntries(args *AppendEntriesArgs) (*AppendEntriesReply, error)

	// SendMembershipChange 向对端发起成员变更请求。
	SendMembershipChange(args *MembershipChangeArgs) (*MembershipChangeReply, error)

	// Close 关闭与对端的连接，释放资源。
	Close() error

	// SetTimeout 动态调整 RPC 调用超时时间（毫秒级）。
	// 用于根据网络环境在运行时调整超时阈值。
	SetTimeout(timeoutMs int)
}
