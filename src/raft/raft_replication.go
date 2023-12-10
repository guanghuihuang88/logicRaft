package raft

import (
	"sort"
	"time"
)

// 心跳RPC

const (
	replicateInterval time.Duration = 70 * time.Millisecond
)

type LogRecord struct {
	Term         int
	CommandValid bool
	Command      interface{}
}

type AppendEntriesArgs struct {
	Term     int // 任期
	LeaderId int // Leader id，以便于Follower重定向请求

	PrevLogIndex int         // 新的日志条目紧随之前的索引值
	PrevLogTerm  int         // PrevLogIndex的任期
	Entries      []LogRecord // 准备存储的日志条目（心跳则为空，一次性发送多个是为了提高效率）
	LeaderCommit int         // Leader已经提交的日志索引值
}

type AppendEntriesReply struct {
	Term    int  // 当前任期
	Success bool // Follower包含了匹配上PrevLogIndex和PrevLogTerm的日志时，为true

	// 一致性检查失败时，用来加速日志修复的参数
	XTerm  int // 这个是Follower中与Leader冲突的Log对应的任期号，如果Follower在对应位置没有Log，那么这里会返回-1。
	XIndex int // 若XTerm!=-1，XIndex对应任期号为XTerm的第一条Log条目的槽位号
	XLen   int // 若XTerm=-1，XLen表示空白的Log槽位数
}

// replicationTicker 心跳（日志同步）loop
func (rf *Raft) replicationTicker(term int) {
	for !rf.killed() {
		contextRemained := rf.startReplication(term)
		if !contextRemained {
			break
		}

		time.Sleep(replicateInterval)
	}
}

// startReplication 开始心跳（日志同步）
func (rf *Raft) startReplication(term int) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查上下文
	if rf.contextLostLocked(Leader, term) {
		LOG(rf.me, rf.currentTerm, DLog, "Lost Leader[%d] to %s[T%d]", term, rf.role, rf.currentTerm)
		return false
	}

	for peer := 0; peer < len(rf.peers); peer++ {
		if peer == rf.me {
			// Don't forget to update Leader's matchIndex
			rf.matchIndex[peer] = len(rf.log) - 1
			rf.nextIndex[peer] = len(rf.log)
			continue
		}

		prevIdx := rf.nextIndex[peer] - 1
		prevTerm := rf.log[prevIdx].Term
		args := &AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderId:     rf.me,
			PrevLogIndex: prevIdx,
			PrevLogTerm:  prevTerm,
			Entries:      rf.log[prevIdx+1:],
			LeaderCommit: rf.commitIndex,
		}
		LOG(rf.me, rf.currentTerm, DDebug, "-> S%d, Send log, Prev=[%d]T%d, Len()=%d", peer, args.PrevLogIndex, args.PrevLogTerm, len(args.Entries))
		go rf.replicateToPeer(peer, args, term)
	}

	return true
}

