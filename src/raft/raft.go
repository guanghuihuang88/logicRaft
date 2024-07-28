package raft

import (
	"encoding/json"
	"github.com/guanghuihuang88/logicRaft/labrpc"
	"sync"
	"sync/atomic"
	"time"
	//	"course/labgob"
)

// ApplyMsg 每次有日志条目提交时，每个 Raft 实例需要向该 channel 发送一条 apply 消息
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For PartD:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

type Role string

const (
	Follower  Role = "Follower"
	Candidate Role = "Candidate"
	Leader    Role = "Leader"
)

const (
	InvalidTerm  int = 0
	InvalidIndex int = 0
)

type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// used for election loop
	role            Role
	currentTerm     int
	votedFor        int
	electionStart   time.Time
	electionTimeout time.Duration

	// log in Peer's local
	log Log

	// only used when it is Leader,
	// log view for each peer
	nextIndex  []int
	matchIndex []int

	// fields for apply loop
	commitIndex int
	lastApplied int
	applyCh     chan ApplyMsg
	applyCond   *sync.Cond
}

// GetState 询问 Raft 实例其当前的 term，以及是否自认为 Leader（但实际由于网络分区的发生，可能并不是）
func (rf *Raft) GetState() (int, bool) {
	// Your code here (PartA).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.role == Leader
}

func (rf *Raft) GetRaftStateSize() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// Start 就一个新的日志条目达成一致
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.role != Leader {
		return 0, 0, false
	}
	rf.log.append(LogRecord{
		CommandValid: true,
		Command:      command,
		Term:         rf.currentTerm,
	})
	rf.persistLocked(nil)
	LOG(rf.me, rf.currentTerm, DLeader, "Leader accept log [%d]T%d", rf.log.size()-1, rf.currentTerm)

	return rf.log.size() - 1, rf.currentTerm, true
}

func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// Make 创建一个新的 Raft 实例
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	//  initialization code
	rf.role = Follower
	rf.currentTerm = 1 // leave 0 to invalid
	rf.votedFor = -1
	// a dummy entry to aovid lots of corner checks
	rf.log = Log{
		Records: make([]LogRecord, 0),
		Base:    0,
	}
	rf.log.append(LogRecord{Term: InvalidTerm})

	// 初始化日志同步所需变量
	rf.matchIndex = make([]int, len(rf.peers))
	rf.nextIndex = make([]int, len(rf.peers))

	// 初始化日志应用所需变量
	rf.applyCh = applyCh
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.applyCond = sync.NewCond(&rf.mu)

	rf.readPersist(persister.ReadRaftState()) // 同步快照信息
	if rf.log.Base > 0 {
		rf.commitIndex = rf.log.Base
		rf.lastApplied = rf.log.Base
	}

	go rf.electionTicker()
	go rf.applicationTicker()

	return rf
}

func DeepCopy(src interface{}) (interface{}, error) {
	// 将对象转换为 JSON 字符串
	jsonBytes, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}

	// 将 JSON 字符串反序列化为新的对象
	var dst interface{}
	err = json.Unmarshal(jsonBytes, &dst)
	if err != nil {
		return nil, err
	}

	return dst, nil
}
