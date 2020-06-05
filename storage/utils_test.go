package storage

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/cassandra"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
)

const (
	userID    = "userID"
	tableName = "test"
)

type storageClientTest func(*testing.T, chunk.IndexClient, chunk.Client)

func forAllFixtures(t *testing.T, storageClientTest storageClientTest) {
	var fixtures []testutils.Fixture
	fixtures = append(fixtures, aws.Fixtures...)
	fixtures = append(fixtures, gcp.Fixtures...)
	fixtures = append(fixtures, local.Fixtures...)
	fixtures = append(fixtures, cassandra.Fixtures()...)
	fixtures = append(fixtures, Fixtures...)

	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			indexClient, objectClient, closer, err := testutils.Setup(fixture, tableName)
			require.NoError(t, err)
			defer closer.Close()
			storageClientTest(t, indexClient, objectClient)
		})
	}
}
