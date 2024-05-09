# Redis Distributed Lock Implementation in Golang

This is a simple implementation of distributed lock using Redis in Golang.

> reference: [Distributed Locks with Redis](https://redis.io/docs/latest/develop/use/patterns/distributed-locks/) by redis.io

## Structure

- client: Redis client, implement a redigo redis client
- mutex: Distributed lock implementation

## Example

```go
package main

import (
	redigolib "github.com/gomodule/redigo/redis"
	"github.com/luocy7/redis_lock/client/redigo"
	"time"
)

func main() {
	mutexLock := NewMutex(
		"lockName",
		redigo.NewPool(
			&redigolib.Pool{
				MaxIdle:     3,
				IdleTimeout: 60 * time.Second,
				Dial: func() (redigolib.Conn, error) {
					return redigolib.Dial("tcp", ":6379")
				},
				TestOnBorrow: func(c redigolib.Conn, t time.Time) error {
					_, err := c.Do("PING")
					return err
				},
			}),
		WithExpireSeconds(20),
	)

	if err := mutexLock.Lock(); err != nil {
		panic(err)
	}

	// Do something

	if _, err := mutexLock.Unlock(); err != nil {
		panic(err)
	}
}
```
