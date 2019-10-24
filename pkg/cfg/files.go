package cfg

import (
	"encoding/json"
	"flag"
	"io/ioutil"

	yaml "gopkg.in/yaml.v2"
)

// JSON returns a Source that opens the supplied `.json` file and loads it.
func JSON(f *string) Source {
	return func(dst interface{}) error {
		if f == nil {
			return nil
		}

		j, err := ioutil.ReadFile(*f)
		if err != nil {
			return err
		}

		return dJSON(j)(dst)
	}
}

// dJSON returns a JSON source and allows dependency injection
func dJSON(y []byte) Source {
	return func(dst interface{}) error {
		return json.Unmarshal(y, dst)
	}
}

// YAML returns a Source that opens the supplied `.yaml` file and loads it.
func YAML(f *string) Source {
	return func(dst interface{}) error {
		if f == nil {
			return nil
		}

		y, err := ioutil.ReadFile(*f)
		if err != nil {
			return err
		}

		return dYAML(y)(dst)
	}
}

// dYAML returns a YAML source and allows dependency injection
func dYAML(y []byte) Source {
	return func(dst interface{}) error {
		return yaml.Unmarshal(y, dst)
	}
}

// YAMLFlag defines a `config.file` flag and loads this file
func YAMLFlag(name, value, help string) Source {
	return func(dst interface{}) error {
		f := flag.String(name, value, help)
		flag.Parse()

		if *f == "" {
			f = nil
		}
		return YAML(f)(dst)
	}
}
