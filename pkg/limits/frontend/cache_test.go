package frontend

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"
)

func TestTTLCache_Get(t *testing.T) {
	c := NewTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// The value should be absent.
	value, ok := c.Get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Set the value and it should be present.
	c.Set("foo", "bar")
	value, ok = c.Get("foo")
	require.Equal(t, "bar", value)
	require.True(t, ok)
	// Advance the time to be 1 second before the expiration time.
	clock.Advance(59 * time.Second)
	value, ok = c.Get("foo")
	require.Equal(t, "bar", value)
	require.True(t, ok)
	// Advance the time to be equal to the expiration time, the value should
	// be absent.
	clock.Advance(time.Second)
	value, ok = c.Get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Advance the time past the expiration time, the value should still be
	// absent.
	clock.Advance(time.Second)
	value, ok = c.Get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
}

func TestTTLCache_Set(t *testing.T) {
	c := NewTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	c.Set("foo", "bar")
	item1, ok := c.items["foo"]
	require.True(t, ok)
	require.Equal(t, c.clock.Now().Add(time.Minute), item1.expiresAt)
	// Set should refresh the expiration time.
	clock.Advance(time.Second)
	c.Set("foo", "bar")
	item2, ok := c.items["foo"]
	require.True(t, ok)
	require.Greater(t, item2.expiresAt, item1.expiresAt)
	require.Equal(t, item2.expiresAt, item1.expiresAt.Add(time.Second))
	// Set should replace the value.
	c.Set("foo", "baz")
	value, ok := c.Get("foo")
	require.True(t, ok)
	require.Equal(t, "baz", value)
}

func TestTTLCache_Delete(t *testing.T) {
	c := NewTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// Set the value and it should be present.
	c.Set("foo", "bar")
	value, ok := c.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", value)
	// Delete the value, it should be absent.
	c.Delete("foo")
	value, ok = c.Get("foo")
	require.False(t, ok)
	require.Equal(t, "", value)
}

func TestTTLCache_Reset(t *testing.T) {
	c := NewTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	// Set two values, both should be present.
	c.Set("foo", "bar")
	value, ok := c.Get("foo")
	require.True(t, ok)
	require.Equal(t, "bar", value)
	c.Set("bar", "baz")
	value, ok = c.Get("bar")
	require.True(t, ok)
	require.Equal(t, "baz", value)
	// Reset the cache, all should be absent.
	c.Reset()
	value, ok = c.Get("foo")
	require.False(t, ok)
	require.Equal(t, "", value)
	value, ok = c.Get("bar")
	require.False(t, ok)
	require.Equal(t, "", value)
	// Should be able to set values following a reset.
	c.Set("baz", "qux")
	value, ok = c.Get("baz")
	require.True(t, ok)
	require.Equal(t, "qux", value)
}

func TestTTLCache_RemoveExpiredItems(t *testing.T) {
	c := NewTTLCache[string, string](time.Minute)
	clock := quartz.NewMock(t)
	c.clock = clock
	c.Set("foo", "bar")
	_, ok := c.items["foo"]
	require.True(t, ok)
	// Advance the clock and update foo, it should not be removed.
	clock.Advance(time.Minute)
	c.Set("foo", "bar")
	_, ok = c.items["foo"]
	require.True(t, ok)
	// Advance the clock again but this time set bar, foo should be removed.
	clock.Advance(time.Minute)
	c.Set("bar", "baz")
	_, ok = c.items["foo"]
	require.False(t, ok)
	_, ok = c.items["bar"]
	require.True(t, ok)
}

func TestNopCache(t *testing.T) {
	c := NewNopCache[string, string]()
	// The value should be absent.
	value, ok := c.Get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
	// Despite setting the value, it should still be absent.
	c.Set("foo", "bar")
	value, ok = c.Get("foo")
	require.Equal(t, "", value)
	require.False(t, ok)
}
