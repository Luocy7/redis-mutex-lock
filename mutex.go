package redis_lock

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/luocy7/redis_lock/client"
)

// A Mutex is a distributed mutual exclusion lock.
type Mutex struct {
	MutexOptions
	name string

	genValueFunc func() (string, error)
	value        string
	pool         client.Pool

	runningDog int32              // watchDog status mark
	stopDog    context.CancelFunc // to stop watchDog
}

func NewMutex(name string, pool client.Pool, options ...MutexOption) *Mutex {
	m := &Mutex{
		name:         KeyPrefix + name,
		genValueFunc: genValue,
		pool:         pool,
	}
	for _, option := range options {
		option(&m.MutexOptions)
	}
	repairMutexOptions(&m.MutexOptions)
	return m
}

// Name returns mutex name (i.e. the Redis key).
func (m *Mutex) Name() string {
	return m.name
}

// Value returns the current random value. The value will be empty until a lock is acquired (or WithValue option is used).
func (m *Mutex) Value() string {
	return m.value
}

// Lock only attempts to lock m once and returns immediately regardless of success or failure without retrying.
func (m *Mutex) Lock() error {
	return m.lockContext(context.Background())
}

// lockContext locks m. In case it returns an error on failure, you may retry to acquire the lock by calling this method again.
func (m *Mutex) lockContext(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if err != nil {
			return
		}
		if m.watchDogMode {
			// start a coroutine to watch the lock
			m.watchDog(ctx)
		}
	}()

	err = m.tryLock(ctx)

	if err == nil {
		return nil
	}

	if !IsRetryableErr(err) {
		return err
	}

	// blocking mode
	if m.isBlock {
		return m.blockingLock(ctx)
	}
	return err
}

// tryLock only attempts to lock m once and returns immediately regardless of success or failure without retrying.
func (m *Mutex) tryLock(ctx context.Context) (err error) {
	value, err := m.genValueFunc()
	if err != nil {
		return err
	}
	acquired, err := m.acquire(ctx, m.pool, value)
	if acquired {
		m.value = value
		return nil
	} else if err != nil {
		return err
	}
	// err == nil means lock is acquired by others
	err = ErrLockAcquiredByOthers
	return
}

// Unlock unlocks m and returns the status of unlock.
func (m *Mutex) Unlock() (bool, error) {
	return m.UnlockContext(context.Background())
}

// UnlockContext unlocks m and returns the status of unlock.
func (m *Mutex) UnlockContext(ctx context.Context) (bool, error) {
	return m.release(ctx, m.pool, m.value)
}

// DelayExpire extend the expireTime of the lock.
func (m *Mutex) DelayExpire(ctx context.Context, expiry time.Duration) (bool, error) {
	return m.delay(ctx, m.pool, int64(expiry/time.Second))
}

func genValue() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (m *Mutex) acquire(ctx context.Context, pool client.Pool, value string) (bool, error) {
	conn, err := pool.Get(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	reply, err := conn.SetNX(m.name, value, m.expireTime)
	if err != nil {
		return false, err
	}
	return reply, nil
}

var deleteScript = client.NewScript(1, `
	local val = client.call("GET", KEYS[1])
	if val == ARGV[1] then
		return client.call("DEL", KEYS[1])
	elseif val == false then
		return -1
	else
		return 0
	end
`)

func (m *Mutex) release(ctx context.Context, pool client.Pool, value string) (bool, error) {
	conn, err := pool.Get(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	status, err := conn.Eval(deleteScript, m.name, value)
	if err != nil {
		return false, err
	}
	if status == int64(-1) {
		return false, ErrLockAlreadyExpired
	}
	return status != int64(0), nil
}

var delayScript = client.NewScript(1, `
	local val = client.call("GET", KEYS[1])
	if val == ARGV[1] then
		return client.call("EXPIRE", KEYS[1], ARGV[2])
	else
		return 0
	end
`)

func (m *Mutex) delay(ctx context.Context, pool client.Pool, delay int64) (bool, error) {
	conn, err := pool.Get(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	status, err := conn.Eval(delayScript, m.name, m.value, delay)
	if err != nil {
		return false, err
	}
	if status == int64(-1) {
		return false, ErrLockAlreadyExpired
	}
	return status != int64(0), nil
}

func (m *Mutex) watchDog(ctx context.Context) {
	// ensure the previous watch dog has been recycled
	for !atomic.CompareAndSwapInt32(&m.runningDog, 0, 1) {
	}

	// start watch dog
	ctx, m.stopDog = context.WithCancel(ctx)
	go func() {
		defer func() {
			atomic.StoreInt32(&m.runningDog, 0)
		}()
		m.runWatchDog(ctx)
	}()
}

func (m *Mutex) runWatchDog(ctx context.Context) {
	ticker := time.NewTicker(WatchDogWorkStep)
	defer ticker.Stop()

	for range ticker.C {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// 看门狗负责在用户未显式解锁时，持续为分布式锁进行续期w
		// 通过 lua 脚本，延期之前会确保保证锁仍然属于自己
		// 为避免因为网络延迟而导致锁被提前释放的问题，watch dog 续约时需要把锁的过期时长额外增加 5 s
		_, _ = m.DelayExpire(ctx, WatchDogWorkStep+5)
	}
}

func (m *Mutex) blockingLock(ctx context.Context) error {
	// 阻塞模式等锁时间上限
	timeoutCh := time.After(m.blockWaitingTime)
	// 轮询 ticker，每隔 50 ms 尝试取锁一次
	ticker := time.NewTicker(time.Duration(50) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		select {
		// ctx 终止了
		case <-ctx.Done():
			return fmt.Errorf("lock failed, ctx timeout, err: %w", ctx.Err())
			// 阻塞等锁达到上限时间
		case <-timeoutCh:
			return fmt.Errorf("block waiting time out, err: %w", ErrLockAcquiredByOthers)
		// 放行
		default:
		}

		// 尝试取锁
		err := m.tryLock(ctx)
		if err == nil {
			// 加锁成功，返回结果
			return nil
		}

		// 不可重试类型的错误，直接返回
		if !IsRetryableErr(err) {
			return err
		}
	}
	// 不可达
	return nil
}
