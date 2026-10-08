package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestBuildSchemaConfig_NoSchemas(t *testing.T) {
	spec := lokiv1.ObjectStorageSpec{}
	status := lokiv1.LokiStackStorageStatus{}

	expected, err := BuildSchemaConfig(time.Now().UTC(), spec, status, nil)

	require.Error(t, err)
	require.Nil(t, expected)
}

func TestBuildSchemaConfig_AddSchema_NoStatuses(t *testing.T) {
	spec := lokiv1.ObjectStorageSpec{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV13,
				EffectiveDate: "2020-10-01",
			},
		},
	}
	status := lokiv1.LokiStackStorageStatus{}

	actual, err := BuildSchemaConfig(time.Now().UTC(), spec, status, nil)
	expected := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV13,
			EffectiveDate: "2020-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

