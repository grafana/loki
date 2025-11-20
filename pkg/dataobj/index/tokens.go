package index

import (
	"context"
	"fmt"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/ngrams"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

type tokenCalculation struct {
	tokens map[string]struct{}
}

func (c *tokenCalculation) Prepare(ctx context.Context, section *dataobj.Section) error {
	c.tokens = make(map[string]struct{})
	return nil
}

func (c *tokenCalculation) ProcessBatch(ctx context.Context, builder *indexobj.Builder, batch []logs.Record) error {
	for _, record := range batch {
		tokens := ngrams.Tokenize([]byte(ngrams.Sanitize(string(record.Line))))
		for _, token := range tokens {
			c.tokens[token] = struct{}{}
		}
	}
	return nil
}

func (c *tokenCalculation) Finish(ctx context.Context, tenantID string, path string, section int64, builder *indexobj.Builder) error {
	bloom := bloom.NewWithEstimates(uint(len(c.tokens)), 1.0/128.0)
	for token := range c.tokens {
		bloom.AddString(token)
	}
	bloomBytes, err := bloom.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal bloom filter: %w", err)
	}
	fmt.Printf("Found %d unique tokens for bloom of %s bytes (tenant: %s, path: %s, section: %d)\n", len(c.tokens), humanize.Bytes(uint64(len(bloomBytes))), tenantID, path, section)
	err = builder.AppendSectionIndex(tenantID, path, section, 0, bloomBytes)
	if err != nil {
		return fmt.Errorf("failed to append column index: %w", err)
	}
	return nil
}
