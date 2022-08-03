# groupcache

A modified version of [group cache](https://github.com/golang/groupcache) with
support for `context.Context`, [go modules](https://github.com/golang/go/wiki/Modules),
and explicit key removal and expiration. See the `CHANGELOG` for a complete list of 
modifications.

## Summary

groupcache is a caching and cache-filling library, intended as a
replacement for memcached in many cases.

For API docs and examples, see http://godoc.org/github.com/mailgun/groupcache

   
### Modifications from original library

* Support for explicit key removal from a group. `Remove()` requests are 
  first sent to the peer who owns the key, then the remove request is 
  forwarded to every peer in the groupcache. NOTE: This is a best case design
  since it is possible a temporary network disruption could occur resulting
  in remove requests never making it their peers. In practice this scenario
  is very rare and the system remains very consistent. In case of an
  inconsistency placing a expiration time on your values will ensure the 
  cluster eventually becomes consistent again.

* Support for expired values. `SetBytes()`, `SetProto()` and `SetString()` now
  accept an optional `time.Time{}` which represents a time in the future when the
  value will expire. Expiration is handled by the LRU Cache when a `Get()` on a 
  key is requested. This means no network coordination of expired values is needed.
  However this does require that time on all nodes in the cluster is synchronized 
  for consistent expiration of values.

* Now always populating the hotcache. A more complex algorithm is unnecessary
  when the LRU cache will ensure the most used values remain in the cache. The
  evict code ensures the hotcache never overcrowds the maincache.

## Comparing Groupcache to memcached

### **Like memcached**, groupcache:

 * shards by key to select which peer is responsible for that key

### **Unlike memcached**, groupcache:

 * does not require running a separate set of servers, thus massively
   reducing deployment/configuration pain.  groupcache is a client
   library as well as a server.  It connects to its own peers.

 * comes with a cache filling mechanism.  Whereas memcached just says
   "Sorry, cache miss", often resulting in a thundering herd of
   database (or whatever) loads from an unbounded number of clients
   (which has resulted in several fun outages), groupcache coordinates
   cache fills such that only one load in one process of an entire
   replicated set of processes populates the cache, then multiplexes
   the loaded value to all callers.

 * does not support versioned values.  If key "foo" is value "bar",
   key "foo" must always be "bar".

## Loading process

In a nutshell, a groupcache lookup of **Get("foo")** looks like:

(On machine #5 of a set of N machines running the same code)

 1. Is the value of "foo" in local memory because it's super hot?  If so, use it.

 2. Is the value of "foo" in local memory because peer #5 (the current
    peer) is the owner of it?  If so, use it.

 3. Amongst all the peers in my set of N, am I the owner of the key
    "foo"?  (e.g. does it consistent hash to 5?)  If so, load it.  If
    other callers come in, via the same process or via RPC requests
    from peers, they block waiting for the load to finish and get the
    same answer.  If not, RPC to the peer that's the owner and get
    the answer.  If the RPC fails, just load it locally (still with
    local dup suppression).

## Example

```go
import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/mailgun/groupcache/v2"
)

func ExampleUsage() {

    // NOTE: It is important to pass the same peer `http://192.168.1.1:8080` to `NewHTTPPoolOpts`
    // which is provided to `pool.Set()` so the pool can identify which of the peers is our instance.
    // The pool will not operate correctly if it can't identify which peer is our instance.
    
    // Pool keeps track of peers in our cluster and identifies which peer owns a key.
    pool := groupcache.NewHTTPPoolOpts("http://192.168.1.1:8080", &groupcache.HTTPPoolOptions{})

    // Add more peers to the cluster You MUST Ensure our instance is included in this list else
    // determining who owns the key accross the cluster will not be consistent, and the pool won't
    // be able to determine if our instance owns the key.
    pool.Set("http://192.168.1.1:8080", "http://192.168.1.2:8080", "http://192.168.1.3:8080")

    server := http.Server{
        Addr:    "192.168.1.1:8080",
        Handler: pool,
    }

    // Start a HTTP server to listen for peer requests from the groupcache
    go func() {
        log.Printf("Serving....\n")
        if err := server.ListenAndServe(); err != nil {
            log.Fatal(err)
        }
    }()
    defer server.Shutdown(context.Background())

    // Create a new group cache with a max cache size of 3MB
    group := groupcache.NewGroup("users", 3000000, groupcache.GetterFunc(
        func(ctx context.Context, id string, dest groupcache.Sink) error {

            // Returns a protobuf struct `User`
            user, err := fetchUserFromMongo(ctx, id)
            if err != nil {
                return err
            }

            // Set the user in the groupcache to expire after 5 minutes
            return dest.SetProto(&user, time.Now().Add(time.Minute*5))
        },
    ))

    var user User

    ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*500)
    defer cancel()

    if err := group.Get(ctx, "12345", groupcache.ProtoSink(&user)); err != nil {
        log.Fatal(err)
    }

    fmt.Printf("-- User --\n")
    fmt.Printf("Id: %s\n", user.Id)
    fmt.Printf("Name: %s\n", user.Name)
    fmt.Printf("Age: %d\n", user.Age)
    fmt.Printf("IsSuper: %t\n", user.IsSuper)

    // Remove the key from the groupcache
    if err := group.Remove(ctx, "12345"); err != nil {
        log.Fatal(err)
    }
}

```
### Note
The call to `groupcache.NewHTTPPoolOpts()` is a bit misleading. `NewHTTPPoolOpts()` creates a new pool internally within the `groupcache` package where it is uitilized by any groups created. The `pool` returned is only a pointer to the internallly registered pool so the caller can update the peers in the pool as needed.
