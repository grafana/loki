package uv

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// DefaultBufferSize is the default size of the input buffer used for reading
// terminal events.
const DefaultBufferSize = 4096

// DefaultEventTimeout is the default duration to wait for input events before
// timing out.
const DefaultEventTimeout = 100 * time.Millisecond

// Options represents options for creating a new [Terminal].
type Options struct {
	// BufferSize is the size of the input buffer used for reading terminal
	// events. If zero, [DefaultBufferSize] is used.
	BufferSize int

	// EventTimeout is the duration to wait for input events before timing out.
	// If zero, a default of 100 milliseconds is used.
	EventTimeout time.Duration

	// LegacyKeyEncoding represents any legacy key encoding ambiguities. By
	// default, the terminal will use its preferred key encoding settings.
	LegacyKeyEncoding LegacyKeyEncoding

	// LookupKeys whether to use a lookup table for common key sequences. If
	// true, the terminal will use a lookup table to quickly identify common
	// key sequences, reducing the need for more complex decoding logic. This
	// can improve performance for common key sequences at the cost of
	// increased memory usage.
	//
	// This is enabled by default.
	LookupKeys bool

	// UseTerminfoKeys whether to use terminfo databases key definitions to
	// build up the keys lookup table. If true, the terminal will use terminfo
	// databases key definitions to build up the keys lookup table, which can
	// provide more accurate key mappings for legacy non-xterm like terminals.
	//
	// This won't take effect if [TerminalOptions.LookupKeys] is false, since
	// the lookup table won't be used.
	//
	// This is disabled by default.
	UseTerminfoKeys bool
}

// DefaultOptions returns the default [Terminal] options.
func DefaultOptions() *Options {
	return &Options{
		BufferSize:   DefaultBufferSize,
		EventTimeout: DefaultEventTimeout,
		LookupKeys:   true,
	}
}

// Terminal represents an interactive terminal application.
type Terminal struct {
	con    Console
	opts   *Options
	scr    *TerminalScreen
	pr     pollReader
	buf    []byte
	evc    chan Event
	errg   errgroup.Group
	winch  chan os.Signal
	donec  chan struct{}
	logger Logger
}

// DefaultTerminal creates a new [Terminal] instance using the default standard
// console and the given options. Options can be nil to use the default
// options.
//
// This is a convenience function for creating a terminal that uses the
// standard input and output file descriptors.
func DefaultTerminal() *Terminal {
	return NewTerminal(nil, nil)
}

// ControllingTerminal creates a new [Terminal] instance using the controlling
// terminal's input and output file descriptors.
// Options can be nil to use the default options.
//
// This is a convenience function for creating a terminal that uses the
// controlling TTY of the current process.
func ControllingTerminal() (*Terminal, error) {
	con, err := ControllingConsole()
	if err != nil {
		return nil, err
	}
	return NewTerminal(con, nil), nil
}

// NewTerminal creates a new [Terminal] instance with the given console and
// options.
// Options can be nil to use the default options.
func NewTerminal(con Console, opts *Options) *Terminal {
	t := &Terminal{}
	if con == nil {
		con = DefaultConsole()
	}
	if opts == nil {
		opts = DefaultOptions()
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = DefaultBufferSize
	}
	if opts.EventTimeout <= 0 {
		opts.EventTimeout = DefaultEventTimeout
	}
	t.con = con
	t.opts = opts
	t.scr = NewTerminalScreen(t.con.Writer(), t.con.Environ())
	t.buf = make([]byte, opts.BufferSize)
	// These channels never close during the terminal's lifetime.
	t.evc = make(chan Event)
	t.winch = make(chan os.Signal, 1) // buffered to avoid missing signals
	return t
}

// SetLogger sets the terminal's logger for tracing I/O operations.
func (t *Terminal) SetLogger(logger Logger) {
	t.logger = logger
	t.scr.rend.SetLogger(logger)
}

// Screen returns the terminal's screen.
func (t *Terminal) Screen() *TerminalScreen {
	return t.scr
}

// Events returns the terminal's event channel.
func (t *Terminal) Events() <-chan Event {
	return t.evc
}

