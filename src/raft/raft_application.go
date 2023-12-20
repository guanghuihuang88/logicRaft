package raft

// applicationTicker 可以理解成log日志的消费者，每次消费完最新可commit的日志后，就挂起消费协程；当生产协程(replicateToPeer函数)更新commitIndex后，消费协程被唤醒
func (rf *Raft) applicationTicker() {
	for !rf.killed() {
		rf.mu.Lock()
		rf.applyCond.Wait()

		// 1. 构造所有待 apply 的 ApplyMsg
		entries := make([]LogRecord, 0)
		for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
			entries = append(entries, rf.log.get(i))
		}
		rf.mu.Unlock()

		// 2. 遍历这些 msgs，进行 apply
		for i, entry := range entries {
			rf.applyCh <- ApplyMsg{
				CommandValid: entry.CommandValid,
				Command:      entry.Command,
				CommandIndex: rf.lastApplied + 1 + i, // must be cautious
			}
		}

		// 3. 更新 lastApplied
		rf.mu.Lock()
		LOG(rf.me, rf.currentTerm, DApply, "Apply log for [%d, %d]", rf.lastApplied+1, rf.lastApplied+len(entries))
		rf.lastApplied += len(entries)
		rf.mu.Unlock()
	}
}
