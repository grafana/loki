package framedstdcopy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/docker/docker/pkg/stdcopy"
)

const (
	// From stdcopy
	stdWriterPrefixLen = 8
	stdWriterFdIndex   = 0
	stdWriterSizeIndex = 4
	startingBufLen     = 32*1024 + stdWriterPrefixLen + 1
	maxFrameLen        = 16384 + 31 // In practice (undocumented) frame payload can be timestamp + 16k
)

// FramedStdCopy is a modified version of stdcopy.StdCopy.
// FramedStdCopy will demultiplex `src` in the same manner as StdCopy, but instead of
// using io.Writer for outputs, channels are used, since each frame payload may contain
// its own inner header (notably, timestamps). Frame payloads are not further parsed here,
// but are passed raw as individual slices through the output channel.
//
// FramedStdCopy will read until it hits EOF on `src`. It will then return a nil error.
// In other words: if `err` is non nil, it indicates a real underlying error.
//
// `written` will hold the total number of bytes written to `dstout` and `dsterr`.
func FramedStdCopy(dstout, dsterr chan []byte, src io.Reader) (written int64, err error) {
	var (
		buf       = make([]byte, startingBufLen)
		bufLen    = len(buf)
		nr        int
		er        error
		out       chan []byte
		frameSize int
	)

	for {
		// Make sure we have at least a full header
		for nr < stdWriterPrefixLen {
			var nr2 int
			nr2, er = src.Read(buf[nr:])
			nr += nr2
			if er == io.EOF {
				if nr < stdWriterPrefixLen {
					return written, nil
				}
				break
			}
			if er != nil {
				return 0, er
			}
		}

		stream := stdcopy.StdType(buf[stdWriterFdIndex])
		// Check the first byte to know where to write
		switch stream {
		case stdcopy.Stdin:
			fallthrough
		case stdcopy.Stdout:
			// Write on stdout
			out = dstout
		case stdcopy.Stderr:
			// Write on stderr
			out = dsterr
		case stdcopy.Systemerr:
			// If we're on Systemerr, we won't write anywhere.
			// NB: if this code changes later, make sure you don't try to write
			// to outstream if Systemerr is the stream
			out = nil
		default:
			return 0, fmt.Errorf("Unrecognized input header: %d", buf[stdWriterFdIndex])
		}

		// Retrieve the size of the frame
		frameSize = int(binary.BigEndian.Uint32(buf[stdWriterSizeIndex : stdWriterSizeIndex+4]))

		// Check if the buffer is big enough to read the frame.
		// Extend it if necessary.
		if frameSize+stdWriterPrefixLen > bufLen {
			buf = append(buf, make([]byte, frameSize+stdWriterPrefixLen-bufLen+1)...)
			bufLen = len(buf)
		}

		// While the amount of bytes read is less than the size of the frame + header, we keep reading
		for nr < frameSize+stdWriterPrefixLen {
			var nr2 int
			nr2, er = src.Read(buf[nr:])
			nr += nr2
			if er == io.EOF {
				if nr < frameSize+stdWriterPrefixLen {
					return written, nil
				}
				break
			}
			if er != nil {
				return 0, er
			}
		}

		// we might have an error from the source mixed up in our multiplexed
		// stream. if we do, return it.
		if stream == stdcopy.Systemerr {
			return written, fmt.Errorf("error from daemon in stream: %s", string(buf[stdWriterPrefixLen:frameSize+stdWriterPrefixLen]))
		}

		// Write the retrieved frame (without header)
		var newBuf = make([]byte, frameSize)
		copy(newBuf, buf[stdWriterPrefixLen:])
		out <- newBuf
		written += int64(frameSize)

		// Move the rest of the buffer to the beginning
		copy(buf, buf[frameSize+stdWriterPrefixLen:nr])
		// Move the index
		nr -= frameSize + stdWriterPrefixLen
	}
}

// Specialized version of FramedStdCopy for when frames have no headers.
// This will happen for output from a container that has TTY set.
// In theory this makes it impossible to find the frame boundaries, which also does not matter if timestamps were not requested,
// but if they were requested, they will still be there at the start of every frame, which might be mid-line.
// In practice we can find most boundaries by looking for newlines, since these result in a new frame.
// Otherwise we rely on using the same max frame size as used in practice by docker.
func NoHeaderFramedStdCopy(dstout chan []byte, src io.Reader) (written int64, err error) {
	var (
		buf    = make([]byte, 32768)
		nrLine int
		nr     int
		nr2    int
		er     error
	)
	for {
		nr2, er = src.Read(buf[nr:])
		if er == io.EOF && nr2 == 0 {
			return written, nil
		} else if er != nil {
			return written, er
		}
		nr += nr2

		// We might have read multiple frames, output all those we find in the buffer
		for nr > 0 {
			nrLine = bytes.Index(buf[:nr], []byte("\n")) + 1
			if nrLine > maxFrameLen {
				// we found a newline but it's in the next frame (most likely)
				nrLine = maxFrameLen
			} else if nrLine < 1 {
				if nr >= maxFrameLen {
					nrLine = maxFrameLen
				} else {
					// no end of frame found and we don't have enough bytes
					break
				}
			}

			// Write the frame
			var newBuf = make([]byte, nrLine)
			copy(newBuf, buf)
			dstout <- newBuf
			written += int64(nrLine)

			// Move the rest of the buffer to the beginning
			copy(buf, buf[nrLine:nr])
			// Move the index
			nr -= nrLine
		}
	}
}
