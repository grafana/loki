package cellbuf

import (
	"io"

	"github.com/charmbracelet/x/ansi"
)

// PenWriter is a writer that writes to a buffer and keeps track of the current
// pen style and link state for the purpose of wrapping with newlines.
type PenWriter struct {
	w     io.Writer
	p     *ansi.Parser
	style Style
	link  Link
}

// NewPenWriter returns a new PenWriter.
func NewPenWriter(w io.Writer) *PenWriter {
	pw := &PenWriter{w: w}
	pw.p = ansi.GetParser()
	handleCsi := func(cmd ansi.Cmd, params ansi.Params) {
		if cmd == 'm' {
			ReadStyle(params, &pw.style)
		}
	}
	handleOsc := func(cmd int, data []byte) {
		if cmd == 8 {
			ReadLink(data, &pw.link)
		}
	}
	pw.p.SetHandler(ansi.Handler{
		HandleCsi: handleCsi,
		HandleOsc: handleOsc,
	})
	return pw
}

// Style returns the current pen style.
func (w *PenWriter) Style() Style {
	return w.style
}

// Link returns the current pen link.
func (w *PenWriter) Link() Link {
	return w.link
}

// Write writes to the buffer.
func (w *PenWriter) Write(p []byte) (int, error) {
	for i := range p {
		b := p[i]
		w.p.Advance(b)
		if b == '\n' {
			if !w.style.Empty() {
				_, _ = w.w.Write([]byte(ansi.ResetStyle))
			}
			if !w.link.Empty() {
				_, _ = w.w.Write([]byte(ansi.ResetHyperlink()))
			}
		}

		_, _ = w.w.Write([]byte{b})
		if b == '\n' {
			if !w.link.Empty() {
				_, _ = w.w.Write([]byte(ansi.SetHyperlink(w.link.URL, w.link.Params)))
			}
			if !w.style.Empty() {
				_, _ = w.w.Write([]byte(w.style.Sequence()))
			}
		}
	}

	return len(p), nil
}

// Close closes the writer, resets the style and link if necessary, and releases
// its parser. Calling it is performance critical, but forgetting it does not
// cause safety issues or leaks.
func (w *PenWriter) Close() error {
	if !w.style.Empty() {
		_, _ = w.w.Write([]byte(ansi.ResetStyle))
	}
	if !w.link.Empty() {
		_, _ = w.w.Write([]byte(ansi.ResetHyperlink()))
	}
	if w.p != nil {
		ansi.PutParser(w.p)
		w.p = nil
	}
	return nil
}
