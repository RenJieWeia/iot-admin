package store

import (
	"hash/fnv"
	"sync"
)

// ShardedLock 按 key 哈希分片的互斥锁：同一终端的写入串行化，不同终端互不阻塞。
// 单实例部署下用它替代单写协程，锁粒度比 (terminal_id,item_code) 更粗但实现更简单。
type ShardedLock struct {
	locks []sync.Mutex
}

func NewShardedLock(shards int) *ShardedLock {
	if shards <= 0 {
		shards = 64
	}
	return &ShardedLock{locks: make([]sync.Mutex, shards)}
}

// Lock 取锁并返回解锁函数。
func (s *ShardedLock) Lock(key string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	m := &s.locks[h.Sum32()%uint32(len(s.locks))]
	m.Lock()
	return m.Unlock
}
