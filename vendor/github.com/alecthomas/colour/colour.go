// Package colour ([docs][1]) provides [Quake-style colour formatting][2] for Unix terminals.
//
// It is a drop-in replacement for the fmt package.
//
// The package level functions can be used to write to stdout (or strings or
// other files). If stdout is not a terminal, colour formatting will be
// stripped.
//
// eg.
//
//     colour.Printf("^0black ^1red ^2green ^3yellow ^4blue ^5magenta ^6cyan ^7white^R\n")
//
//
// For more control a Printer object can be created with various helper
// functions. This can be used to do useful things such as strip formatting,
// write to strings, and so on.
//
// The following sequences are converted to their equivalent ANSI colours:
//
//     ^0 = Black
//     ^1 = Red
//     ^2 = Green
//     ^3 = Yellow
//     ^4 = Blue
//     ^5 = Cyan (light blue)
//     ^6 = Magenta (purple)
//     ^7 = White
//     ^8 = Black Background
//     ^9 = Red Background
//     ^a = Green Background
//     ^b = Yellow Background
//     ^c = Blue Background
//     ^d = Cyan (light blue) Background
//     ^e = Magenta (purple) Background
//     ^f = White Background
//     ^R = Reset
//     ^U = Underline
//     ^B = Bold
//     ^S = Strikethrough
//
// [1]: http://godoc.org/github.com/alecthomas/colour
// [2]: http://www.holysh1t.net/quake-live-colors-nickname/
package colour

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/mattn/go-isatty"
)

var (
	extract = regexp.MustCompile(`(\^[0-9a-fRDUBS])|(\^\^)|([^^]+)`)
	colours = map[byte]string{
		'0': "\033[30m",
		'1': "\033[31m",
		'2': "\033[32m",
		'3': "\033[33m",
		'4': "\033[34m",
		'5': "\033[35m",
		'6': "\033[36m",
		'7': "\033[37m",

		'8': "\033[40m",
		'9': "\033[41m",
		'a': "\033[42m",
		'b': "\033[43m",
		'c': "\033[44m",
		'd': "\033[45m",
		'e': "\033[46m",
		'f': "\033[47m",

		'R': "\033[0m", // reset
		'B': "\033[1m", // bold
		'D': "\033[2m", // dim
		'U': "\033[4m", // underline
		'S': "\033[9m", // strikethrough
	}

	// Stdout is an conditional colour writer for os.Stdout.
	Stdout = TTY(os.Stdout)
	// Stderr is an conditional colour writer for os.Stderr.
	Stderr = TTY(os.Stderr)
)

func Println(args ...interface{}) (n int, err error) {
	return Stdout.Println(args...)
}

func Sprintln(args ...interface{}) string {
	s := String()
	_, _ = s.Println(args...)
	return s.String()
}

func Fprintln(w io.Writer, args ...interface{}) (n int, err error) {
	return TTY(w).Println(args...)
}

func Print(args ...interface{}) (n int, err error) {
	return Stdout.Print(args...)
}

func Sprint(args ...interface{}) string {
	s := String()
	_, _ = s.Print(args...)
	return s.String()
}

func Fprint(w io.Writer, args ...interface{}) (n int, err error) {
	return TTY(w).Print(args...)
}

func Printf(format string, args ...interface{}) (n int, err error) {
	return Stdout.Printf(format, args...)
}

func Sprintf(format string, args ...interface{}) string {
	s := String()
	_, _ = s.Printf(format, args...)
	return s.String()
}

func Fprintf(w io.Writer, format string, args ...interface{}) (n int, err error) {
	return TTY(w).Printf(format, args...)
}

// A Printer implements functions that accept Quake-style colour formatting
// and print coloured text.
type Printer interface {
	Println(args ...interface{}) (n int, err error)
	Print(args ...interface{}) (n int, err error)
	Printf(format string, args ...interface{}) (n int, err error)
}

// TTY creates a Printer that colourises output if w is a terminal, or strips
// formatting if it is not.
func TTY(w io.Writer) Printer {
	if f, ok := w.(*os.File); ok && isatty.IsTerminal(f.Fd()) {
		return &colourPrinter{w}
	}
	return &stripPrinter{w}
}

// Colour creats a new ANSI colour Printer on w.
func Colour(w io.Writer) Printer {
	return &colourPrinter{w}
}

// Strip returns a Printer that strips all colour codes from printed strings before writing to w.
func Strip(w io.Writer) Printer {
	return &stripPrinter{w}
}

type StringPrinter struct {
	Printer
	w *bytes.Buffer
}

// String creates a new Printer that writes ANSI coloured text to a buffer.
// Use the String() method to return the printed string.
func String() *StringPrinter {
	w := &bytes.Buffer{}
	f := Colour(w)
	return &StringPrinter{f, w}
}

// StringStripper writes text stripped of colour formatting codes to a string.
// Use the String() method to return the printed string.
func StringStripper() *StringPrinter {
	w := &bytes.Buffer{}
	f := Strip(w)
	return &StringPrinter{f, w}
}

func (s *StringPrinter) String() string {
	return s.w.String()
}

type colourPrinter struct {
	w io.Writer
}

func (c *colourPrinter) Println(args ...interface{}) (n int, err error) {
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			args[i] = FormatString(s)
		}
	}
	return fmt.Fprintln(c.w, args...)
}

func (c *colourPrinter) Print(args ...interface{}) (n int, err error) {
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			args[i] = FormatString(s)
		}
	}
	return fmt.Fprint(c.w, args...)
}

func (c *colourPrinter) Printf(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(c.w, FormatString(format), args...)
}

type stripPrinter struct {
	w io.Writer
}

func (p *stripPrinter) Println(args ...interface{}) (n int, err error) {
	return fmt.Fprintln(p.w, stripArgs(args...)...)
}

func (p *stripPrinter) Print(args ...interface{}) (n int, err error) {
	return fmt.Fprint(p.w, stripArgs(args...)...)
}

func (p *stripPrinter) Printf(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(p.w, StripFormatting(format), args...)
}

func FormatString(s string) string {
	out := &bytes.Buffer{}
	for _, match := range extract.FindAllStringSubmatch(s, -1) {
		if match[1] != "" {
			n := match[1][1]
			out.WriteString(colours[n])
		} else if match[2] != "" {
			out.WriteString("^")
		} else {
			out.WriteString(match[3])
		}
	}
	return out.String()
}

func StripFormatting(s string) string {
	out := &bytes.Buffer{}
	for _, match := range extract.FindAllStringSubmatch(s, -1) {
		if match[2] != "" {
			out.WriteString("^")
		} else if match[1] == "" {
			out.WriteString(match[3])
		}
	}
	return out.String()
}

func stripArgs(args ...interface{}) []interface{} {
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			args[i] = StripFormatting(s)
		}
	}
	return args
}
