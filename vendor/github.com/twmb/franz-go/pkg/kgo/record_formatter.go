package kgo

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kbin"
)

////////////
// WRITER //
////////////

// RecordFormatter formats records.
type RecordFormatter struct {
	calls atomicI64
	fns   []func([]byte, *FetchPartition, *Record) []byte
}

// AppendRecord appends a record to b given the parsed format and returns the
// updated slice.
func (f *RecordFormatter) AppendRecord(b []byte, r *Record) []byte {
	for _, fn := range f.fns {
		b = fn(b, nil, r)
	}
	return b
}

// AppendPartitionRecord appends a record and partition to b given the parsed
// format and returns the updated slice.
func (f *RecordFormatter) AppendPartitionRecord(b []byte, p *FetchPartition, r *Record) []byte {
	for _, fn := range f.fns {
		b = fn(b, p, r)
	}
	return b
}

// NewRecordFormatter returns a formatter for the given layout, or an error if
// the layout is invalid.
//
// The formatter is very powerful, as such there is a lot to describe. This
// documentation attempts to be as succinct as possible.
//
// Similar to the fmt package, record formatting is based off of slash escapes
// and percent "verbs" (copying fmt package lingo). Slashes are used for common
// escapes,
//
//	\t \n \r \\ \xNN
//
// printing tabs, newlines, carriage returns, slashes, and hex encoded
// characters.
//
// Percent encoding opts in to printing aspects of either a record or a fetch
// partition:
//
//	%t    topic
//	%T    topic length
//	%k    key
//	%K    key length
//	%v    value
//	%V    value length
//	%h    begin the header specification
//	%H    number of headers
//	%p    partition
//	%o    offset
//	%e    leader epoch
//	%d    timestamp (date, formatting described below)
//	%a    record attributes (formatting required, described below)
//	%x    producer id
//	%y    producer epoch
//
// For AppendPartitionRecord, the formatter also undersands the following three
// formatting options:
//
//	%[    partition log start offset
//	%|    partition last stable offset
//	%]    partition high watermark
//
// The formatter internally tracks the number of times AppendRecord or
// AppendPartitionRecord have been called. The special option %i prints the
// iteration / call count:
//
//	%i    format iteration number (starts at 1)
//
// Lastly, there are three escapes to print raw characters that are usually
// used for formatting options:
//
//	%%    percent sign
//	%{    left brace (required if a brace is after another format option)
//	%}    right brace
//
// # Header specification
//
// Specifying headers is essentially a primitive nested format option,
// accepting the key and value escapes above:
//
//	%K    header key length
//	%k    header key
//	%V    header value length
//	%v    header value
//
// For example, "%H %h{%k %v }" will print the number of headers, and then each
// header key and value with a space after each.
//
// # Verb modifiers
//
// Most of the previous verb specifications can be modified by adding braces
// with a given modifier, e.g., "%V{ascii}". All modifiers are described below.
//
// # Numbers
//
// All number verbs accept braces that control how the number is printed:
//
//	%v{ascii}       the default, print the number as ascii
//	%v{number}      alias for ascii
//
//	%v{hex64}       print 16 hex characters for the number
//	%v{hex32}       print 8 hex characters for the number
//	%v{hex16}       print 4 hex characters for the number
//	%v{hex8}        print 2 hex characters for the number
//	%v{hex4}        print 1 hex characters for the number
//	%v{hex}         print as many hex characters as necessary for the number
//
//	%v{big64}       print the number in big endian uint64 format
//	%v{big32}       print the number in big endian uint32 format
//	%v{big16}       print the number in big endian uint16 format
//	%v{big8}        alias for byte
//
//	%v{little64}    print the number in little endian uint64 format
//	%v{little32}    print the number in little endian uint32 format
//	%v{little16}    print the number in little endian uint16 format
//	%v{little8}     alias for byte
//
//	%v{byte}        print the number as a single byte
//	%v{bool}        print "true" if the number is non-zero, otherwise "false"
//
// All numbers are truncated as necessary per each given format.
//
// # Timestamps
//
// Timestamps can be specified in three formats: plain number formatting,
// native Go timestamp formatting, or strftime formatting. Number formatting is
// follows the rules above using the millisecond timestamp value. Go and
// strftime have further internal format options:
//
//	%d{go##2006-01-02T15:04:05Z07:00##}
//	%d{strftime[%F]}
//
// An arbitrary amount of pounds, braces, and brackets are understood before
// beginning the actual timestamp formatting. For Go formatting, the format is
// simply passed to the time package's AppendFormat function. For strftime, all
// "man strftime" options are supported. Time is always in UTC.
//
// # Attributes
//
// Records attributes require formatting, where each formatting option selects
// which attribute to print and how to print it.
//
//	%a{compression}
//	%a{compression;number}
//	%a{compression;big64}
//	%a{compression;hex8}
//
// By default, prints the compression as text ("none", "gzip", ...).
// Compression can be printed as a number with ";number", where number is any
// number formatting option described above.
//
//	%a{timestamp-type}
//	%a{timestamp-type;big64}
//
// Prints -1 for pre-0.10 records, 0 for client generated timestamps, and 1 for
// broker generated. Number formatting can be controlled with ";number".
//
//	%a{transactional-bit}
//	%a{transactional-bit;bool}
//
// Prints 1 if the record is a part of a transaction or 0 if it is not. Number
// formatting can be controlled with ";number".
//
//	%a{control-bit}
//	%a{control-bit;bool}
//
// Prints 1 if the record is a commit marker or 0 if it is not. Number
// formatting can be controlled with ";number".
//
// # Text
//
// Topics, keys, and values have "base64", "base64raw", "hex", and "unpack"
// formatting options:
//
//	%t{hex}
//	%k{unpack{<bBhH>iIqQc.$}}
//	%v{base64}
//	%v{base64raw}
//
// Unpack formatting is inside of enclosing pounds, braces, or brackets, the
// same way that timestamp formatting is understood. The syntax roughly follows
// Python's struct packing/unpacking rules:
//
//	x    pad character (does not parse input)
//	<    parse what follows as little endian
//	>    parse what follows as big endian
//
//	b    signed byte
//	B    unsigned byte
//	h    int16  ("half word")
//	H    uint16 ("half word")
//	i    int32
//	I    uint32
//	q    int64  ("quad word")
//	Q    uint64 ("quad word")
//
//	c    any character
//	.    alias for c
//	s    consume the rest of the input as a string
//	$    match the end of the line (append error string if anything remains)
//
// Unlike python, a '<' or '>' can appear anywhere in the format string and
// affects everything that follows. It is possible to switch endianness
// multiple times. If the parser needs more data than available, or if the more
// input remains after '$', an error message will be appended.
func NewRecordFormatter(layout string) (*RecordFormatter, error) {
	var f RecordFormatter

	var literal []byte // non-formatted raw text to output
	var i int
	for len(layout) > 0 {
		i++
		c, size := utf8.DecodeRuneInString(layout)
		rawc := layout[:size]
		layout = layout[size:]
		switch c {
		default:
			literal = append(literal, rawc...)
			continue

		case '\\':
			c, n, err := parseLayoutSlash(layout)
			if err != nil {
				return nil, err
			}
			layout = layout[n:]
			literal = append(literal, c)
			continue

		case '%':
		}

		if len(layout) == 0 {
			return nil, errors.New("invalid escape sequence at end of layout string")
		}

		cNext, size := utf8.DecodeRuneInString(layout)
		if cNext == '%' || cNext == '{' || cNext == '}' {
			literal = append(literal, byte(cNext))
			layout = layout[size:]
			continue
		}

		var (
			isOpenBrace  = len(layout) > 2 && layout[1] == '{'
			handledBrace bool
			escaped      = layout[0]
		)
		layout = layout[1:]

		// We are entering a format string. If we have any built
		// literal before, this is now raw text that we will append.
		if len(literal) > 0 {
			l := literal
			literal = nil
			f.fns = append(f.fns, func(b []byte, _ *FetchPartition, _ *Record) []byte { return append(b, l...) })
		}

		if isOpenBrace { // opening a brace: layout continues after
			layout = layout[1:]
		}

		switch escaped {
		default:
			return nil, fmt.Errorf("unknown escape sequence %%%s", string(escaped))

		case 'T', 'K', 'V', 'H', 'p', 'o', 'e', 'i', 'x', 'y', '[', '|', ']':
			// Numbers default to ascii, but we support a bunch of
			// formatting options. We parse the format here, and
			// then below is switching on which field to print.
			var numfn func([]byte, int64) []byte
			if handledBrace = isOpenBrace; handledBrace {
				numfn2, n, err := parseNumWriteLayout(layout)
				if err != nil {
					return nil, err
				}
				layout = layout[n:]
				numfn = numfn2
			} else {
				numfn = writeNumASCII
			}
			switch escaped {
			case 'T':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(len(r.Topic))) })
				})
			case 'K':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(len(r.Key))) })
				})
			case 'V':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(len(r.Value))) })
				})
			case 'H':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(len(r.Headers))) })
				})
			case 'p':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(r.Partition)) })
				})
			case 'o':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, r.Offset) })
				})
			case 'e':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(r.LeaderEpoch)) })
				})
			case 'i':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, _ *Record) []byte {
					return numfn(b, f.calls.Add(1))
				})
			case 'x':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, r.ProducerID) })
				})
			case 'y':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, int64(r.ProducerEpoch)) })
				})
			case '[':
				f.fns = append(f.fns, func(b []byte, p *FetchPartition, _ *Record) []byte {
					return writeP(b, p, func(b []byte, p *FetchPartition) []byte { return numfn(b, p.LogStartOffset) })
				})
			case '|':
				f.fns = append(f.fns, func(b []byte, p *FetchPartition, _ *Record) []byte {
					return writeP(b, p, func(b []byte, p *FetchPartition) []byte { return numfn(b, p.LastStableOffset) })
				})
			case ']':
				f.fns = append(f.fns, func(b []byte, p *FetchPartition, _ *Record) []byte {
					return writeP(b, p, func(b []byte, p *FetchPartition) []byte { return numfn(b, p.HighWatermark) })
				})
			}

		case 't', 'k', 'v':
			var appendFn func([]byte, []byte) []byte
			if handledBrace = isOpenBrace; handledBrace {
				switch {
				case strings.HasPrefix(layout, "}"):
					layout = layout[len("}"):]
					appendFn = appendPlain
				case strings.HasPrefix(layout, "base64}"):
					appendFn = appendBase64
					layout = layout[len("base64}"):]
				case strings.HasPrefix(layout, "base64raw}"):
					appendFn = appendBase64raw
					layout = layout[len("base64raw}"):]
				case strings.HasPrefix(layout, "hex}"):
					appendFn = appendHex
					layout = layout[len("hex}"):]
				case strings.HasPrefix(layout, "unpack"):
					unpack, rem, err := nomOpenClose(layout[len("unpack"):])
					if err != nil {
						return nil, fmt.Errorf("unpack parse err: %v", err)
					}
					if len(rem) == 0 || rem[0] != '}' {
						return nil, fmt.Errorf("unpack missing closing } in %q", layout)
					}
					layout = rem[1:]
					appendFn, err = parseUnpack(unpack)
					if err != nil {
						return nil, fmt.Errorf("unpack formatting parse err: %v", err)
					}

				default:
					return nil, fmt.Errorf("unknown %%%s{ escape", string(escaped))
				}
			} else {
				appendFn = appendPlain
			}
			switch escaped {
			case 't':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return appendFn(b, []byte(r.Topic)) })
				})
			case 'k':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return appendFn(b, r.Key) })
				})
			case 'v':
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return appendFn(b, r.Value) })
				})
			}

		case 'a':
			if !isOpenBrace {
				return nil, errors.New("missing open brace sequence on %a signifying how attributes should be written")
			}
			handledBrace = true

			num := func(skipText string, rfn func(*Record) int64) error {
				layout = layout[len(skipText):]
				numfn, n, err := parseNumWriteLayout(layout)
				if err != nil {
					return err
				}
				layout = layout[n:]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, rfn(r)) })
				})
				return nil
			}
			bi64 := func(b bool) int64 {
				if b {
					return 1
				}
				return 0
			}

			switch {
			case strings.HasPrefix(layout, "compression}"):
				layout = layout[len("compression}"):]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte {
						switch codecType(r.Attrs.CompressionType()) {
						case codecNone:
							return append(b, "none"...)
						case codecGzip:
							return append(b, "gzip"...)
						case codecSnappy:
							return append(b, "snappy"...)
						case codecLZ4:
							return append(b, "lz4"...)
						case codecZstd:
							return append(b, "zstd"...)
						default:
							return append(b, "unknown"...)
						}
					})
				})
			case strings.HasPrefix(layout, "compression;"):
				if err := num("compression;", func(r *Record) int64 { return int64(r.Attrs.CompressionType()) }); err != nil {
					return nil, err
				}

			case strings.HasPrefix(layout, "timestamp-type}"):
				layout = layout[len("timestamp-type}"):]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte {
						return strconv.AppendInt(b, int64(r.Attrs.TimestampType()), 10)
					})
				})
			case strings.HasPrefix(layout, "timestamp-type;"):
				if err := num("timestamp-type;", func(r *Record) int64 { return int64(r.Attrs.TimestampType()) }); err != nil {
					return nil, err
				}

			case strings.HasPrefix(layout, "transactional-bit}"):
				layout = layout[len("transactional-bit}"):]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte {
						if r.Attrs.IsTransactional() {
							return append(b, '1')
						}
						return append(b, '0')
					})
				})
			case strings.HasPrefix(layout, "transactional-bit;"):
				if err := num("transactional-bit;", func(r *Record) int64 { return bi64(r.Attrs.IsTransactional()) }); err != nil {
					return nil, err
				}

			case strings.HasPrefix(layout, "control-bit}"):
				layout = layout[len("control-bit}"):]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte {
						if r.Attrs.IsControl() {
							return append(b, '1')
						}
						return append(b, '0')
					})
				})
			case strings.HasPrefix(layout, "control-bit;"):
				if err := num("control-bit;", func(r *Record) int64 { return bi64(r.Attrs.IsControl()) }); err != nil {
					return nil, err
				}

			default:
				return nil, errors.New("unknown %a formatting")
			}

		case 'h':
			if !isOpenBrace {
				return nil, errors.New("missing open brace sequence on %h signifying how headers are written")
			}
			handledBrace = true
			// Headers can have their own internal braces, so we
			// must look for a matching end brace.
			braces := 1
			at := 0
			for braces != 0 && len(layout[at:]) > 0 {
				switch layout[at] {
				case '{':
					if at > 0 && layout[at-1] != '%' {
						braces++
					}
				case '}':
					if at > 0 && layout[at-1] != '%' {
						braces--
					}
				}
				at++
			}
			if braces > 0 {
				return nil, fmt.Errorf("invalid header specification: missing closing brace in %q", layout)
			}

			spec := layout[:at-1]
			layout = layout[at:]
			inf, err := NewRecordFormatter(spec)
			if err != nil {
				return nil, fmt.Errorf("invalid header specification %q: %v", spec, err)
			}

			f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
				reuse := new(Record)
				for _, header := range r.Headers {
					reuse.Key = []byte(header.Key)
					reuse.Value = header.Value
					b = inf.AppendRecord(b, reuse)
				}
				return b
			})

		case 'd':
			// For datetime parsing, we support plain millis in any
			// number format, strftime, or go formatting. We
			// default to plain ascii millis.
			handledBrace = isOpenBrace
			if !handledBrace {
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return strconv.AppendInt(b, r.Timestamp.UnixNano()/1e6, 10) })
				})
				continue
			}

			switch {
			case strings.HasPrefix(layout, "strftime"):
				tfmt, rem, err := nomOpenClose(layout[len("strftime"):])
				if err != nil {
					return nil, fmt.Errorf("strftime parse err: %v", err)
				}
				if len(rem) == 0 || rem[0] != '}' {
					return nil, fmt.Errorf("%%d{strftime missing closing } in %q", layout)
				}
				layout = rem[1:]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return strftimeAppendFormat(b, tfmt, r.Timestamp.UTC()) })
				})

			case strings.HasPrefix(layout, "go"):
				tfmt, rem, err := nomOpenClose(layout[len("go"):])
				if err != nil {
					return nil, fmt.Errorf("go parse err: %v", err)
				}
				if len(rem) == 0 || rem[0] != '}' {
					return nil, fmt.Errorf("%%d{go missing closing } in %q", layout)
				}
				layout = rem[1:]
				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return r.Timestamp.UTC().AppendFormat(b, tfmt) })
				})

			default:
				numfn, n, err := parseNumWriteLayout(layout)
				if err != nil {
					return nil, fmt.Errorf("unknown %%d{ time specification in %q", layout)
				}
				layout = layout[n:]

				f.fns = append(f.fns, func(b []byte, _ *FetchPartition, r *Record) []byte {
					return writeR(b, r, func(b []byte, r *Record) []byte { return numfn(b, r.Timestamp.UnixNano()/1e6) })
				})
			}
		}

		// If we opened a brace, we require a closing brace.
		if isOpenBrace && !handledBrace {
			return nil, fmt.Errorf("unhandled open brace %q", layout)
		}
	}

	// Ensure we print any trailing text.
	if len(literal) > 0 {
		f.fns = append(f.fns, func(b []byte, _ *FetchPartition, _ *Record) []byte { return append(b, literal...) })
	}

	return &f, nil
}

