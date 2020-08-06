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
	return func(dst Cloneable) error {
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
	return func(dst Cloneable) error {
		return json.Unmarshal(y, dst)
	}
}

// YAML returns a Source that opens the supplied `.yaml` file and loads it.
func YAML(f string) Source {
	return func(dst Cloneable) error {
		y, err := ioutil.ReadFile(f)
		if err != nil {
			return err
		}

		err = dYAML(y)(dst)
		return errors.Wrap(err, f)
	}
}

// dYAML returns a YAML source and allows dependency injection
func dYAML(y []byte) Source {
	return func(dst Cloneable) error {
		return yaml.UnmarshalStrict(y, dst)
	}
}

func YAMLFlag(args []string, name string) Source {

	return func(dst Cloneable) error {
		freshFlags := flag.NewFlagSet("config-file-loader", flag.ContinueOnError)

		// Ensure we register flags on a copy of the config so as to not mutate it while
		// parsing out the config file location.
		dst.Clone().RegisterFlags(freshFlags)

		usage := freshFlags.Usage
		freshFlags.Usage = func() { /* don't do anything by default, we will print usage ourselves, but only when requested. */ }

		err := freshFlags.Parse(args)
		if err == flag.ErrHelp {
			// print available parameters to stdout, so that users can grep/less it easily
			freshFlags.SetOutput(os.Stdout)
			usage()
			os.Exit(2)
		} else if err != nil {
			fmt.Fprintln(freshFlags.Output(), "Run with -help to get list of available parameters")
			os.Exit(2)
		}

		f := freshFlags.Lookup(name)
		if f == nil || f.Value.String() == "" {
			return nil
		}

		return YAML(f.Value.String())(dst)

	}
}
