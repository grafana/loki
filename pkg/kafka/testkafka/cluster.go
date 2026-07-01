// SPDX-License-Identifier: AGPL-3.0-only

package testkafka

import (
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// CreateCluster returns a fake Kafka cluster for unit testing.
//
// Recent versions of kfake support consumer groups natively (including
// member-less "simple" offset commits and the OffsetCommit v10 topic-ID
// protocol), so no custom control hooks are needed here.
func CreateCluster(t testing.TB, numPartitions int32, topicName string, opts ...kfake.Opt) (*kfake.Cluster, kafka.Config) {
	cluster, addr := CreateClusterWithoutCustomConsumerGroupsSupport(t, numPartitions, topicName, opts...)
	return cluster, createTestKafkaConfig(addr, topicName)
}

func createTestKafkaConfig(clusterAddr, topicName string) kafka.Config {
	cfg := kafka.Config{}
	flagext.DefaultValues(&cfg)

	cfg.WriterConfig.Address = clusterAddr
	cfg.WriterConfig.ClientID = "test-writer"
	cfg.Topic = topicName
	cfg.WriteTimeout = 2 * time.Second
	cfg.ReaderConfig.Address = clusterAddr
	cfg.ReaderConfig.ClientID = "test-reader"

	return cfg
}

func CreateClusterWithoutCustomConsumerGroupsSupport(t testing.TB, numPartitions int32, topicName string, opts ...kfake.Opt) (*kfake.Cluster, string) {
	cfg := []kfake.Opt{
		kfake.NumBrokers(1),
		kfake.SeedTopics(numPartitions, topicName),
	}

	// Apply options.
	cfg = append(cfg, opts...)

	cluster, err := kfake.NewCluster(cfg...)
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	addrs := cluster.ListenAddrs()
	require.Len(t, addrs, 1)

	return cluster, addrs[0]
}
