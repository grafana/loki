package writer

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki-canary/pkg/comparator"
)

const (
	LogEntry = "%s %s\n"
)

type Writer struct {
	w         io.Writer
	cm        *comparator.Comparator
	interval  time.Duration
	size      int
	prevTsLen int
	pad       string
	quit      chan struct{}
	done      chan struct{}
}

func NewWriter(writer io.Writer, comparator *comparator.Comparator, entryInterval time.Duration, entrySize int) *Writer {

	w := &Writer{
		w:         writer,
		cm:        comparator,
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
	close(w.quit)
	<-w.done
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

			_, _ = fmt.Fprintf(w.w, LogEntry, ts, w.pad)
			w.cm.EntrySent(t)
		case <-w.quit:
			return
		}
	}

}
