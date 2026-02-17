package maxminddb

import "github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"

type (
	// InvalidDatabaseError is returned when the database contains invalid data
	// and cannot be parsed.
	InvalidDatabaseError = mmdberrors.InvalidDatabaseError

	// UnmarshalTypeError is returned when the value in the database cannot be
	// assigned to the specified data type.
	UnmarshalTypeError = mmdberrors.UnmarshalTypeError
)
