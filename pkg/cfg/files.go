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

		j, err := ioutil.ReadFile(*f)
		if err != nil {
			return err
		}

		if err := json.Unmarshal(j, dst); err != nil {
			return err
		}
		return nil
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

		if err := yaml.Unmarshal(y, dst); err != nil {
			return err
		}
		return nil
	}
}

// YAMLFlag defines a `config.file` flag and loads this file
func YAMLFlag() Source {
	f := flag.String("config.file", "", ".yaml configuration file to parse")
	return func(dst interface{}) error {
		if *f == "" {
			f = nil
		}
		return YAML(f)(dst)
	}
}
