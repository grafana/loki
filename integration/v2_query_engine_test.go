//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestV2QueryEngine(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	// Use a fixed tenant ID for both dataobj creation and queries
	tenantID := "test-tenant"
	// Use current time minus 2 hours to ensure data is old enough for v2 engine
	// The v2 engine requires data to be older than 1 hour, using Before() check
	// so we need to be strictly before 1 hour, not exactly 1 hour
	now := time.Now()
	// Truncate to second precision to make test behavior more predictable
	// This ensures timestamps like "to - 45min" are exactly at minute boundaries
	to := now.Add(-90 * time.Minute).Truncate(time.Second) // 1.5 hours ago
	from := to.Add(-2 * time.Hour)

	// Create dataobj files before starting the cluster
	// Get the shared path from the cluster
	sharedPath := clu.ClusterSharedPath()
	_, err := createDataObjFiles(t, sharedPath, tenantID, to)
	require.NoError(t, err, "Failed to create dataobj files")

	// Start all components with v2 engine enabled
	// Instant queries will not use v2 engine by default because they go through query frontend
	// which doesn't preserve the v2 engine setting. We'll test what happens anyway.
	var (
		tAll = clu.AddComponent(
			"all",
			"-target=all",
			"-querier.engine.enable-v2-engine=true", // Enable v2 engine
			"-log.level=info",                       // Log level
			"-dataobj-metastore.index-storage-prefix=",                        // No prefix, files are directly in the bucket root
			fmt.Sprintf("-dataobj-metastore.enabled-tenant-ids=%s", tenantID), // Enable for our test tenant
		)
	)

	require.NoError(t, clu.Run())

	cli := client.New(tenantID, "", tAll.HTTPURL())

	// No need to ingest data - we already created dataobj files with the test data
	// The dataobj files contain the same log lines we would have ingested here

	t.Run("simple log query with v2 engine", func(t *testing.T) {
		// Query for logs with a simple selector
		// The v2 engine should handle this query and add a warning
		// Query for data older than 1 hour to trigger v2 engine
		queryStart := from
		// Add 1 second to queryEnd to include line5 which is exactly at 'to' timestamp
		queryEnd := to.Add(1 * time.Second)
		resp, err := cli.RunRangeQueryWithStartEnd(context.Background(), `{job="app"}`,
			queryStart, queryEnd)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		// Check we got actual data from v2 engine reading dataobj files
		lines := extractLogLines(resp)

		// First check if v2 engine is being used by checking warnings
		respWithWarnings := QueryResponseWithWarnings{}
		err = queryWithWarnings(tenantID, tAll.HTTPURL(), `{job="app"}`,
			queryStart, queryEnd, &respWithWarnings)
		require.NoError(t, err)

		// v2 engine should add a warning about experimental engine
		require.Len(t, respWithWarnings.Warnings, 1, "Should have v2 engine warning")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine", "Should have v2 engine warning")

		// Now check we got data
		require.Greater(t, len(lines), 0, "Should get log lines from dataobj files")

		// We expect lines: line1, line2, line3, line5
		// line1, line3, line5 are from job="app", level="info"
		// line2 is from job="app", level="error"
		// line4 is from job="system", level="debug"
		// line6a and line6b is from job="system", level="error"
		// logfmt'd lines are from job="api"
		expectedLines := []string{"line1", "line2", "line3", "line5"}
		for _, expected := range expectedLines {
			found := slices.Contains(lines, expected)
			assert.True(t, found, "Should find line: %s", expected)
		}
	})

	t.Run("log query with label selector", func(t *testing.T) {
		// Query with a label filter - should return only the error level logs for job="app"
		resp, err := cli.RunRangeQueryWithStartEnd(context.Background(), `{job="app", level="error"}`,
			from, to.Add(1*time.Second))
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		// Check if v2 engine is being used
		respWithWarnings := QueryResponseWithWarnings{}
		err = queryWithWarnings(tenantID, tAll.HTTPURL(), `{job="app", level="error"}`,
			from, to.Add(1*time.Second), &respWithWarnings)
		require.NoError(t, err)

		// The v2 engine should be processing this query
		require.Greater(t, len(respWithWarnings.Warnings), 0, "Should have v2 engine warning")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine")

		// Check we got the expected data
		lines := extractLogLines(resp)

		// We expect only line2 which is from job="app" with level="error"
		// line1, line3, line5 are from job="app", level="info"
		// line2 is from job="app", level="error"
		// line4 is from job="system", level="debug"
		// line6 is from job="system", level="error"
		// logfmt'd lines are from job="api"
		require.Len(t, lines, 1, "Should get exactly 1 line for job='app', level='error'")
		assert.Equal(t, "line2", lines[0], "Should get line2 which is the error log for app")
	})

	t.Run("log query with line filter", func(t *testing.T) {
		// Query with a line filter - should return only logs containing "line1"
		resp, err := cli.RunRangeQueryWithStartEnd(context.Background(), `{job="app"} |= "line1"`,
			from, to.Add(1*time.Second))
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		// Check if v2 engine is being used
		respWithWarnings := QueryResponseWithWarnings{}
		err = queryWithWarnings(tenantID, tAll.HTTPURL(), `{job="app"} |= "line1"`,
			from, to.Add(1*time.Second), &respWithWarnings)
		require.NoError(t, err)

		// The v2 engine should be processing this query
		require.Greater(t, len(respWithWarnings.Warnings), 0, "Should have v2 engine warning")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine")

		// Check we got the expected data
		lines := extractLogLines(resp)

		// We expect only line1 from job="app" logs that contains "line1"
		require.Len(t, lines, 1, "Should get exactly 1 line containing 'line1'")
		assert.Equal(t, "line1", lines[0], "Should get line1 which matches the filter")
	})

	t.Run("simple metric instant query", func(t *testing.T) {
		// Test a sum by job instant query - v2 engine requires sum with grouping
		query := `sum by (job) (count_over_time({job="system"}[1h]))`

		// Make an instant query at the middle of our range
		instantTime := to.Add(-30 * time.Minute)
		v := url.Values{}
		v.Set("query", query)
		v.Set("time", strconv.FormatInt(instantTime.Unix(), 10))

		u, err := url.Parse(tAll.HTTPURL())
		require.NoError(t, err)
		u.Path = "/loki/api/v1/query"
		u.RawQuery = v.Encode()

		req, err := http.NewRequestWithContext(context.Background(), "GET", u.String(), nil)
		require.NoError(t, err)
		req.Header.Set("X-Scope-OrgID", tenantID)

		httpResp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer httpResp.Body.Close()

		body, err := io.ReadAll(httpResp.Body)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, httpResp.StatusCode)

		var respWithWarnings QueryResponseWithWarnings
		err = json.Unmarshal(body, &respWithWarnings)
		require.NoError(t, err)

		// v2 engine must handle all instant queries
		require.Len(t, respWithWarnings.Warnings, 1, "Should have v2 engine warning")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine", "Should have v2 engine warning")
		assert.Equal(t, "vector", respWithWarnings.Data.ResultType)

		// We should get a result for job="system"
		require.Len(t, respWithWarnings.Data.Vector, 1, "Should have exactly one result")
		vec := respWithWarnings.Data.Vector[0]
		count, err := strconv.ParseFloat(vec.Value, 64)
		require.NoError(t, err)
		// We have 2 log entries for job="system" in the time range, line6a and line6b
		// line1, line3, line5 are from job="app", level="info"
		// line2 is from job="app", level="error"
		// line4 is from job="system", level="debug"
		// line6a and line6b is from job="system", level="error"
		// logfmt'd lines are from job="api"
		assert.Equal(t, count, 2.0, "Should have counted log llines for job='system'")
	})

	t.Run("metric instant query with logfmt groupby", func(t *testing.T) {
		// Instant metric query with count_over_time and grouping by parsed logfmt field
		// This tests the v2 engine's logfmt support for metric queries
		query := `sum by (region) (count_over_time({job="api"} | logfmt [5m]))`

		// Make an instant query at a time that includes our data
		// Use a time slightly after to-30min to ensure we capture logs properly
		instantTime := to.Add(-29 * time.Minute)
		v := url.Values{}
		v.Set("query", query)
		v.Set("time", strconv.FormatInt(instantTime.Unix(), 10))

		u, err := url.Parse(tAll.HTTPURL())
		require.NoError(t, err)
		u.Path = "/loki/api/v1/query"
		u.RawQuery = v.Encode()

		req, err := http.NewRequestWithContext(context.Background(), "GET", u.String(), nil)
		require.NoError(t, err)
		req.Header.Set("X-Scope-OrgID", tenantID)

		httpResp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer httpResp.Body.Close()

		// Query must succeed for v2 engine instant queries
		require.Equal(t, http.StatusOK, httpResp.StatusCode, "Query should succeed")

		// Parse response to check for warnings
		var respWithWarnings QueryResponseWithWarnings
		err = json.NewDecoder(httpResp.Body).Decode(&respWithWarnings)
		require.NoError(t, err)

		// v2 engine should add warning for metric queries it handles
		require.Greater(t, len(respWithWarnings.Warnings), 0, "Expected warnings for v2 engine metric query")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine",
			"Expected v2 engine warning for instant metric query with logfmt")

		// Check the actual metric results
		// At instantTime (to - 29min), looking back 5 minutes includes logs from (to-34min, to-29min]
		// Start boundary is EXCLUSIVE, end boundary is INCLUSIVE
		// The api job logs are:
		// - to-45min: outside window
		// - to-40min: outside window
		// - to-35min: outside window  
		// - to-30min: level=debug msg="cache hit" status=200 region=us-west (INCLUDED)
		// - to-25min: outside window
		// So we expect 1 log for us-west region
		assert.Equal(t, "vector", respWithWarnings.Data.ResultType)

		// Build a map of region -> count from the results
		regionCounts := make(map[string]float64)
		for _, vec := range respWithWarnings.Data.Vector {
			if region, ok := vec.Metric["region"]; ok {
				count, err := strconv.ParseFloat(vec.Value, 64)
				require.NoError(t, err)
				regionCounts[region] = count
			}
		}

		// We expect us-west region with 1 log in the 5m window
		// Only the log at to-30min falls within (to-34min, to-29min]
		require.Len(t, regionCounts, 1, "Should have exactly one region")
		assert.Equal(t, 1.0, regionCounts["us-west"], "us-west region should have 1 log in the 5m window")
	})

	t.Run("metric instant query with logfmt groupby and filter", func(t *testing.T) {
		// Instant metric query that groups by one logfmt field (region) and filters by another (status)
		// This tests that multiple parsed fields can be used in different parts of the query
		query := `sum by (region) (count_over_time({job="api"} | logfmt | status="200" [20m]))`

		// Make an instant query at a time that includes our data
		instantTime := to.Add(-25 * time.Minute)
		v := url.Values{}
		v.Set("query", query)
		v.Set("time", strconv.FormatInt(instantTime.Unix(), 10))

		u, err := url.Parse(tAll.HTTPURL())
		require.NoError(t, err)
		u.Path = "/loki/api/v1/query"
		u.RawQuery = v.Encode()

		req, err := http.NewRequestWithContext(context.Background(), "GET", u.String(), nil)
		require.NoError(t, err)
		req.Header.Set("X-Scope-OrgID", tenantID)

		httpResp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer httpResp.Body.Close()

		body, err := io.ReadAll(httpResp.Body)
		require.NoError(t, err)

		// Query must succeed for v2 engine instant queries
		require.Equal(t, http.StatusOK, httpResp.StatusCode, "Query should succeed")

		// Parse response to check for warnings
		var respWithWarnings QueryResponseWithWarnings
		err = json.Unmarshal(body, &respWithWarnings)
		require.NoError(t, err)

		// v2 engine should add warning for metric queries it handles
		require.Greater(t, len(respWithWarnings.Warnings), 0, "Expected warnings for v2 engine metric query")
		assert.Contains(t, respWithWarnings.Warnings[0], "experimental query engine",
			"Expected v2 engine warning for instant metric query with logfmt filter")

		// Check the actual metric results
		// At instantTime (to - 25min), looking back 20 minutes includes logs from (to-45min, to-25min]
		// Start boundary is EXCLUSIVE, end boundary is INCLUSIVE
		// The api job logs are:
		// - to-45min: status=200 region=us-east (EXCLUDED - exactly at start boundary)
		// - to-40min: status=500 region=us-east (filtered out by status!=200)
		// - to-35min: status=200 region=us-west (included)
		// - to-30min: status=200 region=us-west (included)
		// - to-25min: status=504 region=eu-central (included but filtered out by status!=200)
		// So we expect only us-west region with 2 logs
		assert.Equal(t, "vector", respWithWarnings.Data.ResultType)

		// Build a map of region -> count from the results
		regionCounts := make(map[string]float64)
		for _, vec := range respWithWarnings.Data.Vector {
			if region, ok := vec.Metric["region"]; ok {
				count, err := strconv.ParseFloat(vec.Value, 64)
				require.NoError(t, err)
				regionCounts[region] = count
			}
		}

		// We expect only us-west region with status=200 logs:
		// The log at to-45min is excluded (at exact start boundary)
		// The log at to-40min has status=500 so it's filtered out
		// us-west: 2 logs at to-35min and to-30min with status=200
		require.Len(t, regionCounts, 1, "Should have one region after filtering by status=200")
		assert.Equal(t, 2.0, regionCounts["us-west"], "us-west region should have 2 logs with status=200")
	})

}

