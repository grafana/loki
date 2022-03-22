package writer

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"

	"github.com/unchartedsoftware/witch/cursor"
)

const (
	// When the child side of the pty is closed when it dies, the subsequent
	// read on ptmx is expected to fail.
	ptyErr = "read /dev/ptmx: input/output error"
)

var (
	mu = &sync.Mutex{}
)

// PrettyWriter represents a pretty formatteed writer
type PrettyWriter struct {
	file *os.File
	name string
}

// NewPretty instantiates and returns a new pretty writer.
func NewPretty(name string, file *os.File) *PrettyWriter {
	return &PrettyWriter{
		name: name,
		file: file,
	}
}

// Write implements the standard Write interface.
func (w *PrettyWriter) Write(p []byte) (int, error) {
	w.WriteStringf(string(p))
	return len(p), nil
}

func writeStringf(n string, file *os.File, format string, args ...interface{}) {
	stamp := color.HiBlackString("[%s]", time.Now().Format(time.Stamp))
	name := color.GreenString("[%s]", n)
	wand := fmt.Sprintf("%s%s", color.GreenString("--"), color.MagentaString("⭑"))
	msg := color.HiBlackString("%s", fmt.Sprintf(format, args...))
	mu.Lock()
	fmt.Fprintf(file, "%s\r%s %s %s %s", cursor.ClearLine, stamp, name, wand, msg)
	mu.Unlock()
}

// WriteStringf writes the provided formatted string to the underlying
// interface.
func (w *PrettyWriter) WriteStringf(format string, args ...interface{}) {
	writeStringf(w.name, w.file, format, args...)
}

// CmdWriter represents a writer to log an output from the executed cmd.
type CmdWriter struct {
	name         string
	file         *os.File
	proxy        *os.File
	scanner      *bufio.Scanner
	maxTokenSize int
	buffer       string
	kill         chan bool
}

// NewCmd instantiates and returns a new cmd writer.
func NewCmd(name string, file *os.File) *CmdWriter {
	return &CmdWriter{
		name: name,
		file: file,
		kill: make(chan bool),
	}
}

// MaxTokenSize sets the max token size for the underlying scanner.
func (w *CmdWriter) MaxTokenSize(numBytes int) {
	w.maxTokenSize = numBytes
}

// Proxy will forward the output from the provided os.File through the writer.
func (w *CmdWriter) Proxy(f *os.File) {
	mu.Lock()
	// if we have an existing proxy, send EOF to kill it.
	if w.proxy != nil {
		// wait until its dead
		<-w.kill
	}
	// create new proxy
	w.proxy = f
	w.scanner = bufio.NewScanner(w.proxy)

	buf := make([]byte, w.maxTokenSize)
	w.scanner.Buffer(buf, w.maxTokenSize)

	mu.Unlock()
	go func() {
		for w.scanner.Scan() {
			line := w.scanner.Text()
			w.Write([]byte(line + "\n"))
		}
		err := w.scanner.Err()
		if err != nil {
			if err.Error() != ptyErr {
				writeStringf(w.name, w.file, "%s%s\n", color.HiRedString("proxy writer error: "), err.Error())
				os.Exit(3)
			}
		}
		w.kill <- true
	}()
}

// Write implements the standard Write interface.
func (w *CmdWriter) Write(p []byte) (int, error) {
	mu.Lock()
	// append to buffer
	w.buffer += string(p)
	for {
		index := strings.IndexAny(w.buffer, "\n")
		if index == -1 {
			// no endline
			break
		}
		fmt.Fprintf(w.file, "%s\r%s", cursor.ClearLine, w.buffer[0:index+1])
		w.buffer = w.buffer[index+1:]
	}
	mu.Unlock()
	return len(p), nil
}

// Flush writes any buffered data to the underlying io.Writer.
func (w *CmdWriter) Flush() error {
	_, err := w.Write([]byte(w.buffer))
	if err != nil {
		return err
	}
	mu.Lock()
	if len(w.buffer) > 0 {
		fmt.Fprintf(w.file, "%s\r%s\n", cursor.ClearLine, w.buffer)
	}
	mu.Unlock()
	return nil
}
