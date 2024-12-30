# Redsync

[![Go Reference](https://pkg.go.dev/badge/github.com/go-redsync/redsync/v4.svg)](https://pkg.go.dev/github.com/go-redsync/redsync/v4) [![Build Status](https://travis-ci.org/go-redsync/redsync.svg?branch=master)](https://travis-ci.org/go-redsync/redsync) 

Redsync provides a Redis-based distributed mutual exclusion lock implementation for Go as described in [this post](http://redis.io/topics/distlock). A reference library (by [antirez](https://github.com/antirez)) for Ruby is available at [github.com/antirez/redlock-rb](https://github.com/antirez/redlock-rb).

## Installation

Install Redsync using the go get command:

    $ go get github.com/go-redsync/redsync/v4

Two driver implementations will be installed; however, only the one used will be included in your project.

 * [Redigo](https://github.com/gomodule/redigo)
 * [Go-redis](https://github.com/go-redis/redis)

See the [examples](examples) folder for usage of each driver.

## Documentation

- [Reference](https://godoc.org/github.com/go-redsync/redsync)

## Usage

Error handling is simplified to `panic` for shorter example.

```go
package main

import (
	goredislib "github.com/redis/go-redis/v9"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
)

func main() {
	// Create a pool with go-redis (or redigo) which is the pool redisync will
	// use while communicating with Redis. This can also be any pool that
	// implements the `redis.Pool` interface.
	client := goredislib.NewClient(&goredislib.Options{
		Addr: "localhost:6379",
	})
	pool := goredis.NewPool(client) // or, pool := redigo.NewPool(...)

	// Create an instance of redisync to be used to obtain a mutual exclusion
	// lock.
	rs := redsync.New(pool)

	// Obtain a new mutex by using the same name for all instances wanting the
	// same lock.
	mutexname := "my-global-mutex"
	mutex := rs.NewMutex(mutexname)

	// Obtain a lock for our given mutex. After this is successful, no one else
	// can obtain the same lock (the same mutex name) until we unlock it.
	if err := mutex.Lock(); err != nil {
		panic(err)
	}

	// Do your work that requires the lock.

	// Release the lock so other processes or threads can obtain a lock.
	if ok, err := mutex.Unlock(); !ok || err != nil {
		panic("unlock failed")
	}
}
```

## Contributing

Contributions are welcome.

## License

Redsync is available under the [BSD (3-Clause) License](https://opensource.org/licenses/BSD-3-Clause).

## Disclaimer

This code implements an algorithm which is currently a proposal, it was not formally analyzed. Make sure to understand how it works before using it in production environments.

## Real World Uses

Below is a list of public, open source projects that use Redsync:

- [Sourcegraph](https://github.com/sourcegraph/sourcegraph): Universal code search and intelligence platform. Uses Redsync in an internal cache implementation.
- [Open Match](https://github.com/googleforgames/open-match) by Google: Flexible, extensible, and scalable video game matchmaking. Uses Redsync with its state store implementation.
- [Gocron](https://github.com/go-co-op/gocron) by go-co-op: gocron is a job distributed scheduling package which lets you run Go functions at pre-determined intervals using a simple, human-friendly syntax. Uses Redsync with its distributed job scheduler implementation.

If you are using Redsync in a project please send a pull request to add it to the list.