// extractLogLines extracts all log lines from a query response.
// It iterates through all streams and their values, collecting the log line content (val[1])
// from each value tuple [timestamp, line].
func extractLogLines(resp *client.Response) []string {
	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			// val[0] is timestamp, val[1] is the log line
			lines = append(lines, val[1])
		}
	}
	return lines
}

// queryWithWarnings makes a raw query and unmarshals the response including warnings
func queryWithWarnings(tenantID, baseURL, query string, start, end time.Time, resp *QueryResponseWithWarnings) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build the query URL

	v := url.Values{}
	v.Set("query", query)
	v.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	v.Set("end", strconv.FormatInt(end.UnixNano(), 10))

	u, err := url.Parse(baseURL)
	if err != nil {
		return err
	}
	u.Path = "/loki/api/v1/query_range"
	u.RawQuery = v.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return err
	}

	// Add the tenant ID header
	req.Header.Set("X-Scope-OrgID", tenantID)

	httpResp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return fmt.Errorf("query failed with status %d: %s", httpResp.StatusCode, body)
	}

	return json.NewDecoder(httpResp.Body).Decode(resp)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// QueryResponseWithWarnings extends the client Response to include warnings
type QueryResponseWithWarnings struct {
	Status   string          `json:"status"`
	Warnings []string        `json:"warnings,omitempty"`
	Data     client.DataType `json:"data"`
}

