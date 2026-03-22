package framedstdcopy

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/pkg/stdcopy"
)

const (
	tsPrefix                   string = "2024-03-14T15:32:05.358979323Z "
	unprefixedFramePayloadSize int    = 16384
)

func timestamped(bytes []byte) []byte {
	var ts = []byte(tsPrefix)
	return append(ts, bytes...)
}

func getSrcBuffer(stdOutFrames, stdErrFrames [][]byte) (buffer *bytes.Buffer, err error) {
	buffer = new(bytes.Buffer)
	dstOut := stdcopy.NewStdWriter(buffer, stdcopy.Stdout)
	for _, stdOutBytes := range stdOutFrames {
		_, err = dstOut.Write(timestamped(stdOutBytes))
		if err != nil {
			return
		}
	}
	dstErr := stdcopy.NewStdWriter(buffer, stdcopy.Stderr)
	for _, stdErrBytes := range stdErrFrames {
		_, err = dstErr.Write(timestamped(stdErrBytes))
		if err != nil {
			return
		}
	}
	return
}

type streamChans struct {
	out          chan []byte
	err          chan []byte
	outCollected [][]byte
	errCollected [][]byte
	wg           sync.WaitGroup
}

func newChans() streamChans {
	return streamChans{
		out:          make(chan []byte),
		err:          make(chan []byte),
		outCollected: make([][]byte, 0),
		errCollected: make([][]byte, 0),
	}
}

func (crx *streamChans) collectFrames() {
	crx.wg.Add(1)
	outClosed := false
	errClosed := false
	for {
		if outClosed && errClosed {
			crx.wg.Done()
			return
		}
		select {
		case bytes, ok := <-crx.out:
			outClosed = !ok
			if bytes != nil {
				crx.outCollected = append(crx.outCollected, bytes)
			}
		case bytes, ok := <-crx.err:
			errClosed = !ok
			if bytes != nil {
				crx.errCollected = append(crx.errCollected, bytes)
			}
		}
	}
}
func (crx *streamChans) close() {
	close(crx.out)
	close(crx.err)
}

