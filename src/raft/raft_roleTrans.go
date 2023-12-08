package raft

// become a follower in `term`, term could not be decreased
func (rf *Raft) becomeFollowerLocked(term int) {
	if term < rf.currentTerm {
		LOG(rf.me, rf.currentTerm, DError, "Can't become Follower, lower term")
		return
	}

	LOG(rf.me, rf.currentTerm, DLog, "%s -> Follower, For T%d->T%d",
		rf.role, rf.currentTerm, term)

	// important! Could only reset the `votedFor` when term increased
	if term > rf.currentTerm {
		rf.votedFor = -1
	}
	rf.role = Follower
	rf.currentTerm = term
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
}

func (rf *Raft) becomeLeaderLocked() {
	if rf.role != Candidate {
		LOG(rf.me, rf.currentTerm, DLeader,
			"%s, Only candidate can become Leader", rf.role)
		return
	}

	LOG(rf.me, rf.currentTerm, DLeader, "%s -> Leader, For T%d",
		rf.role, rf.currentTerm)
	rf.role = Leader
}
