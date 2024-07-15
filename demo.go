package redis_lock

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrLockNotHold         = errors.New("未持有锁")
	ErrFailedToPreemptLock = errors.New("加锁失败")
	//go:embed scripts/lua/unlock.lua
	luaUnlock string
	//go:embed scripts/lua/refresh.lua
	refreshLock string
	//go:embed scripts/lua/lock.lua
	luaLock string
)

type Client struct {
	client redis.Cmdable
	s      singleflight.Group
}

func NewClient(c redis.Cmdable) *Client {
	return &Client{
		client: c,
	}
}

type Lock struct {
	client     redis.Cmdable
	key        string
	value      string
	expiration time.Duration
	unlock     chan struct{}
}

func newLock(c redis.Cmdable, key string, val string, expiration time.Duration) *Lock {
	return &Lock{
		client:     c,
		key:        key,
		value:      val,
		expiration: expiration,
		unlock:     make(chan struct{}, 1),
	}
}
func (l *Lock) AutoRefresh(interval, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	ch := make(chan struct{}, 1)
	defer close(ch)
	for {
		select {
		case <-ch:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				ch <- struct{}{}
				continue
			}
			if err != nil {
				return err
			}
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cancel()
			if errors.Is(err, context.DeadlineExceeded) {
				ch <- struct{}{}
				continue
			}
			if err != nil {
				return err
			}
		case <-l.unlock:
			return nil
		}
	}
}
func (l *Lock) Refresh(ctx context.Context) error {
	res, err := l.client.Eval(ctx, refreshLock, []string{l.key}, l.value, l.expiration.Milliseconds()).Int64()
	if errors.Is(err, redis.Nil) {
		return ErrLockNotHold
	}
	if err != nil {
		return err
	}
	if res != 1 {
		return ErrLockNotHold
	}
	return nil
}

func (c *Client) SingleFlightLock(ctx context.Context, key string, expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	for {
		flag := false
		resCh := c.s.DoChan(key, func() (interface{}, error) {
			flag = true
			return c.Lock(ctx, key, expiration, retry, timeout)

		})
		select {
		case res := <-resCh:
			if flag {
				if res.Err != nil {
					return nil, res.Err
				}
				return res.Val.(*Lock), nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Client) Lock(ctx context.Context, key string, expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	value := uuid.New().String()
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		_, cancel := context.WithTimeout(ctx, timeout)
		res, err := c.client.Eval(ctx, luaLock, []string{key}, value, expiration).Bool()
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if res {
			return newLock(c.client, key, value, expiration), nil
		}
		interval, ok := retry.Next()
		if !ok {
			return nil, ErrFailedToPreemptLock
		}
		time.Sleep(interval)
		if timer == nil {
			timer = time.NewTimer(interval)
		}
		timer.Reset(interval)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:

		}
	}
}
func (c *Client) TryLock(ctx context.Context, key string, expiration time.Duration) (*Lock, error) {
	value := uuid.New().String()
	res, err := c.client.SetNX(ctx, key, value, expiration).Result()
	if err != nil {
		return nil, err
	}
	if !res {
		return nil, ErrFailedToPreemptLock
	}
	return newLock(c.client, key, value, expiration), nil
}
func (l *Lock) Unlock(ctx context.Context) error {
	//val, err := l.client.Get(ctx, l.key).Result()
	//if err != nil {
	//	return err
	//}
	//if l.value == val {
	//	_, err := l.client.Del(ctx, l.key).Result()
	//	if err != nil {
	//		return err
	//	}
	//}
	//return nil
	defer func() {
		l.unlock <- struct{}{}
		close(l.unlock)
	}()
	res, err := l.client.Eval(ctx, luaUnlock, []string{l.key}, l.value).Int64()
	if errors.Is(err, redis.Nil) {
		//
		return ErrLockNotHold
	}
	if err != nil {
		return err
	}

	if res == 0 {
		return ErrLockNotHold
	}
	return nil
}
