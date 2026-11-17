//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/cluster"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// dataObjCluster is a cluster that writes through Kafka to the chunk store, by an ingester, and to
// data objects, by a dataobj-builder. It reads through a query-frontend and a querier.
type dataObjCluster struct {
	distributor    *cluster.Component
	ingester       *cluster.Component
	indexGateway   *cluster.Component
	querier        *cluster.Component
	queryFrontend  *cluster.Component
	queryStartTime string
}

type dataObjClusterSettings struct {
	extraFlags        []string
	chunksOnlyQuerier bool
}

// dataObjClusterOption changes how startDataObjCluster starts the cluster.
type dataObjClusterOption func(*dataObjClusterSettings)

// withExtraFlags adds flags to every component. A flag overrides an earlier flag of the same name.
func withExtraFlags(flags ...string) dataObjClusterOption {
	return func(settings *dataObjClusterSettings) {
		settings.extraFlags = append(settings.extraFlags, flags...)
	}
}

// withChunksOnlyQuerier starts the querier without the data-object flags, so it reads the chunk
// store only. restartQuerierOnDataObjects adds the flags later.
func withChunksOnlyQuerier() dataObjClusterOption {
	return func(settings *dataObjClusterSettings) {
		settings.chunksOnlyQuerier = true
	}
}

// startDataObjCluster starts a dataObjCluster and returns once every component runs.
//
// The querier and the query-frontend run with store-only and stream-first execution on. Both read
// data objects, unless withChunksOnlyQuerier is set.
func startDataObjCluster(t *testing.T, options ...dataObjClusterOption) *dataObjCluster {
	t.Helper()

	var settings dataObjClusterSettings
	for _, option := range options {
		option(&settings)
	}

	// Single-partition fake Kafka, so the ingester and the dataobj-builder consume records in
	// the same order.
	kafkaCluster, _ := testkafka.CreateCluster(t, 1, "loki")
	kafkaAddr := kafkaCluster.ListenAddrs()[0]

	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer(fmt.Sprintf("v%d", config.LatestSchemaVersion))
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	flags := func(own ...string) []string {
		return append(own, settings.extraFlags...)
	}

	kafkaIngestionFlags := []string{
		"-kafka.writer.address=" + kafkaAddr,
		"-kafka.reader.address=" + kafkaAddr,
		"-kafka.topic=loki",
		"-ingester.kafka-ingestion-enabled=true",
		"-ingester.partition-ring.store=inmemory",
	}

	// Start compactor and index-gateway.
	var (
		tCompactor = clu.AddComponent(
			"compactor",
			flags(
				"-target=compactor",
				"-compactor.compaction-interval=1h",
			)...,
		)
		tIndexGateway = clu.AddComponent(
			"index-gateway",
			flags("-target=index-gateway")...,
		)
	)
	require.NoError(t, clu.Run())

	// Start distributor, ingester, and dataobj-builder, all reading the same Kafka topic.
	var (
		tDistributor = clu.AddComponent(
			"distributor",
			flags(append([]string{
				"-target=distributor",
				"-distributor.kafka-writes-enabled=true",
				"-distributor.ingester-writes-enabled=false",
			}, kafkaIngestionFlags...)...)...,
		)
		tIngester = clu.AddComponent(
			"ingester",
			flags(append([]string{
				"-target=ingester",
				"-ingester.lifecycler.ID=ingester-0", // deterministic partition id 0
				"-ingester.partition-ring.min-partition-owners-duration=0s",
				"-kafka.max-consumer-lag-at-startup=1s",
				"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
			}, kafkaIngestionFlags...)...)...,
		)
		_ = clu.AddComponent(
			"dataobj-builder",
			flags(
				"-target=dataobj-builder",
				"-kafka.reader.address="+kafkaAddr,
				"-dataobj.enabled=true",
				"-dataobj.builder.topic=loki",
				"-dataobj.builder.partition-id=0", // the fake Kafka's only partition
				"-dataobj.builder.idle-flush-timeout=1s",
				"-dataobj.builder.max-builder-age=1s",
			)...,
		)
	)
	require.NoError(t, clu.Run())

	// Start query-scheduler.
	tQueryScheduler := clu.AddComponent(
		"query-scheduler",
		flags(
			"-target=query-scheduler",
			"-query-scheduler.use-scheduler-ring=false",
			"-tsdb.shipper.index-gateway-client.server-address="+tIndexGateway.GRPCURL(),
		)...,
	)
	require.NoError(t, clu.Run())

	// Start querier and query-frontend.
	queryStartTime := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	querierFlags := []string{
		"-target=querier",
		"-querier.scheduler-address=" + tQueryScheduler.GRPCURL(),
		"-querier.query-store-only=true",
		"-querier.stream-first-execution-enabled=true",
		"-common.compactor-address=" + tCompactor.HTTPURL(),
		"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
	}
	if !settings.chunksOnlyQuerier {
		querierFlags = append(querierFlags, dataObjReadFlags(queryStartTime)...)
	}
	var (
		tQuerier       = clu.AddComponent("querier", flags(querierFlags...)...)
		tQueryFrontend = clu.AddComponent(
			"query-frontend",
			flags(append([]string{
				"-target=query-frontend",
				"-frontend.scheduler-address=" + tQueryScheduler.GRPCURL(),
				"-frontend.default-validity=0s",
				"-querier.query-store-only=true",
				"-querier.stream-first-execution-enabled=true",
				"-common.compactor-address=" + tCompactor.HTTPURL(),
				"-tsdb.shipper.index-gateway-client.server-address=" + tIndexGateway.GRPCURL(),
			}, dataObjReadFlags(queryStartTime)...)...)...,
		)
	)
	require.NoError(t, clu.Run())

	return &dataObjCluster{
		distributor:    tDistributor,
		ingester:       tIngester,
		indexGateway:   tIndexGateway,
		querier:        tQuerier,
		queryFrontend:  tQueryFrontend,
		queryStartTime: queryStartTime,
	}
}

// restartQuerierOnDataObjects adds the data-object flags to the querier and restarts it. The extra
// flags go to the querier only.
func (c *dataObjCluster) restartQuerierOnDataObjects(t *testing.T, extraFlags ...string) {
	t.Helper()

	c.querier.AddFlags(dataObjReadFlags(c.queryStartTime)...)
	c.querier.AddFlags(extraFlags...)
	require.NoError(t, c.querier.Restart())
}

// dataObjReadFlags are the flags that make a component read data objects. Stream-first queries read
// data objects from the query start time on.
func dataObjReadFlags(queryStartTime string) []string {
	return []string{
		"-dataobj.enabled=true",
		"-querier.dataobj-query-start-time=" + queryStartTime,
		"-dataobj.storage-lag=0s",
		"-dataobj-metastore.toc-resolver.enabled=false",
	}
}
