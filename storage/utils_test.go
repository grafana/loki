package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/chunk/aws"
	"github.com/weaveworks/cortex/pkg/chunk/cassandra"
	"github.com/weaveworks/cortex/pkg/chunk/gcp"
	"github.com/weaveworks/cortex/pkg/chunk/testutils"
)

const (
	userID    = "userID"
	tableName = "test"
)

type storageClientTest func(*testing.T, chunk.StorageClient)

func forAllFixtures(t *testing.T, storageClientTest storageClientTest) {
	fixtures := append(aws.Fixtures, gcp.Fixtures...)

	cassandraFixtures, err := cassandra.Fixtures()
	require.NoError(t, err)
	fixtures = append(fixtures, cassandraFixtures...)

	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			storageClient, err := testutils.Setup(fixture, tableName)
			require.NoError(t, err)
			defer fixture.Teardown()

			storageClientTest(t, storageClient)
		})
	}
}
