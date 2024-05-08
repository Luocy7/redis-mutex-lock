package redis_lock

import "errors"

// ErrLockAlreadyExpired is the error resulting if trying to release the lock which already expired.
var ErrLockAlreadyExpired = errors.New("lock was already expired")

// ErrLockAcquiredByOthers ErrLockNotAcquired is the error resulting others acquired the lock.
var ErrLockAcquiredByOthers = errors.New("lock is acquired by others")

func IsRetryableErr(err error) bool {
	return errors.Is(err, ErrLockAcquiredByOthers)
}
