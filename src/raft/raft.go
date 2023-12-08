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

	role        Role
	currentTerm int
	votedFor    int

	// used for election loop
	electionStart   time.Time
	electionTimeout time.Duration
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
	index := -1
	term := -1
	isLeader := true

	return index, term, isLeader
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

	return rf
}

func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}
