package cfg

import (
	"reflect"

	"github.com/pkg/errors"
)

// Source is a generic configuration source. This function may do whatever is
// required to obtain the configuration. It is passed a pointer to the
// destination, which will be something compatible to `json.Unmarshal`. The
// obtained configuration may be written to this object, it may also contain
// data from previous sources.
type Source func(interface{}) error

var (
	ErrNotPointer = errors.New("dst is not a pointer")
)

// Unmarshal merges the values of the various configuration sources and sets them on
// `dst`. The object must be compatible with `json.Unmarshal`.
func Unmarshal(dst interface{}, sources ...Source) error {
	if len(sources) == 0 {
		panic("No sources supplied to cfg.Unmarshal(). This is most likely a programming issue and should never happen. Check the code!")
	}
	if reflect.ValueOf(dst).Kind() != reflect.Ptr {
		return ErrNotPointer
	}

	for _, source := range sources {
		if err := source(dst); err != nil {
			return errors.Wrap(err, "sourcing")
		}
	}
	return nil
}

// Parse is a higher level wrapper for Unmarshal that automatically parses flags and a .yaml file
func Parse(dst interface{}) error {
	return dParse(dst,
		Defaults(),
		YAMLFlag("config.file", "", "yaml file to load"),
		Flags(),
	)
}

// dParse is the same as Parse, but with dependency injection for testing
func dParse(dst interface{}, defaults, yaml, flags Source) error {
	// check dst is a pointer
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr {
		return ErrNotPointer
	}

	// unmarshal config
	return Unmarshal(dst,
		defaults,
		yaml,
		flags,
	)
}
