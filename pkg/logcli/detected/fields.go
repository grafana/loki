package detected

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type FieldsQuery struct {
	QueryString   string
	Start         time.Time
	End           time.Time
	Limit         int
	LineLimit     int
	Step          time.Duration
	Quiet         bool
	FieldName     string
	ColoredOutput bool
}

// DoQuery executes the query and prints out the results
func (q *FieldsQuery) Do(c client.Client, outputMode string) {
	var resp *loghttp.DetectedFieldsResponse
	var err error

	resp, err = c.GetDetectedFields(
		q.QueryString,
		q.FieldName,
		q.Limit,
		q.LineLimit,
		q.Start,
		q.End,
		q.Step,
		q.Quiet,
	)
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}

	switch outputMode {
	case "raw":
		out, err := json.Marshal(resp)
		if err != nil {
			log.Fatalf("Error marshalling response: %+v", err)
		}
		fmt.Println(string(out))
	default:
		var output []string
		if len(resp.Fields) > 0 {
			output = make([]string, len(resp.Fields))
			for i, field := range resp.Fields {
				bold := color.New(color.Bold)
				output[i] = fmt.Sprintf("label: %s\t\t", bold.Sprintf("%s", field.Label)) +
					fmt.Sprintf("type: %s\t\t", bold.Sprintf("%s", field.Type)) +
					fmt.Sprintf("cardinality: %s", bold.Sprintf("%d", field.Cardinality))
			}
		} else if len(resp.Values) > 0 {
			output = resp.Values
		}

		slices.Sort(output)
		fmt.Println(strings.Join(output, "\n"))
	}
}
