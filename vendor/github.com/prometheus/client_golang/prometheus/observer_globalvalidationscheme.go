// Copyright 2025 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !localvalidationscheme

package prometheus

// ExemplarObserver is implemented by Observers that offer the option of
// observing a value together with an exemplar. Its ObserveWithExemplar method
// works like the Observe method of an Observer but also replaces the currently
// saved exemplar (if any) with a new one, created from the provided value, the
// current time as timestamp, and the provided Labels. Empty Labels will lead to
// a valid (label-less) exemplar. But if Labels is nil, the current exemplar is
// left in place. ObserveWithExemplar panics if any of the provided labels are
// invalid or if the provided labels contain more than 128 runes in total.
type ExemplarObserver interface {
	ObserveWithExemplar(value float64, exemplar Labels)
}
