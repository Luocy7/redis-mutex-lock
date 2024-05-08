package redis_lock

import (
	redigolib "github.com/gomodule/redigo/redis"
	"github.com/luocy7/redis_lock/client"
	"github.com/luocy7/redis_lock/client/redigo"
	"sync"
	"testing"
	"time"
)

func newPoolRedigo(network, address, password string) client.Pool {
	return redigo.NewPool(
		&redigolib.Pool{
			MaxIdle:     3,
			IdleTimeout: 240 * time.Second,
			Dial: func() (redigolib.Conn, error) {
				return redigolib.Dial(network, address, redigolib.DialPassword(password))
			},
			TestOnBorrow: func(c redigolib.Conn, t time.Time) error {
				_, err := c.Do("PING")
				return err
			},
		})
}

func clearKey(pool client.Pool, key string) {
	conn, err := pool.Get(nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	deleteKeyScript := client.NewScript(1, `return redis.call("DEL", KEYS[1])`)
	_, _ = conn.Eval(deleteKeyScript, KeyPrefix+key)
}

func TestBlockingLock(t *testing.T) {
	testPool := newPoolRedigo("tcp", ":6379", "")
	ch := make(chan struct{})

	testKey := "test_key"
	clearKey(testPool, testKey)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer func() { ch <- struct{}{} }()
		defer wg.Done()

		lock1 := NewMutex(testKey, testPool, WithExpireSeconds(2))
		if err := lock1.Lock(); err != nil {
			t.Error(err)
			return
		}
		t.Log("lock1 locked with token:", lock1.Value())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ch // ensure lock1 locked
		lock2 := NewMutex(testKey, testPool, WithBlock(), WithBlockWaitingSeconds(5))
		if err := lock2.Lock(); err != nil {
			t.Error(err)
			return
		}
		t.Log("lock2 locked with token:", lock2.Value())
	}()
	wg.Wait()
}
