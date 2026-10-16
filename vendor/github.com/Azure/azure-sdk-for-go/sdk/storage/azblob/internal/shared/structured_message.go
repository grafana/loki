// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package shared

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"math"
)

// Structured Message (XSM/1.0) constants
const (
	SMVersion   uint8  = 1
	SMFlagCRC64 uint16 = 0x0001

	SMDefaultSegmentSize = 4 * 1024 * 1024 // 4MB

	SMHeaderSize         = 13 // version(1) + msgLen(8) + flags(2) + numSegments(2)
	SMSegmentHeaderSize  = 10 // segNum(2) + dataLen(8)
	SMSegmentFooterSize  = 8  // CRC64
	SMMessageTrailerSize = 8  // CRC64

	SMHeaderValue = "XSM/1.0; properties=crc64"
)

// SMEncodeResult holds the result of encoding data into structured message format.
type SMEncodeResult struct {
	// EncodedData is the complete structured message binary payload.
	EncodedData []byte
	// OriginalContentLength is the length of the original unframed data.
	OriginalContentLength int64
}

// SMEncode encodes raw data into structured message format.
// segmentSize specifies the maximum segment size (use 0 for default 4MB).
// Returns the full SM binary payload and the original content length.
func SMEncode(data []byte, segmentSize int) SMEncodeResult {
	if segmentSize <= 0 {
		segmentSize = SMDefaultSegmentSize
	}

	totalDataLen := len(data)
	numSegments := totalDataLen / segmentSize
	if totalDataLen%segmentSize != 0 {
		numSegments++
	}
	if numSegments == 0 {
		numSegments = 1
	}

	// Calculate total message length
	msgLen := int64(SMHeaderSize)
	for i := 0; i < numSegments; i++ {
		segStart := i * segmentSize
		segEnd := segStart + segmentSize
		if segEnd > totalDataLen {
			segEnd = totalDataLen
		}
		segDataLen := segEnd - segStart
		msgLen += int64(SMSegmentHeaderSize) + int64(segDataLen) + int64(SMSegmentFooterSize)
	}
	msgLen += int64(SMMessageTrailerSize)

	var buf bytes.Buffer
	buf.Grow(int(msgLen))

	// The return values are intentionally discarded.
	_ = buf.WriteByte(SMVersion)
	_ = binary.Write(&buf, binary.LittleEndian, msgLen)
	_ = binary.Write(&buf, binary.LittleEndian, SMFlagCRC64)
	_ = binary.Write(&buf, binary.LittleEndian, uint16(numSegments))

	// Compute message-level CRC64 over all raw data
	messageCRC := crc64.Checksum(data, CRC64Table)

	for i := 0; i < numSegments; i++ {
		segStart := i * segmentSize
		segEnd := segStart + segmentSize
		if segEnd > totalDataLen {
			segEnd = totalDataLen
		}
		segData := data[segStart:segEnd]

		// Segment Header (10 bytes)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(i+1))
		_ = binary.Write(&buf, binary.LittleEndian, uint64(len(segData)))

		// Segment Data
		_, _ = buf.Write(segData)

		// Segment Footer - CRC64 of segment data (8 bytes)
		segCRC := crc64.Checksum(segData, CRC64Table)
		_ = binary.Write(&buf, binary.LittleEndian, segCRC)
	}

	// Message Trailer - CRC64 of all raw content (8 bytes)
	_ = binary.Write(&buf, binary.LittleEndian, messageCRC)

	return SMEncodeResult{
		EncodedData:           buf.Bytes(),
		OriginalContentLength: int64(totalDataLen),
	}
}

// SMDecodeResult holds the result of decoding a structured message.
type SMDecodeResult struct {
	// Data is the extracted raw content.
	Data []byte
	// Version is the SM protocol version.
	Version uint8
	// Flags are the SM flags (e.g., SMFlagCRC64).
	Flags uint16
	// NumSegments is the number of segments in the message.
	NumSegments uint16
}

type smDecodedSegment struct {
	num        uint16
	data       []byte
	nextOffset int

	expectedCRC uint64
	hasCRC      bool
}

