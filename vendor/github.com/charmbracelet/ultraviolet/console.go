package uv

import (
	"io"
	"os"
	"runtime"

	"github.com/charmbracelet/x/term"
)

var isWindows = runtime.GOOS == "windows"

// File is an interface that represents a file with a file descriptor.
//
// This is typically an [os.File] like [os.Stdin] and [os.Stdout].
type File interface {
	io.ReadWriteCloser
	term.File

	// Name returns the name of the file.
	Name() string
}

// Winsize represents the size of a terminal in cells and pixels.
//
// This is the same as [unix.Winsize], but defined here for cross-platform compatibility.
type Winsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

// Console represents a cross-platform console I/O interface.
type Console interface {
	io.ReadWriteCloser

	// Environ returns the console's environment variables.
	Environ() []string

	// Getenv retrieves the value of the environment variable named by the key.
	Getenv(key string) string

	// LookupEnv retrieves the value of the environment variable named by the key
	// and a boolean indicating whether the variable is present.
	LookupEnv(key string) (string, bool)

	// Reader returns the input reader of the console.
	Reader() io.Reader

	// Writer returns the output writer of the console.
	Writer() io.Writer

	// MakeRaw puts the console input side into raw mode.
	MakeRaw() (state *term.State, err error)

	// Restore restores the console to its previous state.
	Restore() error

	// GetSize returns the current size of the console.
	GetSize() (width, height int, err error)

	// GetWinsize returns the current size of the console in cells and pixels.
	GetWinsize() (*Winsize, error)
}

// TTY represents a Unix TTY device. It implements the [Console] interface.
type TTY struct {
	*console
}

var _ Console = (*TTY)(nil)

// WinCon represents a Windows Console. It implements the [Console] interface.
type WinCon struct {
	*console
}

var _ Console = (*WinCon)(nil)

// Console is a cross-platform console I/O.
type console struct {
	input       File
	inputState  *term.State
	output      File
	outputState *term.State
	environ     Environ
}

// DefaultConsole returns a new default console instance that uses standard I/O
// [os.Stdin], [os.Stdout], and [os.Environ].
//
// To use [os.Stderr] as the output, you can create a new console with
// [NewConsole] and pass [os.Stderr] as the output parameter.
func DefaultConsole() Console {
	return NewConsole(os.Stdin, os.Stdout, os.Environ())
}

// ControllingConsole returns a new console instance that uses the current
// controlling terminal's input and output file descriptors.
func ControllingConsole() (Console, error) {
	inTty, outTty, err := OpenTTY()
	if err != nil {
		return nil, err
	}
	return NewConsole(inTty, outTty, os.Environ()), nil
}

// NewConsole creates a new [Console] with the given input, output, and
// environment variables.
//
// You can use [OpenTTY] to open the current controlling console files and pass
// them to this function. Use [ControllingConsole] for a convenience function
// that does this for you.
//
// Use this to create a new terminal for PTY processes by passing the PTY slave
// file as the input and output and any environment variables the process
// needs.
func NewConsole(input, output File, environ []string) Console {
	if input == nil {
		input = os.Stdin
	}
	if output == nil {
		output = os.Stdout
	}
	if environ == nil {
		environ = os.Environ()
	}
	return newConsole(input, output, environ)
}

// Environ returns the console's environment variables.
func (t *console) Environ() []string {
	return t.environ
}

// Writer returns the output writer of the console.
func (t *console) Writer() io.Writer {
	return t.output
}

// Reader returns the input reader of the console.
func (t *console) Reader() io.Reader {
	return t.input
}

// Write writes data to the console's output.
func (t *console) Write(p []byte) (n int, err error) {
	return t.output.Write(p)
}

// Read reads data from the console's input.
func (t *console) Read(p []byte) (n int, err error) {
	return t.input.Read(p)
}

// Getenv retrieves the value of the environment variable named by the key.
func (t *console) Getenv(key string) string {
	return t.environ.Getenv(key)
}

// LookupEnv retrieves the value of the environment variable named by the key
// and a boolean indicating whether the variable is present.
func (t *console) LookupEnv(key string) (string, bool) {
	return t.environ.LookupEnv(key)
}

// MakeRaw puts the console input side into raw mode.
func (t *console) MakeRaw() (state *term.State, err error) {
	inState, outState, err := makeRaw(t.input, t.output)
	if err != nil {
		return nil, err
	}
	t.inputState = inState
	t.outputState = outState
	if inState != nil {
		return inState, nil
	}
	return outState, nil
}

// Restore restores the console to its previous state.
func (t *console) Restore() error {
	if t.inputState != nil {
		if err := term.Restore(t.input.Fd(), t.inputState); err != nil {
			return err
		}
		t.inputState = nil
	}
	if t.outputState != nil {
		if err := term.Restore(t.output.Fd(), t.outputState); err != nil {
			return err
		}
		t.outputState = nil
	}
	return nil
}

// Close restores the console to its previous state and releases resources.
func (t *console) Close() error {
	if err := t.Restore(); err != nil {
		return err
	}
	return nil
}

// GetSize returns the current size of the console.
func (t *console) GetSize() (width, height int, err error) {
	return getSize(t.input, t.output)
}

// GetWinsize returns the current size of the console in cells and pixels.
func (t *console) GetWinsize() (*Winsize, error) {
	ws, err := getWinsize(t.input, t.output)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}
