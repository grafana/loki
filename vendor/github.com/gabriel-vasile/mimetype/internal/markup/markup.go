// Package markup implements functions for extracting info from
// HTML and XML documents.
package markup

import (
	"bytes"

	"github.com/gabriel-vasile/mimetype/internal/scan"
)

// GetAnAttribute assumes we passed over an SGML tag and extracts first
// attribute and its value.
//
// Initially, this code existed inside charset/charset.go, because it was part of
// implementing the https://html.spec.whatwg.org/multipage/parsing.html#prescan-a-byte-stream-to-determine-its-encoding
// algorithm. But because extracting an attribute from a tag is the same for
// both HTML and XML, then the code was moved here.
func GetAnAttribute(s *scan.Bytes) (name, val []byte, hasMore bool) {
	for scan.ByteIsWS(s.Peek()) || s.Peek() == '/' {
		s.Advance(1)
	}
	if s.Peek() == '>' {
		return nil, nil, false
	}
	origS, end := *s, 0
	// step 4 and 5
	for {
		// bap means byte at position in the specification.
		bap := s.Pop()
		if bap == 0 {
			return nil, nil, false
		}
		if bap == '=' && end > 0 {
			val, hasMore := getAValue(s)
			return origS[:end], val, hasMore
		} else if scan.ByteIsWS(bap) {
			for scan.ByteIsWS(s.Peek()) {
				s.Advance(1)
			}
			if s.Peek() != '=' {
				return origS[:end], nil, true
			}
			s.Advance(1)
			for scan.ByteIsWS(s.Peek()) {
				s.Advance(1)
			}
			val, hasMore := getAValue(s)
			return origS[:end], val, hasMore
		} else if bap == '/' || bap == '>' {
			return origS[:end], nil, false
		} else { // for any ASCII, non-ASCII, just advance
			end++
		}
	}
}

func getAValue(s *scan.Bytes) (_ []byte, hasMore bool) {
	for scan.ByteIsWS(s.Peek()) {
		s.Advance(1)
	}
	origS, end := *s, 0
	bap := s.Pop()
	if bap == 0 {
		return nil, false
	}
	end++
	// Step 10
	switch bap {
	case '"', '\'':
		val := s.PopUntil(bap)
		if s.Pop() != bap {
			return nil, false
		}
		return val, s.Peek() != 0 && s.Peek() != '>'
	case '>':
		return nil, false
	}

	// Step 11
	for {
		bap = s.Pop()
		if bap == 0 {
			return nil, false
		}
		switch {
		case scan.ByteIsWS(bap):
			return origS[:end], true
		case bap == '>':
			return origS[:end], false
		default:
			end++
		}
	}
}

func SkipAComment(s *scan.Bytes) (skipped bool) {
	if bytes.HasPrefix(*s, []byte("<!--")) {
		// Offset by 2 len(<!) because the starting and ending -- can be the same.
		if i := bytes.Index((*s)[2:], []byte("-->")); i != -1 {
			s.Advance(i + 2 + 3) // 2 comes from len(<!) and 3 comes from len(-->).
			return true
		}
	}
	return false
}
