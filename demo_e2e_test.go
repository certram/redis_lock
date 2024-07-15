package redis_lock

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"
)

func TestClient_TryLock_e2e(t *testing.T) {

	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	c := NewClient(rdb)
	c.Wait()
	testCases := []struct {
		name string
		//准备redis数据
		before func()
		//校验redis数据并且清理
		after      func()
		key        string
		expiration time.Duration
		wantErr    error
		wantLock   *Lock
	}{

		{
			name:   "locked",
			key:    "locked-key",
			before: func() {},
			after: func() {
				//验证一下，redis上面的数据
				success, err := rdb.Del(context.Background(), "locked-key").Result()
				require.NoError(t, err)
				require.Equal(t, int64(1), success)
			},

			expiration: time.Minute,
			wantErr:    nil,
			wantLock: &Lock{
				key: "locked-key",
			},
		},
		{
			name: "failed",
			key:  "failed-key",
			before: func() {
				val, err := rdb.Set(context.Background(), "failed-key", "123", time.Minute).Result()
				require.NoError(t, err)
				require.Equal(t, "OK", val)
			},
			after: func() {
				//验证一下，redis上面的数据
				val, err := rdb.Get(context.Background(), "failed-key").Result()
				require.NoError(t, err)
				require.Equal(t, "123", val)
			},

			expiration: time.Minute,
			wantErr:    ErrFailedToPreemptLock,
			wantLock: &Lock{
				key: "failed-key",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.before()

			l, err := c.TryLock(context.Background(), tc.key, tc.expiration)
			assert.Equal(t, tc.wantErr, err)
			if err != nil {
				return
			}
			tc.after()
			assert.NotNil(t, l.client)
			assert.NotEmpty(t, l.value)
		})

	}
}
func (c *Client) Wait() {
	for c.client.Ping(context.Background()) != nil {
		return
	}
}
