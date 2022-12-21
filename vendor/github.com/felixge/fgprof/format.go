package fgprof

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/pprof/profile"
)

// Format decides how the ouput is rendered to the user.
type Format string

const (
	// FormatFolded is used by Brendan Gregg's FlameGraph utility, see
	// https://github.com/brendangregg/FlameGraph#2-fold-stacks.
	FormatFolded Format = "folded"
	// FormatPprof is used by Google's pprof utility, see
	// https://github.com/google/pprof/blob/master/proto/README.md.
	FormatPprof Format = "pprof"
)

func writeFormat(w io.Writer, s map[string]int, f Format, hz int) error {
	switch f {
	case FormatFolded:
		return writeFolded(w, s)
	case FormatPprof:
		return toPprof(s, hz).Write(w)
	default:
		return fmt.Errorf("unknown format: %q", f)
	}
}

func writeFolded(w io.Writer, s map[string]int) error {
	for _, stack := range sortedKeys(s) {
		count := s[stack]
		if _, err := fmt.Fprintf(w, "%s %d\n", stack, count); err != nil {
			return err
		}
	}
	return nil
}

func toPprof(s map[string]int, hz int) *profile.Profile {
	functionID := uint64(1)
	locationID := uint64(1)
	line := int64(1)

	p := &profile.Profile{}
	m := &profile.Mapping{ID: 1, HasFunctions: true}
	p.Mapping = []*profile.Mapping{m}
	p.SampleType = []*profile.ValueType{
		{
			Type: "samples",
			Unit: "count",
		},
		{
			Type: "time",
			Unit: "nanoseconds",
		},
	}

	for stack, count := range s {
		sample := &profile.Sample{
			Value: []int64{
				int64(count),
				int64(1000 * 1000 * 1000 / hz * count),
			},
		}
		for _, fnName := range strings.Split(stack, ";") {
			function := &profile.Function{
				ID:   functionID,
				Name: fnName,
			}
			p.Function = append(p.Function, function)

			location := &profile.Location{
				ID:      locationID,
				Mapping: m,
				Line:    []profile.Line{{Function: function}},
			}
			p.Location = append(p.Location, location)
			sample.Location = append([]*profile.Location{location}, sample.Location...)

			line++

			locationID++
			functionID++
		}
		p.Sample = append(p.Sample, sample)
	}
	return p
}

func sortedKeys(s map[string]int) []string {
	keys := make([]string, len(s))
	i := 0
	for stack := range s {
		keys[i] = stack
		i++
	}
	sort.Strings(keys)
	return keys
}
