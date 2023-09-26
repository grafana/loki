// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

// Initially copied from Thanos and contributed by https://github.com/bisakhmondal.
//
// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package errors provides basic utilities to manipulate errors with a useful stacktrace. It combines the
// benefits of errors.New and fmt.Errorf world into a single package. It is an improved and minimal version of the popular
// https://github.com/pkg/errors (archived) package allowing reliable wrapping of errors with stacktrace. Unfortunately,
// the standard library recommended error wrapping using `%+w` is prone to errors and does not support stacktraces.
package errors
