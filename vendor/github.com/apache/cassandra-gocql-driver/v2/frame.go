/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2012, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"reflect"
	"strings"
	"time"
)

type unsetColumn struct{}

// UnsetValue represents a value used in a query binding that will be ignored by Cassandra.
//
// By setting a field to the unset value Cassandra will ignore the write completely.
// The main advantage is the ability to keep the same prepared statement even when you don't
// want to update some fields, where before you needed to make another prepared statement.
//
// UnsetValue is only available when using the version 4 of the protocol.
var UnsetValue = unsetColumn{}

type namedValue struct {
	name  string
	value interface{}
}

// NamedValue produce a value which will bind to the named parameter in a query
func NamedValue(name string, value interface{}) interface{} {
	return &namedValue{
		name:  name,
		value: value,
	}
}

const (
	protoDirectionMask = 0x80
	protoVersionMask   = 0x7F
	protoVersion1      = 0x01
	protoVersion2      = 0x02
	protoVersion3      = 0x03
	protoVersion4      = 0x04
	protoVersion5      = 0x05

	maxFrameSize = 256 * 1024 * 1024

	maxSegmentPayloadSize = 0x1FFFF
)

type protoVersion byte

func (p protoVersion) request() bool {
	return p&protoDirectionMask == 0x00
}

func (p protoVersion) response() bool {
	return p&protoDirectionMask == 0x80
}

func (p protoVersion) version() byte {
	return byte(p) & protoVersionMask
}

func (p protoVersion) String() string {
	dir := "REQ"
	if p.response() {
		dir = "RESP"
	}

	return fmt.Sprintf("[version=%d direction=%s]", p.version(), dir)
}

type frameOp byte

const (
	// header ops
	opError         frameOp = 0x00
	opStartup       frameOp = 0x01
	opReady         frameOp = 0x02
	opAuthenticate  frameOp = 0x03
	opOptions       frameOp = 0x05
	opSupported     frameOp = 0x06
	opQuery         frameOp = 0x07
	opResult        frameOp = 0x08
	opPrepare       frameOp = 0x09
	opExecute       frameOp = 0x0A
	opRegister      frameOp = 0x0B
	opEvent         frameOp = 0x0C
	opBatch         frameOp = 0x0D
	opAuthChallenge frameOp = 0x0E
	opAuthResponse  frameOp = 0x0F
	opAuthSuccess   frameOp = 0x10
)

func (f frameOp) String() string {
	switch f {
	case opError:
		return "ERROR"
	case opStartup:
		return "STARTUP"
	case opReady:
		return "READY"
	case opAuthenticate:
		return "AUTHENTICATE"
	case opOptions:
		return "OPTIONS"
	case opSupported:
		return "SUPPORTED"
	case opQuery:
		return "QUERY"
	case opResult:
		return "RESULT"
	case opPrepare:
		return "PREPARE"
	case opExecute:
		return "EXECUTE"
	case opRegister:
		return "REGISTER"
	case opEvent:
		return "EVENT"
	case opBatch:
		return "BATCH"
	case opAuthChallenge:
		return "AUTH_CHALLENGE"
	case opAuthResponse:
		return "AUTH_RESPONSE"
	case opAuthSuccess:
		return "AUTH_SUCCESS"
	default:
		return fmt.Sprintf("UNKNOWN_OP_%d", f)
	}
}

const (
	// result kind
	resultKindVoid          = 1
	resultKindRows          = 2
	resultKindKeyspace      = 3
	resultKindPrepared      = 4
	resultKindSchemaChanged = 5

	// rows flags
	flagGlobalTableSpec int = 0x01
	flagHasMorePages    int = 0x02
	flagNoMetaData      int = 0x04
	flagMetaDataChanged int = 0x08

	// query flags
	flagValues                uint32 = 0x01
	flagSkipMetaData          uint32 = 0x02
	flagPageSize              uint32 = 0x04
	flagWithPagingState       uint32 = 0x08
	flagWithSerialConsistency uint32 = 0x10
	flagDefaultTimestamp      uint32 = 0x20
	flagWithNameValues        uint32 = 0x40
	flagWithKeyspace          uint32 = 0x80
	flagWithNowInSeconds      uint32 = 0x100

	// prepare flags
	flagWithPreparedKeyspace uint32 = 0x01

	// header flags
	flagCompress      byte = 0x01
	flagTracing       byte = 0x02
	flagCustomPayload byte = 0x04
	flagWarning       byte = 0x08
	flagBetaProtocol  byte = 0x10
)

// Consistency represents the consistency level for read and write operations.
// Available levels: Any, One, Two, Three, Quorum, All, LocalQuorum, EachQuorum,
// Serial, LocalSerial, LocalOne.
type Consistency uint16

// SerialConsistency is deprecated. Use Consistency instead.
type SerialConsistency = Consistency

const (
	Any         Consistency = 0x00
	One         Consistency = 0x01
	Two         Consistency = 0x02
	Three       Consistency = 0x03
	Quorum      Consistency = 0x04
	All         Consistency = 0x05
	LocalQuorum Consistency = 0x06
	EachQuorum  Consistency = 0x07
	Serial      Consistency = 0x08
	LocalSerial Consistency = 0x09
	LocalOne    Consistency = 0x0A
)

func (c Consistency) String() string {
	switch c {
	case Any:
		return "ANY"
	case One:
		return "ONE"
	case Two:
		return "TWO"
	case Three:
		return "THREE"
	case Quorum:
		return "QUORUM"
	case All:
		return "ALL"
	case LocalQuorum:
		return "LOCAL_QUORUM"
	case EachQuorum:
		return "EACH_QUORUM"
	case LocalOne:
		return "LOCAL_ONE"
	case Serial:
		return "SERIAL"
	case LocalSerial:
		return "LOCAL_SERIAL"
	default:
		return fmt.Sprintf("UNKNOWN_CONS_0x%x", uint16(c))
	}
}

func (c Consistency) MarshalText() (text []byte, err error) {
	return []byte(c.String()), nil
}

func (c *Consistency) UnmarshalText(text []byte) error {
	switch string(text) {
	case "ANY":
		*c = Any
	case "ONE":
		*c = One
	case "TWO":
		*c = Two
	case "THREE":
		*c = Three
	case "QUORUM":
		*c = Quorum
	case "ALL":
		*c = All
	case "LOCAL_QUORUM":
		*c = LocalQuorum
	case "EACH_QUORUM":
		*c = EachQuorum
	case "LOCAL_ONE":
		*c = LocalOne
	case "SERIAL":
		*c = Serial
	case "LOCAL_SERIAL":
		*c = LocalSerial
	default:
		return fmt.Errorf("invalid consistency %q", string(text))
	}

	return nil
}

func (c Consistency) isSerial() bool {
	return c == Serial || c == LocalSerial

}
func ParseConsistency(s string) Consistency {
	var c Consistency
	if err := c.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		panic(err)
	}
	return c
}

// ParseConsistencyWrapper wraps gocql.ParseConsistency to provide an err
// return instead of a panic
func ParseConsistencyWrapper(s string) (consistency Consistency, err error) {
	err = consistency.UnmarshalText([]byte(strings.ToUpper(s)))
	return
}

const (
	apacheCassandraTypePrefix = "org.apache.cassandra.db.marshal."
)

var (
	ErrFrameTooBig = errors.New("frame length is bigger than the maximum allowed")
)

const frameHeadSize = 9

func readInt(p []byte) int32 {
	return int32(p[0])<<24 | int32(p[1])<<16 | int32(p[2])<<8 | int32(p[3])
}

type frameHeader struct {
	version  protoVersion
	flags    byte
	stream   int
	op       frameOp
	length   int
	warnings []string
}

func (f frameHeader) String() string {
	return fmt.Sprintf("[header version=%s flags=0x%x stream=%d op=%s length=%d]", f.version, f.flags, f.stream, f.op, f.length)
}

func (f frameHeader) Header() frameHeader {
	return f
}

const defaultBufSize = 128

type ObservedFrameHeader struct {
	Version protoVersion
	Flags   byte
	Stream  int16
	Opcode  frameOp
	Length  int32

	// StartHeader is the time we started reading the frame header off the network connection.
	Start time.Time
	// EndHeader is the time we finished reading the frame header off the network connection.
	End time.Time

	// Host is Host of the connection the frame header was read from.
	Host *HostInfo
}

func (f ObservedFrameHeader) String() string {
	return fmt.Sprintf("[observed header version=%s flags=0x%x stream=%d op=%s length=%d]", f.Version, f.Flags, f.Stream, f.Opcode, f.Length)
}

