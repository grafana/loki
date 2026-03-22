//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

type DetectedField struct {
	Label       string `json:"label"`
	Type        string `json:"type"`
	Cardinality uint64 `json:"cardinality"`
}

type DetectedFields []DetectedField
type DetectedFieldResponse struct {
	Fields DetectedFields `json:"fields"`
}

func Test_ExploreLogsApis(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDBAndTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// run initially the compactor, indexgateway, and distributor.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			"-target=compactor",
			"-compactor.compaction-interval=1s",
			"-compactor.retention-delete-delay=1s",
			// By default, a minute is added to the delete request start time. This compensates for that.
			"-compactor.delete-request-cancel-period=-60s",
			"-compactor.deletion-mode=filter-and-delete",
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			"-target=index-gateway",
		)
		tDistributor = clu.AddComponent(
			"distributor",
			"-target=distributor",
		)
	)
	require.NoError(t, clu.Run())

	// then, run only the ingester and query scheduler.
	var (
		tIngester = clu.AddComponent(
			"ingester",
			"-target=ingester",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
		tQueryScheduler = clu.AddComponent(
			"query-scheduler",
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)
	)
	require.NoError(t, clu.Run())

	// the run querier.
	var (
		tQuerier = clu.AddComponent(
			"querier",
			"-target=querier",
			"-querier.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
		)
	)
	require.NoError(t, clu.Run())

	// finally, run the query-frontend.
	var (
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			"-target=query-frontend",
			"-frontend.scheduler-address="+tQueryScheduler.GRPCURL(),
			"-boltdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
			"-common.compactor-address="+tCompactor.HTTPURL(),
			"-querier.per-request-limits-enabled=true",
			"-frontend.encoding=protobuf",
			"-querier.shard-aggregations=quantile_over_time",
			"-frontend.tail-proxy-url="+tQuerier.HTTPURL(),
		)
	)
	require.NoError(t, clu.Run())

	tenantID := randStringRunes()

	now := time.Now()
	cliDistributor := client.New(tenantID, "", tDistributor.HTTPURL())
	cliDistributor.Now = now
	cliIngester := client.New(tenantID, "", tIngester.HTTPURL())
	cliIngester.Now = now
	cliQueryFrontend := client.New(tenantID, "", tQueryFrontend.HTTPURL())
	cliQueryFrontend.Now = now

	t.Run("/detected_fields", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=red", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=blue", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=red", now.Add(-5*time.Second), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=purple", now.Add(-5*time.Second), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=green", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliDistributor.PushLogLine("foo=bar color=red", now, nil, map[string]string{"job": "fake"}))

		// validate logs are there
		resp, err := cliQueryFrontend.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}
		assert.ElementsMatch(t, []string{"foo=bar color=red", "foo=bar color=blue", "foo=bar color=red", "foo=bar color=purple", "foo=bar color=green", "foo=bar color=red"}, lines)

		t.Run("non-split queries", func(t *testing.T) {
			start := cliQueryFrontend.Now.Add(-1 * time.Minute)
			end := cliQueryFrontend.Now.Add(time.Minute)

			v := url.Values{}
			v.Set("query", `{job="fake"}`)
			v.Set("start", client.FormatTS(start))
			v.Set("end", client.FormatTS(end))

			u := url.URL{}
			u.Path = "/loki/api/v1/detected_fields"
			u.RawQuery = v.Encode()
			dfResp, err := cliQueryFrontend.Get(u.String())
			require.NoError(t, err)
			defer dfResp.Body.Close()

			buf, err := io.ReadAll(dfResp.Body)
			require.NoError(t, err)

			var detectedFieldResponse DetectedFieldResponse
			err = json.Unmarshal(buf, &detectedFieldResponse)
			require.NoError(t, err)

			require.Equal(t, 2, len(detectedFieldResponse.Fields))

			var fooField, colorField DetectedField
			for _, field := range detectedFieldResponse.Fields {
				if field.Label == "foo" {
					fooField = field
				}

				if field.Label == "color" {
					colorField = field
				}
			}

			require.Equal(t, "string", fooField.Type)
			require.Equal(t, "string", colorField.Type)
			require.Equal(t, uint64(1), fooField.Cardinality)
			require.Equal(t, uint64(3), colorField.Cardinality)
		})

		t.Run("split queries", func(t *testing.T) {
			start := cliQueryFrontend.Now.Add(-24 * time.Hour)
			end := cliQueryFrontend.Now.Add(time.Minute)

			v := url.Values{}
			v.Set("query", `{job="fake"}`)
			v.Set("start", client.FormatTS(start))
			v.Set("end", client.FormatTS(end))

			u := url.URL{}
			u.Path = "/loki/api/v1/detected_fields"
			u.RawQuery = v.Encode()
			dfResp, err := cliQueryFrontend.Get(u.String())
			require.NoError(t, err)
			defer dfResp.Body.Close()

			buf, err := io.ReadAll(dfResp.Body)
			require.NoError(t, err)

			var detectedFieldResponse DetectedFieldResponse
			err = json.Unmarshal(buf, &detectedFieldResponse)
			require.NoError(t, err)

			require.Equal(t, 2, len(detectedFieldResponse.Fields))

			var fooField, colorField DetectedField
			for _, field := range detectedFieldResponse.Fields {
				if field.Label == "foo" {
					fooField = field
				}

				if field.Label == "color" {
					colorField = field
				}
			}

			require.Equal(t, "string", fooField.Type)
			require.Equal(t, "string", colorField.Type)
			require.Equal(t, uint64(1), fooField.Cardinality)
			require.Equal(t, uint64(4), colorField.Cardinality)
		})
	})
}
