package proto

import (
	"bytes"

	"github.com/redis/go-redis/v9/internal/util"
)

// maxBufferedFrameDepth bounds the nesting HasBufferedReply follows. A deeper
// reply reports false, which is always safe: the caller then reads it the
// normal way.
const maxBufferedFrameDepth = 32

// HasBufferedReply reports whether the buffer already holds one complete
// reply, with any push frames and attributes in front of it, so reading that
// reply needs no socket read. It does not read from the socket and consumes
// nothing. A frame it does not know, a malformed one, or a streamed aggregate
// reports false; the caller then reads it the normal way, which reports any
// error.
func (r *Reader) HasBufferedReply() bool {
	b, _ := r.rd.Peek(r.rd.Buffered())
	for len(b) > 0 {
		n, ok := bufferedFrameLen(b, 0)
		if !ok {
			return false
		}
		t := b[0]
		b = b[n:]
		if t != RespPush && t != RespAttr {
			return true
		}
	}
	return false
}

// bufferedValueLen returns the length of one complete value at the start of
// b, including the attribute frames that may prefix it: inside an aggregate an
// attribute decorates the element after it and is not an element itself.
func bufferedValueLen(b []byte, depth int) (int, bool) {
	off := 0
	for {
		n, ok := bufferedFrameLen(b[off:], depth)
		if !ok {
			return 0, false
		}
		t := b[off]
		off += n
		if t != RespAttr {
			return off, true
		}
	}
}

// bufferedFrameLen returns the length of the complete frame at the start of
// b. ok is false when b holds only part of it or the frame is not valid.
func bufferedFrameLen(b []byte, depth int) (n int, ok bool) {
	if depth > maxBufferedFrameDepth {
		return 0, false
	}
	i := bytes.IndexByte(b, '\n')
	if i < 2 || b[i-1] != '\r' {
		return 0, false
	}
	line := b[:i-1]
	hdr := i + 1
	switch line[0] {
	case RespStatus, RespError, RespInt, RespNil, RespFloat, RespBool, RespBigInt:
		return hdr, true
	}
	cnt, err := util.Atoi(line[1:])
	if err != nil || cnt < -1 {
		return 0, false
	}
	switch line[0] {
	case RespString, RespVerbatim, RespBlobError:
		if cnt == -1 {
			return hdr, true
		}
		if cnt > len(b)-hdr-2 {
			return 0, false
		}
		end := hdr + cnt + 2
		if b[end-2] != '\r' || b[end-1] != '\n' {
			return 0, false
		}
		return end, true
	case RespArray, RespSet, RespPush, RespMap, RespAttr:
		if cnt == -1 {
			return hdr, true
		}
		per := 1
		if line[0] == RespMap || line[0] == RespAttr {
			per = 2
		}
		off := hdr
		// Every element takes at least 3 bytes, so a count the buffer cannot
		// hold ends the loop early instead of running cnt times.
		for k := 0; k < cnt; k++ {
			for e := 0; e < per; e++ {
				m, ok := bufferedValueLen(b[off:], depth+1)
				if !ok {
					return 0, false
				}
				off += m
			}
		}
		return off, true
	}
	return 0, false
}
