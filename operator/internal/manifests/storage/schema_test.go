package storage

import (
	"testing"
	"time"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

	"github.com/stretchr/testify/require"
)

func TestBuildSchemaConfig_NoSchemas(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchema{}
	statuses := []lokiv1beta1.ObjectStorageSchema{}

	expected, err := BuildSchemaConfig(time.Now().UTC(), specs, statuses)

	require.Error(t, err)
	require.Nil(t, expected)
}

func TestBuildSchemaConfig_AddSchema_NoStatuses(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.ObjectStorageSchema{}

	actual, err := BuildSchemaConfig(time.Now().UTC(), specs, statuses)
	expected := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestBuildSchemaConfig_AddSchema_WithStatuses_WithValidDate(t *testing.T) {
	utcTime := time.Date(2021, 9, 1, 0, 0, 0, 0, time.UTC)
	specs := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}
	statuses := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	actual, err := BuildSchemaConfig(utcTime, specs, statuses)
	expected := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestBuildSchemaConfig_AddSchema_WithStatuses_WithInvalidDate(t *testing.T) {
	utcTime := time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC)
	updateWindow := utcTime.Add(updateBuffer).Format(lokiv1beta1.StorageSchemaEffectiveDateFormat)
	specs := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateWindow),
		},
	}
	statuses := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	expected, err := BuildSchemaConfig(utcTime, specs, statuses)

	require.Error(t, err)
	require.Nil(t, expected)
}

func TestBuildSchemas(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-12-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-02-01",
		},
	}

	expected := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
	}
	actual := buildSchemas(schemas)

	require.Equal(t, expected, actual)
}

func TestBuildAppliedSchemaMap(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-12-01",
		},
	}

	actual := buildAppliedSchemaMap(schemas, time.Date(2021, 11, 1, 12, 0, 0, 0, time.UTC))
	expected := schemaMap{
		"2021-06-01": lokiv1beta1.ObjectStorageSchemaV12,
		"2021-10-01": lokiv1beta1.ObjectStorageSchemaV11,
		"2021-11-01": lokiv1beta1.ObjectStorageSchemaV12,
	}

	require.Equal(t, expected, actual)
}

func TestValidate_RetroactivelyAddSchema(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-05-01",
		},
	}
	applied := schemaMap{
		"2020-10-01": lokiv1beta1.ObjectStorageSchemaV11,
		"2021-10-01": lokiv1beta1.ObjectStorageSchemaV12,
	}

	err := validate(schemas, applied, time.Now().UTC())
	require.Error(t, err)
}

func TestValidate_RetroactivelyAddSchema_ConflictingVersion(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
	}
	applied := schemaMap{
		"2020-10-01": lokiv1beta1.ObjectStorageSchemaV11,
		"2021-10-01": lokiv1beta1.ObjectStorageSchemaV12,
	}

	err := validate(schemas, applied, time.Now().UTC())
	require.Error(t, err)
}

func TestValidate_RetroactivelyRemoveSchema(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	applied := schemaMap{
		"2020-10-01": lokiv1beta1.ObjectStorageSchemaV11,
		"2021-10-01": lokiv1beta1.ObjectStorageSchemaV12,
	}

	err := validate(schemas, applied, time.Now().UTC())
	require.Error(t, err)
}

func TestReduceSortedSchemas(t *testing.T) {
	schemas := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-02-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-12-01",
		},
	}

	expected := []lokiv1beta1.ObjectStorageSchema{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-11-01",
		},
	}
	actual := reduceSortedSchemas(schemas)

	require.Equal(t, expected, actual)
}
