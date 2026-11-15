package bucket

import (
	"errors"
	"net/http"

	"github.com/minio/minio-go/v7"
	"google.golang.org/api/googleapi"
)

// IsConditionalWriteConflict reports whether err is a conditional write that
// the object store rejected because the object changed after the caller read
// it. The store wrote nothing in that case, so the caller can read the object
// again and retry.
//
// It recognizes the errors of the S3 and GCS clients. Other backends do not
// return such an error: the filesystem bucket locks the file instead, and the
// in-memory bucket does not check the condition.
func IsConditionalWriteConflict(err error) bool {
	if err == nil {
		return false
	}

	if s3Err, ok := errors.AsType[minio.ErrorResponse](err); ok {
		switch s3Err.Code {
		case "PreconditionFailed", "ConditionalRequestConflict":
			return true
		}
		return s3Err.StatusCode == http.StatusPreconditionFailed
	}

	if gcsErr, ok := errors.AsType[*googleapi.Error](err); ok {
		return gcsErr.Code == http.StatusPreconditionFailed
	}

	return false
}
