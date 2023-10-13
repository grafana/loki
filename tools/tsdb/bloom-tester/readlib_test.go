package main

import (
	"github.com/stretchr/testify/require"
	"testing"
)

/*
	for i := 0; i <= tokenizer.getSkip(); i++ {
															numMatches := 0
															if (len(queryExperiment.searchString) - i) >= tokenizer.getMin() {
																tokens := tokenizer.Tokens(queryExperiment.searchString[i:])

																for _, token := range tokens {
																	if sbf.Test(token.Key) {
																		numMatches++
																	}
																}
																if numMatches > 0 {
																	if numMatches == len(tokens) {
																		foundInSbf = true
																		metrics.sbfMatchesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Inc()
																	}
																}
															}
														}
*/
func TestSearchSbf(t *testing.T) {
	tokenizer := fourSkip2

	searchString := "trace"

	experiment := NewExperiment(
		"token=4skip1_error=1%_indexchunks=true",
		tokenizer,
		true,
		onePctError,
	)

	for _, tc := range []struct {
		desc        string
		inputLine   string
		inputSearch string
		exp         bool
	}{
		{
			desc:        "exact match",
			inputLine:   "trace",
			inputSearch: "trace",
			exp:         true,
		},
		{
			desc:        "longer line",
			inputLine:   "trace with other things",
			inputSearch: "trace",
			exp:         true,
		},
		{
			desc:        "offset one",
			inputLine:   " trace with other things",
			inputSearch: "trace",
			exp:         true,
		},
		{
			desc:        "offset two",
			inputLine:   "  trace with other things",
			inputSearch: "trace",
			exp:         true,
		},
		{
			desc:        "offset three",
			inputLine:   "   trace with other things",
			inputSearch: "trace",
			exp:         true,
		},
		{
			desc:        "realistic",
			inputLine:   "(Use *node --trace-deprecation. to show where the warning was created)",
			inputSearch: "trace",
			exp:         true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			sbf := experiment.bloom()
			tokens := tokenizer.Tokens(tc.inputLine)
			for _, token := range tokens {
				sbf.Add(token.Key)
				//fmt.Println(string(token.Key))
			}
			require.Equal(t, tc.exp, searchSbf(sbf, tokenizer, searchString))
		})
	}
}
