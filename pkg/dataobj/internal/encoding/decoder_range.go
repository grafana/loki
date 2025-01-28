package encoding

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// rangeReader is an interface that can read a range of bytes from an object.
type rangeReader interface {
	// Size returns the full size of the object.
	Size(ctx context.Context) (int64, error)

	// ReadRange returns a reader over a range of bytes. Callers may create
	// multiple current instance of ReadRange.
	ReadRange(ctx context.Context, offset int64, length int64) (io.ReadCloser, error)
}

type rangeDecoder struct {
	r rangeReader
}

func (rd *rangeDecoder) Sections(ctx context.Context) ([]*filemd.SectionInfo, error) {
	tailer, err := rd.tailer(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading tailer: %w", err)
	}

	rc, err := rd.r.ReadRange(ctx, int64(tailer.FileSize-tailer.MetadataSize-8), int64(tailer.MetadataSize))
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
	MetadataSize uint64
	FileSize     uint64
}

func (rd *rangeDecoder) tailer(ctx context.Context) (tailer, error) {
	size, err := rd.r.Size(ctx)
	if err != nil {
		return tailer{}, fmt.Errorf("reading attributes: %w", err)
	}

	// Read the last 8 bytes of the object to get the metadata size and magic.
	rc, err := rd.r.ReadRange(ctx, size-8, 8)
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
		MetadataSize: uint64(metadataSize),
		FileSize:     uint64(size),
	}, nil
}

func (rd *rangeDecoder) StreamsDecoder() StreamsDecoder {
	return &rangeStreamsDecoder{rr: rd.r}
}

func (rd *rangeDecoder) LogsDecoder() LogsDecoder {
	return &rangeLogsDecoder{rr: rd.r}
}

type rangeStreamsDecoder struct {
	rr rangeReader
}

func (rd *rangeStreamsDecoder) Columns(ctx context.Context, section *filemd.SectionInfo) ([]*streamsmd.ColumnDesc, error) {
	if got, want := section.Type, filemd.SECTION_TYPE_STREAMS; got != want {
		return nil, fmt.Errorf("unexpected section type: got=%s want=%s", got, want)
	}

	rc, err := rd.rr.ReadRange(ctx, int64(section.MetadataOffset), int64(section.MetadataSize))
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

func (rd *rangeStreamsDecoder) Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc] {
	getPages := func(ctx context.Context, column *streamsmd.ColumnDesc) ([]*streamsmd.PageDesc, error) {
		rc, err := rd.rr.ReadRange(ctx, int64(column.Info.MetadataOffset), int64(column.Info.MetadataSize))
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

func (rd *rangeStreamsDecoder) ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(ctx context.Context, page *streamsmd.PageDesc) (dataset.PageData, error) {
		rc, err := rd.rr.ReadRange(ctx, int64(page.Info.DataOffset), int64(page.Info.DataSize))
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

type rangeLogsDecoder struct {
	rr rangeReader
}

func (rd *rangeLogsDecoder) Columns(ctx context.Context, section *filemd.SectionInfo) ([]*logsmd.ColumnDesc, error) {
	if got, want := section.Type, filemd.SECTION_TYPE_LOGS; got != want {
		return nil, fmt.Errorf("unexpected section type: got=%s want=%s", got, want)
	}
	rc, err := rd.rr.ReadRange(ctx, int64(section.MetadataOffset), int64(section.MetadataSize))
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

func (rd *rangeLogsDecoder) Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc] {
	getPages := func(ctx context.Context, column *logsmd.ColumnDesc) ([]*logsmd.PageDesc, error) {
		rc, err := rd.rr.ReadRange(ctx, int64(column.Info.MetadataOffset), int64(column.Info.MetadataSize))
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

func (rd *rangeLogsDecoder) ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(ctx context.Context, page *logsmd.PageDesc) (dataset.PageData, error) {
		rc, err := rd.rr.ReadRange(ctx, int64(page.Info.DataOffset), int64(page.Info.DataSize))
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