func appendPlain(dst, src []byte) []byte {
	return append(dst, src...)
}

func appendBase64(dst, src []byte) []byte {
	fin := append(dst, make([]byte, base64.StdEncoding.EncodedLen(len(src)))...)
	base64.StdEncoding.Encode(fin[len(dst):], src)
	return fin
}

func appendBase64raw(dst, src []byte) []byte {
	fin := append(dst, make([]byte, base64.RawStdEncoding.EncodedLen(len(src)))...)
	base64.RawStdEncoding.Encode(fin[len(dst):], src)
	return fin
}

func appendHex(dst, src []byte) []byte {
	fin := append(dst, make([]byte, hex.EncodedLen(len(src)))...)
	hex.Encode(fin[len(dst):], src)
	return fin
}

// nomOpenClose extracts a middle section from a string beginning with repeated
// delimiters and returns it as with remaining (past end delimiters) string.
func nomOpenClose(src string) (middle, remaining string, err error) {
	if len(src) == 0 {
		return "", "", errors.New("empty layout")
	}
	delim := src[0]
	openers := 1
	for openers < len(src) && src[openers] == delim {
		openers++
	}
	switch delim {
	case '{':
		delim = '}'
	case '[':
		delim = ']'
	case '(':
		delim = ')'
	}
	src = src[openers:]
	end := strings.Repeat(string(delim), openers)
	idx := strings.Index(src, end)
	if idx < 0 {
		return "", "", fmt.Errorf("missing end delim %q", end)
	}
	middle = src[:idx]
	return middle, src[idx+len(end):], nil
}

