package kvraft

import (
	logicKV "github.com/guanghuihuang88/logicKV"
	"os"
)

type StateMachine struct {
	db *logicKV.DB
}

func NewStateMachine() (*StateMachine, error) {
	opts := logicKV.DefaultOptions
	dir, _ := os.MkdirTemp("", "bitcask-go")
	opts.DirPath = dir
	db, err := logicKV.Open(opts)
	return &StateMachine{
		db: db,
	}, err

}

func (mkv *StateMachine) Get(key string) (string, Err) {
	value, err := mkv.db.Get([]byte(key))

	return string(value), Err(err.Error())
}

func (mkv *StateMachine) Put(key, value string) Err {
	err := mkv.db.Put([]byte(key), []byte(value))

	return Err(err.Error())
}

func (mkv *StateMachine) Append(key, value string) Err {
	err := mkv.db.Put([]byte(key), []byte(value))

	return Err(err.Error())
}
