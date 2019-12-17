package cfg

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
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

		err = dJSON(j)(dst)
		return errors.Wrap(err, *f)
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

		err = dYAML(y)(dst)
		return errors.Wrap(err, *f)
	}
}

// dYAML returns a YAML source and allows dependency injection
func dYAML(y []byte) Source {
	return func(dst interface{}) error {
		return yaml.UnmarshalStrict(y, dst)
	}
}

// YAMLFlag defines a `config.file` flag and loads this file
func YAMLFlag(name, value, help string) Source {
	return func(dst interface{}) error {
		f := flag.String(name, value, help)

		usage := flag.CommandLine.Usage
		flag.CommandLine.Usage = func() { /* don't do anything by default, we will print usage ourselves, but only when requested. */ }

		flag.CommandLine.Init(flag.CommandLine.Name(), flag.ContinueOnError)
		err := flag.CommandLine.Parse(os.Args[1:])
		if err == flag.ErrHelp {
			// print available parameters to stdout, so that users can grep/less it easily
			flag.CommandLine.SetOutput(os.Stdout)
			usage()
			os.Exit(2)
		} else if err != nil {
			fmt.Fprintln(flag.CommandLine.Output(), "Run with -help to get list of available parameters")
			os.Exit(2)
		}

		if *f == "" {
			f = nil
		}
		return YAML(f)(dst)
	}
}