func parseUnpack(layout string) (func([]byte, []byte) []byte, error) {
	// take dst, src; return dst
	// %!q(eof)
	// take 8 bytes, decode it, print decoded
	var fns []func([]byte, []byte) ([]byte, int)
	little := true
	var sawEnd bool
	for i := range layout {
		if sawEnd {
			return nil, errors.New("already saw end-of-input parsing character")
		}

		var need int
		var signed bool
		cs := layout[i : i+1]
		switch cs[0] {
		case 'x':
			continue

		case '<':
			little = true
			continue
		case '>':
			little = false
			continue

		case 'b':
			need = 1
			signed = true
		case 'B':
			need = 1
		case 'h':
			need = 2
			signed = true
		case 'H':
			need = 2
		case 'i':
			need = 4
			signed = true
		case 'I':
			need = 4
		case 'q':
			need = 8
			signed = true
		case 'Q':
			need = 8

		case 'c', '.':
			fns = append(fns, func(dst, src []byte) ([]byte, int) {
				if len(src) < 1 {
					return append(dst, "%!c(no bytes available)"...), 0
				}
				return append(dst, src[0]), 1
			})
			continue

		case 's':
			sawEnd = true
			fns = append(fns, func(dst, src []byte) ([]byte, int) {
				return append(dst, src...), len(src)
			})
			continue

		case '$':
			fns = append(fns, func(dst, src []byte) ([]byte, int) {
				if len(src) != 0 {
					dst = append(dst, "%!$(not end-of-input)"...)
				}
				return dst, len(src)
			})
			sawEnd = true
			continue

		default:
			return nil, fmt.Errorf("invalid unpack parsing character %s", cs)
		}

		islittle := little
		fns = append(fns, func(dst, src []byte) ([]byte, int) {
			if len(src) < need {
				return append(dst, fmt.Sprintf("%%!%%s(have %d bytes, need %d)", len(src), need)...), len(src)
			}

			var ul, ub uint64
			var il, ib int64
			switch need {
			case 1:
				ul = uint64(src[0])
				ub = ul
				il = int64(byte(ul))
				ib = int64(byte(ub))
			case 2:
				ul = uint64(binary.LittleEndian.Uint16(src))
				ub = uint64(binary.BigEndian.Uint16(src))
				il = int64(int16(ul))
				ib = int64(int16(ub))
			case 4:
				ul = uint64(binary.LittleEndian.Uint32(src))
				ub = uint64(binary.BigEndian.Uint32(src))
				il = int64(int32(ul))
				ib = int64(int32(ub))
			case 8:
				ul = binary.LittleEndian.Uint64(src)
				ub = binary.BigEndian.Uint64(src)
				il = int64(ul)
				ib = int64(ub)
			}
			u := ub
			i := ib
			if islittle {
				u = ul
				i = il
			}

			if signed {
				return strconv.AppendInt(dst, i, 10), need
			}
			return strconv.AppendUint(dst, u, 10), need
		})
	}

	return func(dst, src []byte) []byte {
		for _, fn := range fns {
			var n int
			dst, n = fn(dst, src)
			src = src[n:]
		}
		return dst
	}, nil
}

func parseNumWriteLayout(layout string) (func([]byte, int64) []byte, int, error) {
	braceEnd := strings.IndexByte(layout, '}')
	if braceEnd == -1 {
		return nil, 0, errors.New("missing brace end } to close number format specification")
	}
	end := braceEnd + 1
	switch layout = layout[:braceEnd]; layout {
	case "ascii", "number":
		return writeNumASCII, end, nil
	case "hex64":
		return writeNumHex64, end, nil
	case "hex32":
		return writeNumHex32, end, nil
	case "hex16":
		return writeNumHex16, end, nil
	case "hex8":
		return writeNumHex8, end, nil
	case "hex4":
		return writeNumHex4, end, nil
	case "hex":
		return writeNumHex, end, nil
	case "big64":
		return writeNumBig64, end, nil
	case "big32":
		return writeNumBig32, end, nil
	case "big16":
		return writeNumBig16, end, nil
	case "byte", "big8", "little8":
		return writeNumByte, end, nil
	case "little64":
		return writeNumLittle64, end, nil
	case "little32":
		return writeNumLittle32, end, nil
	case "little16":
		return writeNumLittle16, end, nil
	case "bool":
		return writeNumBool, end, nil
	default:
		return nil, 0, fmt.Errorf("invalid output number layout %q", layout)
	}
}

