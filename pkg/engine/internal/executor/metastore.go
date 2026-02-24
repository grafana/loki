package executor

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type metastorePipeline struct {
	reader    metastore.ArrowRecordBatchReader
	logger    log.Logger
	bytesRead int64
}

func (m *metastorePipeline) Open(ctx context.Context) error {
	return m.reader.Open(ctx)
}

func (m *metastorePipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	rec, err := m.reader.Read(ctx)
	err = translateEOF(err, true)
	if errors.Is(err, EOF) {
		level.Debug(m.logger).Log("msg", "pointersscan exhausted", "bytes_read", m.bytesRead)
		return rec, err
	}
	if rec != nil {
		m.bytesRead += RecordSizeBytes(rec)
	}
	return rec, err
}

func (m *metastorePipeline) Close() {
	m.reader.Close()
}

var _ Pipeline = (*metastorePipeline)(nil)

type scanPointersOptions struct {
	metastore metastore.Metastore
	req       metastore.SectionsRequest
	location  string
	logger    log.Logger
}

func newScanPointersPipeline(ctx context.Context, opts scanPointersOptions) (*metastorePipeline, error) {
	resp, err := opts.metastore.IndexSectionsReader(ctx, metastore.IndexSectionsReaderRequest{
		IndexPath:       opts.location,
		SectionsRequest: opts.req,
	})
	if err != nil {
		return nil, translateEOF(err, true)
	}

	return &metastorePipeline{reader: resp.Reader, logger: opts.logger}, nil
}
