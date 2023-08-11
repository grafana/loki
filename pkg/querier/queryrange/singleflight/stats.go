package singleflight

import "github.com/golang/groupcache"

func (c *Group) Name() string {
	return c.cache.Name()
}

func (c *Group) Gets() int64 {
	return c.cache.Stats.Gets.Get()
}

func (c *Group) CacheHits() int64 {
	return c.cache.Stats.CacheHits.Get()
}

func (c *Group) GetFromPeersLatencyLower() int64 {
	// Not implemted in original groupcache
	return 0
}

func (c *Group) PeerLoads() int64 {
	return c.cache.Stats.PeerLoads.Get()
}

func (c *Group) PeerErrors() int64 {
	return c.cache.Stats.PeerErrors.Get()
}

func (c *Group) Loads() int64 {
	return c.cache.Stats.Loads.Get()
}

func (c *Group) LoadsDeduped() int64 {
	return c.cache.Stats.LoadsDeduped.Get()
}

func (c *Group) LocalLoads() int64 {
	return c.cache.Stats.LocalLoads.Get()
}

func (c *Group) LocalLoadErrs() int64 {
	return c.cache.Stats.LocalLoadErrs.Get()
}

func (c *Group) ServerRequests() int64 {
	return c.cache.Stats.ServerRequests.Get()
}

func (c *Group) MainCacheItems() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Items
}

func (c *Group) MainCacheBytes() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Bytes
}

func (c *Group) MainCacheGets() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Gets
}

func (c *Group) MainCacheHits() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Hits
}

func (c *Group) MainCacheEvictions() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Evictions
}

func (c *Group) HotCacheItems() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Items
}

func (c *Group) HotCacheBytes() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Bytes
}

func (c *Group) HotCacheGets() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Gets
}

func (c *Group) HotCacheHits() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Hits
}

func (c *Group) HotCacheEvictions() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Evictions
}