func decodeSMSegment(smData []byte, offset int, expectedSegmentNum uint16, hasCRC bool) (smDecodedSegment, error) {
	if offset+SMSegmentHeaderSize > len(smData) {
		return smDecodedSegment{}, fmt.Errorf("segment %d: insufficient data for segment header at offset %d", expectedSegmentNum, offset)
	}

	segNum := binary.LittleEndian.Uint16(smData[offset : offset+2])
	segDataLen := binary.LittleEndian.Uint64(smData[offset+2 : offset+10])
	offset += SMSegmentHeaderSize

	if segNum != expectedSegmentNum {
		return smDecodedSegment{}, fmt.Errorf("segment number mismatch: expected %d, got %d", expectedSegmentNum, segNum)
	}

	maxSegmentDataLen := uint64(int(^uint(0) >> 1))
	if segDataLen > maxSegmentDataLen {
		return smDecodedSegment{}, fmt.Errorf("segment %d: data length %d exceeds supported size", segNum, segDataLen)
	}

	remaining := uint64(len(smData) - offset)
	footerSize := uint64(0)
	if hasCRC {
		footerSize = SMSegmentFooterSize
	}

	if segDataLen > remaining || remaining-segDataLen < footerSize {
		return smDecodedSegment{}, fmt.Errorf("segment %d: insufficient data for segment content (need %d bytes at offset %d, have %d)", segNum, segDataLen, offset, len(smData))
	}

	dataEnd := offset + int(segDataLen)
	segment := smDecodedSegment{
		num:        segNum,
		data:       smData[offset:dataEnd],
		nextOffset: dataEnd,
		hasCRC:     hasCRC,
	}

	if hasCRC {
		segment.expectedCRC = binary.LittleEndian.Uint64(smData[dataEnd : dataEnd+SMSegmentFooterSize])
		segment.nextOffset += SMSegmentFooterSize
	}

	return segment, nil
}

// SMDecode decodes a structured message binary payload and validates CRC64 checksums.
// Returns the extracted raw data or an error if the SM is malformed or CRC validation fails.
func SMDecode(smData []byte) (SMDecodeResult, error) {
	if len(smData) < SMHeaderSize {
		return SMDecodeResult{}, fmt.Errorf("structured message too short for header: %d bytes (minimum %d)", len(smData), SMHeaderSize)
	}

	// Parse Message Header
	version := smData[0]
	if version != SMVersion {
		return SMDecodeResult{}, fmt.Errorf("unsupported structured message version: %d (expected %d)", version, SMVersion)
	}

	msgLen := binary.LittleEndian.Uint64(smData[1:9])
	if uint64(len(smData)) != msgLen {
		return SMDecodeResult{}, fmt.Errorf("structured message length mismatch: header says %d, got %d bytes", msgLen, len(smData))
	}

	flags := binary.LittleEndian.Uint16(smData[9:11])
	numSegments := binary.LittleEndian.Uint16(smData[11:13])
	// The structured message body is negotiated with properties=crc64, so the CRC64 flag
	// must be present. Rejecting its absence prevents silently skipping validation. Only the
	// CRC64 bit is required; other flag bits are ignored to preserve forward compatibility.
	if flags&SMFlagCRC64 == 0 {
		return SMDecodeResult{}, fmt.Errorf("structured message is missing the required CRC64 flag (flags=0x%04x)", flags)
	}
	hasCRC := flags&SMFlagCRC64 != 0

	offset := SMHeaderSize
	var result []byte
	var msgHasher hash64
	if hasCRC {
		msgHasher = crc64.New(CRC64Table)
	}

	for i := uint16(1); i <= numSegments; i++ {
		segment, err := decodeSMSegment(smData, offset, i, hasCRC)
		if err != nil {
			return SMDecodeResult{}, err
		}

		result = append(result, segment.data...)
		offset = segment.nextOffset

		if !segment.hasCRC {
			continue
		}

		actualCRC := crc64.Checksum(segment.data, CRC64Table)
		if segment.expectedCRC != actualCRC {
			return SMDecodeResult{}, fmt.Errorf("segment %d: CRC64 mismatch (expected 0x%016x, got 0x%016x)", segment.num, segment.expectedCRC, actualCRC)
		}

		_, _ = msgHasher.Write(segment.data)
	}

	if hasCRC {
		if offset+SMMessageTrailerSize > len(smData) {
			return SMDecodeResult{}, fmt.Errorf("insufficient data for message trailer CRC64 at offset %d", offset)
		}

		expectedMsgCRC := binary.LittleEndian.Uint64(smData[offset : offset+8])
		actualMsgCRC := msgHasher.Sum64()
		if expectedMsgCRC != actualMsgCRC {
			return SMDecodeResult{}, fmt.Errorf("message trailer CRC64 mismatch (expected 0x%016x, got 0x%016x)", expectedMsgCRC, actualMsgCRC)
		}
		offset += SMMessageTrailerSize
	}

	// The parsed message must consume the entire input. Declaring fewer segments and appending
	// arbitrary trailing bytes would otherwise pass the header length check yet leave those bytes
	// unvalidated, so reject any leftover data.
	if offset != len(smData) {
		return SMDecodeResult{}, fmt.Errorf("structured message has %d unexpected trailing bytes after offset %d", len(smData)-offset, offset)
	}

	return SMDecodeResult{
		Data:        result,
		Version:     version,
		Flags:       flags,
		NumSegments: numSegments,
	}, nil
}

