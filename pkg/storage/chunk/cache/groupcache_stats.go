package cache

import "github.com/mailgun/groupcache/v2"

func (c *group) Name() string {
	return c.cache.Name()
}

func (c *group) Gets() int64 {
	return c.cache.Stats.Gets.Get()
}

func (c *group) CacheHits() int64 {
	return c.cache.Stats.CacheHits.Get()
}

func (c *group) GetFromPeersLatencyLower() int64 {
	return c.cache.Stats.GetFromPeersLatencyLower.Get()
}

func (c *group) PeerLoads() int64 {
	return c.cache.Stats.PeerLoads.Get()
}

func (c *group) PeerErrors() int64 {
	return c.cache.Stats.PeerErrors.Get()
}

func (c *group) Loads() int64 {
	return c.cache.Stats.Loads.Get()
}

func (c *group) LoadsDeduped() int64 {
	return c.cache.Stats.LoadsDeduped.Get()
}

func (c *group) LocalLoads() int64 {
	return c.cache.Stats.LocalLoads.Get()
}

func (c *group) LocalLoadErrs() int64 {
	return c.cache.Stats.LocalLoadErrs.Get()
}

func (c *group) ServerRequests() int64 {
	return c.cache.Stats.ServerRequests.Get()
}

func (c *group) MainCacheItems() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Items
}

func (c *group) MainCacheBytes() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Bytes
}

func (c *group) MainCacheGets() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Gets
}

func (c *group) MainCacheHits() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Hits
}

func (c *group) MainCacheEvictions() int64 {
	return c.cache.CacheStats(groupcache.MainCache).Evictions
}

func (c *group) HotCacheItems() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Items
}

func (c *group) HotCacheBytes() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Bytes
}

func (c *group) HotCacheGets() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Gets
}

func (c *group) HotCacheHits() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Hits
}

func (c *group) HotCacheEvictions() int64 {
	return c.cache.CacheStats(groupcache.HotCache).Evictions
}
