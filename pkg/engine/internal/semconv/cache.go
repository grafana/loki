package semconv

// IdentifierCache caches Identifier objects for a given FQN string to
// reduce memory allocations. IdentifierCache is not thread-safe.
type IdentifierCache struct {
	cache map[string]cacheRecord
}

type cacheRecord struct {
	ident *Identifier
	err   error
}

func NewIdentifierCache() *IdentifierCache {
	return &IdentifierCache{
		cache: make(map[string]cacheRecord),
	}
}

func (c *IdentifierCache) ParseFQN(fqn string) (*Identifier, error) {
	rec, ok := c.cache[fqn]
	if !ok {
		ident, err := ParseFQN(fqn)
		rec = cacheRecord{
			ident: ident,
			err:   err,
		}
		c.cache[fqn] = rec
	}

	return rec.ident, rec.err
}
