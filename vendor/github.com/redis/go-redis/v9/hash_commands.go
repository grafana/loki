package redis

import "context"

type HashCmdable interface {
	HDel(ctx context.Context, key string, fields ...string) *IntCmd
	HExists(ctx context.Context, key, field string) *BoolCmd
	HGet(ctx context.Context, key, field string) *StringCmd
	HGetAll(ctx context.Context, key string) *MapStringStringCmd
	HIncrBy(ctx context.Context, key, field string, incr int64) *IntCmd
	HIncrByFloat(ctx context.Context, key, field string, incr float64) *FloatCmd
	HKeys(ctx context.Context, key string) *StringSliceCmd
	HLen(ctx context.Context, key string) *IntCmd
	HMGet(ctx context.Context, key string, fields ...string) *SliceCmd
	HSet(ctx context.Context, key string, values ...interface{}) *IntCmd
	HMSet(ctx context.Context, key string, values ...interface{}) *BoolCmd
	HSetNX(ctx context.Context, key, field string, value interface{}) *BoolCmd
	HScan(ctx context.Context, key string, cursor uint64, match string, count int64) *ScanCmd
	HVals(ctx context.Context, key string) *StringSliceCmd
	HRandField(ctx context.Context, key string, count int) *StringSliceCmd
	HRandFieldWithValues(ctx context.Context, key string, count int) *KeyValueSliceCmd
}

func (c cmdable) HDel(ctx context.Context, key string, fields ...string) *IntCmd {
	args := make([]interface{}, 2+len(fields))
	args[0] = "hdel"
	args[1] = key
	for i, field := range fields {
		args[2+i] = field
	}
	cmd := NewIntCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HExists(ctx context.Context, key, field string) *BoolCmd {
	cmd := NewBoolCmd(ctx, "hexists", key, field)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HGet(ctx context.Context, key, field string) *StringCmd {
	cmd := NewStringCmd(ctx, "hget", key, field)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HGetAll(ctx context.Context, key string) *MapStringStringCmd {
	cmd := NewMapStringStringCmd(ctx, "hgetall", key)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HIncrBy(ctx context.Context, key, field string, incr int64) *IntCmd {
	cmd := NewIntCmd(ctx, "hincrby", key, field, incr)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HIncrByFloat(ctx context.Context, key, field string, incr float64) *FloatCmd {
	cmd := NewFloatCmd(ctx, "hincrbyfloat", key, field, incr)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HKeys(ctx context.Context, key string) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "hkeys", key)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HLen(ctx context.Context, key string) *IntCmd {
	cmd := NewIntCmd(ctx, "hlen", key)
	_ = c(ctx, cmd)
	return cmd
}

// HMGet returns the values for the specified fields in the hash stored at key.
// It returns an interface{} to distinguish between empty string and nil value.
func (c cmdable) HMGet(ctx context.Context, key string, fields ...string) *SliceCmd {
	args := make([]interface{}, 2+len(fields))
	args[0] = "hmget"
	args[1] = key
	for i, field := range fields {
		args[2+i] = field
	}
	cmd := NewSliceCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

// HSet accepts values in following formats:
//
//   - HSet("myhash", "key1", "value1", "key2", "value2")
//
//   - HSet("myhash", []string{"key1", "value1", "key2", "value2"})
//
//   - HSet("myhash", map[string]interface{}{"key1": "value1", "key2": "value2"})
//
//     Playing struct With "redis" tag.
//     type MyHash struct { Key1 string `redis:"key1"`; Key2 int `redis:"key2"` }
//
//   - HSet("myhash", MyHash{"value1", "value2"}) Warn: redis-server >= 4.0
//
//     For struct, can be a structure pointer type, we only parse the field whose tag is redis.
//     if you don't want the field to be read, you can use the `redis:"-"` flag to ignore it,
//     or you don't need to set the redis tag.
//     For the type of structure field, we only support simple data types:
//     string, int/uint(8,16,32,64), float(32,64), time.Time(to RFC3339Nano), time.Duration(to Nanoseconds ),
//     if you are other more complex or custom data types, please implement the encoding.BinaryMarshaler interface.
//
// Note that in older versions of Redis server(redis-server < 4.0), HSet only supports a single key-value pair.
// redis-docs: https://redis.io/commands/hset (Starting with Redis version 4.0.0: Accepts multiple field and value arguments.)
// If you are using a Struct type and the number of fields is greater than one,
// you will receive an error similar to "ERR wrong number of arguments", you can use HMSet as a substitute.
func (c cmdable) HSet(ctx context.Context, key string, values ...interface{}) *IntCmd {
	args := make([]interface{}, 2, 2+len(values))
	args[0] = "hset"
	args[1] = key
	args = appendArgs(args, values)
	cmd := NewIntCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

// HMSet is a deprecated version of HSet left for compatibility with Redis 3.
func (c cmdable) HMSet(ctx context.Context, key string, values ...interface{}) *BoolCmd {
	args := make([]interface{}, 2, 2+len(values))
	args[0] = "hmset"
	args[1] = key
	args = appendArgs(args, values)
	cmd := NewBoolCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HSetNX(ctx context.Context, key, field string, value interface{}) *BoolCmd {
	cmd := NewBoolCmd(ctx, "hsetnx", key, field, value)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HVals(ctx context.Context, key string) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "hvals", key)
	_ = c(ctx, cmd)
	return cmd
}

// HRandField redis-server version >= 6.2.0.
func (c cmdable) HRandField(ctx context.Context, key string, count int) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "hrandfield", key, count)
	_ = c(ctx, cmd)
	return cmd
}

// HRandFieldWithValues redis-server version >= 6.2.0.
func (c cmdable) HRandFieldWithValues(ctx context.Context, key string, count int) *KeyValueSliceCmd {
	cmd := NewKeyValueSliceCmd(ctx, "hrandfield", key, count, "withvalues")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) HScan(ctx context.Context, key string, cursor uint64, match string, count int64) *ScanCmd {
	args := []interface{}{"hscan", key, cursor}
	if match != "" {
		args = append(args, "match", match)
	}
	if count > 0 {
		args = append(args, "count", count)
	}
	cmd := NewScanCmd(ctx, c, args...)
	_ = c(ctx, cmd)
	return cmd
}
