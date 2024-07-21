package kvraft

type KVStateMachine struct {
	KV map[string]string
}

func NewKVStateMachine() *KVStateMachine {
	return &KVStateMachine{
		KV: make(map[string]string),
	}
}

func (mkv *KVStateMachine) Get(key string) (string, Err) {
	if value, ok := mkv.KV[key]; ok {
		return value, OK
	}
	return "", ErrNoKey
}

func (mkv *KVStateMachine) Put(key, value string) Err {
	mkv.KV[key] = value
	return OK
}

func (mkv *KVStateMachine) Append(key, value string) Err {
	mkv.KV[key] += value
	return OK
}
