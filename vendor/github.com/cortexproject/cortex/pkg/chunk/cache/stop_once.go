package cache

import "sync"

type stopOnce struct {
	once sync.Once
	Cache
}

// StopOnce wraps a Cache and ensures its only stopped once.
func StopOnce(cache Cache) Cache {
	return &stopOnce{
		Cache: cache,
	}
}

func (s *stopOnce) Stop() error {
	var err error
	s.once.Do(func() {
		err = s.Cache.Stop()
	})
	return err
}
