package raft

import (
	"sync"
	"sync/atomic"
	"time"

	//	"course/labgob"
	"course/labrpc"
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
	log []LogRecord

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

// Start 就一个新的日志条目达成一致
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.role != Leader {
		return 0, 0, false
	}
	rf.log = append(rf.log, LogRecord{
		CommandValid: true,
		Command:      command,
		Term:         rf.currentTerm,
	})
	LOG(rf.me, rf.currentTerm, DLeader, "Leader accept log [%d]T%d", len(rf.log)-1, rf.currentTerm)

	return len(rf.log) - 1, rf.currentTerm, true
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

	// 初始化日志同步所需变量
	rf.log = append(rf.log, LogRecord{})
	rf.matchIndex = make([]int, len(rf.peers))
	rf.nextIndex = make([]int, len(rf.peers))

	go rf.electionTicker()
	go rf.applicationTicker()

	return rf
}

func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}