// FrameHeaderObserver is the interface implemented by frame observers / stat collectors.
//
// Experimental, this interface and use may change
type FrameHeaderObserver interface {
	// ObserveFrameHeader gets called on every received frame header.
	ObserveFrameHeader(context.Context, ObservedFrameHeader)
}

// a framer is responsible for reading, writing and parsing frames on a single stream
type framer struct {
	proto byte
	// flags are for outgoing flags, enabling compression and tracing etc
	flags   byte
	compres Compressor
	// if this frame was read then the header will be here
	header *frameHeader

	// if tracing flag is set this is not nil
	traceID []byte

	// holds a ref to the whole byte slice for buf so that it can be reset to
	// 0 after a read.
	readBuffer []byte

	buf []byte

	customPayload map[string][]byte

	types *RegisteredTypes
}

func newFramer(compressor Compressor, version byte, r *RegisteredTypes) *framer {
	buf := make([]byte, defaultBufSize)
	f := &framer{
		buf:        buf[:0],
		readBuffer: buf,
		types:      r,
	}
	var flags byte
	if compressor != nil {
		flags |= flagCompress
	}

	version &= protoVersionMask

	f.compres = compressor
	f.proto = version
	f.flags = flags

	f.header = nil
	f.traceID = nil

	return f
}

type frame interface {
	Header() frameHeader
	String() string
}

func readHeader(r io.Reader, p []byte) (head frameHeader, err error) {
	_, err = io.ReadFull(r, p[:1])
	if err != nil {
		return frameHeader{}, err
	}

	version := p[0] & protoVersionMask

	if version < protoVersion3 || version > protoVersion5 {
		return frameHeader{}, fmt.Errorf("gocql: unsupported protocol response version: %d", version)
	}

	_, err = io.ReadFull(r, p[1:frameHeadSize])
	if err != nil {
		return frameHeader{}, err
	}

	p = p[:frameHeadSize]

	head.version = protoVersion(p[0])
	head.flags = p[1]

	if len(p) != 9 {
		return frameHeader{}, fmt.Errorf("not enough bytes to read header require 9 got: %d", len(p))
	}

	head.stream = int(int16(p[2])<<8 | int16(p[3]))
	head.op = frameOp(p[4])
	head.length = int(readInt(p[5:]))
	return head, nil
}

// explicitly enables tracing for the framers outgoing requests
func (f *framer) trace() {
	f.flags |= flagTracing
}

// explicitly enables the custom payload flag
func (f *framer) payload() {
	f.flags |= flagCustomPayload
}

// reads a frame form the wire into the framers buffer
func (f *framer) readFrame(r io.Reader, head *frameHeader) error {
	if head.length < 0 {
		return fmt.Errorf("frame body length can not be less than 0: %d", head.length)
	} else if head.length > maxFrameSize {
		// need to free up the connection to be used again
		_, err := io.CopyN(ioutil.Discard, r, int64(head.length))
		if err != nil {
			return fmt.Errorf("error whilst trying to discard frame with invalid length: %v", err)
		}
		return ErrFrameTooBig
	}

	if cap(f.readBuffer) >= head.length {
		f.buf = f.readBuffer[:head.length]
	} else {
		f.readBuffer = make([]byte, head.length)
		f.buf = f.readBuffer
	}

	// assume the underlying reader takes care of timeouts and retries
	n, err := io.ReadFull(r, f.buf)
	if err != nil {
		return fmt.Errorf("unable to read frame body: read %d/%d bytes: %v", n, head.length, err)
	}

	if f.proto < protoVersion5 && head.flags&flagCompress == flagCompress {
		if f.compres == nil {
			return NewErrProtocol("no compressor available with compressed frame body")
		}

		f.buf, err = f.compres.AppendDecompressedWithLength(nil, f.buf)
		if err != nil {
			return err
		}
	}

	f.header = head
	return nil
}

func (f *framer) parseFrame() (frame, error) {
	if f.header.version.request() {
		return nil, NewErrProtocol("got a request frame from server: %v", f.header.version)
	}

	if f.header.flags&flagTracing == flagTracing {
		f.readTrace()
	}

	var err error
	if f.header.flags&flagWarning == flagWarning {
		f.header.warnings, err = f.readStringList()
		if err != nil {
			return nil, err
		}
	}

	if f.header.flags&flagCustomPayload == flagCustomPayload {
		f.customPayload, err = f.readBytesMap()
		if err != nil {
			return nil, err
		}
	}

	// assumes that the frame body has been read into rbuf
	switch f.header.op {
	case opError:
		return f.parseErrorFrame()
	case opReady:
		return f.parseReadyFrame()
	case opResult:
		return f.parseResultFrame()
	case opSupported:
		return f.parseSupportedFrame()
	case opAuthenticate:
		return f.parseAuthenticateFrame()
	case opAuthChallenge:
		return f.parseAuthChallengeFrame()
	case opAuthSuccess:
		return f.parseAuthSuccessFrame()
	case opEvent:
		return f.parseEventFrame()
	default:
		return nil, NewErrProtocol("unknown op in frame header: %s", f.header.op)
	}
}

func (f *framer) parseErrorFrame() (frame, error) {
	code, err := f.readInt()
	if err != nil {
		return nil, err
	}
	msg, err := f.readString()
	if err != nil {
		return nil, err
	}

	errD := errorFrame{
		frameHeader: *f.header,
		code:        code,
		message:     msg,
	}

	switch code {
	case ErrCodeUnavailable:
		cl, err := f.readConsistency()
		if err != nil {
			return nil, err
		}
		required, err := f.readInt()
		if err != nil {
			return nil, err
		}
		alive, err := f.readInt()
		if err != nil {
			return nil, err
		}
		return &RequestErrUnavailable{
			errorFrame:  errD,
			Consistency: cl,
			Required:    required,
			Alive:       alive,
		}, nil
	case ErrCodeWriteTimeout:
		cl, err := f.readConsistency()
		if err != nil {
			return nil, err
		}
		received, err := f.readInt()
		if err != nil {
			return nil, err
		}
		blockfor, err := f.readInt()
		if err != nil {
			return nil, err
		}
		writeType, err := f.readString()
		if err != nil {
			return nil, err
		}
		return &RequestErrWriteTimeout{
			errorFrame:  errD,
			Consistency: cl,
			Received:    received,
			BlockFor:    blockfor,
			WriteType:   writeType,
		}, nil
	case ErrCodeReadTimeout:
		cl, err := f.readConsistency()
		if err != nil {
			return nil, err
		}
		received, err := f.readInt()
		if err != nil {
			return nil, err
		}
		blockfor, err := f.readInt()
		if err != nil {
			return nil, err
		}
		dataPresent, err := f.readByte()
		if err != nil {
			return nil, err
		}
		return &RequestErrReadTimeout{
			errorFrame:  errD,
			Consistency: cl,
			Received:    received,
			BlockFor:    blockfor,
			DataPresent: dataPresent,
		}, nil
	case ErrCodeAlreadyExists:
		ks, err := f.readString()
		if err != nil {
			return nil, err
		}
		table, err := f.readString()
		if err != nil {
			return nil, err
		}
		return &RequestErrAlreadyExists{
			errorFrame: errD,
			Keyspace:   ks,
			Table:      table,
		}, nil
	case ErrCodeUnprepared:
		stmtId, err := f.readShortBytes()
		if err != nil {
			return nil, err
		}
		return &RequestErrUnprepared{
			errorFrame:  errD,
			StatementId: copyBytes(stmtId), // defensively copy
		}, nil
	case ErrCodeReadFailure:
		res := &RequestErrReadFailure{
			errorFrame: errD,
		}
		res.Consistency, err = f.readConsistency()
		if err != nil {
			return nil, err
		}
		res.Received, err = f.readInt()
		if err != nil {
			return nil, err
		}
		res.BlockFor, err = f.readInt()
		if err != nil {
			return nil, err
		}
		if f.proto > protoVersion4 {
			res.ErrorMap, err = f.readErrorMap()
			if err != nil {
				return nil, err
			}
			res.NumFailures = len(res.ErrorMap)
		} else {
			res.NumFailures, err = f.readInt()
			if err != nil {
				return nil, err
			}
		}
		b, err := f.readByte()
		if err != nil {
			return nil, err
		}
		res.DataPresent = b != 0

		return res, nil
	case ErrCodeWriteFailure:
		res := &RequestErrWriteFailure{
			errorFrame: errD,
		}
		res.Consistency, err = f.readConsistency()
		if err != nil {
			return nil, err
		}
		res.Received, err = f.readInt()
		if err != nil {
			return nil, err
		}
		res.BlockFor, err = f.readInt()
		if err != nil {
			return nil, err
		}
		if f.proto > protoVersion4 {
			res.ErrorMap, err = f.readErrorMap()
			if err != nil {
				return nil, err
			}
			res.NumFailures = len(res.ErrorMap)
		} else {
			res.NumFailures, err = f.readInt()
			if err != nil {
				return nil, err
			}
		}
		res.WriteType, err = f.readString()
		if err != nil {
			return nil, err
		}
		return res, nil
	case ErrCodeFunctionFailure:
		res := &RequestErrFunctionFailure{
			errorFrame: errD,
		}
		res.Keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}
		res.Function, err = f.readString()
		if err != nil {
			return nil, err
		}
		res.ArgTypes, err = f.readStringList()
		if err != nil {
			return nil, err
		}
		return res, nil

	case ErrCodeCDCWriteFailure:
		return &RequestErrCDCWriteFailure{
			errorFrame: errD,
		}, nil
	case ErrCodeCASWriteUnknown:
		res := &RequestErrCASWriteUnknown{
			errorFrame: errD,
		}
		res.Consistency, err = f.readConsistency()
		if err != nil {
			return nil, err
		}
		res.Received, err = f.readInt()
		if err != nil {
			return nil, err
		}
		res.BlockFor, err = f.readInt()
		if err != nil {
			return nil, err
		}
		return res, nil
	case ErrCodeInvalid, ErrCodeBootstrapping, ErrCodeConfig, ErrCodeCredentials, ErrCodeOverloaded,
		ErrCodeProtocol, ErrCodeServer, ErrCodeSyntax, ErrCodeTruncate, ErrCodeUnauthorized:
		// TODO(zariel): we should have some distinct types for these errors
		return errD, nil
	default:
		return nil, fmt.Errorf("unknown error code: 0x%x", errD.code)
	}
}

