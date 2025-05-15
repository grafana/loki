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

	metadata, err := reader.Metadata(context.Background())
	if err != nil {
		log.Printf("failed to read sections: %v", err)
		return
	}

	for _, section := range metadata.Sections {
		typ, err := encoding.GetSectionType(metadata, section)
		if err != nil {
			log.Printf("failed to get section type: %s", err)
			continue
		}

		switch typ {
		case encoding.SectionTypeLogs:
			printLogsInfo(reader, metadata, section)
		case encoding.SectionTypeStreams:
			printStreamInfo(reader, metadata, section)
		}
	}
}

func printStreamInfo(reader encoding.Decoder, metadata *filemd.Metadata, section *filemd.SectionInfo) {
	dec := reader.StreamsDecoder(metadata, section)
	fmt.Println("---- Streams Section ----")
	cols, err := dec.Columns(context.Background())
	if err != nil {
		log.Printf("failed to read columns for streams section: %v", err)
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

func printLogsInfo(reader encoding.Decoder, metadata *filemd.Metadata, section *filemd.SectionInfo) {
	fmt.Println("---- Logs Section ----")
	dec := reader.LogsDecoder(metadata, section)
	cols, err := dec.Columns(context.Background())
	if err != nil {
		log.Printf("failed to read columns for logs section: %v", err)
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
