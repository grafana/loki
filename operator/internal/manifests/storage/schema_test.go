package storage

import (
	"testing"
	"time"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

	"github.com/stretchr/testify/require"
)

func BuildSchemaConfigList_AddSchema_NoPriorSchemas(t *testing.T) {
	current := time.Now().UTC()
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{}

	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	actual, err := BuildSchemaConfigList(current, specs, statuses)

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func BuildSchemaConfigList_AddSchema_WithPriorSchemas(t *testing.T) {
	current := time.Now().UTC()
	updateTime := current.Add(time.Hour * 24).Format(DateTimeFormat)
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateTime),
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateTime),
		},
	}
	actual, err := BuildSchemaConfigList(current, specs, statuses)

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func BuildSchemaConfigList_AddSchema_RemoveRedundantSchemas(t *testing.T) {
	current := time.Now().UTC()
	updateTime := current.Add(time.Hour * 24).Format(DateTimeFormat)
	futureTime := current.Add(time.Hour * 24 * 7).Format(DateTimeFormat)
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(futureTime),
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateTime),
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateTime),
		},
	}
	actual, err := BuildSchemaConfigList(current, specs, statuses)

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func BuildSchemaConfigList_NoSchemas(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{}
	statuses := []lokiv1beta1.StorageSchemaStatus{}

	_, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)

	require.Error(t, err)
}

func BuildSchemaConfigList_AddSchema_MissingAppliedSchema(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	_, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)

	require.Error(t, err)
}

func BuildSchemaConfigList_AddSchema_RetroactiveSchemaAddition(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2021-05-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	_, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)

	require.Error(t, err)
}
