package raft

// become a follower in `term`, term could not be decreased
func (rf *Raft) becomeFollowerLocked(term int) {
	if term < rf.currentTerm {
		LOG(rf.me, rf.currentTerm, DError, "Can't become Follower, lower term")
		return
	}

	LOG(rf.me, rf.currentTerm, DLog, "%s -> Follower, For T%d->T%d",
		rf.role, rf.currentTerm, term)

	shouldPersist := term != rf.currentTerm

	// ！！！只有当Term增加时，才需要重置选票
	if term > rf.currentTerm {
		rf.votedFor = -1
	}
	rf.role = Follower
	rf.currentTerm = term
	if shouldPersist {
		rf.persistLocked(nil)
	}
}

func (rf *Raft) becomeCandidateLocked() {
	if rf.role == Leader {
		LOG(rf.me, rf.currentTerm, DError, "Leader can't become Candidate")
		return
	}

	LOG(rf.me, rf.currentTerm, DVote, "%s -> Candidate, For T%d->T%d",
		rf.role, rf.currentTerm, rf.currentTerm+1)
	rf.role = Candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.persistLocked(nil)
}

func (rf *Raft) becomeLeaderLocked() {
	if rf.role != Candidate {
		LOG(rf.me, rf.currentTerm, DError, "Only Candidate can become Leader")
		return
	}

	LOG(rf.me, rf.currentTerm, DLeader, "Become Leader in T%d", rf.currentTerm)
	rf.role = Leader
	for peer := 0; peer < len(rf.peers); peer++ {
		rf.nextIndex[peer] = rf.log.size()
		rf.matchIndex[peer] = rf.log.Base
	}
}
