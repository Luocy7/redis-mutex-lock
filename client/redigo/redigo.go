package redigo

import (
	"context"
	"errors"
	redisclient "github.com/luocy7/redis_lock/client"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

// Pool is an abstract Redigo-based pool of Redis connections.
type Pool interface {
	Get() redis.Conn
	GetContext(ctx context.Context) (redis.Conn, error)
}

// pool is a Redigo-based pool implementation, Also implements redisclient.Pool.
type pool struct {
	delegate Pool
}

// Get returns a connection from the pool
func (p *pool) Get(ctx context.Context) (redisclient.Conn, error) {
	if ctx != nil {
		c, err := p.delegate.GetContext(ctx)
		if err != nil {
			return nil, err
		}
		return &conn{c}, nil
	}
	return &conn{p.delegate.Get()}, nil
}

// NewPool returns a Redigo-based pool implementation.
func NewPool(delegate Pool) redisclient.Pool {
	return &pool{delegate}
}

// Conn is an abstract Redigo-based connection, Also implements redisclient.Conn.
type conn struct {
	delegate redis.Conn
}

func (c *conn) Get(name string) (string, error) {
	value, err := redis.String(c.delegate.Do("GET", name))
	return value, noErrNil(err)
}

func (c *conn) Set(name string, value string) (bool, error) {
	reply, err := redis.String(c.delegate.Do("SET", name, value))
	return reply == "OK", noErrNil(err)
}

func (c *conn) SetNX(name string, value string, expiry time.Duration) (bool, error) {
	reply, err := redis.String(c.delegate.Do("SET", name, value, "NX", "PX", int(expiry/time.Millisecond)))
	return reply == "OK", noErrNil(err)
}

func (c *conn) Eval(script *redisclient.Script, keysAndArgs ...interface{}) (interface{}, error) {
	v, err := c.delegate.Do("EVALSHA", args(script, script.Hash, keysAndArgs)...)
	var e redis.Error
	if errors.As(err, &e) && strings.HasPrefix(string(e), "NOSCRIPT ") {
		v, err = c.delegate.Do("EVAL", args(script, script.Src, keysAndArgs)...)
	}
	return v, noErrNil(err)
}

func (c *conn) Close() error {
	err := c.delegate.Close()
	return noErrNil(err)
}

func noErrNil(err error) error {
	if !errors.Is(err, redis.ErrNil) {
		return err
	}
	return nil
}

func args(script *redisclient.Script, spec string, keysAndArgs []interface{}) []interface{} {
	var args []interface{}
	if script.KeyCount < 0 {
		args = make([]interface{}, 1+len(keysAndArgs))
		args[0] = spec
		copy(args[1:], keysAndArgs)
	} else {
		args = make([]interface{}, 2+len(keysAndArgs))
		args[0] = spec
		args[1] = script.KeyCount
		copy(args[2:], keysAndArgs)
	}
	return args
}
