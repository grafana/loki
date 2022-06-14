package storage

import (
	"sort"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
)

const (
	// updateBuffer is the time before a change to the storage schema
	// is applied to a cluster. This is to avoid a midnight hour change,
	// which could cause some logs to be unreadable.
	updateBuffer = time.Hour * 2
)

type schemaMap map[lokiv1beta1.StorageSchemaEffectiveDate]lokiv1beta1.ObjectStorageSchemaVersion

// BuildSchemaConfig creates a list of schemas to be used to configure
// the storage schemas for the cluster. This method assumes that the following
// validation has been done to the statuses and specs:
//
// 1. All EffectiveDate fields are able to be parsed
// 2. All EffectiveDate fields are unique in their respective list
//
func BuildSchemaConfig(
	utcTime time.Time,
	specSchemas, statusSchemas []lokiv1beta1.ObjectStorageSchema,
) ([]lokiv1beta1.ObjectStorageSchema, error) {
	if len(specSchemas) == 0 {
		return nil, kverrors.New("spec does not contain any schemas")
	}

	schemas := buildSchemas(specSchemas)

	if len(statusSchemas) != 0 {
		// Schemas applied in the future can be ignored cause they have not
		// taken effect yet. The delay is added to avoid edge cases where UTC
		// and local time zones may cause interference.
		//
		// For example, say there is a future change which will occur on 2022-06-02.
		// If the change occurs close to midnight on 2022-06-01, there might be timing
		// issues between the local and UTC. Thus, there is a chance of corruption.
		cutoff := utcTime.Add(updateBuffer)

		// Objects from the status cannot be changed from the spec. So,
		// the status schemas are assumed to be the source of truth.
		appliedMap := buildAppliedSchemaMap(statusSchemas, cutoff)

		if err := validate(schemas, appliedMap, cutoff); err != nil {
			return nil, err
		}
	}

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

// buildAppliedSchemaMap creates a map of schemas which occur before the given time
func buildAppliedSchemaMap(schemas []lokiv1beta1.ObjectStorageSchema, effectiveDate time.Time) schemaMap {
	appliedMap := schemaMap{}

	for _, schema := range schemas {
		date, _ := schema.EffectiveDate.UTCTime()
		if date.Before(effectiveDate) {
			appliedMap[schema.EffectiveDate] = schema.Version
		}
	}

	return appliedMap
}

// validate checks a list of schemas to that no retroactive changes have been made
func validate(schemas []lokiv1beta1.ObjectStorageSchema, applied schemaMap, effectiveDate time.Time) error {
	found := map[lokiv1beta1.StorageSchemaEffectiveDate]bool{}

	for key := range applied {
		found[key] = false
	}

	for _, schema := range schemas {
		date, _ := schema.EffectiveDate.UTCTime()

		if date.After(effectiveDate) {
			continue
		}

		appliedSchemaVersion, ok := applied[schema.EffectiveDate]

		if !ok {
			return kverrors.New("cannot retroactively add schemas",
				"date", schema.EffectiveDate,
			)
		}

		if appliedSchemaVersion != schema.Version {
			return kverrors.New("cannot retroactively change schemas",
				"date", schema.EffectiveDate,
				"version", schema.Version,
			)
		}

		found[schema.EffectiveDate] = true
	}

	for key, value := range found {
		if value {
			continue
		}

		// The `found` map is derived from the `applied` map.
		// Both have the same keys. So this lookup should
		// always be successful.
		version := applied[key]

		return kverrors.New("cannot retroactively remove schema",
			"date", key,
			"version", version,
		)
	}

	return nil
}

// reduceSortedSchemas returns a list of schemas that have removed redundant entries.
func reduceSortedSchemas(schemas []lokiv1beta1.ObjectStorageSchema) []lokiv1beta1.ObjectStorageSchema {
	if len(schemas) == 0 {
		return nil
	}

	lastIndex := 0
	reduced := []lokiv1beta1.ObjectStorageSchema{schemas[0]}

	for _, schema := range schemas {
		if reduced[lastIndex].Version != schema.Version {
			lastIndex++
			reduced = append(reduced, schema)
		}
	}

	return reduced
}
