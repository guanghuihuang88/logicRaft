package raft

import "fmt"

type LogRecord struct {
	Term         int
	CommandValid bool
	Command      interface{}
}

type Log struct {
	Records []LogRecord
	Base    int
}

func (log *Log) append(lr LogRecord) {
	log.Records = append(log.Records, lr)
}

func (log *Log) get(i int) LogRecord {
	return log.Records[i-log.Base]
}

func (log *Log) size() int {
	return log.Base + len(log.Records)
}

func (log *Log) lastIncludedIndex() int {
	return log.Base
}

func (log *Log) lastIncludedTerm() int {
	return log.Records[0].Term
}

func (log *Log) lastLogTerm() int {
	return log.Records[len(log.Records)-1].Term
}

func (log *Log) firstLogFor(term int) int {
	for idx, record := range log.Records {
		if record.Term == term {
			return idx
		} else if record.Term > term {
			break
		}
	}
	return InvalidIndex
}

func (log *Log) logString() string {
	var terms string
	prevTerm := log.Records[0].Term
	prevStart := 0
	for i := 0; i < len(log.Records); i++ {
		if log.Records[i].Term != prevTerm {
			terms += fmt.Sprintf(" [%d, %d]T%d", prevStart, i-1, prevTerm)
			prevTerm = log.Records[i].Term
			prevStart = i
		}
	}
	terms += fmt.Sprintf("[%d, %d]T%d", prevStart, log.size()-1, prevTerm)
	return terms
}
