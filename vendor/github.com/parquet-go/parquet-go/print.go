package parquet

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
)

func PrintSchema(w io.Writer, name string, node Node) error {
	return PrintSchemaIndent(w, name, node, "\t", "\n")
}

func PrintSchemaIndent(w io.Writer, name string, node Node, pattern, newline string) error {
	pw := &printWriter{writer: w}
	pi := &printIndent{}

	if node.Leaf() {
		printSchemaWithIndent(pw, "", node, pi)
	} else {
		pw.WriteString("message ")

		if name == "" {
			pw.WriteString("{")
		} else {
			pw.WriteString(name)
			pw.WriteString(" {")
		}

		pi.pattern = pattern
		pi.newline = newline
		pi.repeat = 1
		pi.writeNewLine(pw)

		for _, field := range node.Fields() {
			printSchemaWithIndent(pw, field.Name(), field, pi)
			pi.writeNewLine(pw)
		}

		pw.WriteString("}")
	}

	return pw.err
}

func printSchemaWithIndent(w io.StringWriter, name string, node Node, indent *printIndent) {
	indent.writeTo(w)

	switch {
	case node.Optional():
		w.WriteString("optional ")
	case node.Repeated():
		w.WriteString("repeated ")
	default:
		w.WriteString("required ")
	}

	if node.Leaf() {
		t := node.Type()
		switch t.Kind() {
		case Boolean:
			w.WriteString("boolean")
		case Int32:
			w.WriteString("int32")
		case Int64:
			w.WriteString("int64")
		case Int96:
			w.WriteString("int96")
		case Float:
			w.WriteString("float")
		case Double:
			w.WriteString("double")
		case ByteArray:
			w.WriteString("binary")
		case FixedLenByteArray:
			w.WriteString("fixed_len_byte_array(")
			w.WriteString(strconv.Itoa(t.Length()))
			w.WriteString(")")
		default:
			w.WriteString("<?>")
		}

		if name != "" {
			w.WriteString(" ")
			w.WriteString(name)
		}

		if annotation := annotationOf(node); annotation != "" {
			w.WriteString(" (")
			w.WriteString(annotation)
			w.WriteString(")")
		}

		if id := node.ID(); id != 0 {
			w.WriteString(" = ")
			w.WriteString(strconv.Itoa(id))
		}

		w.WriteString(";")
	} else {
		w.WriteString("group")

		if name != "" {
			w.WriteString(" ")
			w.WriteString(name)
		}

		if annotation := annotationOf(node); annotation != "" {
			w.WriteString(" (")
			w.WriteString(annotation)
			w.WriteString(")")
		}

		if id := node.ID(); id != 0 {
			w.WriteString(" = ")
			w.WriteString(strconv.Itoa(id))
		}

		w.WriteString(" {")
		indent.writeNewLine(w)
		indent.push()

		for _, field := range node.Fields() {
			printSchemaWithIndent(w, field.Name(), field, indent)
			indent.writeNewLine(w)
		}

		indent.pop()
		indent.writeTo(w)
		w.WriteString("}")
	}
}

func annotationOf(node Node) string {
	if logicalType := node.Type().LogicalType(); logicalType != nil {
		return logicalType.String()
	}
	return ""
}

type printIndent struct {
	pattern string
	newline string
	repeat  int
}

func (i *printIndent) push() {
	i.repeat++
}

func (i *printIndent) pop() {
	i.repeat--
}

func (i *printIndent) writeTo(w io.StringWriter) {
	if i.pattern != "" {
		for n := i.repeat; n > 0; n-- {
			w.WriteString(i.pattern)
		}
	}
}

func (i *printIndent) writeNewLine(w io.StringWriter) {
	if i.newline != "" {
		w.WriteString(i.newline)
	}
}

type printWriter struct {
	writer io.Writer
	err    error
}

func (w *printWriter) Write(b []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(b)
	if err != nil {
		w.err = err
	}
	return n, err
}

func (w *printWriter) WriteString(s string) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := io.WriteString(w.writer, s)
	if err != nil {
		w.err = err
	}
	return n, err
}

var (
	_ io.StringWriter = (*printWriter)(nil)
)

func sprint(name string, node Node) string {
	s := new(strings.Builder)
	PrintSchema(s, name, node)
	return s.String()
}

func PrintRowGroup(w io.Writer, rowGroup RowGroup) error {
	schema := rowGroup.Schema()
	pw := &printWriter{writer: w}
	tw := tablewriter.NewWriter(pw)

	columns := schema.Columns()
	header := make([]string, len(columns))
	footer := make([]string, len(columns))
	alignment := make([]int, len(columns))

	for i, column := range columns {
		leaf, _ := schema.Lookup(column...)
		columnType := leaf.Node.Type()

		header[i] = strings.Join(column, ".")
		footer[i] = columnType.String()

		switch columnType.Kind() {
		case ByteArray:
			alignment[i] = tablewriter.ALIGN_LEFT
		default:
			alignment[i] = tablewriter.ALIGN_RIGHT
		}
	}

	rowbuf := make([]Row, defaultRowBufferSize)
	cells := make([]string, 0, len(columns))
	rows := rowGroup.Rows()
	defer rows.Close()

	for {
		n, err := rows.ReadRows(rowbuf)

		for _, row := range rowbuf[:n] {
			cells = cells[:0]

			for _, value := range row {
				columnIndex := value.Column()

				for len(cells) <= columnIndex {
					cells = append(cells, "")
				}

				if cells[columnIndex] == "" {
					cells[columnIndex] = value.String()
				} else {
					cells[columnIndex] += "," + value.String()
					alignment[columnIndex] = tablewriter.ALIGN_LEFT
				}
			}

			tw.Append(cells)
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}
	}

	tw.SetAutoFormatHeaders(false)
	tw.SetColumnAlignment(alignment)
	tw.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tw.SetFooterAlignment(tablewriter.ALIGN_LEFT)
	tw.SetHeader(header)
	tw.SetFooter(footer)
	tw.Render()

	fmt.Fprintf(pw, "%d rows\n\n", rowGroup.NumRows())
	return pw.err
}

func PrintColumnChunk(w io.Writer, columnChunk ColumnChunk) error {
	pw := &printWriter{writer: w}
	pw.WriteString(columnChunk.Type().String())
	pw.WriteString("\n--------------------------------------------------------------------------------\n")

	values := [42]Value{}
	pages := columnChunk.Pages()
	numPages, numValues := int64(0), int64(0)

	defer pages.Close()
	for {
		p, err := pages.ReadPage()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}
			break
		}

		numPages++
		n := p.NumValues()
		if n == 0 {
			fmt.Fprintf(pw, "*** page %d, no values ***\n", numPages)
		} else {
			fmt.Fprintf(pw, "*** page %d, values %d to %d ***\n", numPages, numValues+1, numValues+n)
			printPage(w, p, values[:], numValues+1)
			numValues += n
		}

		pw.WriteString("\n")
	}

	return pw.err
}

func PrintPage(w io.Writer, page Page) error {
	return printPage(w, page, make([]Value, 42), 0)
}

func printPage(w io.Writer, page Page, values []Value, numValues int64) error {
	r := page.Values()
	for {
		n, err := r.ReadValues(values[:])
		for i, v := range values[:n] {
			_, err := fmt.Fprintf(w, "value %d: %+v\n", numValues+int64(i), v)
			if err != nil {
				return err
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return err
		}
	}
}
