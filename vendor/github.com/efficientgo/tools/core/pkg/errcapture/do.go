// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Initially copied from Thanos
//
// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package errcapture

import (
	"io"
	"io/ioutil"
	"os"

	"github.com/efficientgo/tools/core/pkg/merrors"
	"github.com/pkg/errors"
)

type doFunc func() error

// Do runs function and on error return error by argument including the given error (usually
// from caller function).
func Do(err *error, doer doFunc, format string, a ...interface{}) {
	derr := doer()
	if err == nil {
		return
	}

	// For os closers, it's a common case to double close. From reliability purpose this is not a problem it may only indicate
	// surprising execution path.
	if errors.Is(derr, os.ErrClosed) {
		return
	}

	*err = merrors.New(*err, errors.Wrapf(derr, format, a...)).Err()
}

// ExhaustClose closes the io.ReadCloser with error capture but exhausts the reader before.
func ExhaustClose(err *error, r io.ReadCloser, format string, a ...interface{}) {
	_, copyErr := io.Copy(ioutil.Discard, r)

	Do(err, r.Close, format, a...)

	// Prepend the io.Copy error.
	*err = merrors.New(copyErr, *err).Err()
}
