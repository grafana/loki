package cache

import "context"

type tiered []Cache

// NewTiered makes a new tiered cache.
func NewTiered(caches []Cache) Cache {
	if len(caches) == 1 {
		return caches[0]
	}

	return tiered(caches)
}

func (t tiered) Store(ctx context.Context, keys []string, bufs [][]byte) {
	for _, c := range []Cache(t) {
		c.Store(ctx, keys, bufs)
	}
}

func (t tiered) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string) {
	found := make(map[string][]byte, len(keys))
	missing := keys
	previousCaches := make([]Cache, 0, len(t))

	for _, c := range []Cache(t) {
		var (
			passKeys []string
			passBufs [][]byte
		)

		passKeys, passBufs, missing = c.Fetch(ctx, missing)
		tiered(previousCaches).Store(ctx, passKeys, passBufs)

		for i, key := range passKeys {
			found[key] = passBufs[i]
		}

		if len(missing) == 0 {
			break
		}

		previousCaches = append(previousCaches, c)
	}

	resultKeys := make([]string, 0, len(found))
	resultBufs := make([][]byte, 0, len(found))
	for _, key := range keys {
		if buf, ok := found[key]; ok {
			resultKeys = append(resultKeys, key)
			resultBufs = append(resultBufs, buf)
		}
	}

	return resultKeys, resultBufs, missing
}

func (t tiered) Stop() error {
	for _, c := range []Cache(t) {
		if err := c.Stop(); err != nil {
			return err
		}
	}
	return nil
}
