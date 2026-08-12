package executor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// TestDoLogObjectSort_LocalProfile sorts a real data object from local disk
// through the same sort-only executor path used by compaction. It is opt-in so
// normal test runs do not depend on local test data.
//
// Required environment variables:
//   - LOKI_SORT_PROFILE_OBJECT: absolute or relative path to the input object
//   - LOKI_SORT_PROFILE_TENANT: tenant whose LOG sections should be sorted
//   - LOKI_SORT_PROFILE_SCHEMA: comma-separated FQN sort keys
//
// Optional environment variables:
//   - LOKI_SORT_PROFILE_WORK_DIR: retain output and scratch under this directory
//   - LOKI_SORT_PROFILE_BUFFER_SIZE: uncompressed sort buffer in bytes (default 256 MiB)
//   - LOKI_SORT_PROFILE_STRIPES: stripe merge limit (default 8)
func TestDoLogObjectSort_LocalProfile(t *testing.T) {
	os.Setenv("LOKI_SORT_PROFILE_OBJECT", "/Users/benclive/Downloads/40bd9e2a9d3aa0297a5d190347c58d0e1bc40a460ee81609473bdf")
	os.Setenv("LOKI_SORT_PROFILE_TENANT", "156331")
	os.Setenv("LOKI_SORT_PROFILE_SCHEMA", "label:service_name,label:cluster,label:namespace,label:job")

	inputPath := os.Getenv("LOKI_SORT_PROFILE_OBJECT")
	if inputPath == "" {
		t.Skip("set LOKI_SORT_PROFILE_OBJECT to run the local sort profile")
	}
	tenant := os.Getenv("LOKI_SORT_PROFILE_TENANT")
	require.NotEmpty(t, tenant, "LOKI_SORT_PROFILE_TENANT is required")

	var sortSchema []string
	for _, key := range strings.Split(os.Getenv("LOKI_SORT_PROFILE_SCHEMA"), ",") {
		if key = strings.TrimSpace(key); key != "" {
			sortSchema = append(sortSchema, key)
		}
	}
	require.NotEmpty(t, sortSchema, "LOKI_SORT_PROFILE_SCHEMA is required")

	absoluteInputPath, err := filepath.Abs(inputPath)
	require.NoError(t, err)
	info, err := os.Stat(absoluteInputPath)
	require.NoError(t, err)
	require.False(t, info.IsDir(), "input object must be a file")

	workParent := os.Getenv("LOKI_SORT_PROFILE_WORK_DIR")
	retainOutput := workParent != ""
	if workParent == "" {
		// Keeping the temporary directory beside the source ensures the hard
		// link is on the same filesystem and avoids copying a large input.
		workParent = filepath.Dir(absoluteInputPath)
	} else {
		require.NoError(t, os.MkdirAll(workParent, 0o755))
	}
	workDir, err := os.MkdirTemp(workParent, ".loki-sort-profile-")
	require.NoError(t, err)
	if !retainOutput {
		t.Cleanup(func() { require.NoError(t, os.RemoveAll(workDir)) })
	}

	const sourceObjectPath = "source.dataobj"
	require.NoError(
		t,
		os.Link(absoluteInputPath, filepath.Join(workDir, sourceObjectPath)),
		"hard-linking input failed; place LOKI_SORT_PROFILE_WORK_DIR on the same filesystem as the input",
	)

	bucket, err := filesystem.NewBucket(workDir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, bucket.Close()) })

	scratchDir := filepath.Join(workDir, "scratch")
	require.NoError(t, os.MkdirAll(scratchDir, 0o755))
	scratchStore := scratch.NewMemory()

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:            1 << 20, // 1MB
		MaxPageRows:               10000,
		TargetObjectSize:          1 << 30,   // 1GB
		TargetSectionSize:         512 << 20, // 512MB
		BufferSize:                256 << 20, // 256MB
		SectionStripeMergeLimit:   8,         //
		EstimatedCompressionRatio: 8,
	}
	if value := os.Getenv("LOKI_SORT_PROFILE_BUFFER_SIZE"); value != "" {
		size, err := strconv.ParseInt(value, 10, 64)
		require.NoError(t, err, "LOKI_SORT_PROFILE_BUFFER_SIZE must be bytes")
		require.Positive(t, size)
		cfg.BufferSize = flagext.Bytes(size)
	}
	if value := os.Getenv("LOKI_SORT_PROFILE_STRIPES"); value != "" {
		stripes, err := strconv.Atoi(value)
		require.NoError(t, err)
		require.GreaterOrEqual(t, stripes, 2)
		cfg.SectionStripeMergeLimit = stripes
	}

	obj, err := dataobj.FromBucket(t.Context(), bucket, sourceObjectPath, 0)
	require.NoError(t, err)
	var sectionRefs []*compactionv2pb.SectionRef
	for index, section := range obj.Sections().Filter(logs.CheckSection) {
		if section.Tenant == tenant {
			sectionRefs = append(sectionRefs, &compactionv2pb.SectionRef{
				ObjectPath:   sourceObjectPath,
				SectionIndex: int64(index),
			})
		}
	}
	require.NotEmpty(t, sectionRefs, "input has no LOG sections for tenant %q", tenant)

	executor := &Context{
		bucket:       bucket,
		dataBucket:   bucket,
		scratchStore: scratchStore,
		indexobjCfg:  cfg,
		logger:       log.NewNopLogger(),
	}
	node := &physical.LogMerge{
		Tenant:      tenant,
		SortSchema:  sortSchema,
		SortOnly:    true,
		StreamOrder: compactionv2pb.STREAM_ORDER_STABLE_HASH_V1,
		ShardCount:  streams.ShardFactor,
		Runs: []*compactionv2pb.RunRef{{
			Sections: sectionRefs,
		}},
	}

	start := time.Now()
	artifacts, err := executor.doLogObjectMerge(context.Background(), node)
	require.NoError(t, err)
	require.Len(t, artifacts, 1)
	t.Logf("sorted %s in %s; index=%s; work_dir=%s", absoluteInputPath, time.Since(start), artifacts[0].Path, workDir)
}
