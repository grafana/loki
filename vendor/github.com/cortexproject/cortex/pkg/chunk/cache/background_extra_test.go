package cache

func Flush(c Cache) {
	b := c.(*backgroundCache)
	close(b.bgWrites)
	b.wg.Wait()
}
