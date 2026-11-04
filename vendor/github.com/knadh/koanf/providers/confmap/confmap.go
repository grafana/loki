// Package confmap implements a koanf.Provider that takes nested
// and flat map[string]any config maps and provides them
// to koanf.
package confmap

import (
	"errors"

	"github.com/knadh/koanf/maps"
)

// Confmap implements a raw map[string]any provider.
type Confmap struct {
	mp map[string]any
}

// Provider returns a confmap Provider that takes a flat or nested
// map[string]any. If a delim is provided, it indicates that the
// keys are flat and the map needs to be unflattened by delim.
func Provider(mp map[string]any, delim string) *Confmap {
	cp := maps.Copy(mp)
	maps.IntfaceKeysToStrings(cp)
	if delim != "" {
		cp = maps.Unflatten(cp, delim)
	}
	return &Confmap{mp: cp}
}

// ReadBytes is not supported by the confmap provider.
func (e *Confmap) ReadBytes() ([]byte, error) {
	return nil, errors.New("confmap provider does not support this method")
}

// Read returns the loaded map[string]any.
func (e *Confmap) Read() (map[string]any, error) {
	return e.mp, nil
}
