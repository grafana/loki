package maxminddb

import "github.com/oschwald/maxminddb-golang/v2/internal/mmdberrors"

type (
	// InvalidDatabaseError is returned when database data is malformed or is
	// rejected by decoder structural, resource, or schema validation.
	InvalidDatabaseError = mmdberrors.InvalidDatabaseError

	// UnmarshalTypeError is returned when the value in the database cannot be
	// assigned to the specified data type.
	UnmarshalTypeError = mmdberrors.UnmarshalTypeError
)
