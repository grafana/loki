package runutil

import (
	"fmt"
	"io"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	"github.com/grafana/dskit/multierror"
)

// CloseWithErrCapture closes closer and wraps any error with the provided message and assigns it to err.
func CloseWithErrCapture(err *error, closer io.Closer, msg string) {
	merr := multierror.New(*err, errors.Wrap(closer.Close(), msg))
	*err = merr.Err()
}

// CloseWithLogOnErr closes an io.Closer and logs any relevant error from it wrapped with the provided format string and
// args.
func CloseWithLogOnErr(logger log.Logger, closer io.Closer, format string, args ...interface{}) {
	err := closer.Close()
	if err == nil || errors.Is(err, os.ErrClosed) {
		return
	}

	msg := fmt.Sprintf(format, args...)
	level.Warn(logger).Log("msg", "detected close error", "err", fmt.Sprintf("%s: %s", msg, err.Error()))
}
