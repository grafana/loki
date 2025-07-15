package debug

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
)

func ReaderAt(reader io.ReaderAt, prefix string) io.ReaderAt {
	return &ioReaderAt{
		reader: reader,
		prefix: prefix,
	}
}

type ioReaderAt struct {
	reader io.ReaderAt
	prefix string
}

func (d *ioReaderAt) ReadAt(b []byte, off int64) (int, error) {
	n, err := d.reader.ReadAt(b, off)
	fmt.Printf("%s: Read(%d) @%d => %d %v \n%s\n", d.prefix, len(b), off, n, err, hex.Dump(b[:n]))
	return n, err
}

func Reader(reader io.Reader, prefix string) io.Reader {
	return &ioReader{
		reader: reader,
		prefix: prefix,
	}
}

type ioReader struct {
	reader io.Reader
	prefix string
	offset int64
}

func (d *ioReader) Read(b []byte) (int, error) {
	n, err := d.reader.Read(b)
	fmt.Printf("%s: Read(%d) @%d => %d %v \n%s\n", d.prefix, len(b), d.offset, n, err, hex.Dump(b[:n]))
	d.offset += int64(n)
	return n, err
}

func Writer(writer io.Writer, prefix string) io.Writer {
	return &ioWriter{
		writer: writer,
		prefix: prefix,
	}
}

type ioWriter struct {
	writer io.Writer
	prefix string
	offset int64
}

func (d *ioWriter) Write(b []byte) (int, error) {
	n, err := d.writer.Write(b)
	fmt.Printf("%s: Write(%d) @%d => %d %v \n  %q\n", d.prefix, len(b), d.offset, n, err, b[:n])
	d.offset += int64(n)
	return n, err
}

var (
	TRACEBUF int
)

func init() {
	for _, arg := range strings.Split(os.Getenv("PARQUETGODEBUG"), ",") {
		k := arg
		v := ""
		i := strings.IndexByte(arg, '=')
		if i >= 0 {
			k, v = arg[:i], arg[i+1:]
		}
		var err error
		switch k {
		case "":
			// ignore empty entries
		case "tracebuf":
			if TRACEBUF, err = strconv.Atoi(v); err != nil {
				log.Printf("PARQUETGODEBUG: invalid value for tracebuf: %q", v)
			}
		default:
			log.Printf("PARQUETGODEBUG: unrecognized debug option: %q", k)
		}
	}
}
