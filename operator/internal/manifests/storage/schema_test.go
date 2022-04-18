package storage

import (
	"testing"
	"time"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

	"github.com/stretchr/testify/require"
)

func TestUpdateSchema_AddNewSchema(t *testing.T) {
	expected := []Schema{
		{
			From:    time.Now().Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	actual := UpdateSchemas(lokiv1beta1.ObjectStorageSchemaV12, []Schema{})

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_AppendNewSchema(t *testing.T) {
	expected := []Schema{
		{
			From:    DateTimeFormat,
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
		{
			From:    time.Now().Add(UpdateDelay).Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []Schema{
		{
			From:    DateTimeFormat,
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	actual := UpdateSchemas(lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_IgnoreSameSchema(t *testing.T) {
	expected := []Schema{
		{
			From:    DateTimeFormat,
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	test := []Schema{
		{
			From:    DateTimeFormat,
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	actual := UpdateSchemas(lokiv1beta1.ObjectStorageSchemaV11, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_ModifyUnappliedSchema(t *testing.T) {
	var testVersion lokiv1beta1.ObjectStorageSchemaVersion = "v10"

	from := time.Now().Add(UpdateDelay).Format(DateTimeFormat)

	expected := []Schema{
		{
			From:    time.Now().Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
		{
			From:    from,
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []Schema{
		{
			From:    time.Now().Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
		{
			From:    from,
			Version: testVersion,
		},
	}

	actual := UpdateSchemas(lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}

func TestUpdateSchema_ModifyUnappliedSchema_ReduceSameVersion(t *testing.T) {
	from := time.Now().Add(UpdateDelay).Format(DateTimeFormat)

	expected := []Schema{
		{
			From:    time.Now().Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
	}

	test := []Schema{
		{
			From:    time.Now().Format(DateTimeFormat),
			Version: lokiv1beta1.ObjectStorageSchemaV12,
		},
		{
			From:    from,
			Version: lokiv1beta1.ObjectStorageSchemaV11,
		},
	}

	actual := UpdateSchemas(lokiv1beta1.ObjectStorageSchemaV12, test)

	require.Len(t, actual, len(expected))
	for i := range expected {
		require.Equal(t, expected[i].From, actual[i].From)
		require.Equal(t, expected[i].Version, actual[i].Version)
	}
}
