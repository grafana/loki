// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scrapererror // import "go.opentelemetry.io/collector/receiver/scrapererror"

import "errors"

// PartialScrapeError is an error to represent
// that a subset of metrics were failed to be scraped.
type PartialScrapeError struct {
	error
	Failed int
}

// NewPartialScrapeError creates PartialScrapeError for failed metrics.
// Use this error type only when a subset of data was failed to be scraped.
func NewPartialScrapeError(err error, failed int) PartialScrapeError {
	return PartialScrapeError{
		error:  err,
		Failed: failed,
	}
}

// IsPartialScrapeError checks if an error was wrapped with PartialScrapeError.
func IsPartialScrapeError(err error) bool {
	if err == nil {
		return false
	}

	var partialScrapeErr PartialScrapeError
	return errors.As(err, &partialScrapeErr)
}
