package encoding

import (
	"context"
	"fmt"
	"io"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type bucketDecoder struct {
	bucket objstore.BucketReader
	path   string
}

// BucketDecoder decodes a data object from the provided path within the
// specified [objstore.BucketReader].
func BucketDecoder(bucket objstore.BucketReader, path string) Decoder {
	// TODO(rfratto): bucketDecoder and readSeekerDecoder can be deduplicated
	// into a single implementation that accepts some kind of "range reader"
	// interface.
	return &bucketDecoder{
		bucket: bucket,
		path:   path,
	}
}

func (bd *bucketDecoder) Sections(ctx context.Context) ([]*filemd.SectionInfo, error) {
	tailer, err := bd.tailer(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	rc, err := bd.bucket.GetRange(ctx, bd.path, int64(tailer.FileSize-tailer.MetadataSize-8), int64(tailer.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeFileMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Sections, nil
}

type tailer struct {
	MetadataSize uint32
	FileSize     uint32
}

func (bd *bucketDecoder) tailer(ctx context.Context) (tailer, error) {
	attrs, err := bd.bucket.Attributes(ctx, bd.path)
	if err != nil {
		return tailer{}, fmt.Errorf("reading attributes: %w", err)
	}

	// Read the last 8 bytes of the object to get the metadata size and magic.
	rc, err := bd.bucket.GetRange(ctx, bd.path, attrs.Size-8, 8)
	if err != nil {
		return tailer{}, fmt.Errorf("getting file tailer: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	metadataSize, err := decodeTailer(br)
	if err != nil {
		return tailer{}, fmt.Errorf("scanning tailer: %w", err)
	}

	return tailer{
		MetadataSize: metadataSize,
		FileSize:     uint32(attrs.Size),
	}, nil
}

func (bd *bucketDecoder) StreamsDecoder() StreamsDecoder {
	return &bucketStreamsDecoder{
		bucket: bd.bucket,
		path:   bd.path,
	}
}

func (bd *bucketDecoder) LogsDecoder() LogsDecoder {
	return &bucketLogsDecoder{
		bucket: bd.bucket,
		path:   bd.path,
	}
}

type bucketStreamsDecoder struct {
	bucket objstore.BucketReader
	path   string
}

func (bd *bucketStreamsDecoder) Columns(ctx context.Context, section *filemd.SectionInfo) ([]*streamsmd.ColumnDesc, error) {
	if section.Type != filemd.SECTION_TYPE_STREAMS {
		return nil, fmt.Errorf("unexpected section type: got=%d want=%d", section.Type, filemd.SECTION_TYPE_STREAMS)
	}

	rc, err := bd.bucket.GetRange(ctx, bd.path, int64(section.MetadataOffset), int64(section.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("reading streams section metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeStreamsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (bd *bucketStreamsDecoder) Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc] {
	getPages := func(ctx context.Context, column *streamsmd.ColumnDesc) ([]*streamsmd.PageDesc, error) {
		rc, err := bd.bucket.GetRange(ctx, bd.path, int64(column.Info.MetadataOffset), int64(column.Info.MetadataSize))
		if err != nil {
			return nil, fmt.Errorf("reading column metadata: %w", err)
		}
		defer rc.Close()

		br, release := getBufioReader(rc)
		defer release()

		md, err := decodeStreamsColumnMetadata(br)
		if err != nil {
			return nil, err
		}
		return md.Pages, nil
	}

	// TODO(rfratto): this retrieves all pages for all columns individually; we
	// may be able to batch requests to minimize roundtrips.
	return result.Iter(func(yield func([]*streamsmd.PageDesc) bool) error {
		for _, column := range columns {
			pages, err := getPages(ctx, column)
			if err != nil {
				return err
			} else if !yield(pages) {
				return nil
			}
		}

		return nil
	})
}

func (bd *bucketStreamsDecoder) ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(ctx context.Context, page *streamsmd.PageDesc) (dataset.PageData, error) {
		rc, err := bd.bucket.GetRange(ctx, bd.path, int64(page.Info.DataOffset), int64(page.Info.DataSize))
		if err != nil {
			return nil, fmt.Errorf("reading page data: %w", err)
		}
		defer rc.Close()

		br, release := getBufioReader(rc)
		defer release()

		data := make([]byte, page.Info.DataSize)
		if _, err := io.ReadFull(br, data); err != nil {
			return nil, fmt.Errorf("read page data: %w", err)
		}
		return dataset.PageData(data), nil
	}

	// TODO(rfratto): this retrieves all pages for all columns individually; we
	// may be able to batch requests to minimize roundtrips.
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		for _, page := range pages {
			data, err := getPageData(ctx, page)
			if err != nil {
				return err
			} else if !yield(data) {
				return nil
			}
		}

		return nil
	})
}

type bucketLogsDecoder struct {
	bucket objstore.BucketReader
	path   string
}

func (bd *bucketLogsDecoder) Columns(ctx context.Context, section *filemd.SectionInfo) ([]*logsmd.ColumnDesc, error) {
	if section.Type != filemd.SECTION_TYPE_LOGS {
		return nil, fmt.Errorf("unexpected section type: got=%d want=%d", section.Type, filemd.SECTION_TYPE_STREAMS)
	}

	rc, err := bd.bucket.GetRange(ctx, bd.path, int64(section.MetadataOffset), int64(section.MetadataSize))
	if err != nil {
		return nil, fmt.Errorf("reading streams section metadata: %w", err)
	}
	defer rc.Close()

	br, release := getBufioReader(rc)
	defer release()

	md, err := decodeLogsMetadata(br)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (bd *bucketLogsDecoder) Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc] {
	getPages := func(ctx context.Context, column *logsmd.ColumnDesc) ([]*logsmd.PageDesc, error) {
		rc, err := bd.bucket.GetRange(ctx, bd.path, int64(column.Info.MetadataOffset), int64(column.Info.MetadataSize))
		if err != nil {
			return nil, fmt.Errorf("reading column metadata: %w", err)
		}
		defer rc.Close()

		br, release := getBufioReader(rc)
		defer release()

		md, err := decodeLogsColumnMetadata(br)
		if err != nil {
			return nil, err
		}
		return md.Pages, nil
	}

	// TODO(rfratto): this retrieves all pages for all columns individually; we
	// may be able to batch requests to minimize roundtrips.
	return result.Iter(func(yield func([]*logsmd.PageDesc) bool) error {
		for _, column := range columns {
			pages, err := getPages(ctx, column)
			if err != nil {
				return err
			} else if !yield(pages) {
				return nil
			}
		}

		return nil
	})
}

func (bd *bucketLogsDecoder) ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(ctx context.Context, page *logsmd.PageDesc) (dataset.PageData, error) {
		rc, err := bd.bucket.GetRange(ctx, bd.path, int64(page.Info.DataOffset), int64(page.Info.DataSize))
		if err != nil {
			return nil, fmt.Errorf("reading page data: %w", err)
		}
		defer rc.Close()

		br, release := getBufioReader(rc)
		defer release()

		data := make([]byte, page.Info.DataSize)
		if _, err := io.ReadFull(br, data); err != nil {
			return nil, fmt.Errorf("read page data: %w", err)
		}
		return dataset.PageData(data), nil
	}

	// TODO(rfratto): this retrieves all pages for all columns individually; we
	// may be able to batch requests to minimize roundtrips.
	return result.Iter(func(yield func(dataset.PageData) bool) error {
		for _, page := range pages {
			data, err := getPageData(ctx, page)
			if err != nil {
				return err
			} else if !yield(data) {
				return nil
			}
		}

		return nil
	})
}