func (f *framer) readErrorMap() (ErrorMap, error) {
	numErrs, err := f.readInt()
	if err != nil {
		return nil, err
	}
	errMap := make(ErrorMap, numErrs)
	for i := 0; i < numErrs; i++ {
		ip, err := f.readInetAdressOnly()
		if err != nil {
			return nil, err
		}
		errMap[ip.String()], err = f.readShort()
		if err != nil {
			return nil, err
		}
	}
	return errMap, nil
}

func (f *framer) writeHeader(flags byte, op frameOp, stream int) {
	f.buf = append(f.buf[:0],
		f.proto, flags, byte(stream>>8), byte(stream),
		// pad out length
		byte(op), 0, 0, 0, 0,
	)
}

func (f *framer) setLength(length int) {
	f.buf[5] = byte(length >> 24)
	f.buf[6] = byte(length >> 16)
	f.buf[7] = byte(length >> 8)
	f.buf[8] = byte(length)
}

func (f *framer) finish() error {
	if len(f.buf) > maxFrameSize {
		// huge app frame, lets remove it so it doesn't bloat the heap
		f.buf = make([]byte, defaultBufSize)
		return ErrFrameTooBig
	}

	if f.proto < protoVersion5 && f.buf[1]&flagCompress == flagCompress {
		if f.compres == nil {
			panic("compress flag set with no compressor")
		}

		// TODO: only compress frames which are big enough
		compressed, err := f.compres.AppendCompressedWithLength(nil, f.buf[frameHeadSize:])
		if err != nil {
			return err
		}

		f.buf = append(f.buf[:frameHeadSize], compressed...)
	}
	length := len(f.buf) - frameHeadSize
	f.setLength(length)

	return nil
}

func (f *framer) writeTo(w io.Writer) error {
	_, err := w.Write(f.buf)
	return err
}

func (f *framer) readTrace() {
	if len(f.buf) < 16 {
		panic(fmt.Errorf("not enough bytes in buffer to read trace uuid require 16 got: %d", len(f.buf)))
	}
	if len(f.traceID) != 16 {
		f.traceID = make([]byte, 16)
	}
	copy(f.traceID, f.buf[:16])
	f.buf = f.buf[16:]
}

type readyFrame struct {
	frameHeader
}

func (f *framer) parseReadyFrame() (frame, error) {
	return &readyFrame{
		frameHeader: *f.header,
	}, nil
}

type supportedFrame struct {
	frameHeader

	supported map[string][]string
}

// TODO: if we move the body buffer onto the frameHeader then we only need a single
// framer, and can move the methods onto the header.
func (f *framer) parseSupportedFrame() (frame, error) {
	s, err := f.readStringMultiMap()
	if err != nil {
		return nil, err
	}
	return &supportedFrame{
		frameHeader: *f.header,
		supported:   s,
	}, nil
}

type writeStartupFrame struct {
	opts map[string]string
}

func (w writeStartupFrame) String() string {
	return fmt.Sprintf("[startup opts=%+v]", w.opts)
}

func (w *writeStartupFrame) buildFrame(f *framer, streamID int) error {
	f.writeHeader(f.flags&^flagCompress, opStartup, streamID)
	f.writeStringMap(w.opts)

	return f.finish()
}

type writePrepareFrame struct {
	statement     string
	keyspace      string
	customPayload map[string][]byte
}

func (w *writePrepareFrame) buildFrame(f *framer, streamID int) error {
	if len(w.customPayload) > 0 {
		f.payload()
	}
	f.writeHeader(f.flags, opPrepare, streamID)
	f.writeCustomPayload(&w.customPayload)
	f.writeLongString(w.statement)

	var flags uint32 = 0
	if w.keyspace != "" {
		if f.proto > protoVersion4 {
			flags |= flagWithPreparedKeyspace
		} else {
			panic(fmt.Errorf("the keyspace can only be set with protocol 5 or higher"))
		}
	}
	if f.proto > protoVersion4 {
		f.writeUint(flags)
	}
	if w.keyspace != "" {
		f.writeString(w.keyspace)
	}

	return f.finish()
}

func (f *framer) readParam(param interface{}) (interface{}, error) {
	switch p := param.(type) {
	case string:
		return f.readString()
	case uint16:
		return f.readShort()
	case byte:
		return f.readByte()
	case []byte:
		return f.readShortBytes()
	case int:
		return f.readInt()
	case []string:
		return f.readStringList()
	case []UDTField:
		n, err := f.readShort()
		if err != nil {
			return nil, err
		}
		if len(p) < int(n) {
			p = make([]UDTField, n)
		} else {
			p = p[:n]
		}
		for i := 0; i < int(n); i++ {
			p[i].Name, err = f.readString()
			if err != nil {
				return nil, err
			}
			p[i].Type, err = f.readTypeInfo()
			if err != nil {
				return nil, err
			}
		}
		return p, nil
	case TypeInfo:
		return f.readTypeInfo()
	case *TypeInfo:
		return f.readTypeInfo()
	case []TypeInfo:
		n, err := f.readShort()
		if err != nil {
			return nil, err
		}
		if len(p) < int(n) {
			p = make([]TypeInfo, n)
		} else {
			p = p[:n]
		}
		for i := 0; i < int(n); i++ {
			p[i], err = f.readTypeInfo()
			if err != nil {
				return nil, err
			}
		}
		return p, nil
	case Type:
		// Type is actually an int but it's encoded as short
		s, err := f.readShort()
		if err != nil {
			return nil, err
		}
		return Type(s), nil
	case []interface{}:
		n, err := f.readShort()
		if err != nil {
			return nil, err
		}
		if len(p) != int(n) {
			return nil, fmt.Errorf("wrong length for reading []interface{} from frame %d vs %d", len(p), n)
		}
		for i := 0; i < int(n); i++ {
			p[i], err = f.readParam(p[i])
			if err != nil {
				return nil, err
			}
		}
		return p, nil
	}

	// check if its a pointer
	// we used to do some conversions in here for some types but that was risky
	// since Type is an int but it is read with readShort so we stopped doing that
	// out of caution and instead are just going to error
	valueRef := reflect.ValueOf(param)
	if valueRef.Kind() == reflect.Ptr && !valueRef.IsNil() {
		return f.readParam(valueRef.Elem().Interface())
	}
	return nil, fmt.Errorf("unsupported type for reading from frame: %T", param)
}

