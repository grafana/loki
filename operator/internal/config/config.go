package config

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
)

var (
	errSchemeNotSupplied     = errors.New("scheme not supplied to controller configuration loader")
	errConfigFileLoading     = errors.New("could not read file at path")
	errRuntimeObjectDecoding = errors.New("could not decode file into runtime.Object")
)

// ControllerManagerConfiguration defines the functions necessary to parse a config file
// and to configure the Options struct for the ctrl.Manager.
type ControllerManagerConfiguration interface {
	runtime.Object

	// Complete returns the versioned configuration
	Complete() (configv1.ControllerManagerConfigurationSpec, error)
}

// DeferredFileLoader is used to configure the decoder for loading controller
// runtime component config types.
type DeferredFileLoader struct {
	ControllerManagerConfiguration
	path   string
	scheme *runtime.Scheme
	once   sync.Once
	err    error
}

// File will set up the deferred file loader for the configuration
// this will also configure the defaults for the loader if nothing is
//
// Defaults:
// * Path: "./config.yaml"
// * Kind: GenericControllerManagerConfiguration
func File() *DeferredFileLoader {
	scheme := runtime.NewScheme()
	utilruntime.Must(configv1.AddToScheme(scheme))
	return &DeferredFileLoader{
		path:                           "./config.yaml",
		ControllerManagerConfiguration: &configv1.ControllerManagerConfiguration{},
		scheme:                         scheme,
	}
}

// Complete will use sync.Once to set the scheme.
func (d *DeferredFileLoader) Complete() (configv1.ControllerManagerConfigurationSpec, error) {
	d.once.Do(d.loadFile)
	if d.err != nil {
		return configv1.ControllerManagerConfigurationSpec{}, d.err
	}
	return d.ControllerManagerConfiguration.Complete()
}

// AtPath will set the path to load the file for the decoder.
func (d *DeferredFileLoader) AtPath(path string) *DeferredFileLoader {
	d.path = path
	return d
}

// OfKind will set the type to be used for decoding the file into.
func (d *DeferredFileLoader) OfKind(obj ControllerManagerConfiguration) *DeferredFileLoader {
	d.ControllerManagerConfiguration = obj
	return d
}

// loadFile is used from the mutex.Once to load the file.
func (d *DeferredFileLoader) loadFile() {
	if d.scheme == nil {
		d.err = errSchemeNotSupplied
		return
	}

	content, err := os.ReadFile(d.path)
	if err != nil {
		d.err = fmt.Errorf("%w %s", errConfigFileLoading, d.path)
		return
	}

	codecs := serializer.NewCodecFactory(d.scheme)

	// Regardless of if the bytes are of any external version,
	// it will be read successfully and converted into the internal version
	if err = runtime.DecodeInto(codecs.UniversalDecoder(), content, d.ControllerManagerConfiguration); err != nil {
		d.err = errRuntimeObjectDecoding
	}
}