// replicateToPeer 单次 RPC
func (rf *Raft) replicateToPeer(peer int, args *AppendEntriesArgs, term int) {
	reply := &AppendEntriesReply{}
	ok := rf.sendAppendEntries(peer, args, reply)

	rf.mu.Lock()
	defer rf.mu.Unlock()
	if !ok {
		LOG(rf.me, rf.currentTerm, DLog, "-> S%d, Lost or crashed", peer)
		return
	}

	// align the term
	if reply.Term > rf.currentTerm {
		rf.becomeFollowerLocked(reply.Term)
		return
	}

	// 检查上下文
	if rf.contextLostLocked(Leader, term) {
		LOG(rf.me, rf.currentTerm, DLog, "-> S%d, Context Lost, T%d:Leader->T%d:%s", peer, term, rf.currentTerm, rf.role)
		return
	}

	// 一致性检查失败：
	// 当一致性检查失败，说明 Leader 相应 peer 在 nextIndex 前一个位置的任期不一致 （leader.log[idx].Term > peer.log[idx].Term）
	// 则往前找到 Leader.log 在上一个 Term 的最后一个 index
	if !reply.Success {
		idx := rf.nextIndex[peer] - 1
		term := rf.log[idx].Term
		for idx > 0 && rf.log[idx].Term == term {
			idx--
		}
		rf.nextIndex[peer] = idx + 1
		LOG(rf.me, rf.currentTerm, DLog, "Log not matched in %d, Update next=%d", args.PrevLogIndex, rf.nextIndex[peer])
		return
	}

	// 一致性检查成功：
	// 找到了 Leader 和 Follower 达成一致的最大索引条目，Follower 删除从这一点之后的所有条目，并同步复制 Leader 从那一点之后的条目
	// Leader 更新为该 Follower 维护的 nextIndex，表示 Leader 要发送给 Follower 的下一个日志条目的索引
	// 当选出一个新的 Leader 时，该 Leader 将所有 nextIndex 的值都初始化为自己最后一个日志条目的 index+1
	rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
	rf.nextIndex[peer] = rf.matchIndex[peer] + 1

	// 日志应用
	majorityMatched := rf.getMajorityIndexLocked()
	if majorityMatched > rf.commitIndex {
		LOG(rf.me, rf.currentTerm, DApply, "Leader update the commit index %d->%d", rf.commitIndex, majorityMatched)
		rf.commitIndex = majorityMatched
		rf.applyCond.Signal()
	}
}

// sendRequestVote RPC
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// AppendEntries  心跳（日志同步）RPC 回调函数
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	LOG(rf.me, rf.currentTerm, DDebug, "<- S%d, Receive log, Prev=[%d]T%d, Len()=%d", args.LeaderId, args.PrevLogIndex, args.PrevLogTerm, len(args.Entries))
	reply.Term = rf.currentTerm
	reply.Success = false

	if args.Term < rf.currentTerm {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject Log, Higher term, T%d<T%d", args.LeaderId, args.Term, rf.currentTerm)
		return
	}
	if args.Term >= rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}

	// 一致性检查
	if args.PrevLogIndex >= len(rf.log) {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject Log, Follower log too short, Len:%d <= Prev:%d", args.LeaderId, len(rf.log), args.PrevLogIndex)
		return
	}
	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		LOG(rf.me, rf.currentTerm, DLog2, "<- S%d, Reject Log, Prev log not match, [%d]: T%d != T%d", args.LeaderId, args.PrevLogIndex, rf.log[args.PrevLogIndex].Term, args.PrevLogTerm)
		return
	}

	// 日志同步
	rf.log = append(rf.log[:args.PrevLogIndex+1], args.Entries...)
	LOG(rf.me, rf.currentTerm, DLog2, "Follower append logs: (%d, %d]", args.PrevLogIndex, args.PrevLogIndex+len(args.Entries))
	reply.Success = true

	// 日志应用
	if args.LeaderCommit > rf.commitIndex {
		LOG(rf.me, rf.currentTerm, DApply, "Follower update the commit index %d->%d", rf.commitIndex, args.LeaderCommit)
		rf.commitIndex = args.LeaderCommit
		rf.applyCond.Signal()
	}

	rf.resetElectionTimerLocked()
}

// getMajorityIndexLocked 计算多数 Peer 的匹配点
func (rf *Raft) getMajorityIndexLocked() int {
	// TODO(spw): may could be avoid copying
	tmpIndexes := make([]int, len(rf.matchIndex))
	copy(tmpIndexes, rf.matchIndex)
	sort.Ints(sort.IntSlice(tmpIndexes))
	majorityIdx := (len(tmpIndexes) - 1) / 2
	LOG(rf.me, rf.currentTerm, DDebug, "Match index after sort: %v, majority[%d]=%d", tmpIndexes, majorityIdx, tmpIndexes[majorityIdx])
	return tmpIndexes[majorityIdx] // min -> max
}
