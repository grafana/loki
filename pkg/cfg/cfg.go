package cfg

import (
	"reflect"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/pkg/errors"
)

// Source is a generic configuration source. This function may do whatever is
// required to obtain the configuration. It is passed a pointer to the
// destination, which will be something compatible to `json.Unmarshal`. The
// obtained configuration may be written to this object, it may also contain
// data from previous sources.
type Source func(interface{}) error

// Unmarshal merges the values of the various configuration sources and sets them on
// `dst`. The object must be compatible with `json.Unmarshal`.
func Unmarshal(dst interface{}, sources ...Source) error {
	if len(sources) == 0 {
		panic("No sources supplied to cfg.Unmarshal(). This is most likely a programming issue and should never happen. Check the code!")
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
	// check dst is a pointer
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Ptr {
		panic("dst not a pointer")
	}

	// obtain type of dst for cloning
	t := reflect.Indirect(v).Type()

	// create new instances of dst's type for flags
	d := reflect.New(t).Interface().(flagext.Registerer)
	f := reflect.New(t).Interface().(flagext.Registerer)

	// shared state for FlagDefaultsDangerous and Flags
	var defaultsYaml []byte

	// unmarshal config
	return Unmarshal(dst,
		FlagDefaultsDangerous(d, &defaultsYaml),
		YAMLFlag(),
		Flags(f, defaultsYaml),
	)
}
