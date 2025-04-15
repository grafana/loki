## TTLCache - an in-memory cache with item expiration and generics

[![Go Reference](https://pkg.go.dev/badge/github.com/jellydator/ttlcache/v3.svg)](https://pkg.go.dev/github.com/jellydator/ttlcache/v3)
[![Build Status](https://github.com/jellydator/ttlcache/actions/workflows/go.yml/badge.svg)](https://github.com/jellydator/ttlcache/actions/workflows/go.yml)
[![Coverage Status](https://coveralls.io/repos/github/jellydator/ttlcache/badge.svg?branch=master)](https://coveralls.io/github/jellydator/ttlcache?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/jellydator/ttlcache/v3)](https://goreportcard.com/report/github.com/jellydator/ttlcache/v3)

## Features
- Simple API
- Type parameters
- Item expiration and automatic deletion
- Automatic expiration time extension on each `Get` call
- `Loader` interface that may be used to load/lazily initialize missing cache
- Thread Safe
items
- Event handlers (insertion and eviction)
- Metrics

## Installation
```
go get github.com/jellydator/ttlcache/v3
```

## Usage
The main type of `ttlcache` is `Cache`. It represents a single 
in-memory data store.

To create a new instance of `ttlcache.Cache`, the `ttlcache.New()` function 
should be called:
```go
func main() {
	cache := ttlcache.New[string, string]()
}
```

Note that by default, a new cache instance does not let any of its
items to expire or be automatically deleted. However, this feature
can be activated by passing a few additional options into the 
`ttlcache.New()` function and calling the `cache.Start()` method:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	go cache.Start() // starts automatic expired item deletion
}
```

Even though the `cache.Start()` method handles expired item deletion well,
there may be times when the system that uses `ttlcache` needs to determine 
when to delete the expired items itself. For example, it may need to 
delete them only when the resource load is at its lowest (e.g., after 
midnight, when the number of users/HTTP requests drops). So, in situations 
like these, instead of calling `cache.Start()`, the system could 
periodically call `cache.DeleteExpired()`:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	for {
		time.Sleep(4 * time.Hour)
		cache.DeleteExpired()
	}
}
```

The data stored in `ttlcache.Cache` can be retrieved, checked and updated with 
`Set`, `Get`, `Delete`, `Has` etc. methods:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
	)

	// insert data
	cache.Set("first", "value1", ttlcache.DefaultTTL)
	cache.Set("second", "value2", ttlcache.NoTTL)
	cache.Set("third", "value3", ttlcache.DefaultTTL)

	// retrieve data
	item := cache.Get("first")
	fmt.Println(item.Value(), item.ExpiresAt())

	// check key 
	ok := cache.Has("third")
	
	// delete data
	cache.Delete("second")
	cache.DeleteExpired()
	cache.DeleteAll()

	// retrieve data if in cache otherwise insert data
	item, retrieved := cache.GetOrSet("fourth", "value4", WithTTL[string, string](ttlcache.DefaultTTL))

	// retrieve and delete data
	item, present := cache.GetAndDelete("fourth")
}
```

To subscribe to insertion and eviction events, `cache.OnInsertion()` and 
`cache.OnEviction()` methods should be used:
```go
func main() {
	cache := ttlcache.New[string, string](
		ttlcache.WithTTL[string, string](30 * time.Minute),
		ttlcache.WithCapacity[string, string](300),
	)

	cache.OnInsertion(func(ctx context.Context, item *ttlcache.Item[string, string]) {
		fmt.Println(item.Value(), item.ExpiresAt())
	})
	cache.OnEviction(func(ctx context.Context, reason ttlcache.EvictionReason, item *ttlcache.Item[string, string]) {
		if reason == ttlcache.EvictionReasonCapacityReached {
			fmt.Println(item.Key(), item.Value())
		}
	})

	cache.Set("first", "value1", ttlcache.DefaultTTL)
	cache.DeleteAll()
}
```

To load data when the cache does not have it, a custom or
existing implementation of `ttlcache.Loader` can be used:
```go
func main() {
	loader := ttlcache.LoaderFunc[string, string](
		func(c *ttlcache.Cache[string, string], key string) *ttlcache.Item[string, string] {
			// load from file/make an HTTP request
			item := c.Set("key from file", "value from file")
			return item
		},
	)
	cache := ttlcache.New[string, string](
		ttlcache.WithLoader[string, string](loader),
	)

	item := cache.Get("key from file")
}
```
