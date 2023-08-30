package tests

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
)

const (
	userID    = "userID"
	tableName = "test"
)

type chunkClientTest func(*testing.T, client.Client)
type indexClientTest func(*testing.T, index.Client)

func forAllChunkClientFixtures(t *testing.T, chunkClientTest chunkClientTest) {
	var fixtures []testutils.ChunkFixture
	fixtures = append(fixtures, local.ChunkFixture)
	fixtures = append(fixtures, aws.ChunkFixture)
	fixtures = append(fixtures, gcp.ChunkFixture)

	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			chunkClient, closer, err := fixture.Client()
			require.NoError(t, err)
			defer closer.Close()

			chunkClientTest(t, chunkClient)
		})
	}
}

func forAllIndexClientFixtures(t *testing.T, indexClientTest indexClientTest) {
	var fixtures []testutils.IndexFixture
	fixtures = append(fixtures, local.IndexFixture)
	fixtures = append(fixtures, Fixture)

	for _, fixture := range fixtures {
		t.Run(fixture.Name(), func(t *testing.T) {
			indexClient, closer, err := fixture.Client()
			require.NoError(t, err)
			defer closer.Close()

			indexClientTest(t, indexClient)
		})
	}
}
