// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package confmap // import "go.opentelemetry.io/collector/confmap"

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/multierr"
	"go.uber.org/zap"

	"go.opentelemetry.io/collector/confmap/internal"
)

// follows drive-letter specification:
// https://datatracker.ietf.org/doc/html/draft-kerwin-file-scheme-07.html#section-2.2
var driverLetterRegexp = regexp.MustCompile("^[A-z]:")

// Resolver resolves a configuration as a Conf.
type Resolver struct {
	uris          []location
	providers     map[string]Provider
	defaultScheme string
	converters    []Converter

	closers []CloseFunc
	watcher chan error
}

// ResolverSettings are the settings to configure the behavior of the Resolver.
type ResolverSettings struct {
	// URIs locations from where the Conf is retrieved, and merged in the given order.
	// It is required to have at least one location.
	URIs []string

	// ProviderFactories is a slice of Provider factories.
	// It is required to have at least one factory.
	ProviderFactories []ProviderFactory

	// DefaultScheme is the scheme that is used if ${} syntax is used but no schema is provided.
	// If no DefaultScheme is set, ${} with no schema will not be expanded.
	// It is strongly recommended to set "env" as the default scheme to align with the
	// OpenTelemetry Configuration Specification
	DefaultScheme string

	// ProviderSettings contains settings that will be passed to Provider
	// factories when instantiating Providers.
	ProviderSettings ProviderSettings

	// ConverterFactories is a slice of Converter creation functions.
	ConverterFactories []ConverterFactory

	// ConverterSettings contains settings that will be passed to Converter
	// factories when instantiating Converters.
	ConverterSettings ConverterSettings

	// prevent unkeyed literal initialization
	_ struct{}
}

// NewResolver returns a new Resolver that resolves configuration from multiple URIs.
//
// To resolve a configuration the following steps will happen:
//  1. Retrieves individual configurations from all given "URIs", and merge them in the retrieve order.
//  2. Once the Conf is merged, apply the converters in the given order.
//
// After the configuration was resolved the `Resolver` can be used as a single point to watch for updates in
// the configuration data retrieved via the config providers used to process the "initial" configuration and to generate
// the "effective" one. The typical usage is the following:
//
//	Resolver.Resolve(ctx)
//	Resolver.Watch() // wait for an event.
//	Resolver.Resolve(ctx)
//	Resolver.Watch() // wait for an event.
//	// repeat Resolve/Watch cycle until it is time to shut down the Collector process.
//	Resolver.Shutdown(ctx)
//
// `uri` must follow the "<scheme>:<opaque_data>" format. This format is compatible with the URI definition
// (see https://datatracker.ietf.org/doc/html/rfc3986). An empty "<scheme>" defaults to "file" schema.
func NewResolver(set ResolverSettings) (*Resolver, error) {
	if len(set.URIs) == 0 {
		return nil, errors.New("invalid 'confmap.ResolverSettings' configuration: no URIs")
	}

	if len(set.ProviderFactories) == 0 {
		return nil, errors.New("invalid 'confmap.ResolverSettings' configuration: no Providers")
	}

	if set.ProviderSettings.Logger == nil {
		set.ProviderSettings.Logger = zap.NewNop()
	}

	if set.ConverterSettings.Logger == nil {
		set.ConverterSettings.Logger = zap.NewNop()
	}

	providers := make(map[string]Provider, len(set.ProviderFactories))
	for _, factory := range set.ProviderFactories {
		provider := factory.Create(set.ProviderSettings)
		scheme := provider.Scheme()
		// Check that the scheme follows the pattern.
		if !regexp.MustCompile(schemePattern).MatchString(scheme) {
			return nil, fmt.Errorf("invalid 'confmap.Provider' scheme %q", scheme)
		}
		// Check that the scheme is unique.
		if _, ok := providers[scheme]; ok {
			return nil, fmt.Errorf("duplicate 'confmap.Provider' scheme %q", scheme)
		}

		providers[scheme] = provider
	}

	if set.DefaultScheme != "" {
		_, ok := providers[set.DefaultScheme]
		if !ok {
			return nil, errors.New("invalid 'confmap.ResolverSettings' configuration: DefaultScheme not found in providers list")
		}
	}

	converters := make([]Converter, len(set.ConverterFactories))
	for i, factory := range set.ConverterFactories {
		converters[i] = factory.Create(set.ConverterSettings)
	}

	// Safe copy, ensures the slices and maps cannot be changed from the caller.
	uris := make([]location, len(set.URIs))
	for i, uri := range set.URIs {
		// For backwards compatibility:
		// - empty url scheme means "file".
		// - "^[A-z]:" also means "file"
		if driverLetterRegexp.MatchString(uri) || !strings.Contains(uri, ":") {
			uris[i] = location{scheme: "file", opaqueValue: uri}
			continue
		}
		lURI, err := newLocation(uri)
		if err != nil {
			return nil, err
		}
		if _, ok := providers[lURI.scheme]; !ok {
			return nil, fmt.Errorf("unsupported scheme on URI %q", uri)
		}
		uris[i] = lURI
	}

	return &Resolver{
		uris:          uris,
		providers:     providers,
		defaultScheme: set.DefaultScheme,
		converters:    converters,
		watcher:       make(chan error, 1),
	}, nil
}