// createDataObjFiles creates dataobj files in the cluster's shared directory
// so that the v2 engine can read them. Returns the bucket for cleanup.
func createDataObjFiles(t *testing.T, sharedPath string, tenantID string, oldTime time.Time) (objstore.Bucket, error) {
	// Create dataobj directory structure - v2 engine expects files in fs-store-1/dataobj
	dataobjDir := filepath.Join(sharedPath, "fs-store-1", "dataobj")
	if err := os.MkdirAll(dataobjDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dataobj dir: %w", err)
	}

	// Create metastore directory for the tenant
	metastoreDir := filepath.Join(dataobjDir, fmt.Sprintf("tenant-%s", tenantID), "metastore")
	if err := os.MkdirAll(metastoreDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metastore dir: %w", err)
	}

	// Create index directories
	indexMetastoreDir := filepath.Join(dataobjDir, "index", "v0", fmt.Sprintf("tenant-%s", tenantID), "metastore")
	if err := os.MkdirAll(indexMetastoreDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create index metastore dir: %w", err)
	}

	// Create filesystem bucket
	bucket, err := filesystem.NewBucket(dataobjDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create bucket: %w", err)
	}

	// Create builder for dataobj files
	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:          2 * 1024 * 1024,   // 2MB
		TargetObjectSize:        128 * 1024 * 1024, // 128MB
		TargetSectionSize:       16 * 1024 * 1024,  // 16MB
		BufferSize:              16 * 1024 * 1024,  // 16MB
		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create builder: %w", err)
	}

	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowWarn())
	metastoreToc := metastore.NewTableOfContentsWriter(metastore.Config{}, bucket, tenantID, logger)
	uploaderObj := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID, logger)

	// Create test log streams - same data as we push in the test
	streams := []logproto.Stream{
		{
			Labels: labels.FromStrings("job", "app", "level", "info").String(),
			Entries: []logproto.Entry{
				{Timestamp: oldTime.Add(-45 * time.Minute), Line: "line1"},
				{Timestamp: oldTime.Add(-30 * time.Minute), Line: "line3"},
				{Timestamp: oldTime, Line: "line5"},
			},
		},
		{
			Labels: labels.FromStrings("job", "app", "level", "error").String(),
			Entries: []logproto.Entry{
				{Timestamp: oldTime.Add(-45 * time.Minute), Line: "line2"},
			},
		},
		{
			Labels: labels.FromStrings("job", "system", "level", "debug").String(),
			Entries: []logproto.Entry{
				{Timestamp: oldTime.Add(-40 * time.Minute), Line: "line4a"},
				{Timestamp: oldTime.Add(-30 * time.Minute), Line: "line4b"},
				{Timestamp: oldTime.Add(-20 * time.Minute), Line: "line4c"},
			},
		},
		{
			Labels: labels.FromStrings("job", "system", "level", "error").String(),
			Entries: []logproto.Entry{
				{Timestamp: oldTime.Add(-35 * time.Minute), Line: "line6a"},
				{Timestamp: oldTime.Add(-25 * time.Minute), Line: "line6b"},
				{Timestamp: oldTime, Line: "line6c"},
			},
		},
		{
			Labels: labels.FromStrings("job", "api").String(),
			Entries: []logproto.Entry{
				{Timestamp: oldTime.Add(-45 * time.Minute), Line: `level=info msg="app started" status=200 region=us-east`},
				{Timestamp: oldTime.Add(-40 * time.Minute), Line: `level=error msg="request failed" status=500 region=us-east`},
				{Timestamp: oldTime.Add(-35 * time.Minute), Line: `level=info msg="processing" status=200 region=us-west`},
				{Timestamp: oldTime.Add(-30 * time.Minute), Line: `level=debug msg="cache hit" status=200 region=us-west`},
				{Timestamp: oldTime.Add(-25 * time.Minute), Line: `level=error msg="timeout" status=504 region=eu-central`},
			},
		},
	}

	// Append streams to builder
	for _, stream := range streams {
		if err := builder.Append(stream); err != nil {
			return nil, fmt.Errorf("failed to append stream: %w", err)
		}
	}

	// Flush to create dataobj file
	minTime, maxTime := builder.TimeRange()
	obj, closer, err := builder.Flush()
	if err != nil {
		return nil, fmt.Errorf("failed to flush builder: %w", err)
	}
	defer closer.Close()

	// Upload the data object
	path, err := uploaderObj.Upload(context.Background(), obj)
	if err != nil {
		return nil, fmt.Errorf("failed to upload data object: %w", err)
	}

	// Update metastore ToC
	if err := metastoreToc.WriteEntry(context.Background(), path, minTime, maxTime); err != nil {
		return nil, fmt.Errorf("failed to update metastore: %w", err)
	}

	// Build index for the dataobj
	if err := buildIndex(bucket, tenantID, logger); err != nil {
		return nil, fmt.Errorf("failed to build index: %w", err)
	}

	return bucket, nil
}