type hash64 interface {
	Write(p []byte) (n int, err error)
	Sum64() uint64
}

// --- Streaming Encoder ---

// encoder state for the state-machine based SMEncoder
type encoderState int

const (
	encStateHeader    encoderState = iota // emitting the 13-byte message header
	encStateSegHeader                     // emitting a 10-byte segment header
	encStateSegData                       // streaming segment data from inner source
	encStateSegFooter                     // emitting an 8-byte segment CRC footer
	encStateTrailer                       // emitting the 8-byte message trailer CRC
	encStateDone                          // all bytes emitted
)

// SMEncoder wraps an io.ReadSeeker source and produces SM-encoded output on Read().
// CRC64 checksums are computed on-the-fly as content is read from the inner source.
// Supports Seek(0, io.SeekStart) for retry.
type SMEncoder struct {
	inner       io.ReadSeeker
	contentLen  int64 // total original content length
	segmentSize int
	numSegments int
	encodedLen  int64

	state      encoderState
	segIndex   int    // current segment (0-based)
	segRemain  int64  // bytes remaining in current segment data
	segCRC     hash64 // per-segment CRC hasher
	msgCRC     hash64 // message-level CRC hasher
	pending    []byte // buffered framing bytes (headers/footers) being drained
	pendingOff int    // read offset into pending
	pos        int64  // current read position in the encoded output
}

// NewSMEncoder creates a streaming encoder that wraps the given content source.
// contentLen is the total size of the content (must be known upfront for the SM header).
// segmentSize specifies the max segment size; use 0 for the default (4MB).
func NewSMEncoder(inner io.ReadSeeker, contentLen int64, segmentSize int) *SMEncoder {
	if segmentSize <= 0 {
		segmentSize = SMDefaultSegmentSize
	}

	numSegments := int(contentLen) / segmentSize
	if int(contentLen)%segmentSize != 0 {
		numSegments++
	}
	if numSegments == 0 {
		numSegments = 1
	}

	// Segment count is stored as uint16 in the SM header. If the content is too large
	// for the given segment size, auto-increase segment size to fit within uint16 max.
	if numSegments > math.MaxUint16 {
		segmentSize = int(math.Ceil(float64(contentLen) / float64(math.MaxUint16)))
		numSegments = int(contentLen) / segmentSize
		if int(contentLen)%segmentSize != 0 {
			numSegments++
		}
	}

	// Calculate total encoded length
	encodedLen := int64(SMHeaderSize)
	remaining := contentLen
	for i := 0; i < numSegments; i++ {
		segDataLen := int64(segmentSize)
		if segDataLen > remaining {
			segDataLen = remaining
		}
		encodedLen += int64(SMSegmentHeaderSize) + segDataLen + int64(SMSegmentFooterSize)
		remaining -= segDataLen
	}
	encodedLen += int64(SMMessageTrailerSize)

	e := &SMEncoder{
		inner:       inner,
		contentLen:  contentLen,
		segmentSize: segmentSize,
		numSegments: numSegments,
		encodedLen:  encodedLen,
	}
	e.initState()
	return e
}