func TestStdCopyWriteAndRead(t *testing.T) {
	ostr := strings.Repeat("o", unprefixedFramePayloadSize)
	estr := strings.Repeat("e", unprefixedFramePayloadSize)
	buffer, err := getSrcBuffer(
		[][]byte{
			[]byte(ostr),
			[]byte(ostr[:3] + "\n"),
		},
		[][]byte{
			[]byte(estr),
			[]byte(estr[:3] + "\n"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	rx := newChans()
	go rx.collectFrames()
	written, err := FramedStdCopy(rx.out, rx.err, buffer)
	rx.close()
	rx.wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	tslen := len(tsPrefix)
	expectedTotalWritten := 2*maxFrameLen + 2*(4+tslen)
	if written != int64(expectedTotalWritten) {
		t.Fatalf("Expected to have total of %d bytes written, got %d", expectedTotalWritten, written)
	}
	if !bytes.Equal(rx.outCollected[0][tslen:maxFrameLen], []byte(ostr)) {
		t.Fatal("Expected the first out frame to be all 'o'")
	}
	if !bytes.Equal(rx.outCollected[1][tslen:tslen+4], []byte("ooo\n")) {
		t.Fatal("Expected the second out frame to be 'ooo\\n'")
	}
	if !bytes.Equal(rx.errCollected[0][tslen:maxFrameLen], []byte(estr)) {
		t.Fatal("Expected the first err frame to be all 'e'")
	}
	if !bytes.Equal(rx.errCollected[1][tslen:tslen+4], []byte("eee\n")) {
		t.Fatal("Expected the second err frame to be 'eee\\n'")
	}
}

type customReader struct {
	n            int
	err          error
	totalCalls   int
	correctCalls int
	src          *bytes.Buffer
}

func (f *customReader) Read(buf []byte) (int, error) {
	f.totalCalls++
	if f.totalCalls <= f.correctCalls {
		return f.src.Read(buf)
	}
	return f.n, f.err
}

func TestStdCopyReturnsErrorReadingHeader(t *testing.T) {
	expectedError := errors.New("error")
	reader := &customReader{
		err: expectedError,
	}
	discard := newChans()
	go discard.collectFrames()
	written, err := FramedStdCopy(discard.out, discard.err, reader)
	discard.close()
	if written != 0 {
		t.Fatalf("Expected 0 bytes read, got %d", written)
	}
	if err != expectedError {
		t.Fatalf("Didn't get expected error")
	}
}

func TestStdCopyReturnsErrorReadingFrame(t *testing.T) {
	expectedError := errors.New("error")
	stdOutBytes := []byte(strings.Repeat("o", unprefixedFramePayloadSize))
	stdErrBytes := []byte(strings.Repeat("e", unprefixedFramePayloadSize))
	buffer, err := getSrcBuffer([][]byte{stdOutBytes}, [][]byte{stdErrBytes})
	if err != nil {
		t.Fatal(err)
	}
	reader := &customReader{
		correctCalls: 1,
		n:            stdWriterPrefixLen + 1,
		err:          expectedError,
		src:          buffer,
	}
	discard := newChans()
	go discard.collectFrames()
	written, err := FramedStdCopy(discard.out, discard.err, reader)
	discard.close()
	if written != 0 {
		t.Fatalf("Expected 0 bytes read, got %d", written)
	}
	if err != expectedError {
		t.Fatalf("Didn't get expected error")
	}
}

func TestStdCopyDetectsCorruptedFrame(t *testing.T) {
	stdOutBytes := []byte(strings.Repeat("o", unprefixedFramePayloadSize))
	stdErrBytes := []byte(strings.Repeat("e", unprefixedFramePayloadSize))
	buffer, err := getSrcBuffer([][]byte{stdOutBytes}, [][]byte{stdErrBytes})
	if err != nil {
		t.Fatal(err)
	}
	reader := &customReader{
		correctCalls: 1,
		n:            stdWriterPrefixLen + 1,
		err:          io.EOF,
		src:          buffer,
	}
	discard := newChans()
	go discard.collectFrames()
	written, err := FramedStdCopy(discard.out, discard.err, reader)
	discard.close()
	if written != maxFrameLen {
		t.Fatalf("Expected %d bytes read, got %d", 0, written)
	}
	if err != nil {
		t.Fatal("Didn't get nil error")
	}
}

func TestStdCopyWithInvalidInputHeader(t *testing.T) {
	dst := newChans()
	go dst.collectFrames()
	src := strings.NewReader("Invalid input")
	_, err := FramedStdCopy(dst.out, dst.err, src)
	dst.close()
	if err == nil {
		t.Fatal("FramedStdCopy with invalid input header should fail.")
	}
}

func TestStdCopyWithCorruptedPrefix(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03}
	src := bytes.NewReader(data)
	written, err := FramedStdCopy(nil, nil, src)
	if err != nil {
		t.Fatalf("FramedStdCopy should not return an error with corrupted prefix.")
	}
	if written != 0 {
		t.Fatalf("FramedStdCopy should have written 0, but has written %d", written)
	}
}

// TestStdCopyReturnsErrorFromSystem tests that FramedStdCopy correctly returns an
// error, when that error is muxed into the Systemerr stream.
func TestStdCopyReturnsErrorFromSystem(t *testing.T) {
	// write in the basic messages, just so there's some fluff in there
	stdOutBytes := []byte(strings.Repeat("o", unprefixedFramePayloadSize))
	stdErrBytes := []byte(strings.Repeat("e", unprefixedFramePayloadSize))
	buffer, err := getSrcBuffer([][]byte{stdOutBytes}, [][]byte{stdErrBytes})
	if err != nil {
		t.Fatal(err)
	}
	// add in an error message on the Systemerr stream
	systemErrBytes := []byte(strings.Repeat("S", unprefixedFramePayloadSize))
	systemWriter := stdcopy.NewStdWriter(buffer, stdcopy.Systemerr)
	_, err = systemWriter.Write(systemErrBytes)
	if err != nil {
		t.Fatal(err)
	}

	// now copy and demux. we should expect an error containing the string we
	// wrote out
	discard := newChans()
	go discard.collectFrames()
	_, err = FramedStdCopy(discard.out, discard.err, buffer)
	discard.close()
	if err == nil {
		t.Fatal("expected error, got none")
	}
	if !strings.Contains(err.Error(), string(systemErrBytes)) {
		t.Fatal("expected error to contain message")
	}
}