func writeR(b []byte, r *Record, fn func([]byte, *Record) []byte) []byte {
	if r == nil {
		return append(b, "<nil>"...)
	}
	return fn(b, r)
}

func writeP(b []byte, p *FetchPartition, fn func([]byte, *FetchPartition) []byte) []byte {
	if p == nil {
		return append(b, "<nil>"...)
	}
	return fn(b, p)
}
func writeNumASCII(b []byte, n int64) []byte { return strconv.AppendInt(b, n, 10) }

const hexc = "0123456789abcdef"

func writeNumHex64(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b,
		hexc[(u>>60)&0xf],
		hexc[(u>>56)&0xf],
		hexc[(u>>52)&0xf],
		hexc[(u>>48)&0xf],
		hexc[(u>>44)&0xf],
		hexc[(u>>40)&0xf],
		hexc[(u>>36)&0xf],
		hexc[(u>>32)&0xf],
		hexc[(u>>28)&0xf],
		hexc[(u>>24)&0xf],
		hexc[(u>>20)&0xf],
		hexc[(u>>16)&0xf],
		hexc[(u>>12)&0xf],
		hexc[(u>>8)&0xf],
		hexc[(u>>4)&0xf],
		hexc[u&0xf],
	)
}

func writeNumHex32(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b,
		hexc[(u>>28)&0xf],
		hexc[(u>>24)&0xf],
		hexc[(u>>20)&0xf],
		hexc[(u>>16)&0xf],
		hexc[(u>>12)&0xf],
		hexc[(u>>8)&0xf],
		hexc[(u>>4)&0xf],
		hexc[u&0xf],
	)
}

func writeNumHex16(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b,
		hexc[(u>>12)&0xf],
		hexc[(u>>8)&0xf],
		hexc[(u>>4)&0xf],
		hexc[u&0xf],
	)
}

func writeNumHex8(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b,
		hexc[(u>>4)&0xf],
		hexc[u&0xf],
	)
}

func writeNumHex4(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b,
		hexc[u&0xf],
	)
}

func writeNumHex(b []byte, n int64) []byte {
	return strconv.AppendUint(b, uint64(n), 16)
}

func writeNumBig64(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b, byte(u>>56), byte(u>>48), byte(u>>40), byte(u>>32), byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
}

func writeNumLittle64(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b, byte(u), byte(u>>8), byte(u>>16), byte(u>>24), byte(u>>32), byte(u>>40), byte(u>>48), byte(u>>56))
}

func writeNumBig32(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b, byte(u>>24), byte(u>>16), byte(u>>8), byte(u))
}

func writeNumLittle32(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b, byte(u), byte(u>>8), byte(u>>16), byte(u>>24))
}
func writeNumBig16(b []byte, n int64) []byte { u := uint64(n); return append(b, byte(u>>8), byte(u)) }
func writeNumLittle16(b []byte, n int64) []byte {
	u := uint64(n)
	return append(b, byte(u), byte(u>>8))
}
func writeNumByte(b []byte, n int64) []byte { u := uint64(n); return append(b, byte(u)) }

func writeNumBool(b []byte, n int64) []byte {
	if n == 0 {
		return append(b, "false"...)
	}
	return append(b, "true"...)
}

////////////
// READER //
////////////

// RecordReader reads records from an io.Reader.
type RecordReader struct {
	r *bufio.Reader

	buf []byte
	fns []readParse

	done bool
}

// NewRecordReader returns a record reader for the given layout, or an error if
// the layout is invalid.
//
// Similar to the RecordFormatter, the RecordReader parsing is quite powerful.
// There is a bit less to describe in comparison to RecordFormatter, but still,
// this documentation attempts to be as succinct as possible.
//
// Similar to the fmt package, record parsing is based off of slash escapes and
// percent "verbs" (copying fmt package lingo). Slashes are used for common
// escapes,
//
//	\t \n \r \\ \xNN
//
// reading tabs, newlines, carriage returns, slashes, and hex encoded
// characters.
//
// Percent encoding reads into specific values of a Record:
//
//	%t    topic
//	%T    topic length
//	%k    key
//	%K    key length
//	%v    value
//	%V    value length
//	%h    begin the header specification
//	%H    number of headers
//	%p    partition
//	%o    offset
//	%e    leader epoch
//	%d    timestamp
//	%x    producer id
//	%y    producer epoch
//
// If using length / number verbs (i.e., "sized" verbs), they must occur before
// what they are sizing.
//
// There are three escapes to parse raw characters, rather than opting into
// some formatting option:
//
//	%%    percent sign
//	%{    left brace
//	%}    right brace
//
// Unlike record formatting, timestamps can only be read as numbers because Go
// or strftime formatting can both be variable length and do not play too well
// with delimiters. Timestamps numbers are read as milliseconds.
//
// # Numbers
//
// All size numbers can be parsed in the following ways:
//
//	%v{ascii}       parse numeric digits until a non-numeric
//	%v{number}      alias for ascii
//
//	%v{hex64}       read 16 hex characters for the number
//	%v{hex32}       read 8 hex characters for the number
//	%v{hex16}       read 4 hex characters for the number
//	%v{hex8}        read 2 hex characters for the number
//	%v{hex4}        read 1 hex characters for the number
//
//	%v{big64}       read the number as big endian uint64 format
//	%v{big32}       read the number as big endian uint32 format
//	%v{big16}       read the number as big endian uint16 format
//	%v{big8}        alias for byte
//
//	%v{little64}    read the number as little endian uint64 format
//	%v{little32}    read the number as little endian uint32 format
//	%v{little16}    read the number as little endian uint16 format
//	%v{little8}     read the number as a byte
//
//	%v{byte}        read the number as a byte
//	%v{bool}        read "true" as 1, "false" as 0
//	%v{3}           read 3 characters (any number)
//
// # Header specification
//
// Similar to number formatting, headers are parsed using a nested primitive
// format option, accepting the key and value escapes previously mentioned.
//
// # Text
//
// Topics, keys, and values can be decoded using "base64", "hex", and "json"
// formatting options. Any size specification is the size of the encoded value
// actually being read (i.e., size as seen, not size when decoded). JSON values
// are compacted after being read.
//
//	%T%t{hex}     -  4abcd reads four hex characters "abcd"
//	%V%v{base64}  -  2z9 reads two base64 characters "z9"
//	%v{json} %k   -  {"foo" : "bar"} foo reads a JSON object and then "foo"
//
// As well, these text options can be parsed with regular expressions:
//
//	%k{re[\d*]}%v{re[\s+]}
func NewRecordReader(reader io.Reader, layout string) (*RecordReader, error) {
	r := &RecordReader{r: bufio.NewReader(reader)}
	if err := r.parseReadLayout(layout); err != nil {
		return nil, err
	}
	return r, nil
}

// ReadRecord reads the next record in the reader and returns it, or returns a
// parsing error.
//
// This will return io.EOF only if the underlying reader returns io.EOF at the
// start of a new record. If an io.EOF is returned mid record, this returns
// io.ErrUnexpectedEOF. It is expected for this function to be called until it
// returns io.EOF.
func (r *RecordReader) ReadRecord() (*Record, error) {
	rec := new(Record)
	return rec, r.ReadRecordInto(rec)
}

