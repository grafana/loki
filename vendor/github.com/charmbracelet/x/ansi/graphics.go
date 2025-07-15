package ansi

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi/kitty"
)

// KittyGraphics returns a sequence that encodes the given image in the Kitty
// graphics protocol.
//
//	APC G [comma separated options] ; [base64 encoded payload] ST
//
// See https://sw.kovidgoyal.net/kitty/graphics-protocol/
func KittyGraphics(payload []byte, opts ...string) string {
	var buf bytes.Buffer
	buf.WriteString("\x1b_G")
	buf.WriteString(strings.Join(opts, ","))
	if len(payload) > 0 {
		buf.WriteString(";")
		buf.Write(payload)
	}
	buf.WriteString("\x1b\\")
	return buf.String()
}

var (
	// KittyGraphicsTempDir is the directory where temporary files are stored.
	// This is used in [WriteKittyGraphics] along with [os.CreateTemp].
	KittyGraphicsTempDir = ""

	// KittyGraphicsTempPattern is the pattern used to create temporary files.
	// This is used in [WriteKittyGraphics] along with [os.CreateTemp].
	// The Kitty Graphics protocol requires the file path to contain the
	// substring "tty-graphics-protocol".
	KittyGraphicsTempPattern = "tty-graphics-protocol-*"
)

// WriteKittyGraphics writes an image using the Kitty Graphics protocol with
// the given options to w. It chunks the written data if o.Chunk is true.
//
// You can omit m and use nil when rendering an image from a file. In this
// case, you must provide a file path in o.File and use o.Transmission =
// [kitty.File]. You can also use o.Transmission = [kitty.TempFile] to write
// the image to a temporary file. In that case, the file path is ignored, and
// the image is written to a temporary file that is automatically deleted by
// the terminal.
//
// See https://sw.kovidgoyal.net/kitty/graphics-protocol/
func WriteKittyGraphics(w io.Writer, m image.Image, o *kitty.Options) error {
	if o == nil {
		o = &kitty.Options{}
	}

	if o.Transmission == 0 && len(o.File) != 0 {
		o.Transmission = kitty.File
	}

	var data bytes.Buffer // the data to be encoded into base64
	e := &kitty.Encoder{
		Compress: o.Compression == kitty.Zlib,
		Format:   o.Format,
	}

	switch o.Transmission {
	case kitty.Direct:
		if err := e.Encode(&data, m); err != nil {
			return fmt.Errorf("failed to encode direct image: %w", err)
		}

	case kitty.SharedMemory:
		// TODO: Implement shared memory
		return fmt.Errorf("shared memory transmission is not yet implemented")

	case kitty.File:
		if len(o.File) == 0 {
			return kitty.ErrMissingFile
		}

		f, err := os.Open(o.File)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}

		defer f.Close() //nolint:errcheck

		stat, err := f.Stat()
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}

		mode := stat.Mode()
		if !mode.IsRegular() {
			return fmt.Errorf("file is not a regular file")
		}

		// Write the file path to the buffer
		if _, err := data.WriteString(f.Name()); err != nil {
			return fmt.Errorf("failed to write file path to buffer: %w", err)
		}

	case kitty.TempFile:
		f, err := os.CreateTemp(KittyGraphicsTempDir, KittyGraphicsTempPattern)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}

		defer f.Close() //nolint:errcheck

		if err := e.Encode(f, m); err != nil {
			return fmt.Errorf("failed to encode image to file: %w", err)
		}

		// Write the file path to the buffer
		if _, err := data.WriteString(f.Name()); err != nil {
			return fmt.Errorf("failed to write file path to buffer: %w", err)
		}
	}

	// Encode image to base64
	var payload bytes.Buffer // the base64 encoded image to be written to w
	b64 := base64.NewEncoder(base64.StdEncoding, &payload)
	if _, err := data.WriteTo(b64); err != nil {
		return fmt.Errorf("failed to write base64 encoded image to payload: %w", err)
	}
	if err := b64.Close(); err != nil {
		return err
	}

	// If not chunking, write all at once
	if !o.Chunk {
		_, err := io.WriteString(w, KittyGraphics(payload.Bytes(), o.Options()...))
		return err
	}

	// Write in chunks
	var (
		err error
		n   int
	)
	chunk := make([]byte, kitty.MaxChunkSize)
	isFirstChunk := true

	for {
		// Stop if we read less than the chunk size [kitty.MaxChunkSize].
		n, err = io.ReadFull(&payload, chunk)
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read chunk: %w", err)
		}

		opts := buildChunkOptions(o, isFirstChunk, false)
		if _, err := io.WriteString(w, KittyGraphics(chunk[:n], opts...)); err != nil {
			return err
		}

		isFirstChunk = false
	}

	// Write the last chunk
	opts := buildChunkOptions(o, isFirstChunk, true)
	_, err = io.WriteString(w, KittyGraphics(chunk[:n], opts...))
	return err
}

// buildChunkOptions creates the options slice for a chunk
func buildChunkOptions(o *kitty.Options, isFirstChunk, isLastChunk bool) []string {
	var opts []string
	if isFirstChunk {
		opts = o.Options()
	} else {
		// These options are allowed in subsequent chunks
		if o.Quite > 0 {
			opts = append(opts, fmt.Sprintf("q=%d", o.Quite))
		}
		if o.Action == kitty.Frame {
			opts = append(opts, "a=f")
		}
	}

	if !isFirstChunk || !isLastChunk {
		// We don't need to encode the (m=) option when we only have one chunk.
		if isLastChunk {
			opts = append(opts, "m=0")
		} else {
			opts = append(opts, "m=1")
		}
	}
	return opts
}
