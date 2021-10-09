// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metadata

import (
	"io"
	"net/http"
	"time"

	"github.com/googleapis/gax-go/v2"
)

const (
	maxRetryAttempts = 5
)

var (
	syscallRetryable = func(err error) bool { return false }
)

func newRetryer() *metadataRetryer {
	return &metadataRetryer{bo: &gax.Backoff{Initial: 100 * time.Millisecond}}
}

type backoff interface {
	Pause() time.Duration
}

type metadataRetryer struct {
	bo       backoff
	attempts int
}

func (r *metadataRetryer) Retry(status int, err error) (time.Duration, bool) {
	if status == http.StatusOK {
		return 0, false
	}
	retryOk := shouldRetry(status, err)
	if !retryOk {
		return 0, false
	}
	if r.attempts == maxRetryAttempts {
		return 0, false
	}
	r.attempts++
	return r.bo.Pause(), true
}

func shouldRetry(status int, err error) bool {
	if 500 <= status && status <= 599 {
		return true
	}
	if err == io.ErrUnexpectedEOF {
		return true
	}
	// Transient network errors should be retried.
	if syscallRetryable(err) {
		return true
	}
	if err, ok := err.(interface{ Temporary() bool }); ok {
		if err.Temporary() {
			return true
		}
	}
	if err, ok := err.(interface{ Unwrap() error }); ok {
		return shouldRetry(status, err.Unwrap())
	}
	return false
}
