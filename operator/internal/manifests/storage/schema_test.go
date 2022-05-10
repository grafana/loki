package storage

import (
	"testing"
	"time"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

	"github.com/stretchr/testify/require"
)

func TestUpdateSchema_AddNewSchema(t *testing.T) {
	current := time.Now().UTC()

	expected := []Schema{
		{
			From:    "2020-10-01",
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []lokiv1beta1.StorageSchemaStatus{}

	actual := UpdateSchemas(current, lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_AppendNewSchema(t *testing.T) {
	current := time.Now().UTC()

	expected := []Schema{
		{
			From:    current.Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
		{
			From:    current.Add(UpdateDelay).Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	test := []lokiv1beta1.StorageSchemaStatus{
		{
			DateApplied: current.Format(DateTimeFormat),
			Version:     lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	actual := UpdateSchemas(current, lokiv1beta1.ObjectStorageSchemaV11, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_IgnoreSameSchema(t *testing.T) {
	current := time.Now().UTC()

	expected := []Schema{
		{
			From:    current.Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []lokiv1beta1.StorageSchemaStatus{
		{
			DateApplied: current.Format(DateTimeFormat),
			Version:     lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	actual := UpdateSchemas(current, lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_ModifyUnappliedSchema(t *testing.T) {
	// This scenario can only be tested with 3+ version in play.
	// Otherwise, this test will just reduce the list. So, this
	// "v10" is used as a placeholder for an eventual supported
	// third version.
	var testVersion lokiv1beta1.ObjectStorageSchemaVersion = "v10"

	current := time.Now().UTC()

	expected := []Schema{
		{
			From:    current.Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
		{
			From:    current.Add(UpdateDelay).Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []lokiv1beta1.StorageSchemaStatus{
		{
			DateApplied: current.Format(DateTimeFormat),
			Version:     lokiv1beta1.ObjectStorageSchemaV11,
		},
		{
			DateApplied: current.Add(UpdateDelay).Format(DateTimeFormat),
			Version:     testVersion,
		},
	}

	actual := UpdateSchemas(current, lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_ModifyUnappliedSchema_ReduceSameVersion(t *testing.T) {
	current := time.Now().UTC()

	expected := []Schema{
		{
			From:    current.Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []lokiv1beta1.StorageSchemaStatus{
		{
			DateApplied: current.Format(DateTimeFormat),
			Version:     lokiv1beta1.ObjectStorageSchemaV12,
		},
		{
			DateApplied: current.Add(UpdateDelay).Format(DateTimeFormat),
			Version:     lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	actual := UpdateSchemas(current, lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}
