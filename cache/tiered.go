package cache

import "context"

type tiered []Cache

// NewTiered makes a new tiered cache.
func NewTiered(caches []Cache) Cache {
	return tiered(caches)
}

func (t tiered) StoreChunk(ctx context.Context, key string, buf []byte) error {
	for _, c := range []Cache(t) {
		if err := c.StoreChunk(ctx, key, buf); err != nil {
			return err
		}
	}
	return nil
}

func (t tiered) FetchChunkData(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	found := make(map[string][]byte, len(keys))
	missing := keys
	previousCaches := make([]Cache, 0, len(t))

	for _, c := range []Cache(t) {
		var (
			err      error
			passKeys []string
			passBufs [][]byte
		)

		passKeys, passBufs, missing, err = c.FetchChunkData(ctx, missing)
		if err != nil {
			return nil, nil, nil, err
		}

		for i, key := range passKeys {
			found[key] = passBufs[i]
			tiered(previousCaches).StoreChunk(ctx, key, passBufs[i])
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

	return resultKeys, resultBufs, missing, nil
}

func (t tiered) Stop() error {
	for _, c := range []Cache(t) {
		if err := c.Stop(); err != nil {
			return err
		}
	}
	return nil
}
