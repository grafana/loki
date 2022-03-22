package spinner

import (
	"fmt"
	"io"
	"math/rand"

	"github.com/fatih/color"

	"github.com/unchartedsoftware/witch/cursor"
)

var (
	// `°º¤ø,¸¸,ø¤º°`
	frames = []string{
		"",
		"°",
		"°º",
		"°º¤",
		" º¤ø",
		"  ¤ø,",
		"   ø,¸",
		"    ,¸¸",
		"     ¸¸,",
		"      ¸,ø",
		"       ,ø¤",
		"        ø¤º",
		"         ¤º°",
		"          º°`",
		"           °``",
		"            ``°",
		"             `°º",
		"              °º¤",
		"               º¤ø",
		"                ¤ø,",
		"                 ø,¸",
		"                  ,¸¸",
		"                   ¸¸,",
		"                    ¸,",
		"                     ,",
	}
	colorHash = magicHash([]string{"`", "°", "º", "¤", "ø", ",", "¸"})
)

// Spinner represents a spinning console output.
type Spinner struct {
	c int
	w io.Writer
}

// New instantiates and returns a new spinner struct.
func New(writer io.Writer) *Spinner {
	return &Spinner{
		w: writer,
	}
}

// Tick increments the cursor.
func (s *Spinner) Tick(count uint64) {
	s.c = (s.c + 1) % len(frames)
	magic := fmt.Sprintf("%s%s",
		cursor.Hide,
		castMagic(frames[s.c]))
	s.w.Write([]byte(magic))
}

// Done clears the cursor.
func (s *Spinner) Done() {
	goodbye := fmt.Sprintf("watch terminated %s%s\n",
		color.GreenString("✘"),
		cursor.Show)
	s.w.Write([]byte(goodbye))
}

func magicHash(strs []string) map[string][]string {
	colors := make(map[string][]string)
	for _, str := range strs {
		colors[str] = magicColors(str)
	}
	return colors
}

func magicColors(str string) []string {
	return []string{
		color.MagentaString(str),
		color.BlueString(str),
		color.GreenString(str),
		color.CyanString(str),
	}
}

func castMagic(str string) string {
	res := ""
	for _, c := range str {
		s := string(c)
		if s == " " {
			res += " "
		} else {
			i := int(rand.Float64() * 4)
			res += colorHash[s][i]
		}
	}
	return res
}
