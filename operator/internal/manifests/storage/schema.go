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
func UpdateSchemas(version lokiv1beta1.ObjectStorageSchemaVersion, existing []Schema) []Schema {
	count := len(existing)
	date := time.Now().UTC()

	if count == 0 {
		return []Schema{
			{
				From:    date.Format(DateTimeFormat),
				Version: version,
			},
		}
	}

	last := existing[count-1]

	// Apply the changes to the index for logs starting two days later.
	// Since the timestamps work by UTC, the goal is to avoid the scenario
	// in which logs might be inaccessible because they were created with the
	// wrong schema.
	nextAppliedDate := date.Add(UpdateDelay).Format(DateTimeFormat)

	if last.Version != version {
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
		if last.From == nextAppliedDate {
			schemas := existing[0 : count-1]
			if schemas[len(schemas)-1].Version == version {
				return schemas
			}

			return append(schemas, Schema{
				From:    nextAppliedDate,
				Version: version,
			})
		}

		return append(existing, Schema{
			From:    nextAppliedDate,
			Version: version,
		})
	}

	return existing
}