func (e *SMEncoder) initState() {
	e.state = encStateHeader
	e.segIndex = 0
	e.segRemain = 0
	e.segCRC = nil
	e.msgCRC = crc64.New(CRC64Table)
	e.pending = nil
	e.pendingOff = 0
	e.pos = 0

	// Build the 13-byte message header
	hdr := make([]byte, SMHeaderSize)
	hdr[0] = SMVersion
	binary.LittleEndian.PutUint64(hdr[1:9], uint64(e.encodedLen))
	binary.LittleEndian.PutUint16(hdr[9:11], SMFlagCRC64)
	binary.LittleEndian.PutUint16(hdr[11:13], uint16(e.numSegments))
	e.pending = hdr
	e.pendingOff = 0
}

func (e *SMEncoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	totalRead := 0
	for totalRead < len(p) {
		switch e.state {
		case encStateHeader:
			n := e.drainPending(p[totalRead:])
			totalRead += n
			e.pos += int64(n)
			if e.pendingOff >= len(e.pending) {
				e.advanceToNextSegment()
			}

		case encStateSegHeader:
			n := e.drainPending(p[totalRead:])
			totalRead += n
			e.pos += int64(n)
			if e.pendingOff >= len(e.pending) {
				e.state = encStateSegData
			}

		case encStateSegData:
			// Read from inner source, up to segment remaining and caller buffer
			toRead := int64(len(p) - totalRead)
			if toRead > e.segRemain {
				toRead = e.segRemain
			}
			n, err := e.inner.Read(p[totalRead : totalRead+int(toRead)])
			if n > 0 {
				_, _ = e.segCRC.Write(p[totalRead : totalRead+n])
				_, _ = e.msgCRC.Write(p[totalRead : totalRead+n])
				totalRead += n
				e.pos += int64(n)
				e.segRemain -= int64(n)
			}
			if e.segRemain == 0 {
				// Segment data fully read — emit footer
				footer := make([]byte, SMSegmentFooterSize)
				binary.LittleEndian.PutUint64(footer, e.segCRC.Sum64())
				e.pending = footer
				e.pendingOff = 0
				e.state = encStateSegFooter
			}
			if err != nil {
				finalContentBoundary := e.segRemain == 0 && e.segIndex == e.numSegments
				if errors.Is(err, io.EOF) {
					// EOF is only valid exactly at the final declared content boundary. A premature EOF
					// means the source produced fewer bytes than the declared content length; surface it
					// as io.ErrUnexpectedEOF so callers (e.g. io.ReadAll) don't accept a truncated message.
					if !finalContentBoundary {
						return totalRead, io.ErrUnexpectedEOF
					}
				} else {
					// Any non-EOF error is propagated so a failed read is never encoded as a valid message.
					return totalRead, err
				}
			}

		case encStateSegFooter:
			n := e.drainPending(p[totalRead:])
			totalRead += n
			e.pos += int64(n)
			if e.pendingOff >= len(e.pending) {
				e.advanceToNextSegment()
			}

		case encStateTrailer:
			n := e.drainPending(p[totalRead:])
			totalRead += n
			e.pos += int64(n)
			if e.pendingOff >= len(e.pending) {
				e.state = encStateDone
			}

		case encStateDone:
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, io.EOF
		}
	}
	return totalRead, nil
}

func (e *SMEncoder) advanceToNextSegment() {
	if e.segIndex >= e.numSegments {
		// All segments done — emit trailer
		trailer := make([]byte, SMMessageTrailerSize)
		binary.LittleEndian.PutUint64(trailer, e.msgCRC.Sum64())
		e.pending = trailer
		e.pendingOff = 0
		e.state = encStateTrailer
		return
	}

	// Calculate this segment's data length
	segDataLen := int64(e.segmentSize)
	consumed := int64(e.segIndex) * int64(e.segmentSize)
	if consumed+segDataLen > e.contentLen {
		segDataLen = e.contentLen - consumed
	}

	// Build segment header
	hdr := make([]byte, SMSegmentHeaderSize)
	binary.LittleEndian.PutUint16(hdr[0:2], uint16(e.segIndex+1))
	binary.LittleEndian.PutUint64(hdr[2:10], uint64(segDataLen))
	e.pending = hdr
	e.pendingOff = 0

	e.segRemain = segDataLen
	e.segCRC = crc64.New(CRC64Table)
	e.segIndex++
	e.state = encStateSegHeader
}

