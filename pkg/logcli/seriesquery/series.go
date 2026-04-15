package seriesquery

import (
	"fmt"
	"log"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

// SeriesQuery contains all necessary fields to execute label queries and print out the results
type SeriesQuery struct {
	Matcher       string
	Start         time.Time
	End           time.Time
	AnalyzeLabels bool
	Quiet         bool
}

type labelDetails struct {
	name       string
	inStreams  int
	uniqueVals map[string]struct{}
}

// DoSeries prints out series results
func (q *SeriesQuery) DoSeries(c client.Client) {
	streams := q.GetSeries(c)

	if q.AnalyzeLabels {
		labelMap := map[string]*labelDetails{}

		for _, stream := range streams {
			for labelName, labelValue := range stream {
				if _, ok := labelMap[labelName]; ok {
					labelMap[labelName].inStreams++
					labelMap[labelName].uniqueVals[labelValue] = struct{}{}
				} else {
					labelMap[labelName] = &labelDetails{
						name:       labelName,
						inStreams:  1,
						uniqueVals: map[string]struct{}{labelValue: {}},
					}
				}
			}
		}

		lds := make([]*labelDetails, 0, len(labelMap))
		for _, ld := range labelMap {
			lds = append(lds, ld)
		}
		sort.Slice(lds, func(ld1, ld2 int) bool {
			return len(lds[ld1].uniqueVals) > len(lds[ld2].uniqueVals)
		})

		fmt.Println("Total Streams: ", len(streams))
		fmt.Println("Unique Labels: ", len(labelMap))
		fmt.Println()

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "Label Name\tUnique Values\tFound In Streams\n")
		for _, details := range lds {
			fmt.Fprintf(w, "%v\t%v\t%v\n", details.name, len(details.uniqueVals), details.inStreams)
		}
		w.Flush()

	} else {
		for _, value := range streams {
			fmt.Println(value)
		}
	}

}

// GetSeries returns an array of label sets
func (q *SeriesQuery) GetSeries(c client.Client) []loghttp.LabelSet {
	seriesResponse, err := c.Series([]string{q.Matcher}, q.Start, q.End, q.Quiet)
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	return seriesResponse.Data
}
