package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/moby/moby/v2/daemon/logger"

	"github.com/grafana/loki/v3/clients/cmd/docker-driver/logdriver"
)

type recordingLogger struct {
	mu     sync.Mutex
	lines  []string
	closes int
}

func (r *recordingLogger) Log(m *logger.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, string(m.Line))
	return nil
}

func (r *recordingLogger) Name() string { return "recording" }

func (r *recordingLogger) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closes++
	return nil
}

type countingStream struct {
	io.Reader
	closes int
}

func (c *countingStream) Close() error {
	c.closes++
	return nil
}

// StopLogging and consumeLog can both reach Close, so it must be safe to call
// more than once.
func TestLogPairCloseIsIdempotent(t *testing.T) {
	lokil := &recordingLogger{}
	jsonl := &recordingLogger{}
	stream := &countingStream{Reader: bytes.NewReader(nil)}
	lf := &logPair{lokil: lokil, jsonl: jsonl, stream: stream, logger: log.NewNopLogger()}

	lf.Close()
	lf.Close()
	lf.Close()

	if stream.closes != 1 {
		t.Errorf("stream closed %d times, want 1", stream.closes)
	}
	if lokil.closes != 1 {
		t.Errorf("loki logger closed %d times, want 1", lokil.closes)
	}
	if jsonl.closes != 1 {
		t.Errorf("json logger closed %d times, want 1", jsonl.closes)
	}
}

// A frame whose payload will not unmarshal was still read in full, so the
// consumer must skip it and keep shipping the frames that follow.
func TestConsumeLogSkipsUndecodablePayload(t *testing.T) {
	var buf bytes.Buffer

	// Field 1 declared as varint where LogEntry.Line expects bytes.
	bad := []byte{0x08, 0x80}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(bad)))
	buf.Write(header)
	buf.Write(bad)

	for _, line := range []string{"first survivor", "second survivor"} {
		if err := logdriver.NewLogEntryEncoder(&buf).Encode(&logdriver.LogEntry{
			Line:     []byte(line),
			Source:   "stdout",
			TimeNano: 1,
		}); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}

	lokil := &recordingLogger{}
	stream := &countingStream{Reader: bytes.NewReader(buf.Bytes())}
	lf := &logPair{
		lokil:  lokil,
		stream: stream,
		info:   logger.Info{ContainerID: "abc"},
		logger: log.NewNopLogger(),
	}

	consumeLog(lf)

	want := []string{"first survivor", "second survivor"}
	if len(lokil.lines) != len(want) {
		t.Fatalf("shipped %d lines (%q), want %d after skipping one bad frame", len(lokil.lines), lokil.lines, len(want))
	}
	for i, line := range want {
		if lokil.lines[i] != line {
			t.Errorf("line %d = %q, want %q", i, lokil.lines[i], line)
		}
	}

	// consumeLog owns cleanup on the way out.
	if stream.closes != 1 {
		t.Errorf("stream closed %d times, want 1: consumeLog must close the logPair", stream.closes)
	}
}

// An oversized frame leaves the stream at an unknown offset, so the consumer
// must stop rather than carry on misaligned.
func TestConsumeLogStopsOnFramingError(t *testing.T) {
	var buf bytes.Buffer
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, 1e6+1)
	buf.Write(header)
	buf.Write(bytes.Repeat([]byte("z"), 64))

	// A valid frame after it must NOT be shipped: alignment is already lost.
	if err := logdriver.NewLogEntryEncoder(&buf).Encode(&logdriver.LogEntry{Line: []byte("unreachable")}); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	lokil := &recordingLogger{}
	stream := &countingStream{Reader: bytes.NewReader(buf.Bytes())}
	lf := &logPair{
		lokil:  lokil,
		stream: stream,
		info:   logger.Info{ContainerID: "abc"},
		logger: log.NewNopLogger(),
	}

	consumeLog(lf)

	if len(lokil.lines) != 0 {
		t.Errorf("shipped %q after a framing error, want nothing", lokil.lines)
	}
	if stream.closes != 1 {
		t.Errorf("stream closed %d times, want 1", stream.closes)
	}
}
