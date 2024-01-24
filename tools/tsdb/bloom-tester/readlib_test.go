package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchSbf(t *testing.T) {
	experiment := NewExperiment(
		"token=4skip0_error=1%_indexchunks=true",
		four,
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
		{
			desc:        "foo",
			inputLine:   "level=info ts=2023-10-13T20:03:48.064432622Z caller=readlib.go:280 ****falsenegativeline:=\"{\\\"httpRequest\\\":{\\\"latency\\\":\\\"0.084279s\\\",\\\"remoteIp\\\":\\\"130.211.209.64\\\",\\\"requestMethod\\\":\\\"POST\\\",\\\"requestSize\\\":\\\"151\\\",\\\"requestUrl\\\":\\\"https://prometheus-dev-01-dev-us-central-0.grafana-dev.net/api/prom/api/v1/query\\\",\\\"responseSize\\\":\\\"139\\\",\\\"serverIp\\\":\\\"10.132.64.43\\\",\\\"status\\\":200,\\\"userAgent\\\":\\\"usage_service \\\"},\\\"insertId\\\":\\\"1r9sgvff4fqw78\\\",\\\"jsonPayload\\\":{\\\"@type\\\":\\\"type.googleapis.com/google.cloud.loadbalancing.type.LoadBalancerLogEntry\\\",\\\"backendTargetProjectNumber\\\":\\\"projects/1040409107725\\\",\\\"cacheDecision\\\":[\\\"RESPONSE_HAS_CONTENT_TYPE\\\",\\\"REQUEST_HAS_AUTHORIZATION\\\",\\\"CACHE_MODE_USE_ORIGIN_HEADERS\\\"],\\\"remoteIp\\\":\\\"130.211.209.64\\\",\\\"statusDetails\\\":\\\"response_sent_by_backend\\\"},\\\"logName\\\":\\\"projects/grafanalabs-dev/logs/requests\\\",\\\"receiveTimestamp\\\":\\\"2023-09-25T20:15:17.305415664Z\\\",\\\"resource\\\":{\\\"labels\\\":{\\\"backend_service_name\\\":\\\"k8s1-bb201ea5-cortex-de-prometheus-dev-01-dev-us-cen-8-d92f8647\\\",\\\"forwarding_rule_name\\\":\\\"k8s2-fs-i7ga9yyz-cortex-de-prometheus-dev-01-dev-us-ce-5fyxhcia\\\",\\\"project_id\\\":\\\"grafanalabs-dev\\\",\\\"target_proxy_name\\\":\\\"k8s2-ts-i7ga9yyz-cortex-de-prometheus-dev-01-dev-us-ce-5fyxhcia\\\",\\\"url_map_name\\\":\\\"k8s2-um-i7ga9yyz-cortex-de-prometheus-dev-01-dev-us-ce-5fyxhcia\\\",\\\"zone\\\":\\\"global\\\"},\\\"type\\\":\\\"http_load_balancer\\\"},\\\"severity\\\":\\\"INFO\\\",\\\"spanId\\\":\\\"d2c261eca4d5a01a\\\",\\\"timestamp\\\":\\\"2023-09-25T20:15:16.522437Z\\\",\\\"trace\\\":\\\"projects/grafanalabs-dev/traces/0a178fae3e96ed27aaf81a4a268730c2\\\"}\"",
			inputSearch: "trace",
			exp:         true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			sbf := experiment.bloom()
			tokens := four.Tokens(tc.inputLine)
			for tokens.Next() {
				tok := tokens.At()
				sbf.Add(tok)
			}

			require.Equal(t, tc.exp, searchSbf(sbf, *four, tc.inputSearch))
		})
	}
}
