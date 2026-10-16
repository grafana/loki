package detected

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/lokitool/client"
)

// FieldsQuery contains the parameters of a query for detected fields or the values of a detected field.
type FieldsQuery struct {
	QueryString   string
	Start         time.Time
	End           time.Time
	Limit         int
	LineLimit     int
	Step          time.Duration
	FieldName     string
	ColoredOutput bool
}

// Do executes the query and prints out the results. The output modes raw and jsonl print the response as JSON.
func (q *FieldsQuery) Do(c client.QueryClient, outputMode string) error {
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
	)
	if err != nil {
		return fmt.Errorf("error doing request: %w", err)
	}

	switch outputMode {
	case "raw", "jsonl":
		out, err := json.Marshal(resp)
		if err != nil {
			return fmt.Errorf("error marshalling response: %w", err)
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
	return nil
}