func (e *SMEncoder) drainPending(dst []byte) int {
	n := copy(dst, e.pending[e.pendingOff:])
	e.pendingOff += n
	return n
}

func (e *SMEncoder) Seek(offset int64, whence int) (int64, error) {
	// Support Seek(0, SeekEnd) to report total encoded length.
	// This is needed by ValidateSeekableStreamAt0AndGetCount to compute Content-Length.
	if offset == 0 && whence == io.SeekEnd {
		return e.encodedLen, nil
	}
	// Support Seek(0, SeekCurrent) to report current position in the encoded output.
	if offset == 0 && whence == io.SeekCurrent {
		return e.pos, nil
	}
	if offset != 0 || whence != io.SeekStart {
		return 0, fmt.Errorf("SMEncoder: unsupported Seek(%d, %d); only Seek(0, SeekStart), Seek(0, SeekEnd), and Seek(0, SeekCurrent) are supported", offset, whence)
	}
	// Reset inner source to beginning
	if _, err := e.inner.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	e.initState()
	return 0, nil
}

func (e *SMEncoder) Close() error {
	if closer, ok := e.inner.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// OriginalContentLength returns the length of the original unframed content.
func (e *SMEncoder) OriginalContentLength() int64 {
	return e.contentLen
}

// EncodedLength returns the total length of the SM-encoded output.
func (e *SMEncoder) EncodedLength() int64 {
	return e.encodedLen
}

// --- Streaming Decoder ---

// decoder state for the state-machine based SMDecoder
type decoderState int

const (
	decStateHeader    decoderState = iota // reading the 13-byte message header
	decStateSegHeader                     // reading a 10-byte segment header
	decStateSegData                       // streaming segment data to caller
	decStateSegFooter                     // reading an 8-byte segment CRC footer
	decStateTrailer                       // reading the 8-byte message trailer CRC
	decStateDone                          // all data consumed and validated
	decStateError                         // terminal error state
)

// SMDecoder wraps an SM-encoded io.ReadCloser and yields raw content on Read().
// Framing is parsed incrementally and CRC64 checksums are validated per-segment on-the-fly.
type SMDecoder struct {
	source io.ReadCloser

	state     decoderState
	frameBuf  []byte // small buffer for accumulating framing bytes
	frameNeed int    // how many bytes we need to accumulate
	frameHave int    // how many bytes accumulated so far

	// Message-level metadata (populated after header is parsed)
	version     uint8
	flags       uint16
	numSegments uint16
	hasCRC      bool
	msgLen      uint64

	// Segment tracking
	segIndex  uint16 // 1-based segment number currently being processed
	segRemain int64  // bytes remaining in current segment data
	segCRC    hash64 // per-segment CRC hasher
	msgCRC    hash64 // message-level CRC hasher
	bytesRead int64  // total bytes read from source (for length validation)

	err error
}

// NewSMDecoder creates a streaming decoder that wraps an SM-encoded body.
// CRC64 checksums are validated per-segment as data flows through.
func NewSMDecoder(body io.ReadCloser) *SMDecoder {
	d := &SMDecoder{
		source:    body,
		state:     decStateHeader,
		frameBuf:  make([]byte, SMHeaderSize), // reused for all framing reads
		frameNeed: SMHeaderSize,
		frameHave: 0,
	}
	return d
}

func (d *SMDecoder) Read(p []byte) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	if len(p) == 0 {
		return 0, nil
	}

	totalOut := 0
	// NOTE: framing states (header, segment header/footer, trailer) are processed even once the
	// caller's buffer is full, because that framing is consumed from the source into an internal
	// buffer rather than into p. Only segment *data* requires space in p. This guarantees the final
	// segment footer and message trailer CRC64 are drained and validated within the same Read call
	// that emits the last payload byte, so a bounded reader (e.g. RetryReader) cannot report EOF and
	// skip validation when the payload happens to fill the buffer exactly.
	for {
		switch d.state {
		case decStateHeader:
			done, err := d.fillFrame()
			if err != nil {
				d.setError(err)
				return totalOut, d.err
			}
			if !done {
				return totalOut, nil
			}
			if err := d.parseHeader(); err != nil {
				d.setError(err)
				return totalOut, d.err
			}

		case decStateSegHeader:
			done, err := d.fillFrame()
			if err != nil {
				d.setError(err)
				return totalOut, d.err
			}
			if !done {
				return totalOut, nil
			}
			if err := d.parseSegmentHeader(); err != nil {
				d.setError(err)
				return totalOut, d.err
			}

		case decStateSegData:
			if d.segRemain > 0 {
				if totalOut >= len(p) {
					// Caller's buffer is full and more segment data is pending;
					// the remainder will be produced on the next Read.
					return totalOut, nil
				}
				// Read segment data from source, pass through to caller
				toRead := int64(len(p) - totalOut)
				if toRead > d.segRemain {
					toRead = d.segRemain
				}
				n, err := d.source.Read(p[totalOut : totalOut+int(toRead)])
				if n > 0 {
					d.bytesRead += int64(n)
					if d.hasCRC {
						_, _ = d.segCRC.Write(p[totalOut : totalOut+n])
						_, _ = d.msgCRC.Write(p[totalOut : totalOut+n])
					}
					totalOut += n
					d.segRemain -= int64(n)
				}
				if err != nil && d.segRemain > 0 {
					d.setError(fmt.Errorf("segment %d: %w", d.segIndex, err))
					return totalOut, d.err
				}
				if err != nil && d.segRemain == 0 {
					// The source returned bytes that exactly completed the segment together with
					// an error. Footer/trailer framing must still follow, so even io.EOF is
					// premature here; convert it to io.ErrUnexpectedEOF so RetryReader can retry.
					if errors.Is(err, io.EOF) {
						err = io.ErrUnexpectedEOF
					}
					d.setError(fmt.Errorf("segment %d: %w", d.segIndex, err))
					return totalOut, d.err
				}
			}
			if d.segRemain == 0 {
				// Segment data is fully consumed. Advance to the footer/next segment even when the
				// caller's buffer is full so trailing framing is drained and validated before EOF.
				if d.hasCRC {
					d.prepareFrame(SMSegmentFooterSize)
					d.state = decStateSegFooter
				} else {
					d.advanceAfterSegment()
				}
			}

		case decStateSegFooter:
			done, err := d.fillFrame()
			if err != nil {
				d.setError(err)
				return totalOut, d.err
			}
			if !done {
				return totalOut, nil
			}
			if err := d.validateSegmentCRC(); err != nil {
				d.setError(err)
				return totalOut, d.err
			}

		case decStateTrailer:
			done, err := d.fillFrame()
			if err != nil {
				d.setError(err)
				return totalOut, d.err
			}
			if !done {
				return totalOut, nil
			}
			if err := d.validateTrailerCRC(); err != nil {
				d.setError(err)
				return totalOut, d.err
			}

		case decStateDone:
			if totalOut > 0 {
				return totalOut, nil
			}
			return 0, io.EOF

		case decStateError:
			return totalOut, d.err
		}
	}
}

