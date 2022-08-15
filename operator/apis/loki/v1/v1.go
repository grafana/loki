package v1

import (
	"errors"
	"time"
)

// StorageSchemaEffectiveDate defines the type for the Storage Schema Effect Date
//
// +kubebuilder:validation:Pattern:="^([0-9]{4,})([-]([0-9]{2})){2}$"
type StorageSchemaEffectiveDate string

// UTCTime returns the date as a time object in the UTC time zone
func (d StorageSchemaEffectiveDate) UTCTime() (time.Time, error) {
	return time.Parse(StorageSchemaEffectiveDateFormat, string(d))
}

const (
	// StorageSchemaEffectiveDateFormat is the datetime string need to format the time.
	StorageSchemaEffectiveDateFormat = "2006-01-02"
	// StorageSchemaUpdateBuffer is the amount of time used as a buffer to prevent
	// storage schemas from being added too close to midnight in UTC.
	StorageSchemaUpdateBuffer = time.Hour * 2
)

var (
	// ErrEffectiveDatesNotUnique when effective dates are not unique.
	ErrEffectiveDatesNotUnique = errors.New("Effective dates are not unique")
	// ErrParseEffectiveDates when effective dates cannot be parsed.
	ErrParseEffectiveDates = errors.New("Failed to parse effective date")
	// ErrMissingValidStartDate when a schema list is created without a valid effective date
	ErrMissingValidStartDate = errors.New("Schema does not contain a valid starting effective date")
	// ErrSchemaRetroactivelyAdded when a schema has been retroactively added
	ErrSchemaRetroactivelyAdded = errors.New("Cannot retroactively add schema")
	// ErrSchemaRetroactivelyRemoved when a schema or schemas has been retroactively removed
	ErrSchemaRetroactivelyRemoved = errors.New("Cannot retroactively remove schema(s)")
	// ErrSchemaRetroactivelyChanged when a schema has been retroactively changed
	ErrSchemaRetroactivelyChanged = errors.New("Cannot retroactively change schema")
)
