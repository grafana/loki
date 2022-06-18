package storage

import (
	"sort"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
)

// BuildSchemaConfig creates a list of schemas to be used to configure
// the storage schemas for the cluster. This method assumes that the following
// validation has been done to the statuses and specs:
//
// 1. All EffectiveDate fields are able to be parsed
// 2. All EffectiveDate fields are unique in their respective list
//
func BuildSchemaConfig(
	utcTime time.Time,
	spec lokiv1beta1.ObjectStorageSpec,
	status lokiv1beta1.LokiStackStorageStatus,
) ([]lokiv1beta1.ObjectStorageSchema, error) {
	if len(spec.Schemas) == 0 {
		return nil, kverrors.New("spec does not contain any schemas")
	}

	errors := spec.ValidateSchemas(utcTime, status)
	if len(errors) != 0 {
		return nil, kverrors.Wrap(errors[0], "spec contains invalid schema entry")
	}

	schemas := buildSchemas(spec.Schemas)

	return schemas, nil
}

// buildSchemas creates a sorted and reduced list of schemaConfigs
func buildSchemas(schemas []lokiv1beta1.ObjectStorageSchema) []lokiv1beta1.ObjectStorageSchema {
	sortedSchemas := make([]lokiv1beta1.ObjectStorageSchema, len(schemas))
	copy(sortedSchemas, schemas)

	sort.SliceStable(sortedSchemas, func(i, j int) bool {
		iDate, _ := sortedSchemas[i].EffectiveDate.UTCTime()
		jDate, _ := sortedSchemas[j].EffectiveDate.UTCTime()

		return iDate.Before(jDate)
	})

	return reduceSortedSchemas(sortedSchemas)
}

// reduceSortedSchemas returns a list of schemas that have removed redundant entries.
func reduceSortedSchemas(schemas []lokiv1beta1.ObjectStorageSchema) []lokiv1beta1.ObjectStorageSchema {
	version := ""
	reduced := []lokiv1beta1.ObjectStorageSchema{}

	for _, schema := range schemas {
		strSchemaVersion := string(schema.Version)

		if version != strSchemaVersion {
			version = strSchemaVersion
			reduced = append(reduced, schema)
		}
	}

	return reduced
}