func (d *SMDecoder) parseHeader() error {
	buf := d.frameBuf[:SMHeaderSize]
	d.version = buf[0]
	if d.version != SMVersion {
		return fmt.Errorf("unsupported structured message version: %d (expected %d)", d.version, SMVersion)
	}

	d.msgLen = binary.LittleEndian.Uint64(buf[1:9])
	d.flags = binary.LittleEndian.Uint16(buf[9:11])
	d.numSegments = binary.LittleEndian.Uint16(buf[11:13])
	// The structured message body is negotiated with properties=crc64, so the CRC64 flag
	// must be present. Rejecting its absence prevents silently skipping validation. Only the
	// CRC64 bit is required; other flag bits are ignored to preserve forward compatibility.
	if d.flags&SMFlagCRC64 == 0 {
		return fmt.Errorf("structured message is missing the required CRC64 flag (flags=0x%04x)", d.flags)
	}
	d.hasCRC = d.flags&SMFlagCRC64 != 0

	if d.hasCRC {
		d.msgCRC = crc64.New(CRC64Table)
	}

	d.segIndex = 0
	d.prepareFrame(SMSegmentHeaderSize)
	d.state = decStateSegHeader
	return nil
}

func (d *SMDecoder) parseSegmentHeader() error {
	buf := d.frameBuf[:SMSegmentHeaderSize]
	segNum := binary.LittleEndian.Uint16(buf[0:2])
	segDataLen := binary.LittleEndian.Uint64(buf[2:10])

	d.segIndex++
	if segNum != d.segIndex {
		return fmt.Errorf("segment number mismatch: expected %d, got %d", d.segIndex, segNum)
	}

	maxSegmentDataLen := uint64(int(^uint(0) >> 1))
	if segDataLen > maxSegmentDataLen {
		return fmt.Errorf("segment %d: data length %d exceeds supported size", segNum, segDataLen)
	}

	d.segRemain = int64(segDataLen)
	if d.hasCRC {
		d.segCRC = crc64.New(CRC64Table)
	}
	d.state = decStateSegData
	return nil
}

