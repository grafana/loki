package executor

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/xcap"
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
	req       metastore.SectionsRequest
	region    *xcap.Region

	location string
}

func newScanPointersPipeline(ctx context.Context, opts scanPointersOptions) (*metastorePipeline, error) {
	resp, err := opts.metastore.IndexSectionsReader(ctx, metastore.IndexSectionsReaderRequest{
		IndexPath:       opts.location,
		Region:          opts.region,
		SectionsRequest: opts.req,
	})
	if err != nil {
		return nil, translateEOF(err, true)
	}

	return &metastorePipeline{resp.Reader, opts.region}, nil
}
