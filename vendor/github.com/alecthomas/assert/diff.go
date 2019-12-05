package assert

import (
	"bytes"
	"fmt"

	"github.com/alecthomas/colour"
	"github.com/alecthomas/repr"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func DiffValues(a, b interface{}) string {
	printer := colour.String()
	diff := diffmatchpatch.New()
	at := repr.String(a, repr.OmitEmpty(true))
	bt := repr.String(b, repr.OmitEmpty(true))
	diffs := diff.DiffMain(at, bt, true)
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			if len(d.Text) <= 40 {
				printer.Print(d.Text)
			} else {
				printer.Printf("%s^B...^R%s", d.Text[:15], d.Text[len(d.Text)-15:])
			}
		case diffmatchpatch.DiffDelete:
			printer.Printf("^9%s^R", d.Text)
		case diffmatchpatch.DiffInsert:
			printer.Printf("^a%s^R", d.Text)
		}
	}
	return printer.String()
}

func DiffValuesDefault(a, b interface{}) string {
	diff := diffmatchpatch.New()
	at := repr.String(a)
	bt := repr.String(b)
	diffs := diff.DiffMain(at, bt, true)
	w := bytes.NewBuffer(nil)
	for _, d := range diffs {
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			if len(d.Text) <= 40 {
				w.WriteString(d.Text)
			} else {
				fmt.Fprintf(w, "%s...%s", d.Text[:15], d.Text[len(d.Text)-15:])
			}
		case diffmatchpatch.DiffDelete:
			fmt.Fprintf(w, "-{{%s}}", d.Text)
		case diffmatchpatch.DiffInsert:
			fmt.Fprintf(w, "+{{%s}}", d.Text)
		}
	}
	return w.String()
}
