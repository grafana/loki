package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/dustin/go-humanize"
	"github.com/grafana/loki/v3/pkg/dataobj"
)

func main() {
	flag.Parse()

	for _, f := range flag.Args() {
		printFile(f)
	}
}

func printFile(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}
	defer func() { _ = f.Close() }()

	objBytes, err := io.ReadAll(f)
	if err != nil {
		log.Printf("failed to read file: %v", err)
		return
	}

	reader := dataobj.Decoder(bytes.NewReader(objBytes))

	sections, err := reader.Sections(context.Background())
	if err != nil {
		log.Printf("failed to read sections: %v", err)
		return
	}

	for _, section := range sections {
		switch int(section.Type) {
		case 2:
			fmt.Println("---- Logs Section ----")
			dec := reader.LogsDecoder()
			cols, err := dec.Columns(context.Background(), section)
			if err != nil {
				log.Printf("failed to read columns for section %s: %v", section.Type.String(), err)
				continue
			}
			totalCompressedSize := uint64(0)
			totalUncompressedSize := uint64(0)
			for _, col := range cols {
				totalCompressedSize += uint64(col.Info.CompressedSize)
				totalUncompressedSize += uint64(col.Info.UncompressedSize)
				fmt.Printf("%v[%v]; %d populated rows; %v compressed (%v); %v uncompressed\n", col.Type.String()[12:], col.Info.Name, col.Info.ValuesCount, humanize.Bytes(uint64(col.Info.CompressedSize)), col.Info.Compression.String()[17:], humanize.Bytes(uint64(col.Info.UncompressedSize)))
			}
			fmt.Println("")
			fmt.Printf("Compressed size: %v; Uncompressed size %v\n", humanize.Bytes(totalCompressedSize), humanize.Bytes(totalUncompressedSize))
			fmt.Println("---- /Logs Section ----")
		case 1:
			fmt.Println("---- Streams Section ----")
			dec := reader.StreamsDecoder()
			cols, err := dec.Columns(context.Background(), section)
			if err != nil {
				log.Printf("failed to read columns for section %s: %v", section.Type.String(), err)
				continue
			}
			totalCompressedSize := uint64(0)
			totalUncompressedSize := uint64(0)
			for _, col := range cols {
				totalCompressedSize += uint64(col.Info.CompressedSize)
				totalUncompressedSize += uint64(col.Info.UncompressedSize)
				fmt.Printf("%v[%v]; %d populated rows; %v compressed (%v); %v uncompressed\n", col.Type.String()[12:], col.Info.Name, col.Info.ValuesCount, humanize.Bytes(uint64(col.Info.CompressedSize)), col.Info.Compression.String()[17:], humanize.Bytes(uint64(col.Info.UncompressedSize)))
			}
			fmt.Println("")
			fmt.Printf("Compressed size: %v; Uncompressed size %v\n", humanize.Bytes(totalCompressedSize), humanize.Bytes(totalUncompressedSize))
			fmt.Println("---- /Streams Section ----")
		}
	}
}
