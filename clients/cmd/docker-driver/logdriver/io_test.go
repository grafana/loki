package logdriver

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewLogEntryEncoder(&buf)

	want := []string{"first line", "second line", strings.Repeat("x", 8192)}
	for _, line := range want {
		if err := enc.Encode(&LogEntry{Line: []byte(line), Source: "stdout", TimeNano: 42}); err != nil {
			t.Fatalf("Encode(%.10q): %v", line, err)
		}
	}

	dec := NewLogEntryDecoder(&buf)
	for _, line := range want {
		var got LogEntry
		if err := dec.Decode(&got); err != nil {
			t.Fatalf("Decode(%.10q): %v", line, err)
		}
		if string(got.Line) != line {
			t.Errorf("Line = %.20q, want %.20q", got.Line, line)
		}
		if got.Source != "stdout" || got.TimeNano != 42 {
			t.Errorf("Source/TimeNano = %q/%d, want stdout/42", got.Source, got.TimeNano)
		}
	}

	var extra LogEntry
	if err := dec.Decode(&extra); err != io.EOF {
		t.Errorf("Decode after last entry = %v, want io.EOF", err)
	}
}

// A frame length prefix is attacker- or corruption-controlled, so the decoder
// must reject an oversized one rather than allocate what it asks for.
func TestDecodeRejectsOversizedFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		size uint32
	}{
		{"just over the limit", maxFrameSize + 1},
		{"four gigabytes", ^uint32(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			header := make([]byte, binaryEncodeLen)
			binary.BigEndian.PutUint32(header, tc.size)
			buf.Write(header)

			var got LogEntry
			err := NewLogEntryDecoder(&buf).Decode(&got)
			if err == nil {
				t.Fatalf("Decode(size=%d) = nil, want an error", tc.size)
			}
			if !strings.Contains(err.Error(), "exceeds maximum") {
				t.Errorf("Decode(size=%d) = %v, want a frame-size error", tc.size, err)
			}
		})
	}
}

func TestDecodeAcceptsFrameAtLimit(t *testing.T) {
	var buf bytes.Buffer
	if err := NewLogEntryEncoder(&buf).Encode(&LogEntry{Line: bytes.Repeat([]byte("y"), int(maxFrameSize)-64)}); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	var got LogEntry
	if err := NewLogEntryDecoder(&buf).Decode(&got); err != nil {
		t.Fatalf("Decode of a frame under the limit: %v", err)
	}
	if len(got.Line) != int(maxFrameSize)-64 {
		t.Errorf("len(Line) = %d, want %d", len(got.Line), int(maxFrameSize)-64)
	}
}

// A payload that fails to unmarshal has still been read in full, so the stream
// stays aligned and the next frame must decode normally.
func TestDecodeRecoversAfterBadPayload(t *testing.T) {
	var buf bytes.Buffer

	// A frame whose payload is not a valid LogEntry: field 1 is declared as
	// varint where LogEntry.Line expects bytes.
	bad := []byte{0x08, 0x80}
	header := make([]byte, binaryEncodeLen)
	binary.BigEndian.PutUint32(header, uint32(len(bad)))
	buf.Write(header)
	buf.Write(bad)

	if err := NewLogEntryEncoder(&buf).Encode(&LogEntry{Line: []byte("after the bad frame")}); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	dec := NewLogEntryDecoder(&buf)

	var first LogEntry
	err := dec.Decode(&first)
	if err == nil {
		t.Fatal("Decode of the bad payload = nil, want an error")
	}
	if !errors.Is(err, ErrPayloadUnmarshal) {
		t.Fatalf("Decode of the bad payload = %v, want ErrPayloadUnmarshal so the caller can recover", err)
	}

	var second LogEntry
	if err := dec.Decode(&second); err != nil {
		t.Fatalf("Decode of the frame after a bad payload: %v", err)
	}
	if string(second.Line) != "after the bad frame" {
		t.Errorf("Line = %q, want %q", second.Line, "after the bad frame")
	}
}

// An oversized frame is fatal: the declared length was never consumed, so the
// caller must not treat it as recoverable.
func TestOversizedFrameIsNotRecoverable(t *testing.T) {
	var buf bytes.Buffer
	header := make([]byte, binaryEncodeLen)
	binary.BigEndian.PutUint32(header, maxFrameSize+1)
	buf.Write(header)

	var got LogEntry
	err := NewLogEntryDecoder(&buf).Decode(&got)
	if err == nil {
		t.Fatal("Decode = nil, want an error")
	}
	if errors.Is(err, ErrPayloadUnmarshal) {
		t.Errorf("Decode = %v, must not be ErrPayloadUnmarshal: stream alignment is unknown", err)
	}
}
