package tsdb

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"

	lokitsdb "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func OpenAndInspectIndexes(paths []string) ([]IndexReaderResult, error) {
	results := make([]IndexReaderResult, 0, len(paths))
	for _, path := range paths {
		result, err := openAndInspectIndex(path)
		if err != nil {
			// Close any already-opened index handles before returning.
			for _, r := range results {
				if r.Index != nil {
					_ = r.Index.Close()
				}
			}
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func openAndInspectIndex(path string) (result IndexReaderResult, err error) {
	reader, err := tsdbindex.NewFileReader(path)
	if err != nil {
		return IndexReaderResult{}, fmt.Errorf("open tsdb index %q: %w", path, err)
	}

	defer func() {
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close tsdb index %q: %w", path, closeErr)
		}
	}()

	from, through := reader.Bounds()
	labelNames, err := reader.LabelNames()
	if err != nil {
		return IndexReaderResult{}, fmt.Errorf("read label names from %q: %w", path, err)
	}

	copiedNames := make([]string, len(labelNames))
	for i, name := range labelNames {
		copiedNames[i] = strings.Clone(name)
	}

	// Full index handle for structural discovery (ForSeries traversal).
	// This stays open — the caller must close it via result.Index.Close().
	idx, _, err := lokitsdb.NewTSDBIndexFromFile(path)
	if err != nil {
		return IndexReaderResult{}, fmt.Errorf("open TSDB index for discovery %q: %w", path, err)
	}

	return IndexReaderResult{
		LocalPath: path,
		Bounds: [2]model.Time{
			model.Time(from),
			model.Time(through),
		},
		Version:    reader.Version(),
		LabelNames: copiedNames,
		Index:      idx,
	}, nil
}
