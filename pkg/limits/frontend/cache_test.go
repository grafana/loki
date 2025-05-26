package frontend

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"
)

func TestTTLCache_Get(t *testing.T) {
	c := newTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// The value should be absent.
	value, ok := c.get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Set the value and it should be present.
	c.set("foo", "bar")
	value, ok = c.get("foo")
	require.Equal(t, "bar", value)
	require.True(t, ok)
	// Advance the time to be 1 second before the expiration time.
	clock.Advance(59 * time.Second)
	value, ok = c.get("foo")
	require.Equal(t, "bar", value)
	require.True(t, ok)
	// Advance the time to be equal to the expiration time, the value should
	// be absent.
	clock.Advance(time.Second)
	value, ok = c.get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Advance the time past the expiration time, the value should still be
	// absent.
	clock.Advance(time.Second)
	value, ok = c.get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
}

func TestTTLCache_Set(t *testing.T) {
	c := newTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	c.set("foo", "bar")
	item1, ok := c.items["foo"]
	require.True(t, ok)
	require.Equal(t, c.clock.Now().Add(time.Minute), item1.expiresAt)
	// Set should refresh the expiration time.
	clock.Advance(time.Second)
	c.set("foo", "bar")
	item2, ok := c.items["foo"]
	require.True(t, ok)
	require.Greater(t, item2.expiresAt, item1.expiresAt)
	require.Equal(t, item2.expiresAt, item1.expiresAt.Add(time.Second))
	// Set should replace the value.
	c.set("foo", "baz")
	value, ok := c.get("foo")
	require.True(t, ok)
	require.Equal(t, "baz", value)
}

func TestTTLCache_Delete(t *testing.T) {
	c := newTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// Set the value and it should be present.
	c.set("foo", "bar")
	value, ok := c.get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", value)
	// Delete the value, it should be absent.
	c.delete("foo")
	value, ok = c.get("foo")
	require.False(t, ok)
	require.Equal(t, "", value)
}

func TestTTLCache_Reset(t *testing.T) {
	c := newTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// Set two values, both should be present.
	c.set("foo", "bar")
	value, ok := c.get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", value)
	c.set("bar", "baz")
	value, ok = c.get("bar")
	require.True(t, ok)
	require.Equal(t, "baz", value)
	// Reset the cache, all should be absent.
	c.reset()
	value, ok = c.get("foo")
	require.False(t, ok)
	require.Equal(t, "", value)
	value, ok = c.get("bar")
	require.False(t, ok)
	require.Equal(t, "", value)
	// Should be able to set values following a reset.
	c.set("baz", "qux")
	value, ok = c.get("baz")
	require.True(t, ok)
	require.Equal(t, "qux", value)
}

func TestTTLCache_EvictExpired(t *testing.T) {
	c := newTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	c.set("foo", "bar")
	_, ok := c.items["foo"]
	require.True(t, ok)
	// Advance the clock and update foo, it should not be evicted as its
	// expiration time should be refreshed.
	clock.Advance(time.Minute)
	c.set("foo", "bar")
	_, ok = c.items["foo"]
	require.True(t, ok)
	eviction1 := clock.Now()
	require.Equal(t, eviction1, c.lastEvictedAt)
	// Advance the clock 15 seconds. Since 15 seconds is less than half
	// the TTL (30 seconds) since the last eviction, no eviction should
	// be run.
	clock.Advance(15 * time.Second)
	c.set("bar", "baz")
	_, ok = c.items["foo"]
	require.True(t, ok)
	_, ok = c.items["bar"]
	require.True(t, ok)
	require.Equal(t, eviction1, c.lastEvictedAt)
	// Advance the clock 16 seconds. Since 31 seconds is more than half the TTL
	// since the last eviction, an eviction should be run, but no items should
	// be evicted.
	clock.Advance(16 * time.Second)
	c.set("baz", "qux")
	_, ok = c.items["foo"]
	require.True(t, ok)
	_, ok = c.items["bar"]
	require.True(t, ok)
	_, ok = c.items["baz"]
	require.True(t, ok)
	eviction2 := clock.Now()
	require.Equal(t, eviction2, c.lastEvictedAt)
	// Advance the clock another 15 seconds. Again, since 15 seconds is less
	// than half the TTL (seconds) since the last eviction, no eviction should
	// be run.
	clock.Advance(15 * time.Second)
	c.set("qux", "corge")
	_, ok = c.items["foo"]
	require.True(t, ok)
	_, ok = c.items["bar"]
	require.True(t, ok)
	_, ok = c.items["baz"]
	require.True(t, ok)
	_, ok = c.items["qux"]
	require.True(t, ok)
	require.Equal(t, eviction2, c.lastEvictedAt)
	// Advance the clock another 16 seconds. Since 31 seconds is more than
	// half the TTL since the last eviction, an eviction should be run and
	// this time foo should be evicted as it has expired.
	clock.Advance(16 * time.Second)
	c.set("corge", "jorge")
	_, ok = c.items["foo"]
	require.False(t, ok)
	_, ok = c.items["bar"]
	require.True(t, ok)
	_, ok = c.items["baz"]
	require.True(t, ok)
	_, ok = c.items["qux"]
	require.True(t, ok)
	_, ok = c.items["corge"]
	require.True(t, ok)
	eviction3 := clock.Now()
	require.Equal(t, eviction3, c.lastEvictedAt)
	// Advance the clock one whole minute. All items should be expired.
	clock.Advance(time.Minute)
	c.set("foo", "bar")
	_, ok = c.items["foo"]
	require.True(t, ok)
	_, ok = c.items["bar"]
	require.False(t, ok)
	_, ok = c.items["baz"]
	require.False(t, ok)
	_, ok = c.items["qux"]
	require.False(t, ok)
	_, ok = c.items["qux"]
	require.False(t, ok)
	eviction4 := clock.Now()
	require.Equal(t, eviction4, c.lastEvictedAt)
}

func TestNopCache(t *testing.T) {
	c := newNopCache[string, string]()
	// The value should be absent.
	value, ok := c.get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Despite setting the value, it should still be absent.
	c.set("foo", "bar")
	value, ok = c.get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
}