// ReadRecordInto reads the next record into the given record and returns any
// parsing error
//
// This will return io.EOF only if the underlying reader returns io.EOF at the
// start of a new record. If an io.EOF is returned mid record, this returns
// io.ErrUnexpectedEOF. It is expected for this function to be called until it
// returns io.EOF.
func (r *RecordReader) ReadRecordInto(rec *Record) error {
	if r.done {
		return io.EOF
	}
	return r.next(rec)
}

// SetReader replaces the underlying reader with the given reader.
func (r *RecordReader) SetReader(reader io.Reader) {
	r.r = bufio.NewReader(reader)
	r.done = false
}

const (
	parsesTopic parseRecordBits = 1 << iota
	parsesTopicSize
	parsesKey
	parsesKeySize
	parsesValue
	parsesValueSize
	parsesHeaders
	parsesHeadersNum
)

// The record reading format must be either entirely sized or entirely unsized.
// This type helps us track what's what.
type parseRecordBits uint8

func (p *parseRecordBits) set(r parseRecordBits)     { *p |= r }
func (p parseRecordBits) has(r parseRecordBits) bool { return p&r != 0 }

func (r *RecordReader) parseReadLayout(layout string) error {
	if len(layout) == 0 {
		return errors.New("RecordReader: invalid empty format")
	}

	var (
		// If we are reading by size, we parse the layout size into one
		// of these variables. When reading, we use the captured
		// variable's value.
		topicSize  = new(uint64)
		keySize    = new(uint64)
		valueSize  = new(uint64)
		headersNum = new(uint64)

		bits parseRecordBits

		literal    []byte // raw literal we are currently working on
		addLiteral = func() {
			if len(r.fns) > 0 && r.fns[len(r.fns)-1].read.empty() {
				r.fns[len(r.fns)-1].read.delim = literal
			} else if len(literal) > 0 {
				r.fns = append(r.fns, readParse{
					read: readKind{exact: literal},
				})
			}
			literal = nil
		}
	)

	for len(layout) > 0 {
		c, size := utf8.DecodeRuneInString(layout)
		rawc := layout[:size]
		layout = layout[size:]
		switch c {
		default:
			literal = append(literal, rawc...)
			continue

		case '\\':
			c, n, err := parseLayoutSlash(layout)
			if err != nil {
				return err
			}
			layout = layout[n:]
			literal = append(literal, c)
			continue

		case '%':
		}

		if len(layout) == 0 {
			literal = append(literal, rawc...)
			continue
		}

		cNext, size := utf8.DecodeRuneInString(layout)
		if cNext == '%' || cNext == '{' || cNext == '}' {
			literal = append(literal, byte(cNext))
			layout = layout[size:]
			continue
		}

		var (
			isOpenBrace  = len(layout) > 2 && layout[1] == '{'
			handledBrace bool
			escaped      = layout[0]
		)
		layout = layout[1:]
		addLiteral()

		if isOpenBrace { // opening a brace: layout continues after
			layout = layout[1:]
		}

		switch escaped {
		default:
			return fmt.Errorf("unknown percent escape sequence %q", layout[:1])

		case 'T', 'K', 'V', 'H':
			var dst *uint64
			var bit parseRecordBits
			switch escaped {
			case 'T':
				dst, bit = topicSize, parsesTopicSize
			case 'K':
				dst, bit = keySize, parsesKeySize
			case 'V':
				dst, bit = valueSize, parsesValueSize
			case 'H':
				dst, bit = headersNum, parsesHeadersNum
			}
			if bits.has(bit) {
				return fmt.Errorf("%%%s is doubly specified", string(escaped))
			}
			if bits.has(bit >> 1) {
				return fmt.Errorf("size specification %%%s cannot come after value specification %%%s", string(escaped), strings.ToLower(string(escaped)))
			}
			bits.set(bit)
			fn, n, err := r.parseReadSize("ascii", dst, false)
			if handledBrace = isOpenBrace; handledBrace {
				fn, n, err = r.parseReadSize(layout, dst, true)
			}
			if err != nil {
				return fmt.Errorf("unable to parse %%%s: %s", string(escaped), err)
			}
			layout = layout[n:]
			r.fns = append(r.fns, fn)

		case 'p', 'o', 'e', 'd', 'x', 'y':
			dst := new(uint64)
			fn, n, err := r.parseReadSize("ascii", dst, false)
			if handledBrace = isOpenBrace; handledBrace {
				fn, n, err = r.parseReadSize(layout, dst, true)
			}
			if err != nil {
				return fmt.Errorf("unable to parse %%%s: %s", string(escaped), err)
			}
			layout = layout[n:]
			numParse := fn.parse
			switch escaped {
			case 'p':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.Partition = int32(*dst)
					return nil
				}
			case 'o':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.Offset = int64(*dst)
					return nil
				}
			case 'e':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.LeaderEpoch = int32(*dst)
					return nil
				}
			case 'd':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.Timestamp = time.Unix(0, int64(*dst)*1e6)
					return nil
				}
			case 'x':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.ProducerID = int64(*dst)
					return nil
				}
			case 'y':
				fn.parse = func(b []byte, rec *Record) error {
					if err := numParse(b, nil); err != nil {
						return err
					}
					rec.ProducerEpoch = int16(*dst)
					return nil
				}
			}
			r.fns = append(r.fns, fn)

		case 't', 'k', 'v':
			var decodeFn func([]byte) ([]byte, error)
			var re *regexp.Regexp
			var isJson bool
			if handledBrace = isOpenBrace; handledBrace {
				switch {
				case strings.HasPrefix(layout, "}"):
					layout = layout[len("}"):]
				case strings.HasPrefix(layout, "base64}"):
					decodeFn = decodeBase64
					layout = layout[len("base64}"):]
				case strings.HasPrefix(layout, "hex}"):
					decodeFn = decodeHex
					layout = layout[len("hex}"):]
				case strings.HasPrefix(layout, "json}"):
					isJson = true
					decodeFn = func(b []byte) ([]byte, error) {
						var buf bytes.Buffer
						err := json.Compact(&buf, b)
						return buf.Bytes(), err
					}
					layout = layout[len("json}"):]
				case strings.HasPrefix(layout, "re"):
					restr, rem, err := nomOpenClose(layout[len("re"):])
					if err != nil {
						return fmt.Errorf("re parse err: %v", err)
					}
					if len(rem) == 0 || rem[0] != '}' {
						return fmt.Errorf("re missing closing } in %q", layout)
					}
					layout = rem[1:]
					if !strings.HasPrefix(restr, "^") {
						restr = "^" + restr
					}
					re, err = regexp.Compile(restr)
					if err != nil {
						return fmt.Errorf("re parse err: %v", err)
					}

				default:
					return fmt.Errorf("unknown %%%s{ escape", string(escaped))
				}
			}

			var bit, bitSize parseRecordBits
			var inner func([]byte, *Record)
			var size *uint64
			switch escaped {
			case 't':
				bit, bitSize, size = parsesTopic, parsesTopicSize, topicSize
				inner = func(b []byte, r *Record) { r.Topic = string(b) }
			case 'k':
				bit, bitSize, size = parsesKey, parsesKeySize, keySize
				inner = func(b []byte, r *Record) { r.Key = dupslice(b) }
			case 'v':
				bit, bitSize, size = parsesValue, parsesValueSize, valueSize
				inner = func(b []byte, r *Record) { r.Value = dupslice(b) }
			}

			fn := readParse{parse: func(b []byte, r *Record) error {
				if decodeFn != nil {
					dec, err := decodeFn(b)
					if err != nil {
						return err
					}
					b = dec
				}
				inner(b, r)
				return nil
			}}
			bit.set(bit)
			if bits.has(bitSize) {
				if re != nil {
					return errors.New("cannot specify exact size and regular expression")
				}
				if isJson {
					return errors.New("cannot specify exact size and json")
				}
				fn.read = readKind{sizefn: func() int { return int(*size) }}
			} else if re != nil {
				fn.read = readKind{re: re}
			} else if isJson {
				fn.read = readKind{condition: new(jsonReader).read}
			}
			r.fns = append(r.fns, fn)

		case 'h':
			bits.set(parsesHeaders)
			if !bits.has(parsesHeadersNum) {
				return errors.New("missing header count specification %H before header specification %h")
			}
			if !isOpenBrace {
				return errors.New("missing open brace sequence on %h signifying how headers are encoded")
			}
			handledBrace = true
			// Similar to above, headers can have their own
			// internal braces, so we look for a matching end.
			braces := 1
			at := 0
			for braces != 0 && len(layout[at:]) > 0 {
				switch layout[at] {
				case '{':
					if at > 0 && layout[at-1] != '%' {
						braces++
					}
				case '}':
					if at > 0 && layout[at-1] != '%' {
						braces--
					}
				}
				at++
			}
			if braces > 0 {
				return fmt.Errorf("invalid header specification: missing closing brace in %q", layout)
			}

			// We parse the header specification recursively, but
			// we require that it is sized and contains only keys
			// and values. Checking the delimiter checks sizing.
			var inr RecordReader
			if err := inr.parseReadLayout(layout[:at-1]); err != nil {
				return fmt.Errorf("invalid header specification: %v", err)
			}
			layout = layout[at:]

			// To parse headers, we save the inner reader's parsing
			// function stash the current record's key/value before
			// parsing, and then capture the key/value as a header.
			r.fns = append(r.fns, readParse{read: readKind{handoff: func(r *RecordReader, rec *Record) error {
				k, v := rec.Key, rec.Value
				defer func() { rec.Key, rec.Value = k, v }()
				inr.r = r.r
				for i := uint64(0); i < *headersNum; i++ {
					rec.Key, rec.Value = nil, nil
					if err := inr.next(rec); err != nil {
						return err
					}
					rec.Headers = append(rec.Headers, RecordHeader{Key: string(rec.Key), Value: rec.Value})
				}
				return nil
			}}})
		}

		if isOpenBrace && !handledBrace {
			return fmt.Errorf("unhandled open brace %q", layout)
		}
	}

	addLiteral()

	// We must sort noreads to the front, we use this guarantee when
	// reading to handle EOF properly.
	var noreads, reads []readParse
	for _, fn := range r.fns {
		if fn.read.noread {
			noreads = append(noreads, fn)
		} else {
			reads = append(reads, fn)
		}
	}
	r.fns = make([]readParse, 0, len(noreads)+len(reads))
	r.fns = append(r.fns, noreads...)
	r.fns = append(r.fns, reads...)

	return nil
}

