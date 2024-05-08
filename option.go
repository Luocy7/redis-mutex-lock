package redis_lock

import "time"

type MutexOptions struct {
	isBlock          bool
	watchDogMode     bool
	blockWaitingTime time.Duration
	expireTime       time.Duration
}

type MutexOption func(*MutexOptions)

func WithBlock() MutexOption {
	return func(o *MutexOptions) {
		o.isBlock = true
	}
}

func WithBlockWaitingSeconds(waitingTime time.Duration) MutexOption {
	return func(o *MutexOptions) {
		o.blockWaitingTime = waitingTime * time.Second
	}
}

func WithExpireSeconds(expire time.Duration) MutexOption {
	return func(o *MutexOptions) {
		o.expireTime = expire * time.Second
	}
}

func repairMutexOptions(o *MutexOptions) {
	if o.isBlock && o.blockWaitingTime <= 0 {
		// default block waiting seconds is 5 seconds
		o.blockWaitingTime = DefaultBlockWaitingTime
	}

	if o.expireTime > 0 {
		return
	}

	// if expireTime is not set, use default expire seconds and enable watch dog mode
	o.expireTime = DefaultLockExpire
	o.watchDogMode = true
}
