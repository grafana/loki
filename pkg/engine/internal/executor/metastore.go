package executor

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type metastorePipeline struct {
	reader metastore.ArrowRecordBatchReader
}

func (m *metastorePipeline) Open(ctx context.Context) error {
	return m.reader.Open(ctx)
}

func (m *metastorePipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := m.reader.Read(ctx)
	// metastore reader returns io.EOF that we translate to executor.EOF
	return rec, translateEOF(err, true)
}

func (m *metastorePipeline) Close() {
	m.reader.Close()
}

var _ Pipeline = (*metastorePipeline)(nil)

type scanPointersOptions struct {
	metastore metastore.Metastore
	req       metastore.SectionsRequest

	location      string
	prefetchBytes int64
}

func newScanPointersPipeline(ctx context.Context, opts scanPointersOptions) (*metastorePipeline, error) {
	resp, err := opts.metastore.IndexSectionsReader(ctx, metastore.IndexSectionsReaderRequest{
		IndexPath:       opts.location,
		SectionsRequest: opts.req,
		PrefetchBytes:   opts.prefetchBytes,
	})
	if err != nil {
		return nil, translateEOF(err, true)
	}

	return &metastorePipeline{resp.Reader}, nil
}
