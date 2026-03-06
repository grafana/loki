package tsdb

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"

	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func OpenAndInspectIndexes(paths []string) ([]IndexReaderResult, error) {
	results := make([]IndexReaderResult, 0, len(paths))

	for _, path := range paths {
		result, err := openAndInspectIndex(path)
		if err != nil {
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

	return IndexReaderResult{
		LocalPath: path,
		Bounds: [2]model.Time{
			model.Time(from),
			model.Time(through),
		},
		Version:    reader.Version(),
		LabelNames: copiedNames,
	}, nil
}