// Returns a function that parses a number from the internal reader into dst.
//
// If needBrace is true, the user is specifying how to read the number,
// otherwise we default to ascii. Reading ascii requires us to peek at bytes
// until we get to a non-number byte.
func (*RecordReader) parseReadSize(layout string, dst *uint64, needBrace bool) (readParse, int, error) {
	var end int
	if needBrace {
		braceEnd := strings.IndexByte(layout, '}')
		if braceEnd == -1 {
			return readParse{}, 0, errors.New("missing brace end } to close number size specification")
		}
		layout = layout[:braceEnd]
		end = braceEnd + 1
	}

	switch layout {
	default:
		num, err := strconv.Atoi(layout)
		if err != nil {
			return readParse{}, 0, fmt.Errorf("unrecognized number reading layout %q: %v", layout, err)
		}
		if num <= 0 {
			return readParse{}, 0, fmt.Errorf("invalid zero or negative number %q when parsing read size", layout)
		}
		return readParse{
			readKind{noread: true},
			func([]byte, *Record) error { *dst = uint64(num); return nil },
		}, end, nil

	case "ascii", "number":
		return readParse{
			readKind{condition: func(b byte) int8 {
				if b < '0' || b > '9' {
					return -1
				}
				return 2 // ignore EOF if we hit it after this
			}},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 10, 64)
				return err
			},
		}, end, nil

	case "big64":
		return readParse{
			readKind{size: 8},
			func(b []byte, _ *Record) error { *dst = binary.BigEndian.Uint64(b); return nil },
		}, end, nil
	case "big32":
		return readParse{
			readKind{size: 4},
			func(b []byte, _ *Record) error { *dst = uint64(binary.BigEndian.Uint32(b)); return nil },
		}, end, nil
	case "big16":
		return readParse{
			readKind{size: 2},
			func(b []byte, _ *Record) error { *dst = uint64(binary.BigEndian.Uint16(b)); return nil },
		}, end, nil

	case "little64":
		return readParse{
			readKind{size: 8},
			func(b []byte, _ *Record) error { *dst = binary.LittleEndian.Uint64(b); return nil },
		}, end, nil
	case "little32":
		return readParse{
			readKind{size: 4},
			func(b []byte, _ *Record) error { *dst = uint64(binary.LittleEndian.Uint32(b)); return nil },
		}, end, nil
	case "little16":
		return readParse{
			readKind{size: 2},
			func(b []byte, _ *Record) error { *dst = uint64(binary.LittleEndian.Uint16(b)); return nil },
		}, end, nil

	case "byte", "big8", "little8":
		return readParse{
			readKind{size: 1},
			func(b []byte, _ *Record) error { *dst = uint64(b[0]); return nil },
		}, end, nil

	case "hex64":
		return readParse{
			readKind{size: 16},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 16, 64)
				return err
			},
		}, end, nil
	case "hex32":
		return readParse{
			readKind{size: 8},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 16, 64)
				return err
			},
		}, end, nil
	case "hex16":
		return readParse{
			readKind{size: 4},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 16, 64)
				return err
			},
		}, end, nil
	case "hex8":
		return readParse{
			readKind{size: 2},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 16, 64)
				return err
			},
		}, end, nil
	case "hex4":
		return readParse{
			readKind{size: 1},
			func(b []byte, _ *Record) (err error) {
				*dst, err = strconv.ParseUint(kbin.UnsafeString(b), 16, 64)
				return err
			},
		}, end, nil

	case "bool":
		const (
			stateUnknown uint8 = iota
			stateTrue
			stateFalse
		)
		var state uint8
		var last byte
		return readParse{
			readKind{condition: func(b byte) (done int8) {
				defer func() {
					if done <= 0 {
						state = stateUnknown
						last = 0
					}
				}()

				switch state {
				default: // stateUnknown
					if b == 't' {
						state = stateTrue
						last = b
						return 1
					} else if b == 'f' {
						state = stateFalse
						last = b
						return 1
					}
					return -1

				case stateTrue:
					if last == 't' && b == 'r' || last == 'r' && b == 'u' {
						last = b
						return 1
					} else if last == 'u' && b == 'e' {
						return 0
					}
					return -1

				case stateFalse:
					if last == 'f' && b == 'a' || last == 'a' && b == 'l' || last == 'l' && b == 's' {
						last = b
						return 1
					} else if last == 's' && b == 'e' {
						return 0
					}
					return -1
				}
			}},
			func(b []byte, _ *Record) error {
				switch string(b) {
				case "true":
					*dst = 1
				case "false":
					*dst = 0
				default:
					return fmt.Errorf("invalid bool %s", b)
				}
				return nil
			},
		}, end, nil
	}
}

func decodeBase64(b []byte) ([]byte, error) {
	n, err := base64.StdEncoding.Decode(b[:base64.StdEncoding.DecodedLen(len(b))], b)
	return b[:n], err
}

