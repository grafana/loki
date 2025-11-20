package index

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/ngrams"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/mhr3/streamvbyte"
)

type postingsCalculation struct {
	postings      map[string]*roaring.Bitmap
	rowsProcessed int
}

var alphabetRunes []rune

func init() {
	alphabet := "abcdefghijklmnopqrstuvwxyz0123456789_-.() {}"
	alphabetRunes = []rune(alphabet)
	slices.Sort(alphabetRunes)
}

func (c *postingsCalculation) Prepare(ctx context.Context, section *dataobj.Section) error {
	c.postings = make(map[string]*roaring.Bitmap)
	return nil
}

func (c *postingsCalculation) ProcessBatch(ctx context.Context, builder *indexobj.Builder, batch []logs.Record) error {
	for _, record := range batch {
		tokens := ngrams.BigramOf(ngrams.Sanitize(string(record.Line)))
		page := uint32(c.rowsProcessed / 1)
		for token := range tokens {
			if _, ok := c.postings[token]; !ok {
				c.postings[token] = roaring.NewBitmap()
			}
			c.postings[token].Add(page)
		}
		c.rowsProcessed++
	}
	return nil
}

func (c *postingsCalculation) Finish(ctx context.Context, tenantID string, path string, section int64, mtx *sync.Mutex, builder *indexobj.Builder) error {
	mtx.Lock()
	defer mtx.Unlock()
	bytesSum := 0
	deltaBytesSum := 0
	for key, posting := range c.postings {
		posting.RunOptimize()

		postingFrozen, err := posting.Freeze()
		if err != nil {
			return fmt.Errorf("failed to freeze posting: %w", err)
		}
		bytesSum += len(postingFrozen)

		encoded, err := tryDeltaEncode(posting)
		if err != nil {
			return fmt.Errorf("failed to delta encode posting: %w", err)
		}
		deltaBytesSum += len(encoded)

		idx := ngrams.NgramIndex(key)
		err = builder.AppendSectionIndex(tenantID, path, section, idx, encoded)
		if err != nil {
			return fmt.Errorf("failed to append section index: %w", err)
		}
	}
	fmt.Printf("Appended %d postings (total size: %d bytes, delta encoded size: %d bytes)\n", len(c.postings), bytesSum, deltaBytesSum)
	return nil
}

func tryDeltaEncode(posting *roaring.Bitmap) ([]byte, error) {
	it := posting.Iterator()
	ints := make([]uint32, 0, posting.GetCardinality())
	for it.HasNext() {
		ints = append(ints, it.Next())
	}
	slices.Sort(ints)
	encoded := streamvbyte.DeltaEncodeUint32(ints, nil)
	buf := bytes.NewBuffer(nil)
	count := streamio.WriteUvarint(buf, uint64(len(ints)))
	if count != nil {
		return nil, count
	}
	_, err := buf.Write(encoded)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
