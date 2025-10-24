package push

import (
	"sync"

	"github.com/prometheus/otlptranslator"
)

type labelNameCache struct {
	namer otlptranslator.LabelNamer
	mu    sync.RWMutex
	cache map[string]string
	errs  map[string]error
}

func newLabelNameCache() *labelNameCache {
	return &labelNameCache{
		cache: make(map[string]string, 64),
		errs:  make(map[string]error, 4),
	}
}

func (c *labelNameCache) Build(name string) (string, error) {
	c.mu.RLock()
	if result, ok := c.cache[name]; ok {
		c.mu.RUnlock()
		return result, nil
	}
	if err, ok := c.errs[name]; ok {
		c.mu.RUnlock()
		return "", err
	}
	c.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()

	if result, ok := c.cache[name]; ok {
		return result, nil
	}
	if err, ok := c.errs[name]; ok {
		return "", err
	}

	result, err := c.namer.Build(name)
	if err != nil {
		c.errs[name] = err
		return "", err
	}

	c.cache[name] = result
	return result, nil
}
