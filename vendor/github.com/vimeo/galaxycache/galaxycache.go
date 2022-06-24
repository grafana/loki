/*
Copyright 2012 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package galaxycache provides a data loading mechanism with caching
// and de-duplication that works across a set of peer processes.
//
// Each data Get first consults its local cache, otherwise delegates
// to the requested key's canonical owner, which then checks its cache
// or finally gets the data.  In the common case, many concurrent
// cache misses across a set of peers for the same key result in just
// one cache fill.
package galaxycache // import "github.com/vimeo/galaxycache"

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/vimeo/galaxycache/lru"
	"github.com/vimeo/galaxycache/promoter"
	"github.com/vimeo/galaxycache/singleflight"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
)

// A BackendGetter loads data for a key.
type BackendGetter interface {
	// Get populates dest with the value identified by key
	//
	// The returned data must be unversioned. That is, key must
	// uniquely describe the loaded data, without an implicit
	// current time, and without relying on cache expiration
	// mechanisms.
	Get(ctx context.Context, key string, dest Codec) error
}

// A GetterFunc implements BackendGetter with a function.
type GetterFunc func(ctx context.Context, key string, dest Codec) error

// Get implements Get from BackendGetter
func (f GetterFunc) Get(ctx context.Context, key string, dest Codec) error {
	return f(ctx, key, dest)
}

type universeOpts struct {
	hashOpts *HashOptions
	recorder stats.Recorder
}

// UniverseOpt is a functional Universe option.
type UniverseOpt func(*universeOpts)

// WithHashOpts sets the HashOptions on a universe.
func WithHashOpts(hashOpts *HashOptions) UniverseOpt {
	return func(u *universeOpts) {
		u.hashOpts = hashOpts
	}
}

// WithRecorder allows you to override the default stats.Recorder used for
// stats.
func WithRecorder(recorder stats.Recorder) UniverseOpt {
	return func(u *universeOpts) {
		u.recorder = recorder
	}
}

// Universe defines the primary container for all galaxycache operations.
// It contains the galaxies and PeerPicker
type Universe struct {
	mu         sync.RWMutex
	galaxies   map[string]*Galaxy // galaxies are indexed by their name
	peerPicker *PeerPicker
	recorder   stats.Recorder
}

// NewUniverse is the main constructor for the Universe object. It is passed a
// FetchProtocol (to specify fetching via GRPC or HTTP) and its own URL along
// with options.
func NewUniverse(protocol FetchProtocol, selfURL string, opts ...UniverseOpt) *Universe {
	options := &universeOpts{}
	for _, opt := range opts {
		opt(options)
	}

	c := &Universe{
		galaxies:   make(map[string]*Galaxy),
		peerPicker: newPeerPicker(protocol, selfURL, options.hashOpts),
		recorder:   options.recorder,
	}

	return c
}

// NewUniverseWithOpts is a deprecated constructor for the Universe object that
// defines a non-default hash function and number of replicas.  Please use
// `NewUniverse` with the `WithHashOpts` option instead.
func NewUniverseWithOpts(protocol FetchProtocol, selfURL string, options *HashOptions) *Universe {
	return NewUniverse(protocol, selfURL, WithHashOpts(options))
}

// NewGalaxy creates a coordinated galaxy-aware BackendGetter from a
// BackendGetter.
//
// The returned BackendGetter tries (but does not guarantee) to run only one
// Get is called once for a given key across an entire set of peer
// processes. Concurrent callers both in the local process and in
// other processes receive copies of the answer once the original Get
// completes.
//
// The galaxy name must be unique for each BackendGetter.
func (universe *Universe) NewGalaxy(name string, cacheBytes int64, getter BackendGetter, opts ...GalaxyOption) *Galaxy {
	if getter == nil {
		panic("nil Getter")
	}
	if nameErr := isNameValid(name); nameErr != nil {
		panic(fmt.Errorf("Invalid galaxy name: %s", nameErr))
	}

	universe.mu.Lock()
	defer universe.mu.Unlock()

	if _, dup := universe.galaxies[name]; dup {
		panic("duplicate registration of galaxy " + name)
	}

	gOpts := galaxyOpts{
		promoter:      &promoter.DefaultPromoter{},
		hcRatio:       8, // default hotcache size is 1/8th of cacheBytes
		maxCandidates: 100,
	}
	for _, opt := range opts {
		opt.apply(&gOpts)
	}
	g := &Galaxy{
		name:       name,
		getter:     getter,
		peerPicker: universe.peerPicker,
		cacheBytes: cacheBytes,
		mainCache: cache{
			ctype: MainCache,
			lru:   lru.New(0),
		},
		hotCache: cache{
			ctype: HotCache,
			lru:   lru.New(0),
		},
		candidateCache: cache{
			ctype: CandidateCache,
			lru:   lru.New(gOpts.maxCandidates),
		},
		hcStatsWithTime: HCStatsWithTime{
			hcs: &promoter.HCStats{
				HCCapacity: cacheBytes / gOpts.hcRatio,
			}},
		loadGroup: &singleflight.Group{},
		opts:      gOpts,
	}
	g.mainCache.setLRUOnEvicted(nil)
	g.hotCache.setLRUOnEvicted(g.candidateCache.addToCandidateCache)
	g.candidateCache.lru.OnEvicted = func(key lru.Key, value interface{}) {
		g.candidateCache.nevict++
	}
	g.parent = universe

	universe.galaxies[name] = g
	return g
}

func isNameValid(name string) error {
	// check galaxy name is valid for an opencensus tag value
	_, err := tag.New(context.Background(), tag.Insert(GalaxyKey, name))
	return err
}

// GetGalaxy returns the named galaxy previously created with NewGalaxy, or
// nil if there's no such galaxy.
func (universe *Universe) GetGalaxy(name string) *Galaxy {
	universe.mu.RLock()
	defer universe.mu.RUnlock()
	return universe.galaxies[name]
}

// Set updates the Universe's list of peers (contained in the PeerPicker).
// Each PeerURL value should be a valid base URL,
// for example "example.net:8000".
func (universe *Universe) Set(peerURLs ...string) error {
	return universe.peerPicker.set(peerURLs...)
}

// Shutdown closes all open fetcher connections
func (universe *Universe) Shutdown() error {
	return universe.peerPicker.shutdown()
}

// HCStatsWithTime includes a time stamp along with the hotcache stats
// to ensure updates happen no more than once per second
type HCStatsWithTime struct {
	hcs *promoter.HCStats
	t   time.Time
}

// A Galaxy is a cache namespace and associated data spread over
// a group of 1 or more machines.
type Galaxy struct {
	name       string
	getter     BackendGetter
	peerPicker *PeerPicker
	mu         sync.Mutex
	cacheBytes int64 // limit for sum of mainCache and hotCache size

	// mainCache is a cache of the keys for which this process
	// (amongst its peers) is authoritative. That is, this cache
	// contains keys which consistent hash on to this process's
	// peer number.
	mainCache cache

	// hotCache contains keys/values for which this peer is not
	// authoritative (otherwise they would be in mainCache), but
	// are popular enough to warrant mirroring in this process to
	// avoid going over the network to fetch from a peer.  Having
	// a hotCache avoids network hotspotting, where a peer's
	// network card could become the bottleneck on a popular key.
	// This cache is used sparingly to maximize the total number
	// of key/value pairs that can be stored globally.
	hotCache cache

	candidateCache cache

	hcStatsWithTime HCStatsWithTime

	// loadGroup ensures that each key is only fetched once
	// (either locally or remotely), regardless of the number of
	// concurrent callers.
	loadGroup flightGroup

	opts galaxyOpts

	_ int32 // force Stats to be 8-byte aligned on 32-bit platforms

	// Stats are statistics on the galaxy.
	Stats GalaxyStats

	// pointer to the parent universe that created this galaxy
	parent *Universe
}

// GalaxyOption is an interface for implementing functional galaxy options
type GalaxyOption interface {
	apply(*galaxyOpts)
}

// galaxyOpts contains optional fields for the galaxy (each with a default
// value if not set)
type galaxyOpts struct {
	promoter      promoter.Interface
	hcRatio       int64
	maxCandidates int
}

type funcGalaxyOption func(*galaxyOpts)

func (f funcGalaxyOption) apply(g *galaxyOpts) {
	f(g)
}

func newFuncGalaxyOption(f func(*galaxyOpts)) funcGalaxyOption {
	return funcGalaxyOption(f)
}

// WithPromoter allows the client to specify a promoter for the galaxy;
// defaults to a simple QPS comparison
func WithPromoter(p promoter.Interface) GalaxyOption {
	return newFuncGalaxyOption(func(g *galaxyOpts) {
		g.promoter = p
	})
}

// WithHotCacheRatio allows the client to specify a ratio for the
// main-to-hot cache sizes for the galaxy; defaults to 8:1
func WithHotCacheRatio(r int64) GalaxyOption {
	return newFuncGalaxyOption(func(g *galaxyOpts) {
		g.hcRatio = r
	})
}

// WithMaxCandidates allows the client to specify the size of the
// candidate cache by the max number of candidates held at one time;
// defaults to 100
func WithMaxCandidates(n int) GalaxyOption {
	return newFuncGalaxyOption(func(g *galaxyOpts) {
		g.maxCandidates = n
	})
}

// flightGroup is defined as an interface which flightgroup.Group
// satisfies.  We define this so that we may test with an alternate
// implementation.
type flightGroup interface {
	// Done is called when Do is done.
	Do(key string, fn func() (interface{}, error)) (interface{}, error)
}

// GalaxyStats are per-galaxy statistics.
type GalaxyStats struct {
	Gets              AtomicInt // any Get request, including from peers
	Loads             AtomicInt // (gets - cacheHits)
	CoalescedLoads    AtomicInt // inside singleflight
	MaincacheHits     AtomicInt // number of maincache hits
	HotcacheHits      AtomicInt // number of hotcache hits
	PeerLoads         AtomicInt // either remote load or remote cache hit (not an error)
	PeerLoadErrors    AtomicInt // errors on getFromPeer
	BackendLoads      AtomicInt // load from backend locally
	BackendLoadErrors AtomicInt // total bad local loads

	CoalescedMaincacheHits AtomicInt // maincache hit in singleflight
	CoalescedHotcacheHits  AtomicInt // hotcache hit in singleflight
	CoalescedPeerLoads     AtomicInt // peer load in singleflight
	CoalescedBackendLoads  AtomicInt // backend load in singleflight

	ServerRequests AtomicInt // gets that came over the network from peers
}

// Name returns the name of the galaxy.
func (g *Galaxy) Name() string {
	return g.name
}

// hitLevel specifies the level at which data was found on Get
type hitLevel int

const (
	hitHotcache hitLevel = iota + 1
	hitMaincache
	hitPeer
	hitBackend
	miss // for checking cache hit/miss in lookupCache
)

func (h hitLevel) String() string {
	switch h {
	case hitHotcache:
		return "hotcache"
	case hitMaincache:
		return "maincache"
	case hitPeer:
		return "peer"
	case hitBackend:
		return "backend"
	default:
		return ""
	}
}

func (h hitLevel) isHit() bool {
	return h != miss
}

// recordRequest records the corresponding opencensus measurement
// to the level at which data was found on Get/load
func (g *Galaxy) recordRequest(ctx context.Context, h hitLevel, localAuthoritative bool) {
	span := trace.FromContext(ctx)
	span.Annotatef([]trace.Attribute{trace.StringAttribute("hit_level", h.String())}, "fetched from %s", h)
	switch h {
	case hitMaincache:
		g.Stats.MaincacheHits.Add(1)
		g.recordStats(ctx, []tag.Mutator{tag.Upsert(CacheLevelKey, h.String())}, MCacheHits.M(1))
	case hitHotcache:
		g.Stats.HotcacheHits.Add(1)
		g.recordStats(ctx, []tag.Mutator{tag.Upsert(CacheLevelKey, h.String())}, MCacheHits.M(1))
	case hitPeer:
		g.Stats.PeerLoads.Add(1)
		g.recordStats(ctx, nil, MPeerLoads.M(1))
	case hitBackend:
		g.Stats.BackendLoads.Add(1)
		g.recordStats(ctx, nil, MBackendLoads.M(1))
		if !localAuthoritative {
			span.Annotate(nil, "failed to fetch from peer, not authoritative for key")
		}
	}
}

// Get as defined here is the primary "get" called on a galaxy to
// find the value for the given key, using the following logic:
// - First, try the local cache; if its a cache hit, we're done
// - On a cache miss, search for which peer is the owner of the
// key based on the consistent hash
// - If a different peer is the owner, use the corresponding fetcher
// to Fetch from it; otherwise, if the calling instance is the key's
// canonical owner, call the BackendGetter to retrieve the value
// (which will now be cached locally)
func (g *Galaxy) Get(ctx context.Context, key string, dest Codec) error {
	ctx, tagErr := tag.New(ctx, tag.Upsert(GalaxyKey, g.name))
	if tagErr != nil {
		panic(fmt.Errorf("Error tagging context: %s", tagErr))
	}

	ctx, span := trace.StartSpan(ctx, "galaxycache.(*Galaxy).Get on "+g.name)
	startTime := time.Now()
	defer func() {
		g.recordStats(ctx, nil, MRoundtripLatencyMilliseconds.M(sinceInMilliseconds(startTime)))
		span.End()
	}()

	g.Stats.Gets.Add(1)
	g.recordStats(ctx, nil, MGets.M(1))
	if dest == nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeInvalidArgument, Message: "no Codec was provided"})
		return errors.New("galaxycache: no Codec was provided")
	}
	value, hlvl := g.lookupCache(key)
	g.recordStats(ctx, nil, MKeyLength.M(int64(len(key))))

	if hlvl.isHit() {
		span.Annotatef([]trace.Attribute{trace.BoolAttribute("cache_hit", true)}, "Cache hit in %s", hlvl)
		value.stats.touch()
		g.recordRequest(ctx, hlvl, false)
		g.recordStats(ctx, nil, MValueLength.M(int64(len(value.data))))
		return dest.UnmarshalBinary(value.data)
	}

	span.Annotatef([]trace.Attribute{trace.BoolAttribute("cache_hit", false)}, "Cache miss")
	// Optimization to avoid double unmarshalling or copying: keep
	// track of whether the dest was already populated. One caller
	// (if local) will set this; the losers will not. The common
	// case will likely be one caller.
	destPopulated := false
	value, destPopulated, err := g.load(ctx, key, dest)
	if err != nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeUnknown, Message: "Failed to load key: " + err.Error()})
		g.recordStats(ctx, nil, MLoadErrors.M(1))
		return err
	}
	value.stats.touch()
	g.recordStats(ctx, nil, MValueLength.M(int64(len(value.data))))
	if destPopulated {
		return nil
	}
	return dest.UnmarshalBinary(value.data)
}

type valWithLevel struct {
	val                *valWithStat
	level              hitLevel
	localAuthoritative bool
	peerErr            error
	localErr           error
}

// load loads key either by invoking the getter locally or by sending it to another machine.
func (g *Galaxy) load(ctx context.Context, key string, dest Codec) (value *valWithStat, destPopulated bool, err error) {
	g.Stats.Loads.Add(1)
	g.recordStats(ctx, nil, MLoads.M(1))

	viewi, err := g.loadGroup.Do(key, func() (interface{}, error) {
		// Check the cache again because singleflight can only dedup calls
		// that overlap concurrently.  It's possible for 2 concurrent
		// requests to miss the cache, resulting in 2 load() calls.  An
		// unfortunate goroutine scheduling would result in this callback
		// being run twice, serially.  If we don't check the cache again,
		// cache.nbytes would be incremented below even though there will
		// be only one entry for this key.
		//
		// Consider the following serialized event ordering for two
		// goroutines in which this callback gets called twice for the
		// same key:
		// 1: Get("key")
		// 2: Get("key")
		// 1: lookupCache("key")
		// 2: lookupCache("key")
		// 1: load("key")
		// 2: load("key")
		// 1: loadGroup.Do("key", fn)
		// 1: fn()
		// 2: loadGroup.Do("key", fn)
		// 2: fn()
		if value, hlvl := g.lookupCache(key); hlvl.isHit() {
			if hlvl == hitHotcache {
				g.Stats.CoalescedHotcacheHits.Add(1)
			} else {
				g.Stats.CoalescedMaincacheHits.Add(1)
			}
			g.recordStats(ctx, []tag.Mutator{tag.Insert(CacheLevelKey, hlvl.String())}, MCoalescedCacheHits.M(1))
			return &valWithLevel{value, hlvl, false, nil, nil}, nil

		}
		g.Stats.CoalescedLoads.Add(1)
		g.recordStats(ctx, nil, MCoalescedLoads.M(1))

		authoritative := true
		var peerErr error
		if peer, ok := g.peerPicker.pickPeer(key); ok {
			value, peerErr = g.getFromPeer(ctx, peer, key)
			authoritative = false
			if peerErr == nil {
				g.Stats.CoalescedPeerLoads.Add(1)
				g.recordStats(ctx, nil, MCoalescedPeerLoads.M(1))
				return &valWithLevel{value, hitPeer, false, nil, nil}, nil
			}

			g.Stats.PeerLoadErrors.Add(1)
			g.recordStats(ctx, nil, MPeerLoadErrors.M(1))
			// TODO(bradfitz): log the peer's error? keep
			// log of the past few for /galaxycache?  It's
			// probably boring (normal task movement), so not
			// worth logging I imagine.
		}
		data, err := g.getLocally(ctx, key, dest)
		if err != nil {
			g.Stats.BackendLoadErrors.Add(1)
			g.recordStats(ctx, nil, MBackendLoadErrors.M(1))
			return nil, err
		}

		g.Stats.CoalescedBackendLoads.Add(1)
		g.recordStats(ctx, nil, MCoalescedBackendLoads.M(1))
		destPopulated = true // only one caller of load gets this return value
		value = newValWithStat(data, nil)
		g.populateCache(ctx, key, value, &g.mainCache)
		return &valWithLevel{value, hitBackend, authoritative, peerErr, err}, nil
	})
	if err == nil {
		value = viewi.(*valWithLevel).val
		level := viewi.(*valWithLevel).level
		authoritative := viewi.(*valWithLevel).localAuthoritative
		g.recordRequest(ctx, level, authoritative) // record the hits for all load calls, including those that tagged onto the singleflight
	}
	return
}

func (g *Galaxy) getLocally(ctx context.Context, key string, dest Codec) ([]byte, error) {
	err := g.getter.Get(ctx, key, dest)
	if err != nil {
		return nil, err
	}
	return dest.MarshalBinary()
}

func (g *Galaxy) getFromPeer(ctx context.Context, peer RemoteFetcher, key string) (*valWithStat, error) {
	data, err := peer.Fetch(ctx, g.name, key)
	if err != nil {
		return nil, err
	}
	vi, ok := g.candidateCache.get(key)
	if !ok {
		vi = g.addNewToCandidateCache(key)
	}

	g.maybeUpdateHotCacheStats() // will update if at least a second has passed since the last update

	kStats := vi.(*keyStats)
	stats := promoter.Stats{
		KeyQPS:  kStats.val(),
		HCStats: g.hcStatsWithTime.hcs,
	}
	value := newValWithStat(data, kStats)
	if g.opts.promoter.ShouldPromote(key, value.data, stats) {
		g.populateCache(ctx, key, value, &g.hotCache)
	}
	return value, nil
}

func (g *Galaxy) lookupCache(key string) (*valWithStat, hitLevel) {
	if g.cacheBytes <= 0 {
		return nil, miss
	}
	vi, ok := g.mainCache.get(key)
	if ok {
		return vi.(*valWithStat), hitMaincache
	}
	vi, ok = g.hotCache.get(key)
	if !ok {
		return nil, miss
	}
	g.Stats.HotcacheHits.Add(1)
	return vi.(*valWithStat), hitHotcache
}

func (g *Galaxy) populateCache(ctx context.Context, key string, value *valWithStat, cache *cache) {
	if g.cacheBytes <= 0 {
		return
	}
	cache.add(key, value)
	// Record the size of this cache after we've finished evicting any necessary values.
	defer func() {
		g.recordStats(ctx, []tag.Mutator{tag.Upsert(CacheTypeKey, cache.ctype.String())},
			MCacheSize.M(cache.bytes()), MCacheEntries.M(cache.items()))
	}()

	// Evict items from cache(s) if necessary.
	for {
		mainBytes := g.mainCache.bytes()
		hotBytes := g.hotCache.bytes()
		if mainBytes+hotBytes <= g.cacheBytes {
			return
		}

		// TODO(bradfitz): this is good-enough-for-now logic.
		// It should be something based on measurements and/or
		// respecting the costs of different resources.
		victim := &g.mainCache
		if hotBytes > mainBytes/g.opts.hcRatio {
			victim = &g.hotCache
		}
		victim.removeOldest()
	}
}

func (g *Galaxy) recordStats(ctx context.Context, mutators []tag.Mutator, measurements ...stats.Measurement) {
	stats.RecordWithOptions(
		ctx,
		stats.WithMeasurements(measurements...),
		stats.WithTags(mutators...),
		stats.WithRecorder(g.parent.recorder),
	)
}

// CacheType represents a type of cache.
type CacheType uint8

const (
	// MainCache is the cache for items that this peer is the
	// owner of.
	MainCache CacheType = iota + 1

	// HotCache is the cache for items that seem popular
	// enough to replicate to this node, even though it's not the
	// owner.
	HotCache

	// CandidateCache is the cache for peer-owned keys that
	// may become popular enough to put in the HotCache
	CandidateCache
)

// CacheStats returns stats about the provided cache within the galaxy.
func (g *Galaxy) CacheStats(which CacheType) CacheStats {
	switch which {
	case MainCache:
		return g.mainCache.stats()
	case HotCache:
		return g.hotCache.stats()
	case CandidateCache:
		return g.candidateCache.stats()
	default:
		return CacheStats{}
	}
}

// cache is a wrapper around an *lru.Cache that adds synchronization
// and counts the size of all keys and values. Candidate cache only
// utilizes the lru.Cache and mutex, not the included stats.
type cache struct {
	mu         sync.Mutex
	nbytes     int64 // of all keys and values
	lru        *lru.Cache
	nhit, nget int64
	nevict     int64 // number of evictions
	ctype      CacheType
}

func (c *cache) stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{
		Bytes:     c.nbytes,
		Items:     c.itemsLocked(),
		Gets:      c.nget,
		Hits:      c.nhit,
		Evictions: c.nevict,
	}
}

type valWithStat struct {
	data  []byte
	stats *keyStats
}

// sizeOfValWithStats returns the total size of the value in the hot/main
// cache, including the data, key stats, and a pointer to the val itself
func (v *valWithStat) size() int64 {
	// using cap() instead of len() for data leads to inconsistency
	// after unmarshaling/marshaling the data
	return int64(unsafe.Sizeof(*v.stats)) + int64(len(v.data)) + int64(unsafe.Sizeof(v)) + int64(unsafe.Sizeof(*v))
}

func (c *cache) setLRUOnEvicted(f func(key string, kStats *keyStats)) {
	c.lru.OnEvicted = func(key lru.Key, value interface{}) {
		val := value.(*valWithStat)
		c.nbytes -= int64(len(key.(string))) + val.size()
		c.nevict++
		if f != nil {
			f(key.(string), val.stats)
		}
	}
}

func (c *cache) add(key string, value *valWithStat) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Add(key, value)
	c.nbytes += int64(len(key)) + value.size()
}

func (c *cache) get(key string) (vi interface{}, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nget++
	if c.lru == nil {
		return
	}
	vi, ok = c.lru.Get(key)
	if !ok {
		return
	}
	c.nhit++
	return vi, true
}

func (c *cache) removeOldest() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru != nil {
		c.lru.RemoveOldest()
	}

}

func (c *cache) bytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nbytes
}

func (c *cache) items() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.itemsLocked()
}

func (c *cache) itemsLocked() int64 {
	if c.lru == nil {
		return 0
	}
	return int64(c.lru.Len())
}

// An AtomicInt is an int64 to be accessed atomically.
type AtomicInt int64

// Add atomically adds n to i.
func (i *AtomicInt) Add(n int64) {
	atomic.AddInt64((*int64)(i), n)
}

// Get atomically gets the value of i.
func (i *AtomicInt) Get() int64 {
	return atomic.LoadInt64((*int64)(i))
}

func (i *AtomicInt) String() string {
	return strconv.FormatInt(i.Get(), 10)
}

// CacheStats are returned by stats accessors on Galaxy.
type CacheStats struct {
	Bytes     int64
	Items     int64
	Gets      int64
	Hits      int64
	Evictions int64
}

//go:generate stringer -type=CacheType