// Resolve returns the configuration as a Conf, or error otherwise.
// Should never be called concurrently with itself, Watch or Shutdown.
func (mr *Resolver) Resolve(ctx context.Context) (*Conf, error) {
	// First check if already an active watching, close that if any.
	if err := mr.closeIfNeeded(ctx); err != nil {
		return nil, fmt.Errorf("cannot close previous watch: %w", err)
	}

	// Retrieves individual configurations from all URIs in the given order, and merge them in retMap.
	retMap := New()
	for _, uri := range mr.uris {
		ret, err := mr.retrieveValue(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("cannot retrieve the configuration: %w", err)
		}
		mr.closers = append(mr.closers, ret.Close)
		retCfgMap, err := ret.AsConf()
		if err != nil {
			return nil, err
		}

		if err := retMap.Merge(retCfgMap); err != nil {
			return nil, err
		}
	}

	cfgMap := make(map[string]any)
	for _, k := range retMap.AllKeys() {
		ug := internal.UnsanitizedGetter{Conf: retMap}
		val, err := mr.expandValueRecursively(ctx, ug.UnsanitizedGet(k))
		if err != nil {
			return nil, err
		}
		cfgMap[k] = escapeDollarSigns(val)
	}
	retMap = NewFromStringMap(cfgMap)

	// Apply the converters in the given order.
	for _, confConv := range mr.converters {
		if err := confConv.Convert(ctx, retMap); err != nil {
			return nil, fmt.Errorf("cannot convert the confmap.Conf: %w", err)
		}
	}

	return retMap, nil
}

func escapeDollarSigns(val any) any {
	switch v := val.(type) {
	case string:
		return strings.ReplaceAll(v, "$$", "$")
	case internal.ExpandedValue:
		v.Original = strings.ReplaceAll(v.Original, "$$", "$")
		v.Value = escapeDollarSigns(v.Value)
		return v
	case []any:
		nslice := make([]any, len(v))
		for i, x := range v {
			nslice[i] = escapeDollarSigns(x)
		}
		return nslice
	case map[string]any:
		nmap := make(map[string]any, len(v))
		for k, x := range v {
			nmap[k] = escapeDollarSigns(x)
		}
		return nmap
	default:
		return val
	}
}

// Watch blocks until any configuration change was detected or an unrecoverable error
// happened during monitoring the configuration changes.
//
// Error is nil if the configuration is changed and needs to be re-fetched. Any non-nil
// error indicates that there was a problem with watching the configuration changes.
//
// Should never be called concurrently with itself or Get.
func (mr *Resolver) Watch() <-chan error {
	return mr.watcher
}

// Shutdown signals that the provider is no longer in use and the that should close
// and release any resources that it may have created. It terminates the Watch channel.
//
// Should never be called concurrently with itself or Get.
func (mr *Resolver) Shutdown(ctx context.Context) error {
	close(mr.watcher)

	var errs error
	errs = multierr.Append(errs, mr.closeIfNeeded(ctx))
	for _, p := range mr.providers {
		errs = multierr.Append(errs, p.Shutdown(ctx))
	}

	return errs
}

func (mr *Resolver) onChange(event *ChangeEvent) {
	mr.watcher <- event.Error
}

func (mr *Resolver) closeIfNeeded(ctx context.Context) error {
	var err error
	for _, ret := range mr.closers {
		err = multierr.Append(err, ret(ctx))
	}
	mr.closers = nil
	return err
}

func (mr *Resolver) retrieveValue(ctx context.Context, uri location) (*Retrieved, error) {
	p, ok := mr.providers[uri.scheme]
	if !ok {
		return nil, fmt.Errorf("scheme %q is not supported for uri %q", uri.scheme, uri.asString())
	}
	return p.Retrieve(ctx, uri.asString(), mr.onChange)
}
