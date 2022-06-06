package storage

import (
	"testing"
	"time"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"

	"github.com/stretchr/testify/require"
)

func BuildSchemaConfigList_NoSchemas(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{}
	statuses := []lokiv1beta1.StorageSchemaStatus{}

	_, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)

	require.Error(t, err)
}

func BuildSchemaConfigList_AddSchema_NoStatuses(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{}

	actual, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)
	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func BuildSchemaConfigList_AddSchema_WithPriorSchemas_OutsideUpdateWindow(t *testing.T) {
	utcTime := time.Date(2021, 9, 1, 0, 0, 0, 0, time.UTC)
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	actual, err := BuildSchemaConfigList(utcTime, specs, statuses)
	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
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

func BuildSchemaConfigList_AddSchema_WithPriorSchemas_InsideUpdateWindow(t *testing.T) {
	utcTime := time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC)
	updateWindow := utcTime.Add(UpdateDelay).Add(time.Hour * -1).Format(DateTimeFormat)
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(updateWindow),
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
	}

	_, err := BuildSchemaConfigList(utcTime, specs, statuses)

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

func BuildSchemaConfigList_SortSchema_ChronologicalOrder(t *testing.T) {
	specs := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-01-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
	}
	statuses := []lokiv1beta1.StorageSchemaStatus{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-01-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	actual, err := BuildSchemaConfigList(time.Now().UTC(), specs, statuses)
	expected := []lokiv1beta1.ObjectStorageSchemaSpec{
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV11,
			EffectiveDate: "2020-10-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-01-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-06-01",
		},
		{
			Version:       lokiv1beta1.ObjectStorageSchemaV12,
			EffectiveDate: "2021-10-01",
		},
	}

	require.NoError(t, err)
	require.Equal(t, expected, actual)
}

func TestIsApplied(t *testing.T) {
	type test struct {
		desc          string
		schema        schemaConfig
		effectiveDate time.Time
		want          bool
	}

	testDate := time.Date(2020, 10, 2, 0, 0, 0, 0, time.UTC)

	tests := []test{
		{
			desc: "day before effective date",
			schema: schemaConfig{
				version:       lokiv1beta1.ObjectStorageSchemaV11,
				effectiveDate: testDate,
			},
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
			want:          false,
		},
		{
			desc: "day of effective date",
			schema: schemaConfig{
				version:       lokiv1beta1.ObjectStorageSchemaV11,
				effectiveDate: testDate,
			},
			effectiveDate: time.Date(2020, 10, 2, 0, 0, 0, 0, time.UTC),
			want:          true,
		},
		{
			desc: "day after effective date",
			schema: schemaConfig{
				version:       lokiv1beta1.ObjectStorageSchemaV11,
				effectiveDate: testDate,
			},
			effectiveDate: time.Date(2020, 10, 3, 0, 0, 0, 0, time.UTC),
			want:          true,
		},
	}

	for _, tst := range tests {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			res := isApplied(tst.schema, tst.effectiveDate)
			require.Equal(t, tst.want, res)
		})
	}
}

func TestFilterAppliedSchemas(t *testing.T) {
	cutoff := time.Date(2021, 6, 1, 5, 30, 15, 0, time.UTC)
	schemas := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	expected := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	actual := filterAppliedSchemas(schemas, cutoff)

	require.Equal(t, expected, actual)
}

func TestFilterUnappliedSchemas(t *testing.T) {
	cutoff := time.Date(2021, 8, 1, 0, 0, 0, 0, time.UTC)
	schemas := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	expected := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	actual := filterUnappliedSchemas(schemas, cutoff)

	require.Equal(t, expected, actual)
}

func TestContainsSchemas(t *testing.T) {
	type test struct {
		desc    string
		schemas []schemaConfig
		subset  []schemaConfig
		want    bool
	}

	tests := []test{
		{
			desc: "contains subset",
			schemas: []schemaConfig{
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			subset: []schemaConfig{
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			want: true,
		},
		{
			desc: "doesn't contains subset",
			schemas: []schemaConfig{
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV12,
					effectiveDate: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			subset: []schemaConfig{
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2020, 11, 1, 0, 0, 0, 0, time.UTC),
				},
				{
					version:       lokiv1beta1.ObjectStorageSchemaV11,
					effectiveDate: time.Date(2021, 5, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			want: false,
		},
	}

	for _, tst := range tests {
		tst := tst
		t.Run(tst.desc, func(t *testing.T) {
			t.Parallel()

			res := containsSchemas(tst.schemas, tst.subset)
			require.Equal(t, tst.want, res)
		})
	}
}

func TestReduceSortedSchemas(t *testing.T) {
	schemas := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV12,
			effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV12,
			effectiveDate: time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV12,
			effectiveDate: time.Date(2021, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	expected := []schemaConfig{
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2020, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV12,
			effectiveDate: time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV11,
			effectiveDate: time.Date(2021, 10, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			version:       lokiv1beta1.ObjectStorageSchemaV12,
			effectiveDate: time.Date(2021, 11, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	actual := reduceSortedSchemas(schemas)

	require.Equal(t, expected, actual)
}
