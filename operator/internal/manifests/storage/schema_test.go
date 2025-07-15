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

	expected, err := BuildSchemaConfig(time.Now().UTC(), spec, status)

	require.Error(t, err)
	require.Nil(t, expected)
}

func TestBuildSchemaConfig_AddSchema_NoStatuses(t *testing.T) {
	spec := lokiv1.ObjectStorageSpec{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
		},
	}
	status := lokiv1.LokiStackStorageStatus{}

	actual, err := BuildSchemaConfig(time.Now().UTC(), spec, status)
	expected := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestBuildSchemaConfig_AddSchema_WithStatuses_WithValidDate(t *testing.T) {
	utcTime := time.Date(2021, 9, 1, 0, 0, 0, 0, time.UTC)
	spec := lokiv1.ObjectStorageSpec{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
			{
				Version:       lokiv1.ObjectStorageSchemaV12,
				EffectiveDate: "2021-10-01",
			},
		},
	}
	status := lokiv1.LokiStackStorageStatus{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
		},
	}

	actual, err := BuildSchemaConfig(utcTime, spec, status)
	expected := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestBuildSchemaConfig_AddSchema_WithStatuses_WithInvalidDate(t *testing.T) {
	utcTime := time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC)
	updateWindow := utcTime.Add(lokiv1.StorageSchemaUpdateBuffer).Format(lokiv1.StorageSchemaEffectiveDateFormat)
	spec := lokiv1.ObjectStorageSpec{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
			{
				Version:       lokiv1.ObjectStorageSchemaV12,
				EffectiveDate: lokiv1.StorageSchemaEffectiveDate(updateWindow),
			},
		},
	}
	status := lokiv1.LokiStackStorageStatus{
		Schemas: []lokiv1.ObjectStorageSchema{
			{
				Version:       lokiv1.ObjectStorageSchemaV11,
				EffectiveDate: "2020-10-01",
			},
		},
	}

	expected, err := BuildSchemaConfig(utcTime, spec, status)

	require.Error(t, err)
	require.Nil(t, expected)
}

func TestBuildSchemas(t *testing.T) {
	schemas := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-12-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-02-01",
		},
	}

	expected := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
	}
	actual := buildSchemas(schemas)

	require.Equal(t, expected, actual)
}

func TestReduceSortedSchemas(t *testing.T) {
	schemas := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-02-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-12-01",
		},
	}

	expected := []lokiv1.ObjectStorageSchema{
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
	}
	actual := reduceSortedSchemas(schemas)

	require.Equal(t, expected, actual)
}