func (f *framer) readTypeInfo() (TypeInfo, error) {
	i, err := f.readShort()
	if err != nil {
		return nil, err
	}
	typ := Type(i)
	if typ == TypeCustom {
		name, err := f.readString()
		if err != nil {
			return nil, err
		}
		return f.types.typeInfoFromString(int(f.proto), name)
	}

	if ti := f.types.fastTypeInfoLookup(typ); ti != nil {
		return ti, nil
	}

	cqlt := f.types.fastRegisteredTypeLookup(typ)
	if cqlt == nil {
		return nil, unknownTypeError(fmt.Sprintf("%d", typ))
	}

	params := cqlt.Params(int(f.proto))
	for i := range params {
		params[i], err = f.readParam(params[i])
		if err != nil {
			return nil, err
		}
	}
	return cqlt.TypeInfoFromParams(int(f.proto), params)
}

type preparedMetadata struct {
	resultMetadata

	// proto v4+
	pkeyColumns []int

	keyspace string

	table string
}

func (r preparedMetadata) String() string {
	return fmt.Sprintf("[prepared flags=0x%x pkey=%v paging_state=% X columns=%v col_count=%d actual_col_count=%d]", r.flags, r.pkeyColumns, r.pagingState, r.columns, r.colCount, r.actualColCount)
}

func (f *framer) parsePreparedMetadata() (preparedMetadata, error) {
	// TODO: deduplicate this from parseMetadata
	meta := preparedMetadata{}

	var err error
	meta.flags, err = f.readInt()
	if err != nil {
		return preparedMetadata{}, err
	}
	meta.colCount, err = f.readInt()
	if err != nil {
		return preparedMetadata{}, err
	}
	if meta.colCount < 0 {
		return preparedMetadata{}, fmt.Errorf("received negative column count: %d", meta.colCount)
	}
	meta.actualColCount = meta.colCount

	if f.proto >= protoVersion4 {
		pkeyCount, err := f.readInt()
		if err != nil {
			return preparedMetadata{}, err
		}
		pkeys := make([]int, pkeyCount)
		for i := 0; i < pkeyCount; i++ {
			c, err := f.readShort()
			if err != nil {
				return preparedMetadata{}, err
			}
			pkeys[i] = int(c)
		}
		meta.pkeyColumns = pkeys
	}

	if meta.flags&flagHasMorePages == flagHasMorePages {
		b, err := f.readBytes()
		if err != nil {
			return preparedMetadata{}, err
		}
		meta.pagingState = copyBytes(b)
	}

	if meta.flags&flagNoMetaData == flagNoMetaData {
		return meta, nil
	}

	globalSpec := meta.flags&flagGlobalTableSpec == flagGlobalTableSpec
	if globalSpec {
		meta.keyspace, err = f.readString()
		if err != nil {
			return preparedMetadata{}, err
		}
		meta.table, err = f.readString()
		if err != nil {
			return preparedMetadata{}, err
		}
	}

	var cols []ColumnInfo
	if meta.colCount < 1000 {
		// preallocate columninfo to avoid excess copying
		cols = make([]ColumnInfo, meta.colCount)
		for i := 0; i < meta.colCount; i++ {
			err = f.readCol(&cols[i], &meta.resultMetadata, globalSpec, meta.keyspace, meta.table)
			if err != nil {
				return preparedMetadata{}, err
			}
		}
	} else {
		// use append, huge number of columns usually indicates a corrupt frame or
		// just a huge row.
		for i := 0; i < meta.colCount; i++ {
			var col ColumnInfo
			err = f.readCol(&col, &meta.resultMetadata, globalSpec, meta.keyspace, meta.table)
			if err != nil {
				return preparedMetadata{}, err
			}
			cols = append(cols, col)
		}
	}

	meta.columns = cols

	return meta, nil
}

type resultMetadata struct {
	flags int

	// only if flagPageState
	pagingState []byte

	columns  []ColumnInfo
	colCount int

	// this is a count of the total number of columns which can be scanned,
	// it is at minimum len(columns) but may be larger, for instance when a column
	// is a UDT or tuple.
	actualColCount int

	newMetadataID []byte
}

func (r *resultMetadata) morePages() bool {
	return r.flags&flagHasMorePages == flagHasMorePages
}

func (r *resultMetadata) noMetaData() bool {
	return r.flags&flagNoMetaData == flagNoMetaData
}

func (r resultMetadata) String() string {
	return fmt.Sprintf("[metadata flags=0x%x paging_state=% X columns=%v new_metadata_id=% X]", r.flags, r.pagingState, r.columns, r.newMetadataID)
}

func (f *framer) readCol(col *ColumnInfo, meta *resultMetadata, globalSpec bool, keyspace, table string) error {
	var err error
	if !globalSpec {
		col.Keyspace, err = f.readString()
		if err != nil {
			return err
		}
		col.Table, err = f.readString()
		if err != nil {
			return err
		}
	} else {
		col.Keyspace = keyspace
		col.Table = table
	}

	col.Name, err = f.readString()
	if err != nil {
		return err
	}
	col.TypeInfo, err = f.readTypeInfo()
	if err != nil {
		return err
	}
	// maybe also UDT
	if t, ok := col.TypeInfo.(TupleTypeInfo); ok {
		// -1 because we already included the tuple column
		meta.actualColCount += len(t.Elems) - 1
	}
	return nil
}

func (f *framer) parseResultMetadata() (resultMetadata, error) {
	var meta resultMetadata

	var err error
	meta.flags, err = f.readInt()
	if err != nil {
		return resultMetadata{}, err
	}
	meta.colCount, err = f.readInt()
	if err != nil {
		return resultMetadata{}, err
	}
	if meta.colCount < 0 {
		return resultMetadata{}, fmt.Errorf("received negative column count: %d", meta.colCount)
	}
	meta.actualColCount = meta.colCount

	if meta.flags&flagHasMorePages == flagHasMorePages {
		b, err := f.readBytes()
		if err != nil {
			return resultMetadata{}, err
		}
		meta.pagingState = copyBytes(b)
	}

	if f.proto > protoVersion4 && meta.flags&flagMetaDataChanged == flagMetaDataChanged {
		b, err := f.readShortBytes()
		if err != nil {
			return resultMetadata{}, err
		}
		meta.newMetadataID = copyBytes(b)
	}

	if meta.noMetaData() {
		return meta, nil
	}

	var keyspace, table string
	globalSpec := meta.flags&flagGlobalTableSpec == flagGlobalTableSpec
	if globalSpec {
		keyspace, err = f.readString()
		if err != nil {
			return resultMetadata{}, err
		}
		table, err = f.readString()
		if err != nil {
			return resultMetadata{}, err
		}
	}

	var cols []ColumnInfo
	if meta.colCount < 1000 {
		// preallocate columninfo to avoid excess copying
		cols = make([]ColumnInfo, meta.colCount)
		for i := 0; i < meta.colCount; i++ {
			err = f.readCol(&cols[i], &meta, globalSpec, keyspace, table)
			if err != nil {
				return resultMetadata{}, err
			}
		}

	} else {
		// use append, huge number of columns usually indicates a corrupt frame or
		// just a huge row.
		for i := 0; i < meta.colCount; i++ {
			var col ColumnInfo
			err = f.readCol(&col, &meta, globalSpec, keyspace, table)
			if err != nil {
				return resultMetadata{}, err
			}
			cols = append(cols, col)
		}
	}

	meta.columns = cols

	return meta, nil
}

type resultVoidFrame struct {
	frameHeader
}

func (f *resultVoidFrame) String() string {
	return "[result_void]"
}

func (f *framer) parseResultFrame() (frame, error) {
	kind, err := f.readInt()
	if err != nil {
		return nil, err
	}

	switch kind {
	case resultKindVoid:
		return &resultVoidFrame{frameHeader: *f.header}, nil
	case resultKindRows:
		return f.parseResultRows()
	case resultKindKeyspace:
		return f.parseResultSetKeyspace()
	case resultKindPrepared:
		return f.parseResultPrepared()
	case resultKindSchemaChanged:
		return f.parseResultSchemaChange()
	}

	return nil, NewErrProtocol("unknown result kind: %x", kind)
}

type resultRowsFrame struct {
	frameHeader

	meta resultMetadata
	// dont parse the rows here as we only need to do it once
	numRows int
}

func (f *resultRowsFrame) String() string {
	return fmt.Sprintf("[result_rows meta=%v]", f.meta)
}

func (f *framer) parseResultRows() (frame, error) {
	result := &resultRowsFrame{}
	var err error
	result.meta, err = f.parseResultMetadata()
	if err != nil {
		return nil, err
	}

	result.numRows, err = f.readInt()
	if err != nil {
		return nil, err
	}
	if result.numRows < 0 {
		return nil, fmt.Errorf("invalid row_count in result frame: %d", result.numRows)
	}

	return result, nil
}

type resultKeyspaceFrame struct {
	frameHeader
	keyspace string
}

