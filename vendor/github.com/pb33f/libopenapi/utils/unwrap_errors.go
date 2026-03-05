// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package utils

func UnwrapErrors(err error) []error {
	if err == nil {
		return []error{}
	}
	if uw, ok := err.(interface{ Unwrap() []error }); ok {
		return uw.Unwrap()
	} else {
		return []error{err}
	}
}
