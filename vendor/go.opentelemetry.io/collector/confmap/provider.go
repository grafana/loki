// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	yaml "sigs.k8s.io/yaml/goyaml.v3"
)

// ProviderSettings are the settings to initialize a Provider.
type ProviderSettings struct {
	// Logger is a zap.Logger that will be passed to Providers.
	// Providers should be able to rely on the Logger being non-nil;
	// when instantiating a Provider with a ProviderFactory,
	// nil Logger references should be replaced with a no-op Logger.
	Logger *zap.Logger

	// prevent unkeyed literal initialization
	_ struct{}
}

// ProviderFactory defines a factory that can be used to instantiate
// new instances of a Provider.
type ProviderFactory = moduleFactory[Provider, ProviderSettings]

// CreateProviderFunc is a function that creates a Provider instance.
type CreateProviderFunc = createConfmapFunc[Provider, ProviderSettings]

// NewProviderFactory can be used to create a ProviderFactory.
func NewProviderFactory(f CreateProviderFunc) ProviderFactory {
	return newConfmapModuleFactory(f)
}

// Provider is an interface that helps to retrieve a config map and watch for any
// changes to the config map. Implementations may load the config from a file,
// a database or any other source.
//
// The typical usage is the following:
//
//	r, err := provider.Retrieve("file:/path/to/config")
//	// Use r.Map; wait for watcher to be called.
//	r.Close()
//	r, err = provider.Retrieve("file:/path/to/config")
//	// Use r.Map; wait for watcher to be called.
//	r.Close()
//	// repeat retrieve/wait/close cycle until it is time to shut down the Collector process.
//	// ...
//	provider.Shutdown()
type Provider interface {
	// Retrieve goes to the configuration source and retrieves the selected data which
	// contains the value to be injected in the configuration and the corresponding watcher that
	// will be used to monitor for updates of the retrieved value.
	//
	// `uri` must follow the "<scheme>:<opaque_data>" format. This format is compatible
	// with the URI definition (see https://datatracker.ietf.org/doc/html/rfc3986). The "<scheme>"
	// must be always included in the `uri`. The "<scheme>" supported by any provider:
	//   - MUST consist of a sequence of characters beginning with a letter and followed by any
	//     combination of letters, digits, plus ("+"), period ("."), or hyphen ("-").
	//     See https://datatracker.ietf.org/doc/html/rfc3986#section-3.1.
	//   - MUST be at least 2 characters long to avoid conflicting with a driver-letter identifier as specified
	//     in https://tools.ietf.org/id/draft-kerwin-file-scheme-07.html#syntax.
	//   - For testing, all implementation MUST check that confmaptest.ValidateProviderScheme returns no error.
	//
	// `watcher` callback is called when the config changes. watcher may be called from
	// a different go routine. After watcher is called, Provider.Retrieve should be called
	// to get the new config. See description of Retrieved for more details.
	// watcher may be nil, which indicates that the caller is not interested in
	// knowing about the changes.
	//
	// If ctx is cancelled should return immediately with an error.
	// Should never be called concurrently with itself or with Shutdown.
	Retrieve(ctx context.Context, uri string, watcher WatcherFunc) (*Retrieved, error)

	// Scheme returns the location scheme used by Retrieve.
	Scheme() string

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
type ChangeEvent struct {
	// Error is nil if the config is changed and needs to be re-fetched.
	// Any non-nil error indicates that there was a problem with watching the config changes.
	Error error
	// prevent unkeyed literal initialization
	_ struct{}
}

// Retrieved holds the result of a call to the Retrieve method of a Provider object.
type Retrieved struct {
	rawConf   any
	errorHint error
	closeFunc CloseFunc

	stringRepresentation string
	isSetString          bool
}

type retrievedSettings struct {
	errorHint            error
	stringRepresentation string
	isSetString          bool
	closeFunc            CloseFunc
}

// RetrievedOption options to customize Retrieved values.
type RetrievedOption interface {
	apply(*retrievedSettings)
}

type retrievedOptionFunc func(*retrievedSettings)

func (of retrievedOptionFunc) apply(e *retrievedSettings) {
	of(e)
}

// WithRetrievedClose overrides the default Retrieved.Close function.
// The default Retrieved.Close function does nothing and always returns nil.
func WithRetrievedClose(closeFunc CloseFunc) RetrievedOption {
	return retrievedOptionFunc(func(settings *retrievedSettings) {
		settings.closeFunc = closeFunc
	})
}

func withStringRepresentation(stringRepresentation string) RetrievedOption {
	return retrievedOptionFunc(func(settings *retrievedSettings) {
		settings.stringRepresentation = stringRepresentation
		settings.isSetString = true
	})
}

func withErrorHint(errorHint error) RetrievedOption {
	return retrievedOptionFunc(func(settings *retrievedSettings) {
		settings.errorHint = errorHint
	})
}

// NewRetrievedFromYAML returns a new Retrieved instance that contains the deserialized data from the yaml bytes.
// * yamlBytes the yaml bytes that will be deserialized.
// * opts specifies options associated with this Retrieved value, such as CloseFunc.
func NewRetrievedFromYAML(yamlBytes []byte, opts ...RetrievedOption) (*Retrieved, error) {
	var rawConf any
	if err := yaml.Unmarshal(yamlBytes, &rawConf); err != nil {
		// If the string is not valid YAML, we try to use it verbatim as a string.
		strRep := string(yamlBytes)
		return NewRetrieved(strRep, append(opts,
			withStringRepresentation(strRep),
			withErrorHint(fmt.Errorf("assuming string type since contents are not valid YAML: %w", err)),
		)...)
	}

	switch rawConf.(type) {
	case string:
		val := string(yamlBytes)
		return NewRetrieved(val, append(opts, withStringRepresentation(val))...)
	default:
		opts = append(opts, withStringRepresentation(string(yamlBytes)))
	}

	return NewRetrieved(rawConf, opts...)
}

// NewRetrieved returns a new Retrieved instance that contains the data from the raw deserialized config.
// The rawConf can be one of the following types:
//   - Primitives: int, int32, int64, float32, float64, bool, string;
//   - []any;
//   - map[string]any;
func NewRetrieved(rawConf any, opts ...RetrievedOption) (*Retrieved, error) {
	if err := checkRawConfType(rawConf); err != nil {
		return nil, err
	}
	set := retrievedSettings{}
	for _, opt := range opts {
		opt.apply(&set)
	}
	return &Retrieved{
		rawConf:              rawConf,
		errorHint:            set.errorHint,
		closeFunc:            set.closeFunc,
		stringRepresentation: set.stringRepresentation,
		isSetString:          set.isSetString,
	}, nil
}

// AsConf returns the retrieved configuration parsed as a Conf.
func (r *Retrieved) AsConf() (*Conf, error) {
	if r.rawConf == nil {
		return New(), nil
	}
	val, ok := r.rawConf.(map[string]any)
	if !ok {
		if r.errorHint != nil {
			return nil, fmt.Errorf("retrieved value (type=%T) cannot be used as a Conf: %w", r.rawConf, r.errorHint)
		}
		return nil, fmt.Errorf("retrieved value (type=%T) cannot be used as a Conf", r.rawConf)
	}
	return NewFromStringMap(val), nil
}

// AsRaw returns the retrieved configuration parsed as an any which can be one of the following types:
//   - Primitives: int, int32, int64, float32, float64, bool, string;
//   - []any - every member follows the same rules as the given any;
//   - map[string]any - every value follows the same rules as the given any;
func (r *Retrieved) AsRaw() (any, error) {
	return r.rawConf, nil
}

// AsString returns the retrieved configuration as a string.
// If the retrieved configuration is not convertible to a string unambiguously, an error is returned.
// If the retrieved configuration is a string, the string is returned.
// This method is used to resolve ${} references in inline position.
func (r *Retrieved) AsString() (string, error) {
	if !r.isSetString {
		if str, ok := r.rawConf.(string); ok {
			return str, nil
		}
		return "", fmt.Errorf("retrieved value does not have unambiguous string representation: %v", r.rawConf)
	}
	return r.stringRepresentation, nil
}

// Close and release any watchers that Provider.Retrieve may have created.
//
// Should block until all resources are closed, and guarantee that `onChange` is not
// going to be called after it returns except when `ctx` is cancelled.
//
// Should never be called concurrently with itself.
func (r *Retrieved) Close(ctx context.Context) error {
	if r.closeFunc == nil {
		return nil
	}
	return r.closeFunc(ctx)
}

// CloseFunc a function equivalent to Retrieved.Close.
type CloseFunc func(context.Context) error

func checkRawConfType(rawConf any) error {
	if rawConf == nil {
		return nil
	}
	switch rawConf.(type) {
	case int, int32, int64, float32, float64, bool, string, []any, map[string]any, time.Time:
		return nil
	default:
		return fmt.Errorf(
			"unsupported type=%T for retrieved config,"+
				" ensure that values are wrapped in quotes", rawConf)
	}
}
