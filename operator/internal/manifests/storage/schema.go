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
	// is applied to a cluster. This should be a minimum of two days.
	// This will avoid scenarios in which the difference between UTC and
	// local time zones will cause logs to be unavailable.
	UpdateDelay = time.Hour * 24 * 2
)

// BuildSchemaConfigList creates a list of schemas to be used to configure
// the storage schemas for the cluster. This method assumes that the following
// validation has been done to the statuses and specs:
//
// 1. All EffectiveDate fields are able to be parsed with a YYYY-MM-DD format
// 2. All EffectiveDate fields are unique in their respective list
func BuildSchemaConfigList(
	utcTime time.Time,
	specs []lokiv1beta1.ObjectStorageSchemaSpec,
	statuses []lokiv1beta1.StorageSchemaStatus,
) ([]lokiv1beta1.ObjectStorageSchemaSpec, error) {

	if len(specs) == 0 {
		return nil, kverrors.New("Empty schema list.")
	}

	// Schemas applied in the future can be ignored cause they have not
	// taken effect yet. The delay is added to avoid edge cases where UTC
	// and local time zones may cause interference.
	//
	// For example, say there is a future change which will occur on 6/02/22.
	// If change occurs close to midnight on 6/01/22, there might be timing
	// issues between the local and UTC. Thus, there is a chance of corruption.
	// To avoid this, the change on 6/02/22 should be considered as already
	// applied and an error should be sent back to change the date of the
	// in-flight change.
	cutoff := utcTime.Add(UpdateDelay)
	applied := filterUnappliedSchemas(statuses, cutoff)

	if !containsAppliedSchemas(specs, applied) {
		return nil, kverrors.New("Spec is missing schemas which have already been applied.")
	}

	// Schemas cannot be retroactively added in the past. Only new changes can
	// be applied to configurations safely.
	unapplied := filterAppliedSchemas(specs, cutoff)
	if len(specs)-len(unapplied) != len(applied) {
		return nil, kverrors.New("Spec contains schemas which cannot be retroactively applied.")
	}

	sort.SliceStable(specs, func(i, j int) bool {
		iDate, _ := time.Parse(DateTimeFormat, specs[i].EffectiveDate)
		jDate, _ := time.Parse(DateTimeFormat, specs[j].EffectiveDate)

		return iDate.Before(jDate)
	})

	return reduceSortedSchemas(specs), nil
}

// filterUnappliedSchemas returns a slice of statuses that have a date which
// occurs before the cutoff time.
func filterUnappliedSchemas(statuses []lokiv1beta1.StorageSchemaStatus, cutoff time.Time) []lokiv1beta1.StorageSchemaStatus {
	applied := []lokiv1beta1.StorageSchemaStatus{}

	for _, status := range statuses {
		date, _ := time.Parse(DateTimeFormat, status.EffectiveDate)

		if date.Before(cutoff) {
			applied = append(applied, status)
		}
	}

	return applied
}

// filterAppliedSchemas returns a slice of specs that have a date which
// occurs after the cutoff time.
func filterAppliedSchemas(specs []lokiv1beta1.ObjectStorageSchemaSpec, cutoff time.Time) []lokiv1beta1.ObjectStorageSchemaSpec {
	unapplied := []lokiv1beta1.StorageSchemaStatus{}

	for _, spec := range specs {
		date, _ := time.Parse(DateTimeFormat, string(spec.EffectiveDate))

		if date.After(cutoff) {
			unapplied = append(unapplied, spec)
		}
	}

	return unapplied
}

// containsAppliedSchemas returns a boolean indicating if a list of given specs
// has all the elements in a list of statuses.
//
// This method assumes that all elements in specs and statuses given have unique
// effective dates.
func containsAppliedSchemas(specs []lokiv1beta1.ObjectStorageSchemaSpec, applied []lokiv1beta1.StorageSchemaStatus) bool {
	schemaMap := map[string]lokiv1beta1.ObjectStorageSchemaSpec{}

	for _, spec := range specs {
		schemaMap[string(spec.EffectiveDate)] = spec
	}

	contains := true

	for _, status := range applied {
		spec, ok := schemaMap[status.EffectiveDate]

		if !ok || (ok && spec.Version != status.Version) {
			contains = false
		}
	}

	return contains
}

// reduceSortedSchemas returns a list of specs that have removed redundant entries.
func reduceSortedSchemas(specs []lokiv1beta1.ObjectStorageSchemaSpec) []lokiv1beta1.ObjectStorageSchemaSpec {
	lastIndex := 0
	reduced := []lokiv1beta1.StorageSchemaStatus{specs[0]}

	for _, spec := range specs {
		if reduced[lastIndex].Version != spec.Version {
			lastIndex++
			reduced = append(reduced, spec)
		}
	}

	return reduced
}
