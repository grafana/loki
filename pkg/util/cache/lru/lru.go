package lru

import (
	"fmt"

	lru_hashicorp "github.com/hashicorp/golang-lru"
	"k8s.io/utils/keymutex"
)

func defaultKeyStringifier[K comparable](key K) string {
	return fmt.Sprintf("%+v", key)
}

// Cache is a simple LRU cache wrapper that locks the key until the value supplier provides the value.
// It can be used if it's necessary to make sure the value for the key was created/fetched only once even during concurrent access.
type Cache[K comparable, V any] struct {
	delegate       *lru_hashicorp.Cache
	onKeyMissing   func(key K) (V, error)
	keyLock        keymutex.KeyMutex
	keyStringifier func(K) string
}

type valueSupplier[K comparable, V any] func(key K) (V, error)

func NewCache[K comparable, V any](
	size int,
	onKeyMissing valueSupplier[K, V],
	onEvicted func(key K, value V),
) (*Cache[K, V], error) {
	var onEvictedAdapter func(key, val any)
	if onEvicted != nil {
		onEvictedAdapter = func(key, val any) {
			onEvicted(key.(K), val.(V))
		}
	}
	c, err := lru_hashicorp.NewWithEvict(size, onEvictedAdapter)
	if err != nil {
		return nil, fmt.Errorf("error creating LRU cache: %w", err)
	}
	return &Cache[K, V]{
		delegate:       c,
		onKeyMissing:   onKeyMissing,
		keyLock:        keymutex.NewHashed(size),
		keyStringifier: defaultKeyStringifier[K],
	}, nil
}

func (c *Cache[K, V]) Get(key K) (V, error) {
	value, ok := c.delegate.Get(key)
	if ok {
		return value.(V), nil
	}

	keyString := c.keyStringifier(key)
	c.keyLock.LockKey(keyString)
	defer func(keyLock keymutex.KeyMutex, keyString string) {
		_ = keyLock.UnlockKey(keyString)
	}(c.keyLock, keyString)
	value, ok = c.delegate.Get(key)
	if ok {
		return value.(V), nil
	}

	value, err := c.onKeyMissing(key)
	if err != nil {
		return value.(V), fmt.Errorf("can not get the value for the key:'%v' due to error: %w", key, err)
	}
	c.delegate.Add(key, value)
	return value.(V), nil
}