func decodeHex(b []byte) ([]byte, error) {
	n, err := hex.Decode(b[:hex.DecodedLen(len(b))], b)
	return b[:n], err
}

type readKind struct {
	noread    bool
	exact     []byte
	condition func(byte) int8 // -2: error, -1: stop, do not consume input; 0: stop, consume input; 1: keep going, consume input, 2: keep going, consume input, can EOF
	size      int
	sizefn    func() int
	handoff   func(*RecordReader, *Record) error
	delim     []byte
	re        *regexp.Regexp
}

func (r *readKind) empty() bool {
	return !r.noread &&
		r.exact == nil &&
		r.condition == nil &&
		r.size == 0 &&
		r.sizefn == nil &&
		r.handoff == nil &&
		r.delim == nil &&
		r.re == nil
}

type readParse struct {
	read  readKind
	parse func([]byte, *Record) error
}

func dupslice(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	dup := make([]byte, len(b))
	copy(dup, b)
	return dup
}

func (r *RecordReader) next(rec *Record) error {
	for i, fn := range r.fns {
		r.buf = r.buf[:0]

		var err error
		switch {
		case fn.read.noread:
			// do nothing
		case fn.read.exact != nil:
			err = r.readExact(fn.read.exact)
		case fn.read.condition != nil:
			err = r.readCondition(fn.read.condition)
		case fn.read.size > 0:
			err = r.readSize(fn.read.size)
		case fn.read.sizefn != nil:
			err = r.readSize(fn.read.sizefn())
		case fn.read.handoff != nil:
			err = fn.read.handoff(r, rec)
		case fn.read.re != nil:
			err = r.readRe(fn.read.re)
		default:
			err = r.readDelim(fn.read.delim) // we *always* fall back to delim parsing
		}

		switch err {
		default:
			return err
		case nil:
		case io.EOF, io.ErrUnexpectedEOF:
			r.done = true
			// We guarantee that all noread parses are at
			// the front, so if we io.EOF on the first
			// non-noread, then we bubble it up.
			if len(r.buf) == 0 && (i == 0 || r.fns[i-1].read.noread) {
				return io.EOF
			}
			if i != len(r.fns)-1 || err == io.ErrUnexpectedEOF {
				return io.ErrUnexpectedEOF
			}
		}

		if fn.parse == nil {
			continue
		}

		if err := fn.parse(r.buf, rec); err != nil {
			return err
		}
	}
	return nil
}

func (r *RecordReader) readCondition(fn func(byte) int8) error {
	var ignoreEOF bool
	for {
		peek, err := r.r.Peek(1)
		if err != nil {
			if err == io.EOF && ignoreEOF {
				err = nil
			}
			return err
		}
		ignoreEOF = false
		c := peek[0]
		switch fn(c) {
		case -2:
			return fmt.Errorf("invalid input %q", c)
		case -1:
			return nil
		case 0:
			r.r.Discard(1)
			r.buf = append(r.buf, c)
			return nil
		case 1:
		case 2:
			ignoreEOF = true
		}
		r.r.Discard(1)
		r.buf = append(r.buf, c)
	}
}

type reReader struct {
	r    *RecordReader
	peek []byte
	err  error
}

func (re *reReader) ReadRune() (r rune, size int, err error) {
	re.peek, re.err = re.r.r.Peek(len(re.peek) + 1)
	if re.err != nil {
		return 0, 0, re.err
	}
	return rune(re.peek[len(re.peek)-1]), 1, nil
}

func (r *RecordReader) readRe(re *regexp.Regexp) error {
	reader := reReader{r: r}
	loc := re.FindReaderIndex(&reader)
	if loc == nil {
		if reader.err == io.EOF && len(reader.peek) > 0 {
			return fmt.Errorf("regexp text mismatch, saw %q", reader.peek)
		}
		return reader.err
	}
	n := loc[1] // we ensure the regexp begins with ^, so we only need the end
	r.buf = append(r.buf, reader.peek[:n]...)
	r.r.Discard(n)
	if n == len(reader.peek) {
		return reader.err
	}
	return nil
}

func (r *RecordReader) readSize(n int) error {
	r.buf = append(r.buf, make([]byte, n)...)
	n, err := io.ReadFull(r.r, r.buf)
	r.buf = r.buf[:n]
	return err
}

func (r *RecordReader) readExact(d []byte) error {
	if err := r.readSize(len(d)); err != nil {
		return err
	}
	if !bytes.Equal(d, r.buf) {
		return fmt.Errorf("exact text mismatch, read %q when expecting %q", r.buf, d)
	}
	return nil
}

func (r *RecordReader) readDelim(d []byte) error {
	// Empty delimiters opt in to reading the rest of the text.
	if len(d) == 0 {
		b, err := io.ReadAll(r.r)
		r.buf = b
		// ReadAll stops at io.EOF, but we need to bubble that up.
		if err == nil {
			return io.EOF
		}
		return err
	}

	// We use the simple inefficient search algorithm, which can be O(nm),
	// but we aren't expecting huge search spaces. Long term we could
	// convert to a two-way search.
	for {
		peek, err := r.r.Peek(len(d))
		if err != nil {
			// If we peek an io.EOF, we were looking for our delim
			// and hit the end. This is unexpected.
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return err
		}
		if !bytes.Equal(peek, d) {
			// We did not find our delim. Skip the first char
			// then continue again.
			r.buf = append(r.buf, peek[0])
			r.r.Discard(1)
			continue
		}
		// We found our delim. We discard it and return.
		r.r.Discard(len(d))
		return nil
	}
}

type jsonReader struct {
	state int8
	n     int8 // misc.
	nexts []int8
}

func (*jsonReader) isHex(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'a', 'b', 'c', 'd', 'e', 'f',
		'A', 'B', 'C', 'D', 'E', 'F':
		return true
	default:
		return false
	}
}

