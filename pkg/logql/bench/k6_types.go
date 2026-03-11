package bench

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// K6TestCase represents a test case in the format consumed by the k6 test runner.
type K6TestCase struct {
	Name      string `json:"name"`
	Query     string `json:"query"`
	TenantID  int    `json:"tenantId"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Step      string `json:"step"`
	Direction string `json:"direction"`
	Kind      string `json:"kind"`
	Source    string `json:"source"`
}

// ConvertTestCaseToK6 converts a resolved TestCase into the k6 JSON format.
// length is the query window duration from the template's TimeRangeConfig.
// buffer is how far from "now" the query window ends.
func ConvertTestCaseToK6(tc TestCase, tenantID int, length time.Duration, buffer time.Duration) K6TestCase {
	direction := "backward"
	if tc.Direction == logproto.FORWARD {
		direction = "forward"
	}

	kind := tc.Kind() // reuse existing method, do not reparse

	name := tc.Source
	if tc.QueryDesc != "" {
		name = fmt.Sprintf("%s - %s", tc.Source, tc.QueryDesc)
	}
	if kind == "log" {
		name = fmt.Sprintf("%s [%s]", name, directionLabel(tc.Direction))
	}

	step := ""
	if tc.Step > 0 {
		step = formatDuration(tc.Step)
	}

	return K6TestCase{
		Name:      name,
		Query:     tc.Query,
		TenantID:  tenantID,
		Start:     fmt.Sprintf("-%s", formatDuration(length+buffer)),
		End:       fmt.Sprintf("-%s", formatDuration(buffer)),
		Step:      step,
		Direction: direction,
		Kind:      kind,
		Source:    tc.Source,
	}
}

func directionLabel(d logproto.Direction) string {
	if d == logproto.FORWARD {
		return "FORWARD"
	}
	return "BACKWARD"
}
