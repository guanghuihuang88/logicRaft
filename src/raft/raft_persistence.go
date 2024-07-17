package raft

import (
	"bytes"
	"fmt"
	"github.com/guanghuihuang88/logicRaft/labgob"
)

func (rf *Raft) persistString() string {
	return fmt.Sprintf("T%d, VotedFor: %d, Log: [0: %d)", rf.currentTerm, rf.votedFor, rf.log.size())
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).

func (rf *Raft) persistLocked(snapshot []byte) {
	if snapshot == nil {
		snapshot = rf.persister.ReadSnapshot()
	}
	buf := new(bytes.Buffer)
	encode := labgob.NewEncoder(buf)
	encode.Encode(rf.currentTerm)
	encode.Encode(rf.votedFor)
	encode.Encode(rf.log)
	rf.persister.Save(buf.Bytes(), snapshot)
	LOG(rf.me, rf.currentTerm, DPersist, "Persist: %v", rf.persistString())
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (PartC).
	// Example:
	var currentTerm int
	var votedFor int
	var log Log

	buf := bytes.NewBuffer(data)
	decoder := labgob.NewDecoder(buf)
	if err := decoder.Decode(&currentTerm); err != nil {
		LOG(rf.me, rf.currentTerm, DPersist, "Read currentTerm error: %v", err)
		return
	}
	rf.currentTerm = currentTerm

	if err := decoder.Decode(&votedFor); err != nil {
		LOG(rf.me, rf.currentTerm, DPersist, "Read votedFor error: %v", err)
		return
	}
	rf.votedFor = votedFor

	if err := decoder.Decode(&log); err != nil {
		LOG(rf.me, rf.currentTerm, DPersist, "Read log error: %v", err)
		return
	}
	rf.log = log

	LOG(rf.me, rf.currentTerm, DPersist, "Read Persist %v", rf.persistString())
}
