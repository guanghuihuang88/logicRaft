package raft

type InstallSnapshotArgs struct {
}

type InstallSnapshotReply struct {
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
	if index > rf.commitIndex || index < rf.log.lastIncludedIndex() {
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
		record, err := DeepCopy(rf.log.get(i))
		if err != nil {
			LOG(rf.me, rf.currentTerm, DSnap, "DeepCopy error: %v", err)
		}
		newLog.append(record.(LogRecord))
	}
	rf.log = newLog

	rf.persister.Save(rf.persister.ReadRaftState(), snapshot)
}

func (rf *Raft) CondInstallSnap() {

}

// snapshotToPeer 单次 RPC
func (rf *Raft) snapshotToPeer(peer int, args *InstallSnapshotArgs, term int) {
	reply := &InstallSnapshotReply{}
	ok := rf.sendInstallSnapshot(peer, args, reply)
	println(ok)
}

// sendInstallSnapshot RPC
func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
}

// InstallSnapshot 快照 RPC 回调函数
func (rf *Raft) InstallSnapshot(args *RequestVoteArgs, reply *RequestVoteReply) {
}
