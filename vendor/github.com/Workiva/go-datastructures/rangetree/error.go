/*
Copyright 2014 Workiva, LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rangetree

import "fmt"

// NoEntriesError is returned from an operation that requires
// existing entries when none are found.
type NoEntriesError struct{}

func (nee NoEntriesError) Error() string {
	return `No entries in this tree.`
}

// OutOfDimensionError is returned when a requested operation
// doesn't meet dimensional requirements.
type OutOfDimensionError struct {
	provided, max uint64
}

func (oode OutOfDimensionError) Error() string {
	return fmt.Sprintf(`Provided dimension: %d is 
		greater than max dimension: %d`,
		oode.provided, oode.max,
	)
}
