package storage

import (
	"time"

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

// UpdateSchemas returns a list of schemas that can be used by Loki. It will
// append a new schema if there are no existing, the last schema is not the
// latest, or the last schema is not using the right object storage.
func UpdateSchemas(
	utcTime time.Time,
	version lokiv1beta1.ObjectStorageSchemaVersion,
	statuses []lokiv1beta1.StorageSchemaStatus,
) []Schema {
	count := len(statuses)

	if count == 0 {
		// Loki Operator was first released with this schema (v11, 2020-10-01).
		// Since the default is v11, old clusters will configure this fine. New
		// clusters will either install with v11 or v12 based on the crd.
		return []Schema{
			{
				From:    "2020-10-01",
				Version: version,
			},
		}
	}

	mutableSchemas := make([]Schema, 1)
	staticSchemas := make([]Schema, count-1)

	for i, status := range statuses {
		schema := Schema{
			From:    status.DateApplied,
			Version: status.Version,
		}

		if i != count-1 {
			staticSchemas[i] = schema
		} else {
			// The most recent addition to the list is not
			// guaranteed to be usable (in the case of multiple
			// same day changes). Generally outside of this, the
			// last change made will be added back to the list.
			mutableSchemas[0] = schema
		}
	}

	// No changes required. Return the converted list exactly as is.
	if mutableSchemas[0].Version == version {
		return append(staticSchemas, mutableSchemas...)
	}

	// Apply the changes to the index for logs starting two days later.
	// Since the timestamps work by UTC, the goal is to avoid the scenario
	// in which logs might be inaccessible because they were created with the
	// wrong schema.
	newSchema := Schema{
		From:    utcTime.Add(UpdateDelay).Format(DateTimeFormat),
		Version: version,
	}

	// There is a pending update that has not been applied yet.
	// In order to avoid possible timing scenarios, updates are only
	// changed if they occur on the same day. If an update is a day
	// beforehand, the update in progress is unchanged.
	//
	// Example:
	//
	// 2022-05-05: Accept and set update to v12 (2022-05-07)
	// 2022-05-06: Accept and set update to v11 (2022-05-08)
	// 2022-05-07: v12 Update will be applied
	// 2022-05-08: v11 Update will be applied
	//
	// Ideally, the v12 update could be modified as long the change
	// occurred before 2022-05-07. However making the change within a day
	// of the update, might cause trouble. Therefore, the changes are
	// avoided.
	if mutableSchemas[0].From == newSchema.From {
		i := count - 2
		if i >= 0 && staticSchemas[i].Version == version {
			return staticSchemas
		}
		return append(staticSchemas, newSchema)
	}

	mutableSchemas = append(mutableSchemas, newSchema)
	return append(staticSchemas, mutableSchemas...)
}
