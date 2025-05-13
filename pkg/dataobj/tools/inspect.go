package tools

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

func Inspect(dataobj io.ReaderAt, size int64) {
	reader := encoding.ReaderAtDecoder(dataobj, size)

	sections, err := reader.Sections(context.Background())
	if err != nil {
		log.Printf("failed to read sections: %v", err)
		return
	}

	for _, section := range sections {
		switch section.Kind {
		case filemd.SECTION_KIND_LOGS:
			printLogsInfo(reader, section)
		case filemd.SECTION_KIND_STREAMS:
			printStreamInfo(reader, section)
		}
	}
}

func printStreamInfo(reader encoding.Decoder, section *filemd.SectionInfo) {
	if section.Kind != filemd.SECTION_KIND_STREAMS {
		log.Printf("Input section is a %v, expected streams section\n", section.Kind)
		return
	}

	dec := reader.StreamsDecoder(section)
	fmt.Println("---- Streams Section ----")
	cols, err := dec.Columns(context.Background())
	if err != nil {
		log.Printf("failed to read columns for section %s: %v", section.Kind.String(), err)
		return
	}
	totalCompressedSize := uint64(0)
	totalUncompressedSize := uint64(0)
	for _, col := range cols {
		totalCompressedSize += col.Info.CompressedSize
		totalUncompressedSize += col.Info.UncompressedSize
		fmt.Printf("%v[%v]; %d populated rows; %v compressed (%v); %v uncompressed\n", col.Type.String()[12:], col.Info.Name, col.Info.ValuesCount, humanize.Bytes(col.Info.CompressedSize), col.Info.Compression.String()[17:], humanize.Bytes(col.Info.UncompressedSize))
	}
	fmt.Println("")
	fmt.Printf("Streams Section Summary: %d columns; compressed size: %v; uncompressed size %v\n", len(cols), humanize.Bytes(totalCompressedSize), humanize.Bytes(totalUncompressedSize))
	fmt.Println("")
}

func printLogsInfo(reader encoding.Decoder, section *filemd.SectionInfo) {
	if section.Kind != filemd.SECTION_KIND_LOGS {
		log.Printf("Input section is a %v, expected logs section\n", section.Kind)
		return
	}

	fmt.Println("---- Logs Section ----")
	dec := reader.LogsDecoder(section)
	cols, err := dec.Columns(context.Background())
	if err != nil {
		log.Printf("failed to read columns for section %s: %v", section.Kind.String(), err)
		return
	}
	totalCompressedSize := uint64(0)
	totalUncompressedSize := uint64(0)
	for _, col := range cols {
		totalCompressedSize += col.Info.CompressedSize
		totalUncompressedSize += col.Info.UncompressedSize
		fmt.Printf("%v[%v]; %d populated rows; %v compressed (%v); %v uncompressed\n", col.Type.String()[12:], col.Info.Name, col.Info.ValuesCount, humanize.Bytes(col.Info.CompressedSize), col.Info.Compression.String()[17:], humanize.Bytes(col.Info.UncompressedSize))
	}
	fmt.Println("")
	fmt.Printf("Logs Section Summary: %d columns; compressed size: %v; uncompressed size %v\n", len(cols), humanize.Bytes(totalCompressedSize), humanize.Bytes(totalUncompressedSize))
	fmt.Println("")
}
