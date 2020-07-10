package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"
	"gopkg.in/yaml.v2"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/loki"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/util/validation"
)

func init() {
	prometheus.MustRegister(version.NewCollector("loki"))
}

var lineReplacer = strings.NewReplacer("\n", "\\n  ")

func main() {
	printVersion := flag.Bool("version", false, "Print this builds version information")
	printConfig := flag.Bool("print-config-stderr", false, "Dump the entire Loki config object to stderr")
	printConfigInline := flag.Bool("print-config-stderr-inline", false, "Dump the entire Loki config object to stderr broken up by sections with escaped newline characters, "+
		"this will display much better when captured by Loki and shown in Grafana")

	var config loki.Config
	if err := cfg.Parse(&config); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}
	if *printVersion {
		fmt.Println(version.Print("loki"))
		os.Exit(0)
	}

	// This global is set to the config passed into the last call to `NewOverrides`. If we don't
	// call it atleast once, the defaults are set to an empty struct.
	// We call it with the flag values so that the config file unmarshalling only overrides the values set in the config.
	validation.SetDefaultLimitsForYAMLUnmarshalling(config.LimitsConfig)

	// Init the logger which will honor the log level set in config.Server
	if reflect.DeepEqual(&config.Server.LogLevel, &logging.Level{}) {
		level.Error(util.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util.InitLogger(&config.Server)

	// Validate the config once both the config file has been loaded
	// and CLI flags parsed.
	err := config.Validate(util.Logger)
	if err != nil {
		level.Error(util.Logger).Log("msg", "validating config", "err", err.Error())
		os.Exit(1)
	}

	if *printConfig {
		err := dumpConfig(os.Stderr, &config, false)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if *printConfigInline {
		err := dumpConfig(os.Stderr, &config, true)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if config.Tracing.Enabled {
		// Setting the environment variable JAEGER_AGENT_HOST enables tracing
		trace, err := tracing.NewFromEnv(fmt.Sprintf("loki-%s", config.Target))
		if err != nil {
			level.Error(util.Logger).Log("msg", "error in initializing tracing. tracing will not be enabled", "err", err)
		}
		defer func() {
			if trace != nil {
				if err := trace.Close(); err != nil {
					level.Error(util.Logger).Log("msg", "error closing tracing", "err", err)
				}
			}

		}()
	}

	// Start Loki
	t, err := loki.New(config)
	util.CheckFatal("initialising loki", err)

	level.Info(util.Logger).Log("msg", "Starting Loki", "version", version.Info())

	err = t.Run()
	util.CheckFatal("running loki", err)
}

func dumpConfig(w io.Writer, config *loki.Config, escapeNewlines bool) error {
	if !escapeNewlines {
		lc, err := yaml.Marshal(&config)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "---\n# Loki Config\n# %s\n%s\n\n", version.Info(), string(lc))
		return nil
	}

	s := reflect.ValueOf(config).Elem()
	typeOfT := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		if !f.CanInterface() {
			continue
		}
		// Lookup the yaml tag to print the field name by the yaml tag, skip if it's not present or empty
		if alias, ok := typeOfT.Field(i).Tag.Lookup("yaml"); ok {
			if alias == "" {
				continue
			} else {
				// yaml tags can contain other data like `omitempty` so split by comma and only return first result
				fmt.Fprint(w, strings.Split(alias, ",")[0]+": ")
			}
		} else {
			continue
		}

		if f.Kind() == reflect.Slice {
			return errors.New("the config dumping function was not programmed to handle a configuration array as a top level item in the Loki config")
		}

		kind := f.Kind()
		val := f.Interface()

		// Dereference any pointers so we can properly determine the kind of the underlying object
		if f.Kind() == reflect.Ptr {
			if !f.IsNil() {
				kind = f.Elem().Kind()
				val = f.Elem().Interface()
			}
		}

		// If the field is a struct, unmarshal it with a yaml unmarshaller and print it
		if kind == reflect.Struct {
			err := unmarshall(w, val)
			if err != nil {
				return err
			}
		} else {
			// If it's not a struct, just print the value
			fmt.Fprint(w, val)
		}
		fmt.Fprint(w, "\n")
	}

	os.Exit(0)

	return nil
}

func unmarshall(w io.Writer, v interface{}) error {
	fmt.Fprint(w, "\\n")
	sc, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Fprint(w, lineReplacer.Replace(string(sc)))
	return nil
}
