// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Initially copied from Thanos
//
// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package logerrcapture

import (
	"fmt"
	"io"
	"os"

	"github.com/efficientgo/core/errors"
)

// Logger interface compatible with go-kit/logger.
type Logger interface {
	Log(keyvals ...interface{}) error
}

type doFunc func() error

// Do is making sure we log every error, even those from best effort tiny functions.
func Do(logger Logger, doer doFunc, format string, a ...interface{}) {
	derr := doer()
	if derr == nil {
		return
	}

	// For os closers, it's a common case to double close. From reliability purpose this is not a problem it may only indicate
	// surprising execution path.
	if errors.Is(derr, os.ErrClosed) {
		return
	}

	_ = logger.Log("msg", "detected do error", "err", errors.Wrap(derr, fmt.Sprintf(format, a...)))
}

// ExhaustClose closes the io.ReadCloser with a log message on error but exhausts the reader before.
func ExhaustClose(logger Logger, r io.ReadCloser, format string, a ...interface{}) {
	_, err := io.Copy(io.Discard, r)
	if err != nil {
		_ = logger.Log("msg", "failed to exhaust reader, performance may be impeded", "err", err)
	}

	Do(logger, r.Close, format, a...)
}
