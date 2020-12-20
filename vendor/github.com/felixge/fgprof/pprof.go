package fgprof

import (
	"strings"

	"github.com/google/pprof/profile"
)

func toProfile(s stackCounter, hz int) *profile.Profile {
	functionID := uint64(1)
	locationID := uint64(1)

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

	for _, stack := range sortedKeys(s) {
		count := s[stack]
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
			sample.Location = append(sample.Location, location)

			locationID++
			functionID++
		}
		p.Sample = append(p.Sample, sample)
	}
	return p
}
