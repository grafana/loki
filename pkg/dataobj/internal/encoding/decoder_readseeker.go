package encoding

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type readSeekerDecoder struct {
	rs io.ReadSeeker
}

// ReadSeekerDecoder decodes a data object from the provided [io.ReadSeeker].
func ReadSeekerDecoder(rs io.ReadSeeker) Decoder {
	return &readSeekerDecoder{rs: rs}
}

func (dec *readSeekerDecoder) Sections(_ context.Context) ([]*filemd.SectionInfo, error) {
	var metadataSize uint32
	if _, err := dec.rs.Seek(-8, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("seek to file metadata size: %w", err)
	} else if err := binary.Read(dec.rs, binary.LittleEndian, &metadataSize); err != nil {
		return nil, fmt.Errorf("reading file metadata size: %w", err)
	}

	if _, err := dec.rs.Seek(-int64(metadataSize)-8, io.SeekEnd); err != nil {
		return nil, fmt.Errorf("seek to file metadata: %w", err)
	}

	r := bufio.NewReader(io.LimitReader(dec.rs, int64(metadataSize)))

	md, err := decodeFileMetadata(r)
	if err != nil {
		return nil, err
	}
	return md.Sections, nil
}

func (dec *readSeekerDecoder) StreamsDecoder() StreamsDecoder {
	return &readSeekerStreamsDecoder{rs: dec.rs}
}

func (dec *readSeekerDecoder) LogsDecoder() LogsDecoder {
	return &readSeekerLogsDecoder{rs: dec.rs}
}

type readSeekerStreamsDecoder struct {
	rs io.ReadSeeker
}

func (dec *readSeekerStreamsDecoder) Columns(_ context.Context, section *filemd.SectionInfo) ([]*streamsmd.ColumnDesc, error) {
	if section.Type != filemd.SECTION_TYPE_STREAMS {
		return nil, fmt.Errorf("unexpected section type: got=%d want=%d", section.Type, filemd.SECTION_TYPE_STREAMS)
	}

	if _, err := dec.rs.Seek(int64(section.MetadataOffset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to streams metadata: %w", err)
	}
	r := bufio.NewReader(io.LimitReader(dec.rs, int64(section.MetadataSize)))

	md, err := decodeStreamsMetadata(r)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (dec *readSeekerStreamsDecoder) Pages(ctx context.Context, columns []*streamsmd.ColumnDesc) result.Seq[[]*streamsmd.PageDesc] {
	getPages := func(_ context.Context, column *streamsmd.ColumnDesc) ([]*streamsmd.PageDesc, error) {
		if _, err := dec.rs.Seek(int64(column.Info.MetadataOffset), io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek to column metadata: %w", err)
		}
		r := bufio.NewReader(io.LimitReader(dec.rs, int64(column.Info.MetadataSize)))

		md, err := decodeStreamsColumnMetadata(r)
		if err != nil {
			return nil, err
		}
		return md.Pages, nil
	}

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

func (dec *readSeekerStreamsDecoder) ReadPages(ctx context.Context, pages []*streamsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(_ context.Context, page *streamsmd.PageDesc) (dataset.PageData, error) {
		if _, err := dec.rs.Seek(int64(page.Info.DataOffset), io.SeekStart); err != nil {
			return nil, err
		}
		data := make([]byte, page.Info.DataSize)
		if _, err := io.ReadFull(dec.rs, data); err != nil {
			return nil, fmt.Errorf("read page data: %w", err)
		}
		return dataset.PageData(data), nil
	}

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

type readSeekerLogsDecoder struct {
	rs io.ReadSeeker
}

func (dec *readSeekerLogsDecoder) Columns(_ context.Context, section *filemd.SectionInfo) ([]*logsmd.ColumnDesc, error) {
	if section.Type != filemd.SECTION_TYPE_LOGS {
		return nil, fmt.Errorf("unexpected section type: got=%d want=%d", section.Type, filemd.SECTION_TYPE_LOGS)
	}

	if _, err := dec.rs.Seek(int64(section.MetadataOffset), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek to streams metadata: %w", err)
	}
	r := bufio.NewReader(io.LimitReader(dec.rs, int64(section.MetadataSize)))

	md, err := decodeLogsMetadata(r)
	if err != nil {
		return nil, err
	}
	return md.Columns, nil
}

func (dec *readSeekerLogsDecoder) Pages(ctx context.Context, columns []*logsmd.ColumnDesc) result.Seq[[]*logsmd.PageDesc] {
	getPages := func(_ context.Context, column *logsmd.ColumnDesc) ([]*logsmd.PageDesc, error) {
		if _, err := dec.rs.Seek(int64(column.Info.MetadataOffset), io.SeekStart); err != nil {
			return nil, fmt.Errorf("seek to column metadata: %w", err)
		}
		r := bufio.NewReader(io.LimitReader(dec.rs, int64(column.Info.MetadataSize)))

		md, err := decodeLogsColumnMetadata(r)
		if err != nil {
			return nil, err
		}
		return md.Pages, nil
	}

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

func (dec *readSeekerLogsDecoder) ReadPages(ctx context.Context, pages []*logsmd.PageDesc) result.Seq[dataset.PageData] {
	getPageData := func(_ context.Context, page *logsmd.PageDesc) (dataset.PageData, error) {
		if _, err := dec.rs.Seek(int64(page.Info.DataOffset), io.SeekStart); err != nil {
			return nil, err
		}
		data := make([]byte, page.Info.DataSize)
		if _, err := io.ReadFull(dec.rs, data); err != nil {
			return nil, fmt.Errorf("read page data: %w", err)
		}
		return dataset.PageData(data), nil
	}

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