func (r *resultKeyspaceFrame) String() string {
	return fmt.Sprintf("[result_keyspace keyspace=%s]", r.keyspace)
}

func (f *framer) parseResultSetKeyspace() (frame, error) {
	k, err := f.readString()
	if err != nil {
		return nil, err
	}
	return &resultKeyspaceFrame{
		frameHeader: *f.header,
		keyspace:    k,
	}, nil
}

type resultPreparedFrame struct {
	frameHeader

	preparedID       []byte
	resultMetadataID []byte
	reqMeta          preparedMetadata
	respMeta         resultMetadata
}

func (f *framer) parseResultPrepared() (frame, error) {
	b, err := f.readShortBytes()
	if err != nil {
		return nil, err
	}
	frame := &resultPreparedFrame{
		frameHeader: *f.header,
		preparedID:  b,
	}

	if f.proto > protoVersion4 {
		b, err = f.readShortBytes()
		if err != nil {
			return nil, err
		}
		frame.resultMetadataID = copyBytes(b)
	}

	frame.reqMeta, err = f.parsePreparedMetadata()
	if err != nil {
		return nil, err
	}
	frame.respMeta, err = f.parseResultMetadata()
	if err != nil {
		return nil, err
	}

	return frame, nil
}

type schemaChangeKeyspace struct {
	frameHeader

	change   string
	keyspace string
}

func (f schemaChangeKeyspace) String() string {
	return fmt.Sprintf("[event schema_change_keyspace change=%q keyspace=%q]", f.change, f.keyspace)
}

type schemaChangeTable struct {
	frameHeader

	change   string
	keyspace string
	object   string
}

func (f schemaChangeTable) String() string {
	return fmt.Sprintf("[event schema_change change=%q keyspace=%q object=%q]", f.change, f.keyspace, f.object)
}

type schemaChangeType struct {
	frameHeader

	change   string
	keyspace string
	object   string
}

type schemaChangeFunction struct {
	frameHeader

	change   string
	keyspace string
	name     string
	args     []string
}

type schemaChangeAggregate struct {
	frameHeader

	change   string
	keyspace string
	name     string
	args     []string
}

func (f *framer) parseResultSchemaChange() (frame, error) {
	change, err := f.readString()
	if err != nil {
		return nil, err
	}
	target, err := f.readString()
	if err != nil {
		return nil, err
	}

	// TODO: could just use a separate type for each target
	switch target {
	case "KEYSPACE":
		frame := &schemaChangeKeyspace{
			frameHeader: *f.header,
			change:      change,
		}

		frame.keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}

		return frame, err
	case "TABLE":
		frame := &schemaChangeTable{
			frameHeader: *f.header,
			change:      change,
		}

		frame.keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.object, err = f.readString()
		if err != nil {
			return nil, err
		}

		return frame, err
	case "TYPE":
		frame := &schemaChangeType{
			frameHeader: *f.header,
			change:      change,
		}

		frame.keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.object, err = f.readString()
		if err != nil {
			return nil, err
		}

		return frame, nil
	case "FUNCTION":
		frame := &schemaChangeFunction{
			frameHeader: *f.header,
			change:      change,
		}

		frame.keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.name, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.args, err = f.readStringList()
		if err != nil {
			return nil, err
		}

		return frame, nil
	case "AGGREGATE":
		frame := &schemaChangeAggregate{
			frameHeader: *f.header,
			change:      change,
		}

		frame.keyspace, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.name, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.args, err = f.readStringList()
		if err != nil {
			return nil, err
		}

		return frame, nil
	default:
		return nil, fmt.Errorf("gocql: unknown SCHEMA_CHANGE target: %q change: %q", target, change)
	}
}

type authenticateFrame struct {
	frameHeader

	class string
}

func (a *authenticateFrame) String() string {
	return fmt.Sprintf("[authenticate class=%q]", a.class)
}

func (f *framer) parseAuthenticateFrame() (frame, error) {
	cls, err := f.readString()
	if err != nil {
		return nil, err
	}
	return &authenticateFrame{
		frameHeader: *f.header,
		class:       cls,
	}, nil
}

type authSuccessFrame struct {
	frameHeader

	data []byte
}

func (a *authSuccessFrame) String() string {
	return fmt.Sprintf("[auth_success data=%q]", a.data)
}

func (f *framer) parseAuthSuccessFrame() (frame, error) {
	b, err := f.readBytes()
	if err != nil {
		return nil, err
	}
	return &authSuccessFrame{
		frameHeader: *f.header,
		data:        b,
	}, nil
}

type authChallengeFrame struct {
	frameHeader

	data []byte
}

func (a *authChallengeFrame) String() string {
	return fmt.Sprintf("[auth_challenge data=%q]", a.data)
}

func (f *framer) parseAuthChallengeFrame() (frame, error) {
	b, err := f.readBytes()
	if err != nil {
		return nil, err
	}
	return &authChallengeFrame{
		frameHeader: *f.header,
		data:        b,
	}, nil
}

type statusChangeEventFrame struct {
	frameHeader

	change string
	host   net.IP
	port   int
}

func (t statusChangeEventFrame) String() string {
	return fmt.Sprintf("[status_change change=%s host=%v port=%v]", t.change, t.host, t.port)
}

// essentially the same as statusChange
type topologyChangeEventFrame struct {
	frameHeader

	change string
	host   net.IP
	port   int
}

func (t topologyChangeEventFrame) String() string {
	return fmt.Sprintf("[topology_change change=%s host=%v port=%v]", t.change, t.host, t.port)
}

func (f *framer) parseEventFrame() (frame, error) {
	eventType, err := f.readString()
	if err != nil {
		return nil, err
	}

	switch eventType {
	case "TOPOLOGY_CHANGE":
		frame := &topologyChangeEventFrame{frameHeader: *f.header}
		frame.change, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.host, frame.port, err = f.readInet()
		if err != nil {
			return nil, err
		}

		return frame, nil
	case "STATUS_CHANGE":
		frame := &statusChangeEventFrame{frameHeader: *f.header}
		frame.change, err = f.readString()
		if err != nil {
			return nil, err
		}
		frame.host, frame.port, err = f.readInet()
		if err != nil {
			return nil, err
		}

		return frame, nil
	case "SCHEMA_CHANGE":
		// this should work for all versions
		return f.parseResultSchemaChange()
	default:
		panic(fmt.Errorf("gocql: unknown event type: %q", eventType))
	}

}

type writeAuthResponseFrame struct {
	data []byte
}

func (a *writeAuthResponseFrame) String() string {
	return fmt.Sprintf("[auth_response data=%q]", a.data)
}

func (a *writeAuthResponseFrame) buildFrame(framer *framer, streamID int) error {
	return framer.writeAuthResponseFrame(streamID, a.data)
}

func (f *framer) writeAuthResponseFrame(streamID int, data []byte) error {
	f.writeHeader(f.flags, opAuthResponse, streamID)
	f.writeBytes(data)
	return f.finish()
}

type queryValues struct {
	value []byte

	// optional name, will set With names for values flag
	name    string
	isUnset bool
}

type queryParams struct {
	consistency Consistency
	// v2+
	skipMeta          bool
	values            []queryValues
	pageSize          int
	pagingState       []byte
	serialConsistency Consistency
	// v3+
	defaultTimestamp      bool
	defaultTimestampValue int64
	// v5+
	keyspace     string
	nowInSeconds *int
}

func (q queryParams) String() string {
	return fmt.Sprintf("[query_params consistency=%v skip_meta=%v page_size=%d paging_state=%q serial_consistency=%v default_timestamp=%v values=%v keyspace=%s now_in_seconds=%v]",
		q.consistency, q.skipMeta, q.pageSize, q.pagingState, q.serialConsistency, q.defaultTimestamp, q.values, q.keyspace, q.nowInSeconds)
}

