package index

import (
	"context"
	"fmt"

	"github.com/bits-and-blooms/bloom/v3"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/ngrams"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

const (
	alphabetSize     = 26 /* a-z */ + 10 /* 0-9 */ + 1 /* _ */ + 1 /* - */ + 1 /* . */ + 1 /* ( */ + 1 /* ) */ + 1 /*  */
	alphabetSizePow3 = alphabetSize * alphabetSize * alphabetSize
	alphabetSizePow4 = alphabetSize * alphabetSize * alphabetSize * alphabetSize
)

type ngramCalculation struct {
	bloom         *bloom.BloomFilter
	rowsPerBloom  int
	rowsProcessed int
	pendingBlooms [][]byte
	ngrams        map[string]struct{}
}

func (c *ngramCalculation) Prepare(ctx context.Context, section *dataobj.Section) error {
	c.bloom = bloom.NewWithEstimates(uint(alphabetSizePow4), 1.0/128.0)
	c.rowsPerBloom = 100000
	c.ngrams = make(map[string]struct{})
	return nil
}

func (c *ngramCalculation) ProcessBatch(ctx context.Context, builder *indexobj.Builder, batch []logs.Record) error {
	for _, record := range batch {
		c.rowsProcessed++
		/* 		record.Metadata.Range(func(md labels.Label) {
			addTrigrams(c.bloom, ngrams.Sanitize(md.Name), c.ngrams)
			addTrigrams(c.bloom, ngrams.Sanitize(md.Value), c.ngrams)
		}) */
		addTrigrams(c.bloom, ngrams.Sanitize(string(record.Line)), c.ngrams)
	}

	return nil
}

func (c *ngramCalculation) Finish(ctx context.Context, tenantID string, path string, section int64, builder *indexobj.Builder) error {
	bloomBytes, err := c.bloom.MarshalBinary()
	if err != nil {
		return fmt.Errorf("failed to marshal bloom filter: %w", err)
	}
	fmt.Printf("Found %d unique ngrams. %.2f%% of possible 4grams processed\n", len(c.ngrams), float64(len(c.ngrams))/float64(alphabetSizePow4)*100)
	c.pendingBlooms = append(c.pendingBlooms, bloomBytes)

	fmt.Printf("Appended text blooms: %d\n", len(c.pendingBlooms))
	for _, pendingBloom := range c.pendingBlooms {
		err = builder.AppendSectionIndex(tenantID, path, section, 0, pendingBloom)
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}
	}
	return nil
}

func addTrigrams(bloom *bloom.BloomFilter, s string, uniqs map[string]struct{}) {
	for val := range ngrams.FourgramOf(s) {
		bloom.AddString(val)
		if _, ok := uniqs[val]; !ok {
			uniqs[val] = struct{}{}
		}
	}
}