func (d *SMDecoder) validateSegmentCRC() error {
	expected := binary.LittleEndian.Uint64(d.frameBuf[:SMSegmentFooterSize])
	actual := d.segCRC.Sum64()
	if expected != actual {
		return fmt.Errorf("segment %d: CRC64 mismatch (expected 0x%016x, got 0x%016x)", d.segIndex, expected, actual)
	}
	d.advanceAfterSegment()
	return nil
}

func (d *SMDecoder) advanceAfterSegment() {
	if d.segIndex >= d.numSegments {
		if d.hasCRC {
			d.prepareFrame(SMMessageTrailerSize)
			d.state = decStateTrailer
		} else {
			d.state = decStateDone
		}
	} else {
		d.prepareFrame(SMSegmentHeaderSize)
		d.state = decStateSegHeader
	}
}

func (d *SMDecoder) validateTrailerCRC() error {
	expected := binary.LittleEndian.Uint64(d.frameBuf[:SMMessageTrailerSize])
	actual := d.msgCRC.Sum64()
	if expected != actual {
		return fmt.Errorf("message trailer CRC64 mismatch (expected 0x%016x, got 0x%016x)", expected, actual)
	}
	// The consumed byte count must match the declared message length, so a stream that declares
	// fewer segments (leaving trailing bytes unvalidated) is rejected rather than silently accepted.
	if d.msgLen > 0 && d.bytesRead != int64(d.msgLen) {
		return fmt.Errorf("structured message length mismatch: header says %d, consumed %d bytes", d.msgLen, d.bytesRead)
	}
	d.state = decStateDone
	return nil
}

// fillFrame reads from source into frameBuf until frameNeed bytes are accumulated.
// Returns (true, nil) when the full frame is available.
func (d *SMDecoder) fillFrame() (bool, error) {
	for d.frameHave < d.frameNeed {
		n, err := d.source.Read(d.frameBuf[d.frameHave:d.frameNeed])
		d.frameHave += n
		d.bytesRead += int64(n)
		if d.frameHave >= d.frameNeed {
			return true, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return false, fmt.Errorf("reading structured message framing (have %d of %d bytes): %w", d.frameHave, d.frameNeed, io.ErrUnexpectedEOF)
			}
			return false, err
		}
	}
	return true, nil
}

func (d *SMDecoder) prepareFrame(size int) {
	if cap(d.frameBuf) < size {
		d.frameBuf = make([]byte, size)
	} else {
		d.frameBuf = d.frameBuf[:size]
	}
	d.frameNeed = size
	d.frameHave = 0
}

func (d *SMDecoder) setError(err error) {
	d.err = err
	d.state = decStateError
}

func (d *SMDecoder) Close() error {
	if d.source != nil {
		return d.source.Close()
	}
	return nil
}

// DecodeResult returns the decoded message metadata after the header has been parsed.
// Returns nil if the header has not yet been read.
func (d *SMDecoder) DecodeResult() *SMDecodeResult {
	if d.state == decStateHeader {
		return nil
	}
	return &SMDecodeResult{
		Version:     d.version,
		Flags:       d.flags,
		NumSegments: d.numSegments,
	}
}