func (f *framer) writeQueryParams(opts *queryParams) {
	f.writeConsistency(opts.consistency)

	var flags uint32
	names := false

	if len(opts.values) > 0 {
		flags |= flagValues
	}
	if opts.skipMeta {
		flags |= flagSkipMetaData
	}
	if opts.pageSize > 0 {
		flags |= flagPageSize
	}
	if len(opts.pagingState) > 0 {
		flags |= flagWithPagingState
	}
	if opts.serialConsistency > 0 {
		flags |= flagWithSerialConsistency
	}

	if opts.defaultTimestamp {
		flags |= flagDefaultTimestamp
	}

	if len(opts.values) > 0 && opts.values[0].name != "" {
		flags |= flagWithNameValues
		names = true
	}

	if opts.keyspace != "" {
		if f.proto < protoVersion5 {
			panic(fmt.Errorf("the keyspace can only be set with protocol 5 or higher"))
		}
		flags |= flagWithKeyspace
	}

	if opts.nowInSeconds != nil {
		if f.proto < protoVersion5 {
			panic(fmt.Errorf("now_in_seconds can only be set with protocol 5 or higher"))
		}
		flags |= flagWithNowInSeconds
	}

	if f.proto > protoVersion4 {
		f.writeUint(flags)
	} else {
		f.writeByte(byte(flags))
	}

	if n := len(opts.values); n > 0 {
		f.writeShort(uint16(n))

		for i := 0; i < n; i++ {
			if names {
				f.writeString(opts.values[i].name)
			}
			if opts.values[i].isUnset {
				f.writeUnset()
			} else {
				f.writeBytes(opts.values[i].value)
			}
		}
	}

	if opts.pageSize > 0 {
		f.writeInt(int32(opts.pageSize))
	}

	if len(opts.pagingState) > 0 {
		f.writeBytes(opts.pagingState)
	}

	if opts.serialConsistency > 0 {
		f.writeConsistency(opts.serialConsistency)
	}

	if opts.defaultTimestamp {
		// timestamp in microseconds
		var ts int64
		if opts.defaultTimestampValue != 0 {
			ts = opts.defaultTimestampValue
		} else {
			ts = time.Now().UnixNano() / 1000
		}
		f.writeLong(ts)
	}

	if opts.keyspace != "" {
		f.writeString(opts.keyspace)
	}

	if opts.nowInSeconds != nil {
		f.writeInt(int32(*opts.nowInSeconds))
	}
}

type writeQueryFrame struct {
	statement string
	params    queryParams

	// v4+
	customPayload map[string][]byte
}

func (w *writeQueryFrame) String() string {
	return fmt.Sprintf("[query statement=%q params=%v]", w.statement, w.params)
}

func (w *writeQueryFrame) buildFrame(framer *framer, streamID int) error {
	return framer.writeQueryFrame(streamID, w.statement, &w.params, w.customPayload)
}

func (f *framer) writeQueryFrame(streamID int, statement string, params *queryParams, customPayload map[string][]byte) error {
	if len(customPayload) > 0 {
		f.payload()
	}
	f.writeHeader(f.flags, opQuery, streamID)
	f.writeCustomPayload(&customPayload)
	f.writeLongString(statement)
	f.writeQueryParams(params)

	return f.finish()
}

type frameBuilder interface {
	buildFrame(framer *framer, streamID int) error
}

type frameWriterFunc func(framer *framer, streamID int) error

func (f frameWriterFunc) buildFrame(framer *framer, streamID int) error {
	return f(framer, streamID)
}

type writeExecuteFrame struct {
	preparedID []byte
	params     queryParams

	// v4+
	customPayload map[string][]byte

	// v5+
	resultMetadataID []byte
}

func (e *writeExecuteFrame) String() string {
	return fmt.Sprintf("[execute id=% X params=%v]", e.preparedID, &e.params)
}

func (e *writeExecuteFrame) buildFrame(fr *framer, streamID int) error {
	return fr.writeExecuteFrame(streamID, e.preparedID, e.resultMetadataID, &e.params, &e.customPayload)
}

func (f *framer) writeExecuteFrame(streamID int, preparedID, resultMetadataID []byte, params *queryParams, customPayload *map[string][]byte) error {
	if len(*customPayload) > 0 {
		f.payload()
	}
	f.writeHeader(f.flags, opExecute, streamID)
	f.writeCustomPayload(customPayload)
	f.writeShortBytes(preparedID)

	if f.proto > protoVersion4 {
		f.writeShortBytes(resultMetadataID)
	}

	f.writeQueryParams(params)

	return f.finish()
}

// TODO: can we replace BatchStatemt with batchStatement? As they prety much
// duplicate each other
type batchStatment struct {
	preparedID []byte
	statement  string
	values     []queryValues
}

type writeBatchFrame struct {
	typ         BatchType
	statements  []batchStatment
	consistency Consistency

	// v3+
	serialConsistency     Consistency
	defaultTimestamp      bool
	defaultTimestampValue int64

	//v4+
	customPayload map[string][]byte

	//v5+
	keyspace     string
	nowInSeconds *int
}

func (w *writeBatchFrame) buildFrame(framer *framer, streamID int) error {
	return framer.writeBatchFrame(streamID, w, w.customPayload)
}

func (f *framer) writeBatchFrame(streamID int, w *writeBatchFrame, customPayload map[string][]byte) error {
	if len(customPayload) > 0 {
		f.payload()
	}
	f.writeHeader(f.flags, opBatch, streamID)
	f.writeCustomPayload(&customPayload)
	f.writeByte(byte(w.typ))

	n := len(w.statements)
	f.writeShort(uint16(n))

	var flags uint32

	for i := 0; i < n; i++ {
		b := &w.statements[i]
		if len(b.preparedID) == 0 {
			f.writeByte(0)
			f.writeLongString(b.statement)
		} else {
			f.writeByte(1)
			f.writeShortBytes(b.preparedID)
		}

		f.writeShort(uint16(len(b.values)))
		for j := range b.values {
			col := b.values[j]
			if col.name != "" {
				// TODO: move this check into the caller and set a flag on writeBatchFrame
				// to indicate using named values
				if f.proto <= protoVersion5 {
					return fmt.Errorf("gocql: named query values are not supported in batches, please see https://issues.apache.org/jira/browse/CASSANDRA-10246")
				}
				flags |= flagWithNameValues
				f.writeString(col.name)
			}
			if col.isUnset {
				f.writeUnset()
			} else {
				f.writeBytes(col.value)
			}
		}
	}

	f.writeConsistency(w.consistency)

	if w.serialConsistency > 0 {
		flags |= flagWithSerialConsistency
	}

	if w.defaultTimestamp {
		flags |= flagDefaultTimestamp
	}

	if w.keyspace != "" {
		if f.proto < protoVersion5 {
			panic(fmt.Errorf("the keyspace can only be set with protocol 5 or higher"))
		}
		flags |= flagWithKeyspace
	}

	if w.nowInSeconds != nil {
		if f.proto < protoVersion5 {
			panic(fmt.Errorf("now_in_seconds can only be set with protocol 5 or higher"))
		}
		flags |= flagWithNowInSeconds
	}

	if f.proto > protoVersion4 {
		f.writeUint(flags)
	} else {
		f.writeByte(byte(flags))
	}

	if w.serialConsistency > 0 {
		f.writeConsistency(Consistency(w.serialConsistency))
	}

	if w.defaultTimestamp {
		var ts int64
		if w.defaultTimestampValue != 0 {
			ts = w.defaultTimestampValue
		} else {
			ts = time.Now().UnixNano() / 1000
		}
		f.writeLong(ts)
	}

	if w.keyspace != "" {
		f.writeString(w.keyspace)
	}

	if w.nowInSeconds != nil {
		f.writeInt(int32(*w.nowInSeconds))
	}

	return f.finish()
}

type writeOptionsFrame struct{}

func (w *writeOptionsFrame) buildFrame(framer *framer, streamID int) error {
	return framer.writeOptionsFrame(streamID, w)
}

func (f *framer) writeOptionsFrame(stream int, _ *writeOptionsFrame) error {
	f.writeHeader(f.flags&^flagCompress, opOptions, stream)
	return f.finish()
}

type writeRegisterFrame struct {
	events []string
}

func (w *writeRegisterFrame) buildFrame(framer *framer, streamID int) error {
	return framer.writeRegisterFrame(streamID, w)
}

func (f *framer) writeRegisterFrame(streamID int, w *writeRegisterFrame) error {
	f.writeHeader(f.flags, opRegister, streamID)
	f.writeStringList(w.events)

	return f.finish()
}

func (f *framer) readByte() (byte, error) {
	if len(f.buf) < 1 {
		return 0, fmt.Errorf("not enough bytes in buffer to read byte require 1 got: %d", len(f.buf))
	}

	b := f.buf[0]
	f.buf = f.buf[1:]
	return b, nil
}

func (f *framer) readInt() (int, error) {
	if len(f.buf) < 4 {
		return 0, fmt.Errorf("not enough bytes in buffer to read int require 4 got: %d", len(f.buf))
	}

	n := int(int32(f.buf[0])<<24 | int32(f.buf[1])<<16 | int32(f.buf[2])<<8 | int32(f.buf[3]))
	f.buf = f.buf[4:]
	return n, nil
}

