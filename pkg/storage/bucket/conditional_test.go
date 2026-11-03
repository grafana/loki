package bucket

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/minio/minio-go/v7"
	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"
)

func TestIsConditionalWriteConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "returns false for a nil error",
			err:  nil,
			want: false,
		},
		{
			name: "returns true for an S3 PreconditionFailed error wrapped by the S3 bucket",
			err:  pkgerrors.Wrap(minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: http.StatusPreconditionFailed}, "upload s3 object"),
			want: true,
		},
		{
			name: "returns true for an S3 ConditionalRequestConflict error",
			err:  minio.ErrorResponse{Code: "ConditionalRequestConflict", StatusCode: http.StatusConflict},
			want: true,
		},
		{
			name: "returns true for an S3 error with status 412 and another code",
			err:  minio.ErrorResponse{Code: "Unknown", StatusCode: http.StatusPreconditionFailed},
			want: true,
		},
		{
			name: "returns false for an S3 error of another kind",
			err:  minio.ErrorResponse{Code: "SlowDown", StatusCode: http.StatusServiceUnavailable},
			want: false,
		},
		{
			name: "returns true for a GCS 412 error wrapped with fmt.Errorf",
			err:  fmt.Errorf("upload: %w", &googleapi.Error{Code: http.StatusPreconditionFailed}),
			want: true,
		},
		{
			name: "returns false for a GCS error of another kind",
			err:  &googleapi.Error{Code: http.StatusTooManyRequests},
			want: false,
		},
		{
			name: "returns false for an error of no object store",
			err:  errors.New("context deadline exceeded"),
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsConditionalWriteConflict(tc.err))
		})
	}
}
