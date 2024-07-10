package raft

type InstallSnapshotArgs struct {
	Term              int
	LeaderId          int
	LastIncludedIndex int
	LastIncludedTerm  int
	Offset            int
	Data              []byte
	Done              bool
}

type InstallSnapshotReply struct {
	Term int
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// 1. 停止接收新的客户端请求：Leader 需要停止接收新的客户端请求，以确保在快照创建期间不会有新的状态变化。
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 如果index大于raft自身的提交，则不能安装快照；如果raft自身快照的下标大于index，则不需要安装快照
	if index > rf.commitIndex || index < rf.log.Base {
		return
	}

	// 构造新的日志
	lastIncludedTerm := rf.log.get(index).Term
	lastIncludedIndex := index
	newLog := Log{
		Records: make([]LogRecord, 0),
		Base:    lastIncludedIndex,
	}
	newLog.append(LogRecord{Term: lastIncludedTerm})

	for i := index + 1; i < rf.log.size(); i++ {
		record := rf.log.get(i)
		newLog.append(record.copy())
	}
	rf.log = newLog

	rf.persistLocked(snapshot)
}

func (rf *Raft) CondInstallSnap() {

}

// snapshotToPeer 单次 RPC
func (rf *Raft) snapshotToPeer(peer int, args *InstallSnapshotArgs, term int) {
	reply := &InstallSnapshotReply{}
	ok := rf.sendInstallSnapshot(peer, args, reply)

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if !ok {
		LOG(rf.me, rf.currentTerm, DLog, "-> S%d, Lost or crashed", peer)
		return
	}

	if reply.Term > rf.currentTerm {
		rf.becomeFollowerLocked(reply.Term)
		return
	}

	rf.nextIndex[peer] = args.LastIncludedIndex + 1
	rf.matchIndex[peer] = args.LastIncludedIndex
}

// sendInstallSnapshot RPC
func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

// InstallSnapshot 快照 RPC 回调函数
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	LOG(rf.me, rf.currentTerm, DSnap, "<- S%d, Receive snap, Prev=[%d]T%d, Len()=%d")
	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		LOG(rf.me, rf.currentTerm, DSnap, "<- S%d, Reject Log, Higher term, T%d<T%d", args.LeaderId, args.Term, rf.currentTerm)
		return
	}

	if args.Term >= rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}

	if args.LastIncludedIndex <= rf.log.Base {
		return
	}

	// 构造新的日志
	newLog := Log{
		Records: make([]LogRecord, 0),
		Base:    args.LastIncludedIndex,
	}
	newLog.append(LogRecord{Term: args.LastIncludedTerm})

	for i := args.LastIncludedIndex + 1; i < rf.log.size(); i++ {
		record := rf.log.get(i)
		newLog.append(record.copy())
	}
	rf.log = newLog

	if args.LastIncludedIndex > rf.commitIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	if args.LastIncludedIndex > rf.lastApplied {
		rf.lastApplied = args.LastIncludedIndex
	}

	rf.persistLocked(args.Data)

	rf.applyCh <- ApplyMsg{
		SnapshotValid: true,
		Snapshot:      args.Data,
		SnapshotTerm:  args.LastIncludedTerm,
		SnapshotIndex: args.LastIncludedIndex,
	}

}
