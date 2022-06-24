# galaxycache

[![Build Status](https://travis-ci.org/vimeo/galaxycache.svg?branch=master)](https://travis-ci.org/vimeo/galaxycache)

galaxycache is a caching and cache-filling library, adapted from groupcache, intended as a
replacement for memcached in many cases.

For API docs and examples, see http://godoc.org/github.com/vimeo/galaxycache

## Quick Start

### Initializing a peer
```go
// Generate the protocol for this peer to Fetch from others with (package includes HTTP and gRPC)
grpcProto := NewGRPCFetchProtocol(grpc.WithInsecure())

// HTTP protocol as an alternative (passing the nil argument ensures use of the default basepath
// and opencensus Transport as an http.RoundTripper)
httpProto := NewHTTPFetchProtocol(nil)

// Create a new Universe with the chosen peer connection protocol and the URL of this process
u := NewUniverse(grpcProto, "my-url")

// Set the Universe's list of peer addresses for the distributed cache
u.Set("peer1-url", "peer2-url", "peer3-url")

// Define a BackendGetter (here as a function) for retrieving data
getter := GetterFunc(func(ctx context.Context, key string, dest Codec) error {
   // Define your method for retrieving non-cached data here, i.e. from a database
})

// Create a new Galaxy within the Universe with a name, the max capacity of cache space you would
// like to allocate, and your BackendGetter
g := u.NewGalaxy("galaxy-1", 1 << 20, getter)

// In order to receive Fetch requests from peers over HTTP or gRPC, we must register this universe
// to handle those requests

// gRPC Server registration (note: you must create the server with an ocgrpc.ServerHandler for
// opencensus metrics to propogate properly)
grpcServer := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{}))
RegisterGRPCServer(u, grpcServer)

// HTTP Handler registration (passing nil for the second argument will ensure use of the default 
// basepath, passing nil for the third argument will ensure use of the DefaultServeMux wrapped 
// by opencensus)
RegisterHTTPHandler(u, nil, nil)

// Refer to the http/grpc godocs for information on how to serve using the registered HTTP handler
// or gRPC server, respectively:
// HTTP: https://golang.org/pkg/net/http/#Handler
// gRPC: https://godoc.org/google.golang.org/grpc#Server

```
### Getting a value
```go
// Create a Codec for unmarshaling data into your format of choice - the package includes 
// implementations for []byte and string formats, and the protocodec subpackage includes the 
// protobuf adapter
sCodec := StringCodec{}

// Call Get on the Galaxy to retrieve data and unmarshal it into your Codec
ctx := context.Background()
err := g.Get(ctx, "my-key", &sCodec)
if err != nil {
   // handle if Get returns an error
}

// Shutdown all open connections between peers before killing the process
u.Shutdown()

```

## Concepts and Glossary

### Consistent hash determines authority

A consistent hashing algorithm determines the sharding of keys across peers in galaxycache. Further reading can be found [here](https://medium.com/@orijtech/groupcache-instrumented-by-opencensus-6a625c3724c) and [here](https://www.toptal.com/big-data/consistent-hashing).

### Universe 

To keep galaxycache instances non-global (i.e. for multithreaded testing), a [`Universe`] object contains all of the moving parts of the cache, including the logic for connecting to peers, consistent hashing, and maintaining the set of galaxies.

### Galaxy

A [`Galaxy`] is a grouping of keys based on a category determined by the user. For example, you might have a galaxy for Users and a galaxy for Video Metadata; those data types may require different fetching protocols on the backend -- separating them into different [`Galaxies`](https://godoc.org/github.com/vimeo/galaxycache#Galaxy) allows for this flexibility.

Each [`Galaxy`] contains its own cache space. The cache is immutable; all cache population and eviction is handled by internal logic.

### Maincache vs Hotcache

The cache within each galaxy is divided into a "maincache" and a "hotcache".

The "maincache" contains data that the local process is authoritative over. The maincache is always populated whenever data is fetched from the backend (with a LRU eviction policy). 

In order to eliminate network hops, a portion of the cache space in each process is reserved for especially popular keys that the local process is not authoritative over. By default, this "hotcache" is populated by a key and its associated data by means of a requests-per-second metric. The logic for hotcache promotion can be configured by implementing a custom solution with the [`ShouldPromote.Interface`].

## Step-by-Step Breakdown of a Get()

![galaxycache Caching Example Diagram](/diagram.png)

When [`Get`] is called for a key in a [`Galaxy`] in some process called Process_A:
1. The local cache (both maincache and hotcache) in Process_A is checked first
2. On a cache miss, the [`PeerPicker`] object delegates to the peer authoritative over the requested key
3. Depends on which peer is authoritative over this key...
- If the Process_A is the authority:
   - Process_A uses its [`BackendGetter`] to get the data, and populates its local maincache
- If Process_A is _not_ the authority:
   - Process_A calls `Fetch` on the authoritative remote peer, Process_B (method determined by [`FetchProtocol`])
   - Process_B then performs a [`Get`] to either find the data from its own local cache or use the specified [`BackendGetter`] to get the data from elsewhere, such as by querying a database
   - Process_B populates its maincache with the data before serving it back to Process_A
   - Process_A determines whether the key is hot enough to promote to the hotcache
      - If it is, then the hotcache for Process_A is populated with the key/data
4. The data is unmarshaled into the [`Codec`] passed into [`Get`]

## Changes from groupcache

Our changes include the following:
* Overhauled API to improve usability and configurability
* Improvements to testing by removing global state
* Improvement to connection efficiency between peers with the addition of [gRPC](https://godoc.org/github.com/vimeo/galaxycache/grpc)
* Added a [`Promoter.Interface`](https://godoc.org/github.com/vimeo/galaxycache/promoter#Interface) for choosing which keys get hotcached
* Made some core functionality more generic (e.g. replaced the `Sink` object with a [`Codec`] marshaler interface, removed `Byteview`)

### New architecture and API

* Renamed `Group` type to [`Galaxy`], `Getter` to [`BackendGetter`], `Get` to `Fetch` (for newly named [`RemoteFetcher`] interface, previously called `ProtoGetter`)
* Reworked [`PeerPicker`] interface into a struct; contains a [`FetchProtocol`] and [`RemoteFetchers`](https://godoc.org/github.com/vimeo/galaxycache#RemoteFetcher) (generalizing for HTTP and GRPC fetching implementations), a hash map of other peer addresses, and a self URL

### No more global state

* Removed all global variables to allow for multithreaded testing by implementing a `Universe` container that holds the [`Galaxies`] (previously a global `groups` map) and [`PeerPicker`] (part of what used to be `HTTPPool`)
* Added methods to [`Universe`] to allow for simpler handling of most galaxycache operations (setting Peers, instantiating a Picker, etc)

### New structure for fetching from peers (with gRPC support)

* Added an [`HTTPHandler`](https://godoc.org/github.com/vimeo/galaxycache/http#HTTPHandler) and associated [registration function](https://godoc.org/github.com/vimeo/galaxycache/http#RegisterHTTPHandler) for serving HTTP requests by reaching into an associated [`Universe`] (deals with the other function of the deprecated `HTTPPool`)
* Added [gRPC support](https://godoc.org/github.com/vimeo/galaxycache/grpc) for peer communication, with a function for [gRPC server registration](https://godoc.org/github.com/vimeo/galaxycache/grpc#RegisterGRPCServer)
* Reworked tests to fit new architecture
* Renamed files to match new type names

### A smarter Hotcache with configurable promotion logic

* New default promotion logic uses key access statistics tied to every key to make decisions about populating the hotcache
* Promoter package provides a [`ShouldPromote.Interface`] for creating your own method to determine whether a key should be added to the hotcache
* Newly added candidate cache keeps track of peer-owned keys (without associated data) that have not yet been promoted to the hotcache
* Provided variadic options for [`Galaxy`] construction to override default promotion logic (with your promoter, max number of candidates, and relative hotcache size to maincache)


## Comparison to memcached

See: https://github.com/golang/groupcache/blob/master/README.md

## Help

Use the golang-nuts mailing list for any discussion or questions.

[`Universe`]:https://godoc.org/github.com/vimeo/galaxycache#Universe
[`Galaxy`]:https://godoc.org/github.com/vimeo/galaxycache#Galaxy
[`Get`]:https://godoc.org/github.com/vimeo/galaxycache#Galaxy.Get
[`PeerPicker`]:https://godoc.org/github.com/vimeo/galaxycache#PeerPicker
[`Codec`]:https://godoc.org/github.com/vimeo/galaxycache#Codec
[`FetchProtocol`]:https://godoc.org/github.com/vimeo/galaxycache#FetchProtocol
[`RemoteFetcher`]:https://godoc.org/github.com/vimeo/galaxycache#RemoteFetcher
[`BackendGetter`]:https://godoc.org/github.com/vimeo/galaxycache#BackendGetter
[`ShouldPromote.Interface`]:https://godoc.org/github.com/vimeo/galaxycache/promoter#Interface

