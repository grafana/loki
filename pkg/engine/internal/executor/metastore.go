package executor

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/prometheus/prometheus/model/labels"
)

type metastorePipeline struct {
	reader metastore.ArrowRecordBatchReader
	region *xcap.Region
}

func (m *metastorePipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := m.reader.Read(xcap.ContextWithRegion(ctx, m.region))
	// metastore reader returns io.EOF that we translate to executor.EOF
	return rec, translateEOF(err, true)
}

func (m *metastorePipeline) Close() {
	if m.region != nil {
		m.region.End()
	}
	m.reader.Close()
}

var _ Pipeline = (*metastorePipeline)(nil)

type scanPointersOptions struct {
	metastore metastore.Metastore
	region    *xcap.Region

	location   string
	start, end time.Time
	matchers   []*labels.Matcher
}

func newScanPointersPipeline(ctx context.Context, opts scanPointersOptions) (*metastorePipeline, error) {
	reader, err := opts.metastore.ScanPointers(ctx, opts.location, opts.start, opts.end, opts.matchers)
	if err != nil {
		return nil, translateEOF(err, true)
	}

	return &metastorePipeline{reader, opts.region}, nil
}