// Start starts the terminal application event loop. This is a non-blocking
// call. Use [Terminal.Wait] to wait for the terminal to exit.
func (t *Terminal) Start() error {
	_, err := t.con.MakeRaw()
	if err != nil {
		return fmt.Errorf("failed to set terminal to raw mode: %w", err)
	}

	evs := newEventScanner()
	evs.lookup = t.opts.LookupKeys
	if evs.lookup {
		evs.table = buildKeysTable(t.opts.LegacyKeyEncoding, t.con.Getenv("TERM"), t.opts.UseTerminfoKeys)
	}
	if t.logger != nil {
		evs.setLogger(t.logger)
	}
	bufc := make(chan []byte)
	t.donec = make(chan struct{})
	t.pr, err = newPollReader(t.con.Reader())
	if err != nil {
		return fmt.Errorf("failed to create poll reader: %w", err)
	}

	// input loop
	t.errg.Go(func() error {
		for {
			n, err := t.pr.Read(t.buf)
			if err != nil {
				return fmt.Errorf("reading terminal input: %w", err)
			}
			select {
			case bufc <- t.buf[:n]:
			case <-t.donec:
				return nil
			}
		}
	})

	// event loop
	sendEvents := func(buf []byte, expired bool) int {
		n, events := evs.scanEvents(buf, expired)
		for _, ev := range events {
			t.SendEvent(ev)
		}
		return n
	}
	t.errg.Go(func() error {
		var buf []byte
		timer := time.NewTimer(t.opts.EventTimeout)
		timeout := time.Now().Add(t.opts.EventTimeout)

		for {
			select {
			case <-t.donec:
				return nil
			case <-timer.C:
				expired := len(buf) > 0 && time.Now().After(timeout)
				n := sendEvents(buf, expired)
				if n > 0 {
					buf = buf[min(n, len(buf)):]
				}
				if len(buf) > 0 {
					timer.Reset(t.opts.EventTimeout)
				}
			case data := <-bufc:
				buf = append(buf, data...)
				n := sendEvents(buf, false)
				timeout = time.Now().Add(t.opts.EventTimeout)
				timer.Stop()
				if n > 0 {
					buf = buf[min(n, len(buf)):]
				}
				if len(buf) > 0 {
					timer.Reset(t.opts.EventTimeout)
				}
			}
		}
	})

	sendWinsize := func() error {
		ws, err := t.con.GetWinsize()
		if err != nil {
			return fmt.Errorf("getting terminal size: %w", err)
		}
		if ws.Col > 0 && ws.Row > 0 {
			t.SendEvent(WindowSizeEvent{
				Width:  int(ws.Col),
				Height: int(ws.Row),
			})
		}
		if ws.Xpixel > 0 && ws.Ypixel > 0 {
			t.SendEvent(PixelSizeEvent{
				Width:  int(ws.Xpixel),
				Height: int(ws.Ypixel),
			})
		}
		return nil
	}

	// winch handler
	NotifyWinch(t.winch)
	t.errg.Go(func() error {
		for {
			select {
			case <-t.donec:
				return nil
			case <-t.winch:
				if err := sendWinsize(); err != nil {
					return err
				}
			}
		}
	})

	// init window size
	t.errg.Go(func() error {
		if err := sendWinsize(); err != nil {
			return err
		}
		return nil
	})

	// Restore any previous screen state.
	if err := t.scr.Restore(); err != nil {
		return fmt.Errorf("failed to restore terminal screen: %w", err)
	}
	if err := t.scr.Flush(); err != nil {
		return fmt.Errorf("failed to flush terminal screen: %w", err)
	}

	return nil
}

// Wait waits for the terminal event loop to exit and returns any error that
// occurred.
func (t *Terminal) Wait() error {
	if err := t.errg.Wait(); err != nil {
		return fmt.Errorf("terminal event loop error: %w", err)
	}
	return nil
}

// Stop stops the terminal event loop. This is a non-blocking call. Use
// [Terminal.Wait] to wait for the terminal to exit.
func (t *Terminal) Stop() error {
	sync.OnceFunc(func() {
		close(t.donec)
	})
	signal.Stop(t.winch)
	if t.pr != nil {
		t.pr.Cancel()
		_ = t.pr.Close()
	}
	if err := t.scr.Reset(); err != nil {
		_ = t.scr.Flush()
		_ = t.con.Restore()
		return fmt.Errorf("failed to reset terminal screen: %w", err)
	}
	if err := t.scr.Flush(); err != nil {
		_ = t.con.Restore()
		return fmt.Errorf("failed to flush terminal screen: %w", err)
	}
	if err := t.con.Restore(); err != nil {
		return fmt.Errorf("failed to restore terminal state: %w", err)
	}
	return nil
}

// SendEvent sends an event to the terminal's event channel.
//
// This can be used to inject custom events into the terminal's event loop,
// such as timer events, signals, or application-specific events.
func (t *Terminal) SendEvent(e Event) {
	select {
	case t.evc <- e:
	case <-t.donec:
	}
}

// Write writes data directly to the terminal's console output.
//
// This is a low-level operation that bypasses the terminal screen buffering
// and writes directly to the console output handler, usually [os.Stdout] or
// the controlling TTY.
func (t *Terminal) Write(p []byte) (n int, err error) {
	return t.con.Write(p)
}

// Read reads data from the terminal's console input.
//
// This is a low-level operation that bypasses the terminal event processing
// and reads directly from the console input handler, usually [os.Stdin] or the
// controlling TTY. Use this method with caution, as it may interfere with the
// terminal's event loop and screen management.
func (t *Terminal) Read(p []byte) (n int, err error) {
	return t.con.Read(p)
}
