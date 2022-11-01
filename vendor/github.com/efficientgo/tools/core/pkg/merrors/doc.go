// Copyright (c) The EfficientGo Authors.
// Licensed under the Apache License 2.0.

package merrors

// Safe multi error implementation that chains errors on the same level. Supports errors.As and errors.Is functions.
//
// Example 1:
//
//  return merrors.New(err1, err2).Err()
//
// Example 2:
//
//  merr := merrors.New(err1)
//  merr.Add(err2, errOrNil3)
//  for _, err := range errs {
//    merr.Add(err)
//  }
//  return merr.Err()
//
