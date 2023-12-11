package raft

import (
	"fmt"
	"math/rand"
	"time"
)

const (
	electionTimeoutMin time.Duration = 250 * time.Millisecond
	electionTimeoutMax time.Duration = 400 * time.Millisecond
)

// 选举 RPC

type RequestVoteArgs struct {
	Term        int // 任期
	CandidateId int // Raft编号

	// 用于日志比较
	LastLogIndex int // candidate的log最新索引
	LastLogTerm  int // candidate的log最新索引的Term
}

func (args *RequestVoteArgs) String() string {
	return fmt.Sprintf("Candidate-%d, T%d, Last: [%d]T%d", args.CandidateId, args.Term, args.LastLogIndex, args.LastLogTerm)
}

type RequestVoteReply struct {
	Term        int  // 投票者任期
	VoteGranted bool // 是否投票
}

func (reply *RequestVoteReply) String() string {
	return fmt.Sprintf("T%d, VoteGranted: %v", reply.Term, reply.VoteGranted)
}

// electionTicker 选举loop
func (rf *Raft) electionTicker() {
	for !rf.killed() {

		// 判断是否发起Leader选举
		rf.mu.Lock()
		if rf.role != Leader && rf.isElectionTimeoutLocked() {
			rf.becomeCandidateLocked()

			go rf.startElection(rf.currentTerm)
		}
		rf.mu.Unlock()

		// 挂起一段 50 ~ 350 milliseconds 的随机时间
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// startElection 开始选举
func (rf *Raft) startElection(term int) bool {
	votes := 0

	// askVoteFromPeer 单次 RPC（这里实现为嵌套函数的作用是，可以在各个 goroutine 中修改外层的局部变量 votes
	askVoteFromPeer := func(peer int, args *RequestVoteArgs, term int) {

		// send RPC
		reply := &RequestVoteReply{}
		ok := rf.sendRequestVote(peer, args, reply)

		// handle the response
		rf.mu.Lock()
		defer rf.mu.Unlock()
		if !ok {
			LOG(rf.me, rf.currentTerm, DDebug, "Ask vote from %d, Lost or error", peer)
			return
		}

		// align the term
		if reply.Term > rf.currentTerm {
			rf.becomeFollowerLocked(reply.Term)
			return
		}

		// check the context
		if rf.contextLostLocked(Candidate, rf.currentTerm) {
			LOG(rf.me, rf.currentTerm, DVote, "Lost context, abort RequestVoteReply in T%d", rf.currentTerm)
			return
		}

		// count votes
		if reply.VoteGranted {
			votes++
		}
		if votes > len(rf.peers)/2 {
			rf.becomeLeaderLocked()
			go rf.replicationTicker(term)
		}
	}

	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查上下文
	if rf.contextLostLocked(Candidate, term) {
		return false
	}

	for peer := 0; peer < len(rf.peers); peer++ {
		if peer == rf.me {
			votes++
			continue
		}

		args := &RequestVoteArgs{
			Term:         rf.currentTerm,
			CandidateId:  rf.me,
			LastLogIndex: len(rf.log) - 1,
			LastLogTerm:  rf.log[len(rf.log)-1].Term,
		}

		go askVoteFromPeer(peer, args, term)
	}

	return true
}

// sendRequestVote RPC
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// RequestVote 选举 RPC 回调函数
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (PartA, PartB).

	rf.mu.Lock()
	defer rf.mu.Unlock()
	LOG(rf.me, rf.currentTerm, DDebug, "<- S%d, VoteAsked, Args=%v", args.CandidateId, args.String())

	reply.Term = rf.currentTerm
	reply.VoteGranted = false
	// align the term
	if args.Term < rf.currentTerm {
		LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Reject Vote, Higher term, T%d>T%d", args.CandidateId, rf.currentTerm, args.Term)
		return
	}
	if args.Term > rf.currentTerm {
		rf.becomeFollowerLocked(args.Term)
	}

	// check for votedFor
	if rf.votedFor != -1 {
		LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Reject Voted, Already voted to S%d", args.CandidateId, rf.votedFor)
		return
	}

	// check log, only grante vote when the candidates have more up-to-date log
	if rf.isMoreUpToDateLocked(args.LastLogTerm, args.LastLogIndex) {
		LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Reject Vote, S%d's log less up-to-date", args.CandidateId)
		return
	}

	reply.VoteGranted = true
	rf.votedFor = args.CandidateId
	rf.resetElectionTimerLocked()
	LOG(rf.me, rf.currentTerm, DVote, "-> S%d, Vote granted", args.CandidateId)
}

// isElectionTimeoutLocked 是否超时
func (rf *Raft) isElectionTimeoutLocked() bool {
	return time.Since(rf.electionStart) > rf.electionTimeout
}

// resetElectionTimerLocked 重置选举超时时间
func (rf *Raft) resetElectionTimerLocked() {
	// 上次重置时间
	rf.electionStart = time.Now()
	randRange := int64(electionTimeoutMax - electionTimeoutMin)
	// 随机选举超时时间（ 250 * time.Millisecond ~ 400 * time.Millisecond ）
	rf.electionTimeout = electionTimeoutMin + time.Duration(rand.Int63()%randRange)
}

// contextLostLocked 上下文检查:「上下文」就是指 Term 和 Role。即在一个任期内，只要你的角色没有变化，就能放心地推进状态机。
func (rf *Raft) contextLostLocked(role Role, term int) bool {
	return !(rf.currentTerm == term && rf.role == role)
}

// isMoreUpToDateLocked 日志比较
func (rf *Raft) isMoreUpToDateLocked(candidateTerm, candidateIndex int) bool {
	l := len(rf.log)
	lastTerm, lastIndex := rf.log[l-1].Term, l-1
	LOG(rf.me, rf.currentTerm, DVote, "Compare last log, Me: [%d]T%d, Candidate: [%d]T%d", lastIndex, lastTerm, candidateIndex, candidateTerm)

	if lastTerm != candidateTerm {
		return lastTerm > candidateTerm
	}
	return lastIndex > candidateIndex
}
