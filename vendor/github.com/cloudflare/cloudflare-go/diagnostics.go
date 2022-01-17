package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// DiagnosticsTracerouteConfiguration is the overarching structure of the
// diagnostics traceroute requests.
type DiagnosticsTracerouteConfiguration struct {
	Targets []string                                  `json:"targets"`
	Colos   []string                                  `json:"colos,omitempty"`
	Options DiagnosticsTracerouteConfigurationOptions `json:"options,omitempty"`
}

// DiagnosticsTracerouteConfigurationOptions contains the options for performing
// traceroutes.
type DiagnosticsTracerouteConfigurationOptions struct {
	PacketsPerTTL int    `json:"packets_per_ttl"`
	PacketType    string `json:"packet_type"`
	MaxTTL        int    `json:"max_ttl"`
	WaitTime      int    `json:"wait_time"`
}

// DiagnosticsTracerouteResponse is the outer response of the API response.
type DiagnosticsTracerouteResponse struct {
	Response
	Result []DiagnosticsTracerouteResponseResult `json:"result"`
}

// DiagnosticsTracerouteResponseResult is the inner API response for the
// traceroute request.
type DiagnosticsTracerouteResponseResult struct {
	Target string                               `json:"target"`
	Colos  []DiagnosticsTracerouteResponseColos `json:"colos"`
}

// DiagnosticsTracerouteResponseColo contains the Name and City of a colocation.
type DiagnosticsTracerouteResponseColo struct {
	Name string `json:"name"`
	City string `json:"city"`
}

// DiagnosticsTracerouteResponseNodes holds a summary of nodes contacted in the
// traceroute.
type DiagnosticsTracerouteResponseNodes struct {
	Asn         string  `json:"asn"`
	IP          string  `json:"ip"`
	Name        string  `json:"name"`
	PacketCount int     `json:"packet_count"`
	MeanRttMs   float64 `json:"mean_rtt_ms"`
	StdDevRttMs float64 `json:"std_dev_rtt_ms"`
	MinRttMs    float64 `json:"min_rtt_ms"`
	MaxRttMs    float64 `json:"max_rtt_ms"`
}

// DiagnosticsTracerouteResponseHops holds packet and node information of the
// hops.
type DiagnosticsTracerouteResponseHops struct {
	PacketsTTL  int                                  `json:"packets_ttl"`
	PacketsSent int                                  `json:"packets_sent"`
	PacketsLost int                                  `json:"packets_lost"`
	Nodes       []DiagnosticsTracerouteResponseNodes `json:"nodes"`
}

// DiagnosticsTracerouteResponseColos is the summary struct of a colocation test.
type DiagnosticsTracerouteResponseColos struct {
	Error            string                              `json:"error"`
	Colo             DiagnosticsTracerouteResponseColo   `json:"colo"`
	TracerouteTimeMs int                                 `json:"traceroute_time_ms"`
	TargetSummary    DiagnosticsTracerouteResponseNodes  `json:"target_summary"`
	Hops             []DiagnosticsTracerouteResponseHops `json:"hops"`
}

// PerformTraceroute initiates a traceroute from the Cloudflare network to the
// requested targets.
//
// API documentation: https://api.cloudflare.com/#diagnostics-traceroute
func (api *API) PerformTraceroute(ctx context.Context, accountID string, targets, colos []string, tracerouteOptions DiagnosticsTracerouteConfigurationOptions) ([]DiagnosticsTracerouteResponseResult, error) {
	uri := fmt.Sprintf("/accounts/%s/diagnostics/traceroute", accountID)
	diagnosticsPayload := DiagnosticsTracerouteConfiguration{
		Targets: targets,
		Colos:   colos,
		Options: tracerouteOptions,
	}

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, diagnosticsPayload)
	if err != nil {
		return []DiagnosticsTracerouteResponseResult{}, err
	}

	var diagnosticsResponse DiagnosticsTracerouteResponse
	err = json.Unmarshal(res, &diagnosticsResponse)
	if err != nil {
		return []DiagnosticsTracerouteResponseResult{}, errors.Wrap(err, errUnmarshalError)
	}

	return diagnosticsResponse.Result, nil
}
