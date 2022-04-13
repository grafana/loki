// Copyright The OpenTelemetry Authors
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

package config // import "go.opentelemetry.io/collector/config"

import (
	"context"
)

// MapProvider is an interface that helps to retrieve a config map and watch for any
// changes to the config map. Implementations may load the config from a file,
// a database or any other source.
//
// The typical usage is the following:
//
//		r, err := mapProvider.Retrieve("file:/path/to/config")
//		// Use r.Map; wait for watcher to be called.
//		r.Close()
//		r, err = mapProvider.Retrieve("file:/path/to/config")
//		// Use r.Map; wait for watcher to be called.
//		r.Close()
//		// repeat retrieve/wait/close cycle until it is time to shut down the Collector process.
//		// ...
//		mapProvider.Shutdown()
type MapProvider interface {
	// Retrieve goes to the configuration source and retrieves the selected data which
	// contains the value to be injected in the configuration and the corresponding watcher that
	// will be used to monitor for updates of the retrieved value.
	//
	// `uri` must follow the "<scheme>:<opaque_data>" format. This format is compatible
	// with the URI definition (see https://datatracker.ietf.org/doc/html/rfc3986). The "<scheme>"
	// must be always included in the `uri`. The scheme supported by any provider MUST be at
	// least 2 characters long to avoid conflicting with a driver-letter identifier as specified
	// in https://tools.ietf.org/id/draft-kerwin-file-scheme-07.html#syntax.
	//
	// `watcher` callback is called when the config changes. watcher may be called from
	// a different go routine. After watcher is called Retrieved.Get should be called
	// to get the new config. See description of Retrieved for more details.
	// watcher may be nil, which indicates that the caller is not interested in
	// knowing about the changes.
	//
	// If ctx is cancelled should return immediately with an error.
	// Should never be called concurrently with itself or with Shutdown.
	Retrieve(ctx context.Context, uri string, watcher WatcherFunc) (Retrieved, error)

	// Shutdown signals that the configuration for which this Provider was used to
	// retrieve values is no longer in use and the Provider should close and release
	// any resources that it may have created.
	//
	// This method must be called when the Collector service ends, either in case of
	// success or error. Retrieve cannot be called after Shutdown.
	//
	// Should never be called concurrently with itself or with Retrieve.
	// If ctx is cancelled should return immediately with an error.
	Shutdown(ctx context.Context) error
}

type WatcherFunc func(*ChangeEvent)

// ChangeEvent describes the particular change event that happened with the config.
// TODO: see if this can be eliminated.
type ChangeEvent struct {
	// Error is nil if the config is changed and needs to be re-fetched.
	// Any non-nil error indicates that there was a problem with watching the config changes.
	Error error
}

// Retrieved holds the result of a call to the Retrieve method of a Provider object.
type Retrieved struct {
	*Map

	// CloseFunc specifies a function to be invoked when the configuration for which it was
	// used to retrieve values is no longer in use and should close and release any watchers
	// that it may have created.
	//
	// If nil, then nothing to be closed.
	CloseFunc
}

// CloseFunc a function to close and release any watchers that it may have created.
//
// Should block until all resources are closed, and guarantee that `onChange` is not
// going to be called after it returns except when `ctx` is cancelled.
//
// Should never be called concurrently with itself.
type CloseFunc func(context.Context) error
