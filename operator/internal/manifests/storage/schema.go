package storage

import (
	"fmt"
	"sort"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/validation"
)

// DeprecatedSchemaError is returned when the spec contains deprecated schema
// versions which must be migrated before the LokiStack can reconcile.
type DeprecatedSchemaError struct {
	Versions []lokiv1.ObjectStorageSchemaVersion
}

func (e *DeprecatedSchemaError) Error() string {
	return fmt.Sprintf("spec contains deprecated schema versions: %v", e.Versions)
}

// BuildSchemaConfig creates a list of schemas to be used to configure
// the storage schemas for the cluster. This method assumes that the following
// validation has been done to the statuses and specs:
//
// 1. All EffectiveDate fields are able to be parsed
// 2. All EffectiveDate fields are unique in their respective list
func BuildSchemaConfig(
	utcTime time.Time,
	spec lokiv1.ObjectStorageSpec,
	status lokiv1.LokiStackStorageStatus,
	limits *lokiv1.LimitsSpec,
) ([]lokiv1.ObjectStorageSchema, error) {
	if len(spec.Schemas) == 0 {
		return nil, kverrors.New("spec does not contain any schemas")
	}

	// Reject any v11/v12 schemas - only v13 is supported
	deprecated := make([]lokiv1.ObjectStorageSchemaVersion, 0, len(spec.Schemas))
	for _, schema := range spec.Schemas {
		if schema.Version == lokiv1.ObjectStorageSchemaV11 || schema.Version == lokiv1.ObjectStorageSchemaV12 { //nolint:staticcheck
			deprecated = append(deprecated, schema.Version)
		}
	}
	if len(deprecated) > 0 {
		return nil, &DeprecatedSchemaError{Versions: deprecated}
	}

	errors := validation.ValidateSchemas(&spec, utcTime, status, limits)
	if len(errors) != 0 {
		return nil, kverrors.Wrap(errors[0], "spec contains invalid schema entry")
	}

	schemas := buildSchemas(spec.Schemas)

	return schemas, nil
}

// buildSchemas creates a sorted and reduced list of schemaConfigs
func buildSchemas(schemas []lokiv1.ObjectStorageSchema) []lokiv1.ObjectStorageSchema {
	sortedSchemas := make([]lokiv1.ObjectStorageSchema, len(schemas))
	copy(sortedSchemas, schemas)

	sort.SliceStable(sortedSchemas, func(i, j int) bool {
		iDate, _ := sortedSchemas[i].EffectiveDate.UTCTime()
		jDate, _ := sortedSchemas[j].EffectiveDate.UTCTime()

		return iDate.Before(jDate)
	})

	return reduceSortedSchemas(sortedSchemas)
}

// reduceSortedSchemas returns a list of schemas that have removed redundant entries.
func reduceSortedSchemas(schemas []lokiv1.ObjectStorageSchema) []lokiv1.ObjectStorageSchema {
	version := ""
	reduced := []lokiv1.ObjectStorageSchema{}

	for _, schema := range schemas {
		strSchemaVersion := string(schema.Version)

		if version != strSchemaVersion {
			version = strSchemaVersion
			reduced = append(reduced, schema)
		}
	}

	return reduced
}
