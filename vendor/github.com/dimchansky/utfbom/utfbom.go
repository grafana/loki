// Package utfbom implements the detection of the BOM (Unicode Byte Order Mark) and removing as necessary.
// It wraps an io.Reader object, creating another object (Reader) that also implements the io.Reader
// interface but provides automatic BOM checking and removing as necessary.
package utfbom

import (
	"errors"
	"io"
)

var _ io.Reader = (*Reader)(nil)

// Encoding is a UTF encoding identified by a BOM.
type Encoding int

// Constants to identify detected UTF encodings.
const (
	// Unknown encoding, returned when no BOM was detected
	Unknown Encoding = iota

	// UTF8, BOM bytes: EF BB BF
	UTF8

	// UTF-16, big-endian, BOM bytes: FE FF
	UTF16BigEndian

	// UTF-16, little-endian, BOM bytes: FF FE
	UTF16LittleEndian

	// UTF-32, big-endian, BOM bytes: 00 00 FE FF
	UTF32BigEndian

	// UTF-32, little-endian, BOM bytes: FF FE 00 00
	UTF32LittleEndian
)

// String returns a user-friendly string representation of the encoding. Satisfies fmt.Stringer interface.
func (e Encoding) String() string {
	switch e {
	case UTF8:
		return "UTF8"
	case UTF16BigEndian:
		return "UTF16BigEndian"
	case UTF16LittleEndian:
		return "UTF16LittleEndian"
	case UTF32BigEndian:
		return "UTF32BigEndian"
	case UTF32LittleEndian:
		return "UTF32LittleEndian"
	default:
		return "Unknown"
	}
}

const maxConsecutiveEmptyReads = 100

// Skip returns a Reader that reads from rd without its leading BOM (Unicode Byte Order Mark),
// and the encoding indicated by that BOM, or Unknown if no supported BOM was detected.
//
// Skip reads up to 4 bytes from rd before it returns, so it may block. An error
// that occurs while reading them is not returned by Skip: the returned Reader's Read
// returns it once, after the bytes that remain after BOM removal.
//
// If rd is already a *Reader, Skip returns rd itself and the encoding it detected,
// without reading from it. Use SkipOnly if the encoding is not needed.
func Skip(rd io.Reader) (*Reader, Encoding) {
	// Is it already a Reader?
	b, ok := rd.(*Reader)
	if ok {
		return b, b.enc
	}

	enc, left, err := detectUtf(rd)
	return &Reader{
		rd:  rd,
		buf: left,
		err: err,
		enc: enc,
	}, enc
}

// SkipOnly is like Skip but does not return the encoding. It also reads from rd
// immediately and may block, and an error from that read is returned later by Read.
func SkipOnly(rd io.Reader) *Reader {
	r, _ := Skip(rd)
	return r
}

// Reader is an io.Reader that returns the bytes of the underlying reader
// without the leading BOM (Unicode Byte Order Mark). Create it with Skip or SkipOnly.
// Reader does not close the underlying reader.
type Reader struct {
	rd  io.Reader // reader provided by the client
	buf []byte    // buffered data
	err error     // last error
	enc Encoding  // encoding
}

// Read implements io.Reader. The BOM is detected once, by Skip or SkipOnly:
// Read returns the bytes read past the BOM during detection, then any error
// hit during detection (once), then reads from the underlying reader directly.
func (r *Reader) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	if r.buf == nil {
		if r.err != nil {
			return 0, r.readErr()
		}

		return r.rd.Read(p)
	}

	// copy as much as we can
	n = copy(p, r.buf)
	r.buf = nilIfEmpty(r.buf[n:])
	return n, nil
}

func (r *Reader) readErr() error {
	err := r.err
	r.err = nil
	return err
}

var errNegativeRead = errors.New("utfbom: reader returned negative count from Read")

func detectUtf(rd io.Reader) (enc Encoding, buf []byte, err error) {
	buf, err = readBOM(rd)

	if len(buf) >= 4 {
		if isUTF32BigEndianBOM4(buf) {
			return UTF32BigEndian, nilIfEmpty(buf[4:]), err
		}
		if isUTF32LittleEndianBOM4(buf) {
			return UTF32LittleEndian, nilIfEmpty(buf[4:]), err
		}
	}

	if len(buf) > 2 && isUTF8BOM3(buf) {
		return UTF8, nilIfEmpty(buf[3:]), err
	}

	// A non-EOF error before all 4 bytes are read leaves FF FE ambiguous (it may start UTF-32LE);
	// FE FF is kept Unknown in this case too, for compatibility.
	if (err != nil && err != io.EOF && len(buf) < 4) || len(buf) < 2 {
		return Unknown, nilIfEmpty(buf), err
	}

	if isUTF16BigEndianBOM2(buf) {
		return UTF16BigEndian, nilIfEmpty(buf[2:]), err
	}
	if isUTF16LittleEndianBOM2(buf) {
		return UTF16LittleEndian, nilIfEmpty(buf[2:]), err
	}

	return Unknown, nilIfEmpty(buf), err
}

func readBOM(rd io.Reader) (buf []byte, err error) {
	const maxBOMSize = 4
	var bom [maxBOMSize]byte // used to read BOM

	// read as many bytes as possible
	for nEmpty, n := 0, 0; err == nil && len(buf) < maxBOMSize; buf = bom[:len(buf)+n] {
		if n, err = rd.Read(bom[len(buf):]); n < 0 {
			panic(errNegativeRead)
		}
		if n > 0 {
			nEmpty = 0
		} else if err == nil {
			nEmpty++
			if nEmpty >= maxConsecutiveEmptyReads {
				err = io.ErrNoProgress
			}
		}
	}
	return
}

func isUTF32BigEndianBOM4(buf []byte) bool {
	return buf[0] == 0x00 && buf[1] == 0x00 && buf[2] == 0xFE && buf[3] == 0xFF
}

func isUTF32LittleEndianBOM4(buf []byte) bool {
	return buf[0] == 0xFF && buf[1] == 0xFE && buf[2] == 0x00 && buf[3] == 0x00
}

func isUTF8BOM3(buf []byte) bool {
	return buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF
}

func isUTF16BigEndianBOM2(buf []byte) bool {
	return buf[0] == 0xFE && buf[1] == 0xFF
}

func isUTF16LittleEndianBOM2(buf []byte) bool {
	return buf[0] == 0xFF && buf[1] == 0xFE
}

func nilIfEmpty(buf []byte) (res []byte) {
	if len(buf) > 0 {
		res = buf
	}
	return
}
