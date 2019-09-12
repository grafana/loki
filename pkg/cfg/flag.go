package cfg

import (
	"flag"
	"os"
	"reflect"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	ghodss "github.com/ghodss/yaml"
	yaml "gopkg.in/yaml.v2"
)

// FlagDefaultsDangerous obtains defaults from a flag Registerer and DANGEROUSLY
// replaces (!!) the dst object with reg.
// Make sure this is always the first source, or it will overwrite other data.
// The defaults are set into ptr, if is an empty byte slice and not nil.
func FlagDefaultsDangerous(reg flagext.Registerer, ptr *[]byte) Source {
	// some cortex code uses global `flag.*Var`. To avoid the flag package panicking, the default flagset is temporarily swapped out
	tmp := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet("", flag.ContinueOnError)
	flagext.DefaultValues(reg)
	flag.CommandLine = tmp

	// dump defaults for subsequent `Flags()` call
	if ptr != nil {
		d, err := yaml.Marshal(reg)
		if err != nil {
			panic(err)
		}

		*ptr = d
	}

	return func(dst interface{}) error {
		// set *reg for *dst
		v := reflect.Indirect(reflect.ValueOf(dst))
		v.Set(reflect.Indirect(reflect.ValueOf(reg)))
		return nil
	}
}

// Flags returns a source that sets values from changed (!) flags onto dst.
// The must have the same structure as dst, usually it is even the same type.
// The defaults slice MUST contain valid defaults as yaml from the same registerer.
func Flags(reg flagext.Registerer, defaults []byte) Source {
	return dFlags(os.Args[1:], reg, defaults)
}

// dFlags parses flags defined by reg using the args slice onto dst.
// The defaults slice MUST contain valid defaults as yaml from the same registerer.
func dFlags(args []string, reg flagext.Registerer, defaults []byte) Source {
	flagext.RegisterFlags(reg)
	_ = flag.CommandLine.Parse(args)
	data, err := yaml.Marshal(reg)
	if err != nil {
		panic(err)
	}
	return func(dst interface{}) error {
		return stripDefaults(dst, data, defaults)
	}
}

// stripDefaults takes the final config, the config from flags and the defaults,
// but merges only non-default values onto dst
func stripDefaults(dst interface{}, flagsStr, defStr []byte) error {
	// using ghodss.Unmarshal to receive map[string]interface{}
	var live, def map[string]interface{}
	if err := ghodss.Unmarshal(flagsStr, &live); err != nil {
		return err
	}
	if err := ghodss.Unmarshal(defStr, &def); err != nil {
		return err
	}

	res := deleteSame(live, def, "")

	out, err := yaml.Marshal(res)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(out, dst)
}

// deleteSame filters all equal values out of deeply nested map[string]interface{}'s
func deleteSame(live, def map[string]interface{}, path string) map[string]interface{} {
	for k := range live {
		if reflect.DeepEqual(live[k], def[k]) {
			delete(live, k)
			continue
		}

		if _, ok := live[k].(map[string]interface{}); ok {
			live[k] = deleteSame(live[k].(map[string]interface{}), def[k].(map[string]interface{}), path+"."+k)
		}

	}
	return live
}
