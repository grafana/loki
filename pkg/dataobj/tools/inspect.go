package tools

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
)

func Inspect(dataobj io.ReaderAt, size int64) {
	reader := encoding.ReaderAtDecoder(dataobj, size)

	metadata, err := reader.Metadata(context.Background())
	if err != nil {
		log.Printf("failed to read sections: %v", err)
		return
	}

	for _, section := range metadata.Sections {
		sectionReader := reader.SectionReader(metadata, section)

		typ, err := sectionReader.Type()
		if err != nil {
			log.Printf("failed to get section type: %s", err)
			continue
		}

		switch typ {
		case encoding.SectionTypeLogs:
			logsDec, _ := logs.NewDecoder(sectionReader)
			printLogsInfo(reader, logsDec)
		case encoding.SectionTypeStreams:
			streamsDec, _ := streams.NewDecoder(sectionReader)
			printStreamInfo(reader, streamsDec)
		}
	}
}

func printStreamInfo(reader encoding.Decoder, dec *streams.Decoder) {
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

func printLogsInfo(reader encoding.Decoder, dec *logs.Decoder) {
	fmt.Println("---- Logs Section ----")
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
