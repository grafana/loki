package cache

func Flush(c Cache) {
	b := c.(*backgroundCache)
	close(b.bgWrites)
	b.wg.Wait()
}

func QueueSize(c Cache) int64 {
	b := c.(*backgroundCache)
	return b.size.Load()
}