// buildIndex creates index files for the dataobj files
func buildIndex(bucket objstore.Bucket, tenantID string, logger log.Logger) error {
	indexDirPrefix := "index/v0"
	indexWriterBucket := objstore.NewPrefixedBucket(bucket, indexDirPrefix)
	indexMetastoreToc := metastore.NewTableOfContentsWriter(metastore.Config{}, indexWriterBucket, tenantID, logger)

	builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
		TargetPageSize:          128 * 1024,        // 128KB
		TargetObjectSize:        128 * 1024 * 1024, // 128MB
		TargetSectionSize:       16 * 1024 * 1024,  // 16MB
		BufferSize:              16 * 1024 * 1024,  // 16MB
		SectionStripeMergeLimit: 2,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to create index builder: %w", err)
	}

	calculator := index.NewCalculator(builder)

	// Find all dataobj files and create index for them
	err = bucket.Iter(context.Background(), "", func(name string) error {
		if !strings.Contains(name, "objects") {
			return nil
		}

		reader, err := dataobj.FromBucket(context.Background(), bucket, name)
		if err != nil {
			return fmt.Errorf("failed to read object: %w", err)
		}

		if err := calculator.Calculate(context.Background(), logger, reader, name); err != nil {
			return fmt.Errorf("failed to calculate index: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Check if calculator has data before flushing
	minTime, maxTime := calculator.TimeRange()
	if minTime.IsZero() && maxTime.IsZero() {
		// No index data, skip index creation
		return nil
	}

	// Flush index
	obj, closer, err := calculator.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush index: %w", err)
	}
	defer closer.Close()

	key, err := index.ObjectKey(context.Background(), tenantID, obj)
	if err != nil {
		return fmt.Errorf("failed to create object key: %w", err)
	}

	reader, err := obj.Reader(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create reader for index object: %w", err)
	}
	defer reader.Close()

	if err := indexWriterBucket.Upload(context.Background(), key, reader); err != nil {
		return fmt.Errorf("failed to upload index: %w", err)
	}

	if err := indexMetastoreToc.WriteEntry(context.Background(), key, minTime, maxTime); err != nil {
		return fmt.Errorf("failed to update index metastore: %w", err)
	}

	return nil
}