func (*jsonReader) isNum(c byte) bool {
	switch c {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func (*jsonReader) isNat(c byte) bool {
	switch c {
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return true
	}
	return false
}

func (*jsonReader) isE(c byte) bool {
	return c == 'e' || c == 'E'
}

const (
	jrstAny int8 = iota
	jrstObj
	jrstObjSep
	jrstObjFin
	jrstArr
	jrstArrFin
	jrstStrBegin
	jrstStr
	jrstStrEsc
	jrstStrEscU
	jrstTrue
	jrstFalse
	jrstNull
	jrstNeg
	jrstOne
	jrstDotOrE
	jrstDot
	jrstE
)

func (r *jsonReader) read(c byte) (rr int8) {
start:
	switch r.state {
	case jrstAny:
		switch c {
		case ' ', '\t', '\n', '\r':
			return 1 // skip whitespace, need more
		case '{':
			r.state = jrstObj
			return 1 // object open, need more
		case '[':
			r.state = jrstArr
			return 1 // array open, need more
		case '"':
			r.state = jrstStr
			return 1 // string open, need more
		case 't':
			r.state = jrstTrue
			r.n = 0
			return 1 // beginning of true, need more
		case 'f':
			r.state = jrstFalse
			r.n = 0
			return 1 // beginning of false, need more
		case 'n':
			r.state = jrstNull
			r.n = 0
			return 1 // beginning of null, need more
		case '-':
			r.state = jrstNeg
			return 1 // beginning of negative number, need more
		case '0':
			r.state = jrstDotOrE
			return 1 // beginning of 0e or 0., need more
		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			r.state = jrstOne
			return 1 // beginning of number, need more
		default:
			return -2 // invalid json
		}

	case jrstObj:
		switch c {
		case ' ', '\t', '\n', '\r':
			return 1 // skip whitespace in json object, need more
		case '"':
			r.pushState(jrstStr, jrstObjSep)
			return 1 // beginning of object key, need to finish, transition to obj sep
		case '}':
			return r.popState() // end of object, this is valid json end, pop state
		default:
			return -2 // invalid json: expected object key
		}
	case jrstObjSep:
		switch c {
		case ' ', '\t', '\n', '\r':
			return 1 // skip whitespace in json object, need more
		case ':':
			r.pushState(jrstAny, jrstObjFin)
			return 1 // beginning of object value, need to finish, transition to obj fin
		default:
			return -2 // invalid json: expected object separator
		}
	case jrstObjFin:
		switch c {
		case ' ', '\r', '\t', '\n':
			return 1 // skip whitespace in json object, need more
		case ',':
			r.pushState(jrstStrBegin, jrstObjSep)
			return 1 // beginning of new object key, need to finish, transition to obj sep
		case '}':
			return r.popState() // end of object, this is valid json end, pop state
		default:
			return -2 // invalid json
		}

	case jrstArr:
		switch c {
		case ' ', '\r', '\t', '\n':
			return 1 // skip whitespace in json array, need more
		case ']':
			return r.popState() // end of array, this is valid json end, pop state
		default:
			r.pushState(jrstAny, jrstArrFin)
			goto start // array value began: immediately transition to it
		}
	case jrstArrFin:
		switch c {
		case ' ', '\r', '\t', '\n':
			return 1 // skip whitespace in json array, need more
		case ',':
			r.state = jrstArr
			return 1 // beginning of new array value, need more
		case ']':
			return r.popState() // end of array, this is valid json end, pop state
		default:
			return -2 // invalid json
		}

	case jrstStrBegin:
		switch c {
		case ' ', '\r', '\t', '\n':
			return 1 // skip whitespace in json object (before beginning of key), need more
		case '"':
			r.state = jrstStr
			return 1 // beginning of object key, need more
		default:
			return -2 // invalid json
		}

	case jrstStr:
		switch c {
		case 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
			20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31:
			return -2 // invalid json: control characters not allowed in string
		case '"':
			return r.popState() // end of string, this is valid json end, pop state
		case '\\':
			r.state = jrstStrEsc
			return 1 // beginning of escape sequence, need more
		default:
			return 1 // continue string, need more
		}
	case jrstStrEsc:
		switch c {
		case 'b', 'f', 'n', 'r', 't', '\\', '/', '"':
			r.state = jrstStr
			return 1 // end of escape sequence, still need to finish string
		case 'u':
			r.state = jrstStrEscU
			r.n = 0
			return 1 // beginning of unicode escape sequence, need more
		default:
			return -2 // invalid json: invalid escape sequence
		}
	case jrstStrEscU:
		if !r.isHex(c) {
			return -2 // invalid json: invalid unicode escape sequence
		}
		r.n++
		if r.n == 4 {
			r.state = jrstStr
		}
		return 1 // end of unicode escape sequence, still need to finish string

	case jrstTrue:
		switch {
		case r.n == 0 && c == 'r':
			r.n++
			return 1
		case r.n == 1 && c == 'u':
			r.n++
			return 1
		case r.n == 2 && c == 'e':
			return r.popState() // end of true, this is valid json end, pop state
		}
	case jrstFalse:
		switch {
		case r.n == 0 && c == 'a':
			r.n++
			return 1
		case r.n == 1 && c == 'l':
			r.n++
			return 1
		case r.n == 2 && c == 's':
			r.n++
			return 1
		case r.n == 3 && c == 'e':
			return r.popState() // end of false, this is valid json end, pop state
		}
	case jrstNull:
		switch {
		case r.n == 0 && c == 'u':
			r.n++
			return 1
		case r.n == 1 && c == 'l':
			r.n++
			return 1
		case r.n == 2 && c == 'l':
			return r.popState() // end of null, this is valid json end, pop state
		}

	case jrstNeg:
		if c == '0' {
			r.state = jrstDotOrE
			return r.oneOrTwo() // beginning of -0, need to see if there is more (potentially end)
		} else if r.isNat(c) {
			r.state = jrstOne
			return r.oneOrTwo() // beginning of -1 (or 2,3,..9), need to see if there is more (potentially end)
		}
		return -2 // invalid, -a or something
	case jrstOne:
		if r.isNum(c) {
			return r.oneOrTwo() // continue the number (potentially end)
		}
		fallthrough // not a number, check if e or .
	case jrstDotOrE:
		if r.isE(c) {
			r.state = jrstE
			return 1 // beginning of exponent, need more
		}
		if c == '.' {
			r.state = jrstDot
			r.n = 0
			return 1 // beginning of dot, need more
		}
		if r.popStateToStart() {
			goto start
		}
		return -1 // done with number, no more state to bubble to: we are done

	case jrstDot:
		switch r.n {
		case 0:
			if !r.isNum(c) {
				return -2 // first char after dot must be a number
			}
			r.n = 1
			return r.oneOrTwo() // saw number, keep and continue (potentially end)
		case 1:
			if r.isNum(c) {
				return r.oneOrTwo() // more number, keep and continue (potentially end)
			}
			if r.isE(c) {
				r.state = jrstE
				r.n = 0
				return 1 // beginning of exponent (-0.1e), need more
			}
			if r.popStateToStart() {
				goto start
			}
			return -1 // done with number, no more state to bubble to: we are done
		}
	case jrstE:
		switch r.n {
		case 0:
			if c == '+' || c == '-' {
				r.n = 1
				return 1 // beginning of exponent sign, need more
			}
			fallthrough
		case 1:
			if !r.isNum(c) {
				return -2 // first char after exponent must be sign or number
			}
			r.n = 2
			return r.oneOrTwo() // saw number, keep and continue (potentially end)
		case 2:
			if r.isNum(c) {
				return r.oneOrTwo() // more number, keep and continue (potentially end)
			}
			if r.popStateToStart() {
				goto start
			}
			return -1 // done with number, no more state to bubble to: we are done
		}
	}
	return -2 // unknown state
}

func (r *jsonReader) pushState(next, next2 int8) {
	r.nexts = append(r.nexts, next2)
	r.state = next
}

func (r *jsonReader) popState() int8 {
	if len(r.nexts) == 0 {
		r.state = jrstAny
		return 0
	}
	r.state = r.nexts[len(r.nexts)-1]
	r.nexts = r.nexts[:len(r.nexts)-1]
	return 1
}

func (r *jsonReader) popStateToStart() bool {
	if len(r.nexts) == 0 {
		r.state = jrstAny
		return false
	}
	r.state = r.nexts[len(r.nexts)-1]
	r.nexts = r.nexts[:len(r.nexts)-1]
	return true
}

func (r *jsonReader) oneOrTwo() int8 {
	if len(r.nexts) > 0 {
		return 1
	}
	return 2
}

////////////
// COMMON //
////////////

func parseLayoutSlash(layout string) (byte, int, error) {
	if len(layout) == 0 {
		return 0, 0, errors.New("invalid slash escape at end of delim string")
	}
	switch layout[0] {
	case 't':
		return '\t', 1, nil
	case 'n':
		return '\n', 1, nil
	case 'r':
		return '\r', 1, nil
	case '\\':
		return '\\', 1, nil
	case 'x':
		if len(layout) < 3 { // on x, need two more
			return 0, 0, errors.New("invalid non-terminated hex escape sequence at end of delim string")
		}
		hex := layout[1:3]
		n, err := strconv.ParseInt(hex, 16, 8)
		if err != nil {
			return 0, 0, fmt.Errorf("unable to parse hex escape sequence %q: %v", hex, err)
		}
		return byte(n), 3, nil
	default:
		return 0, 0, fmt.Errorf("unknown slash escape sequence %q", layout[:1])
	}
}
