package writer

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	LogEntry = "%s %s\n"
)

type Writer struct {
	w         io.Writer
	sent      chan time.Time
	interval  time.Duration
	size      int
	prevTsLen int
	pad       string
	quit      chan struct{}
	done      chan struct{}
}

func NewWriter(writer io.Writer, sentChan chan time.Time, entryInterval time.Duration, entrySize int) *Writer {

	w := &Writer{
		w:         writer,
		sent:      sentChan,
		interval:  entryInterval,
		size:      entrySize,
		prevTsLen: 0,
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
	}

	go w.run()

	return w
}

func (w *Writer) Stop() {
	if w.quit != nil {
		close(w.quit)
		<-w.done
		w.quit = nil
	}
}

func (w *Writer) run() {
	t := time.NewTicker(w.interval)
	defer func() {
		t.Stop()
		close(w.done)
	}()
	for {
		select {
		case <-t.C:
			t := time.Now()
			ts := strconv.FormatInt(t.UnixNano(), 10)
			tsLen := len(ts)

			// I guess some day this could happen????
			if w.prevTsLen != tsLen {
				var str strings.Builder
				// Total line length includes timestamp, white space separator, new line char.  Subtract those out
				for str.Len() < w.size-tsLen-2 {
					str.WriteString("p")
				}
				w.pad = str.String()
				w.prevTsLen = tsLen
			}

			fmt.Fprintf(w.w, LogEntry, ts, w.pad)
			w.sent <- t
		case <-w.quit:
			return
		}
	}

}
