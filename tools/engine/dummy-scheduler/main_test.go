package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	*flagWire = "local"
	*flagSchedulers = 1
	*flagWorkers = 400
	*flagThreads = 128
	*flagWorkflows = 2000
	*flagParallelism = 1000
	*flagBatches = 1000
	*flagBatchSize = 1
	*flagSleep = time.Millisecond * 1000

	batches := int64(*flagBatches)
	workflows := int64(*flagWorkflows)
	workers := int64(*flagWorkers)
	threads := int64(*flagThreads)
	totalBatches := batches * workflows
	totalThreads := workers * threads
	sleep := (*flagSleep).Nanoseconds()
	fmt.Printf("Total dummy workflow time: %v\n", time.Duration(sleep*totalBatches))
	fmt.Printf("Ideal total execution time: %v\n", time.Duration(sleep*totalBatches/totalThreads))

	ctx := user.InjectOrgID(context.Background(), tenantID)
	//logger := gokitlog.NewLogfmtLogger(gokitlog.NewSyncWriter(os.Stderr))
	logger := gokitlog.NewNopLogger()
	err := run(ctx, logger)
	require.NoError(t, err)
}