func (f *framer) readShort() (uint16, error) {
	if len(f.buf) < 2 {
		return 0, fmt.Errorf("not enough bytes in buffer to read short require 2 got: %d", len(f.buf))
	}
	n := uint16(f.buf[0])<<8 | uint16(f.buf[1])
	f.buf = f.buf[2:]
	return n, nil
}

func (f *framer) readString() (string, error) {
	size, err := f.readShort()
	if err != nil {
		return "", err
	}

	if len(f.buf) < int(size) {
		return "", fmt.Errorf("not enough bytes in buffer to read string require %d got: %d", size, len(f.buf))
	}

	s := string(f.buf[:size])
	f.buf = f.buf[size:]
	return s, nil
}

func (f *framer) readLongString() (string, error) {
	size, err := f.readInt()
	if err != nil {
		return "", err
	}

	if len(f.buf) < size {
		return "", fmt.Errorf("not enough bytes in buffer to read long string require %d got: %d", size, len(f.buf))
	}

	s := string(f.buf[:size])
	f.buf = f.buf[size:]
	return s, err
}

func (f *framer) readStringList() ([]string, error) {
	size, err := f.readShort()
	if err != nil {
		return nil, err
	}

	l := make([]string, size)
	for i := 0; i < int(size); i++ {
		l[i], err = f.readString()
		if err != nil {
			return nil, err
		}
	}

	return l, nil
}

func (f *framer) readBytes() ([]byte, error) {
	size, err := f.readInt()
	if err != nil {
		return nil, err
	}
	if size < 0 {
		return nil, nil
	}

	if len(f.buf) < size {
		return nil, fmt.Errorf("not enough bytes in buffer to read bytes require %d got: %d", size, len(f.buf))
	}

	l := f.buf[:size]
	f.buf = f.buf[size:]

	return l, nil
}

func (f *framer) readShortBytes() ([]byte, error) {
	size, err := f.readShort()
	if err != nil {
		return nil, err
	}
	if len(f.buf) < int(size) {
		return nil, fmt.Errorf("not enough bytes in buffer to read short bytes: require %d got %d", size, len(f.buf))
	}

	b := f.buf[:size]
	f.buf = f.buf[size:]
	return b, nil
}

func (f *framer) readInetAdressOnly() (net.IP, error) {
	if len(f.buf) < 1 {
		return nil, fmt.Errorf("not enough bytes in buffer to read inet size require %d got: %d", 1, len(f.buf))
	}

	size := f.buf[0]
	f.buf = f.buf[1:]
	if !(size == 4 || size == 16) {
		return nil, fmt.Errorf("invalid IP size: %d", size)
	}

	if len(f.buf) < 1 {
		return nil, fmt.Errorf("not enough bytes in buffer to read inet require %d got: %d", size, len(f.buf))
	}

	ip := make([]byte, size)
	copy(ip, f.buf[:size])
	f.buf = f.buf[size:]
	// TODO: should we check if IP is nil?
	return net.IP(ip), nil
}

func (f *framer) readInet() (net.IP, int, error) {
	ip, err := f.readInetAdressOnly()
	if err != nil {
		return nil, 0, err
	}
	port, err := f.readShort()
	if err != nil {
		return nil, 0, err
	}
	return ip, int(port), nil
}

func (f *framer) readConsistency() (Consistency, error) {
	c, err := f.readShort()
	if err != nil {
		return 0, err
	}
	return Consistency(c), err
}

