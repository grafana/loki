package storage

import (
	"sort"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
)

const (
	// DateTimeFormat is the datetime string need to format the time.
	DateTimeFormat = "2006-01-02"

	// UpdateDelay is the time before a change to the storage schema
	// is applied to a cluster. This will avoid scenarios in which
	// the difference between UTC and local time zones will cause
	// logs to be unavailable.
	UpdateDelay = time.Hour * 24
)

type schemaConfig struct {
	version       lokiv1beta1.ObjectStorageSchemaVersion
	effectiveDate time.Time
}

// BuildSchemaConfig creates a list of schemas to be used to configure
// the storage schemas for the cluster. This method assumes that the following
// validation has been done to the statuses and specs:
//
// 1. All EffectiveDate fields are able to be parsed
// 2. All EffectiveDate fields are unique in their respective list
//
func BuildSchemaConfig(
	utcTime time.Time,
	specs []lokiv1beta1.ObjectStorageSchemaSpec,
	statuses []lokiv1beta1.StorageSchemaStatus,
) ([]lokiv1beta1.ObjectStorageSchemaSpec, error) {
	if len(specs) == 0 {
		return nil, kverrors.New("Spec does not contain any schemas.")
	}

	specSchemas := schemaConfigFromSpec(specs)

	if len(statuses) != 0 {
		statusSchemas := schemaConfigFromStatus(statuses)

		// Schemas applied in the future can be ignored cause they have not
		// taken effect yet. The delay is added to avoid edge cases where UTC
		// and local time zones may cause interference.
		//
		// For example, say there is a future change which will occur on 2022-06-02.
		// If change occurs close to midnight on 2022-06-01, there might be timing
		// issues between the local and UTC. Thus, there is a chance of corruption.
		// To avoid this, the change on 2022-06-02 should be considered as already
		// applied and an error should be sent back to change the date of the
		// in-flight change.
		cutoff := utcTime.Add(UpdateDelay)
		applied := filterAppliedSchemas(statusSchemas, cutoff)
		if !containsSchemas(specSchemas, applied) {
			return nil, kverrors.New("Cannot retroactively remove previously applied schemas")
		}

		unapplied := filterUnappliedSchemas(specSchemas, cutoff)
		if len(specSchemas)-len(unapplied) != len(applied) {
			return nil, kverrors.New("Cannot retroactively add schemas.")
		}
	}

	sort.SliceStable(specSchemas, func(i, j int) bool {
		return specSchemas[i].effectiveDate.Before(specSchemas[j].effectiveDate)
	})

	reducedSpecSchemas := reduceSortedSchemas(specSchemas)
	return buildSpecs(reducedSpecSchemas), nil
}

// schemaConfigFromStatus creates a list of schemaConfigs based on the given statuses
func schemaConfigFromStatus(statuses []lokiv1beta1.StorageSchemaStatus) []schemaConfig {
	configs := make([]schemaConfig, len(statuses))

	for i, status := range statuses {
		date, _ := time.Parse(DateTimeFormat, status.EffectiveDate)

		configs[i] = schemaConfig{
			version:       status.Version,
			effectiveDate: date,
		}
	}

	return configs
}

// schemaConfigFromSpec creates a list of schemaConfigs based on the given specs
func schemaConfigFromSpec(specs []lokiv1beta1.ObjectStorageSchemaSpec) []schemaConfig {
	configs := make([]schemaConfig, len(specs))

	for i, spec := range specs {
		date, _ := time.Parse(DateTimeFormat, string(spec.EffectiveDate))

		configs[i] = schemaConfig{
			version:       spec.Version,
			effectiveDate: date,
		}
	}

	return configs
}

// buildSpecs creates a list of ObjectStorageSchemaSpecs based on the given configs
func buildSpecs(configs []schemaConfig) []lokiv1beta1.ObjectStorageSchemaSpec {
	specs := make([]lokiv1beta1.ObjectStorageSchemaSpec, len(configs))

	for i, config := range configs {
		date := config.effectiveDate.Format(DateTimeFormat)

		specs[i] = lokiv1beta1.ObjectStorageSchemaSpec{
			Version:       config.version,
			EffectiveDate: lokiv1beta1.StorageSchemaEffectiveDate(date),
		}
	}

	return specs
}

// isApplied returns whether a schemaConfig has been applied or not
func isApplied(config schemaConfig, effectiveDate time.Time) bool {
	year, month, day := config.effectiveDate.Date()
	utcTime := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)

	return config.effectiveDate.Before(effectiveDate) || utcTime.Equal(effectiveDate)
}

// filterAppliedSchemas returns a list of configs that have been applied.
func filterAppliedSchemas(configs []schemaConfig, cutoff time.Time) []schemaConfig {
	applied := []schemaConfig{}

	for _, config := range configs {
		if isApplied(config, cutoff) {
			applied = append(applied, config)
		}
	}

	return applied
}

// filterUnappliedSchemas returns a list of configs that have not been applied.
func filterUnappliedSchemas(configs []schemaConfig, cutoff time.Time) []schemaConfig {
	unapplied := []schemaConfig{}

	for _, config := range configs {
		if !isApplied(config, cutoff) {
			unapplied = append(unapplied, config)
		}
	}

	return unapplied
}

// containsSchemas returns a boolean indicating if a list of configs
// has all the elements in another list.
func containsSchemas(configs []schemaConfig, subset []schemaConfig) bool {
	for _, element := range subset {
		contains := false

		for _, config := range configs {
			areVersionsEqual := config.version == element.version
			areDatesEqual := config.effectiveDate.Equal(element.effectiveDate)

			if areVersionsEqual && areDatesEqual {
				contains = true
			}
		}

		if !contains {
			return false
		}
	}

	return true
}

// reduceSortedSchemas returns a list of configs that have removed redundant entries.
func reduceSortedSchemas(configs []schemaConfig) []schemaConfig {
	if len(configs) == 0 {
		return nil
	}

	lastIndex := 0
	reduced := []schemaConfig{configs[0]}

	for _, config := range configs {
		if reduced[lastIndex].version != config.version {
			lastIndex++
			reduced = append(reduced, config)
		}
	}

	return reduced
}
