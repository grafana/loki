package v1

import (
	"fmt"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"

	"github.com/grafana/loki/pkg/push"
)

type StructuredMetadataTokenizer struct {
	// prefix to add to tokens, typically the encoded chunkref
	prefix string
	tokens []string
}

func NewStructuredMetadataTokenizer(prefix string) *StructuredMetadataTokenizer {
	return &StructuredMetadataTokenizer{
		prefix: prefix,
		tokens: make([]string, 6),
	}
}

func (t *StructuredMetadataTokenizer) Tokens(kv push.LabelAdapter) iter.Iterator[string] {
	combined := fmt.Sprintf("%s=%s", kv.Name, kv.Value)
	t.tokens = append(t.tokens[:0],
		kv.Name, t.prefix+kv.Name,
		combined, t.prefix+combined,
	)
	return iter.NewSliceIter(t.tokens)
}