func (f *framer) readBytesMap() (map[string][]byte, error) {
	size, err := f.readShort()
	if err != nil {
		return nil, err
	}
	m := make(map[string][]byte, size)
	var k string
	for i := 0; i < int(size); i++ {
		k, err = f.readString()
		if err != nil {
			return nil, err
		}
		m[k], err = f.readBytes()
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (f *framer) readStringMultiMap() (map[string][]string, error) {
	size, err := f.readShort()
	m := make(map[string][]string, size)
	var k string
	for i := 0; i < int(size); i++ {
		k, err = f.readString()
		if err != nil {
			return nil, err
		}
		m[k], err = f.readStringList()
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (f *framer) writeByte(b byte) {
	f.buf = append(f.buf, b)
}

func appendBytes(p []byte, d []byte) []byte {
	if d == nil {
		return appendInt(p, -1)
	}
	p = appendInt(p, int32(len(d)))
	p = append(p, d...)
	return p
}

func appendShort(p []byte, n uint16) []byte {
	return append(p,
		byte(n>>8),
		byte(n),
	)
}

func appendInt(p []byte, n int32) []byte {
	return append(p, byte(n>>24),
		byte(n>>16),
		byte(n>>8),
		byte(n))
}

func appendUint(p []byte, n uint32) []byte {
	return append(p, byte(n>>24),
		byte(n>>16),
		byte(n>>8),
		byte(n))
}

func appendLong(p []byte, n int64) []byte {
	return append(p,
		byte(n>>56),
		byte(n>>48),
		byte(n>>40),
		byte(n>>32),
		byte(n>>24),
		byte(n>>16),
		byte(n>>8),
		byte(n),
	)
}

func (f *framer) writeCustomPayload(customPayload *map[string][]byte) {
	if len(*customPayload) > 0 {
		if f.proto < protoVersion4 {
			panic("Custom payload is not supported with version V3 or less")
		}
		f.writeBytesMap(*customPayload)
	}
}

// these are protocol level binary types
func (f *framer) writeInt(n int32) {
	f.buf = appendInt(f.buf, n)
}

func (f *framer) writeUint(n uint32) {
	f.buf = appendUint(f.buf, n)
}

func (f *framer) writeShort(n uint16) {
	f.buf = appendShort(f.buf, n)
}

func (f *framer) writeLong(n int64) {
	f.buf = appendLong(f.buf, n)
}

func (f *framer) writeString(s string) {
	f.writeShort(uint16(len(s)))
	f.buf = append(f.buf, s...)
}

func (f *framer) writeLongString(s string) {
	f.writeInt(int32(len(s)))
	f.buf = append(f.buf, s...)
}

func (f *framer) writeStringList(l []string) {
	f.writeShort(uint16(len(l)))
	for _, s := range l {
		f.writeString(s)
	}
}

func (f *framer) writeUnset() {
	// Protocol version 4 specifies that bind variables do not require having a
	// value when executing a statement.   Bind variables without a value are
	// called 'unset'. The 'unset' bind variable is serialized as the int
	// value '-2' without following bytes.
	f.writeInt(-2)
}

func (f *framer) writeBytes(p []byte) {
	// TODO: handle null case correctly,
	//     [bytes]        A [int] n, followed by n bytes if n >= 0. If n < 0,
	//					  no byte should follow and the value represented is `null`.
	if p == nil {
		f.writeInt(-1)
	} else {
		f.writeInt(int32(len(p)))
		f.buf = append(f.buf, p...)
	}
}

func (f *framer) writeShortBytes(p []byte) {
	f.writeShort(uint16(len(p)))
	f.buf = append(f.buf, p...)
}

func (f *framer) writeConsistency(cons Consistency) {
	f.writeShort(uint16(cons))
}

func (f *framer) writeStringMap(m map[string]string) {
	f.writeShort(uint16(len(m)))
	for k, v := range m {
		f.writeString(k)
		f.writeString(v)
	}
}

func (f *framer) writeBytesMap(m map[string][]byte) {
	f.writeShort(uint16(len(m)))
	for k, v := range m {
		f.writeString(k)
		f.writeBytes(v)
	}
}

func (f *framer) prepareModernLayout() error {
	// Ensure protocol version is V5 or higher
	if f.proto < protoVersion5 {
		panic("Modern layout is not supported with version V4 or less")
	}

	selfContained := true

	var (
		adjustedBuf []byte
		tempBuf     []byte
		err         error
	)

	// Process the buffer in chunks if it exceeds the max payload size
	for len(f.buf) > maxSegmentPayloadSize {
		if f.compres != nil {
			tempBuf, err = newCompressedSegment(f.buf[:maxSegmentPayloadSize], false, f.compres)
		} else {
			tempBuf, err = newUncompressedSegment(f.buf[:maxSegmentPayloadSize], false)
		}
		if err != nil {
			return err
		}

		adjustedBuf = append(adjustedBuf, tempBuf...)
		f.buf = f.buf[maxSegmentPayloadSize:]
		selfContained = false
	}

	// Process the remaining buffer
	if f.compres != nil {
		tempBuf, err = newCompressedSegment(f.buf, selfContained, f.compres)
	} else {
		tempBuf, err = newUncompressedSegment(f.buf, selfContained)
	}
	if err != nil {
		return err
	}

	adjustedBuf = append(adjustedBuf, tempBuf...)
	f.buf = adjustedBuf

	return nil
}

const (
	crc24Size = 3
	crc32Size = 4
)

func readUncompressedSegment(r io.Reader) ([]byte, bool, error) {
	const (
		headerSize = 3
	)

	header := [headerSize + crc24Size]byte{}

	// Read the frame header
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, false, fmt.Errorf("gocql: failed to read uncompressed frame, err: %w", err)
	}

	// Compute and verify the header CRC24
	computedHeaderCRC24 := Crc24(header[:headerSize])
	readHeaderCRC24 := uint32(header[3]) | uint32(header[4])<<8 | uint32(header[5])<<16
	if computedHeaderCRC24 != readHeaderCRC24 {
		return nil, false, fmt.Errorf("gocql: crc24 mismatch in frame header, computed: %d, got: %d", computedHeaderCRC24, readHeaderCRC24)
	}

	// Extract the payload length and self-contained flag
	headerInt := uint32(header[0]) | uint32(header[1])<<8 | uint32(header[2])<<16
	payloadLen := int(headerInt & maxSegmentPayloadSize)
	isSelfContained := (headerInt & (1 << 17)) != 0

	// Read the payload
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, false, fmt.Errorf("gocql: failed to read uncompressed frame payload, err: %w", err)
	}

	// Read and verify the payload CRC32
	if _, err := io.ReadFull(r, header[:crc32Size]); err != nil {
		return nil, false, fmt.Errorf("gocql: failed to read payload crc32, err: %w", err)
	}

	computedPayloadCRC32 := Crc32(payload)
	readPayloadCRC32 := binary.LittleEndian.Uint32(header[:crc32Size])
	if computedPayloadCRC32 != readPayloadCRC32 {
		return nil, false, fmt.Errorf("gocql: payload crc32 mismatch, computed: %d, got: %d", computedPayloadCRC32, readPayloadCRC32)
	}

	return payload, isSelfContained, nil
}

func newUncompressedSegment(payload []byte, isSelfContained bool) ([]byte, error) {
	const (
		headerSize       = 6
		selfContainedBit = 1 << 17
	)

	payloadLen := len(payload)
	if payloadLen > maxSegmentPayloadSize {
		return nil, fmt.Errorf("gocql: payload length (%d) exceeds maximum size of %d", payloadLen, maxSegmentPayloadSize)
	}

	// Create the segment
	segmentSize := headerSize + payloadLen + crc32Size
	segment := make([]byte, segmentSize)

	// First 3 bytes: payload length and self-contained flag
	headerInt := uint32(payloadLen)
	if isSelfContained {
		headerInt |= selfContainedBit // Set the self-contained flag
	}

	// Encode the first 3 bytes as a single little-endian integer
	segment[0] = byte(headerInt)
	segment[1] = byte(headerInt >> 8)
	segment[2] = byte(headerInt >> 16)

	// Calculate CRC24 for the first 3 bytes of the header
	crc := Crc24(segment[:3])

	// Encode CRC24 into the next 3 bytes of the header
	segment[3] = byte(crc)
	segment[4] = byte(crc >> 8)
	segment[5] = byte(crc >> 16)

	copy(segment[headerSize:], payload) // Copy the payload to the segment

	// Calculate CRC32 for the payload
	payloadCRC32 := Crc32(payload)
	binary.LittleEndian.PutUint32(segment[headerSize+payloadLen:], payloadCRC32)

	return segment, nil
}

func newCompressedSegment(uncompressedPayload []byte, isSelfContained bool, compressor Compressor) ([]byte, error) {
	const (
		headerSize       = 5
		selfContainedBit = 1 << 34
	)

	uncompressedLen := len(uncompressedPayload)
	if uncompressedLen > maxSegmentPayloadSize {
		return nil, fmt.Errorf("gocql: payload length (%d) exceeds maximum size of %d", uncompressedPayload, maxSegmentPayloadSize)
	}

	compressedPayload, err := compressor.AppendCompressed(nil, uncompressedPayload)
	if err != nil {
		return nil, err
	}

	compressedLen := len(compressedPayload)

	// Compression is not worth it
	if uncompressedLen < compressedLen {
		// native_protocol_v5.spec
		// 2.2
		//  An uncompressed length of 0 signals that the compressed payload
		//  should be used as-is and not decompressed.
		compressedPayload = uncompressedPayload
		compressedLen = uncompressedLen
		uncompressedLen = 0
	}

	// Combine compressed and uncompressed lengths and set the self-contained flag if needed
	combined := uint64(compressedLen) | uint64(uncompressedLen)<<17
	if isSelfContained {
		combined |= selfContainedBit
	}

	var headerBuf [headerSize + crc24Size]byte

	// Write the combined value into the header buffer
	binary.LittleEndian.PutUint64(headerBuf[:], combined)

	// Create a buffer with enough capacity to hold the header, compressed payload, and checksums
	buf := bytes.NewBuffer(make([]byte, 0, headerSize+crc24Size+compressedLen+crc32Size))

	// Write the first 5 bytes of the header (compressed and uncompressed sizes)
	buf.Write(headerBuf[:headerSize])

	// Compute and write the CRC24 checksum of the first 5 bytes
	headerChecksum := Crc24(headerBuf[:headerSize])

	// LittleEndian 3 bytes
	headerBuf[0] = byte(headerChecksum)
	headerBuf[1] = byte(headerChecksum >> 8)
	headerBuf[2] = byte(headerChecksum >> 16)
	buf.Write(headerBuf[:3])

	buf.Write(compressedPayload)

	// Compute and write the CRC32 checksum of the payload
	payloadChecksum := Crc32(compressedPayload)
	binary.LittleEndian.PutUint32(headerBuf[:], payloadChecksum)
	buf.Write(headerBuf[:4])

	return buf.Bytes(), nil
}

func readCompressedSegment(r io.Reader, compressor Compressor) ([]byte, bool, error) {
	const headerSize = 5
	var (
		headerBuf [headerSize + crc24Size]byte
		err       error
	)

	if _, err = io.ReadFull(r, headerBuf[:]); err != nil {
		return nil, false, err
	}

	// Reading checksum from frame header
	readHeaderChecksum := uint32(headerBuf[5]) | uint32(headerBuf[6])<<8 | uint32(headerBuf[7])<<16
	if computedHeaderChecksum := Crc24(headerBuf[:headerSize]); computedHeaderChecksum != readHeaderChecksum {
		return nil, false, fmt.Errorf("gocql: crc24 mismatch in frame header, read: %d, computed: %d", readHeaderChecksum, computedHeaderChecksum)
	}

	// First 17 bits - payload size after compression
	compressedLen := uint32(headerBuf[0]) | uint32(headerBuf[1])<<8 | uint32(headerBuf[2]&0x1)<<16

	// The next 17 bits - payload size before compression
	uncompressedLen := (uint32(headerBuf[2]) >> 1) | uint32(headerBuf[3])<<7 | uint32(headerBuf[4]&0b11)<<15

	// Self-contained flag
	selfContained := (headerBuf[4] & 0b100) != 0

	compressedPayload := make([]byte, compressedLen)
	if _, err = io.ReadFull(r, compressedPayload); err != nil {
		return nil, false, fmt.Errorf("gocql: failed to read compressed frame payload, err: %w", err)
	}

	if _, err = io.ReadFull(r, headerBuf[:crc32Size]); err != nil {
		return nil, false, fmt.Errorf("gocql: failed to read payload crc32, err: %w", err)
	}

	// Ensuring if payload checksum matches
	readPayloadChecksum := binary.LittleEndian.Uint32(headerBuf[:crc32Size])
	if computedPayloadChecksum := Crc32(compressedPayload); readPayloadChecksum != computedPayloadChecksum {
		return nil, false, fmt.Errorf("gocql: crc32 mismatch in payload, read: %d, computed: %d", readPayloadChecksum, computedPayloadChecksum)
	}

	var uncompressedPayload []byte
	if uncompressedLen > 0 {
		if uncompressedPayload, err = compressor.AppendDecompressed(nil, compressedPayload, uncompressedLen); err != nil {
			return nil, false, err
		}
		if uint32(len(uncompressedPayload)) != uncompressedLen {
			return nil, false, fmt.Errorf("gocql: length mismatch after payload decoding, got %d, expected %d", len(uncompressedPayload), uncompressedLen)
		}
	} else {
		uncompressedPayload = compressedPayload
	}

	return uncompressedPayload, selfContained, nil
}
