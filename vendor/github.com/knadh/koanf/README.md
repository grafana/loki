<a href="https://zerodha.tech"><img src="https://zerodha.tech/static/images/github-badge.svg" align="right" /></a>

![koanf](https://user-images.githubusercontent.com/547147/72681838-6981dd00-3aed-11ea-8f5d-310816c70c08.png)

**koanf** (pronounced _conf_; a play on the Japanese _Koan_) is a library for reading configuration from different sources in different formats in Go applications. It is a cleaner, lighter [alternative to spf13/viper](#alternative-to-viper) with better abstractions and extensibility and fewer dependencies.

koanf comes with built in support for reading configuration from files, command line flags, and environment variables, and can parse JSON, YAML, TOML, and Hashicorp HCL.

[![Build Status](https://travis-ci.com/knadh/koanf.svg?branch=master)](https://travis-ci.com/knadh/koanf) [![GoDoc](https://godoc.org/github.com/knadh/koanf?status.svg)](https://godoc.org/github.com/knadh/koanf) 

### Installation

`go get -u github.com/knadh/koanf`

### Contents

- [Concepts](#concepts)
- [Reading config from files](#reading-config-from-files)
- [Watching files for changes](#watching-files-for-changes)
- [Reading from command line](#reading-from-command-line)
- [Reading environment variables](#reading-environment-variables)
- [Reading raw bytes](#reading-raw-bytes)
- [Unmarshalling and marshalling](#unmarshalling-and-marshalling)
- [Unmarshalling with flat paths](#unmarshalling-with-flat-paths)
- [Setting default values](#setting-default-values)
- [Order of merge and key case senstivity](#order-of-merge-and-key-case-sensitivity)
- [Custom Providers and Parsers](#custom-providers-and-parsers)
- [Custom Merge Strategies](#custom-merge-strategies)
- [List of Providers, Parsers, and functions](#api)

### Concepts

- `koanf.Provider` is a generic interface that provides configuration, for example, from files, environment variables, HTTP sources, or anywhere. The configuration can either be raw bytes that a parser can parse, or it can be a nested map[string]interface{} that can be directly loaded.
- `koanf.Parser` is a generic interface that takes raw bytes, parses, and returns a nested map[string]interface{} representation. For example, JSON and YAML parsers.
- Once loaded into koanf, configuration are values queried by a delimited key path syntax. eg: `app.server.port`. Any delimiter can be chosen.
- Configuration from multiple sources can be loaded and merged into a koanf instance, for example, load from a file first and override certain values with flags from the command line.

With these two interface implementations, koanf can obtain a configuration from multiple sources and parse any format and make it available to an application.

### Reading config from files

```go
package main

import (
	"fmt"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
)

// Global koanf instance. Use "." as the key path delimiter. This can be "/" or any character.
var k = koanf.New(".")

func main() {
	// Load JSON config.
	if err := k.Load(file.Provider("mock/mock.json"), json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Load YAML config and merge into the previously loaded config (because we can).
	k.Load(file.Provider("mock/mock.yaml"), yaml.Parser())

	fmt.Println("parent's name is = ", k.String("parent1.name"))
	fmt.Println("parent's ID is = ", k.Int("parent1.id"))
}

```

### Watching files for changes
The `koanf.Provider` interface has a `Watch(cb)` method that asks a provider
to watch for changes and trigger the given callback that can live reload the
configuration.

Currently, `file.Provider` supports this.


```go
package main

import (
	"fmt"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
)

// Global koanf instance. Use "." as the key path delimiter. This can be "/" or any character.
var k = koanf.New(".")

func main() {
	// Load JSON config.
	f := file.Provider("mock/mock.json")
	if err := k.Load(f, json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Load YAML config and merge into the previously loaded config (because we can).
	k.Load(file.Provider("mock/mock.yaml"), yaml.Parser())

	fmt.Println("parent's name is = ", k.String("parent1.name"))
	fmt.Println("parent's ID is = ", k.Int("parent1.id"))

	// Watch the file and get a callback on change. The callback can do whatever,
	// like re-load the configuration.
	// File provider always returns a nil `event`.
	f.Watch(func(event interface{}, err error) {
		if err != nil {
			log.Printf("watch error: %v", err)
			return
		}

		// Throw away the old config and load a fresh copy.
		log.Println("config changed. Reloading ...")
		k = koanf.New(".")
		k.Load(f, json.Parser())
		k.Print()
	})

	// Block forever (and manually make a change to mock/mock.json) to
	// reload the config.
	log.Println("waiting forever. Try making a change to mock/mock.json to live reload")
	<-make(chan bool)
}
```


### Reading from command line

The following example shows the use of `posflag.Provider`, a wrapper over the [spf13/pflag](https://github.com/spf13/pflag) library, an advanced commandline lib. For Go's built in `flag` package, use `basicflag.Provider`.

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	flag "github.com/spf13/pflag"
)

// Global koanf instance. Use "." as the key path delimiter. This can be "/" or any character.
var k = koanf.New(".")

func main() {
	// Use the POSIX compliant pflag lib instead of Go's flag lib.
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.Usage = func() {
		fmt.Println(f.FlagUsages())
		os.Exit(0)
	}
	// Path to one or more config files to load into koanf along with some config params.
	f.StringSlice("conf", []string{"mock/mock.toml"}, "path to one or more .toml config files")
	f.String("time", "2020-01-01", "a time string")
	f.String("type", "xxx", "type of the app")
	f.Parse(os.Args[1:])

	// Load the config files provided in the commandline.
	cFiles, _ := f.GetStringSlice("conf")
	for _, c := range cFiles {
		if err := k.Load(file.Provider(c), toml.Parser()); err != nil {
			log.Fatalf("error loading file: %v", err)
		}
	}

	// "time" and "type" may have been loaded from the config file, but
	// they can still be overridden with the values from the command line.
	// The bundled posflag.Provider takes a flagset from the spf13/pflag lib.
	// Passing the Koanf instance to posflag helps it deal with default command
	// line flag values that are not present in conf maps from previously loaded
	// providers.
	if err := k.Load(posflag.Provider(f, ".", k), nil); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	fmt.Println("time is = ", k.String("time"))
}
```

### Reading environment variables

```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
)

// Global koanf instance. Use . as the key path delimiter. This can be / or anything.
var k = koanf.New(".")

func main() {
	// Load JSON config.
	if err := k.Load(file.Provider("mock/mock.json"), json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Load environment variables and merge into the loaded config.
	// "MYVAR" is the prefix to filter the env vars by.
	// "." is the delimiter used to represent the key hierarchy in env vars.
	// The (optional, or can be nil) function can be used to transform
	// the env var names, for instance, to lowercase them.
	//
	// For example, env vars: MYVAR_TYPE and MYVAR_PARENT1_CHILD1_NAME
	// will be merged into the "type" and the nested "parent1.child1.name"
	// keys in the config file here as we lowercase the key, 
	// replace `_` with `.` and strip the MYVAR_ prefix so that 
	// only "parent1.child1.name" remains.
	k.Load(env.Provider("MYVAR_", ".", func(s string) string {
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, "MYVAR_")), "_", ".", -1)
	}), nil)

	fmt.Println("name is = ", k.String("parent1.child1.name"))
}
```

You can also use the `env.ProviderWithValue` with a callback that supports mutating both the key and value
to return types other than a string. For example, here, env values separated by spaces are
returned as string slices or arrays. eg: `MYVAR_slice=a b c` becomes `slice: [a, b, c]`.

```go
	k.Load(env.ProviderWithValue("MYVAR_", ".", func(s string, v string) (string, interface{}) {
		// Strip out the MYVAR_ prefix and lowercase and get the key while also replacing
		// the _ character with . in the key (koanf delimeter).
		key := strings.Replace(strings.ToLower(strings.TrimPrefix(s, "MYVAR_")), "_", ".", -1)

		// If there is a space in the value, split the value into a slice by the space.
		if strings.Contains(v, " ") {
			return key, strings.Split(v, " ")
		}

		// Otherwise, return the plain string.
		return key, v
	}), nil)
```

### Reading from an S3 bucket

```go
// Load JSON config from s3.
if err := k.Load(s3.Provider(s3.Config{
	AccessKey: os.Getenv("AWS_S3_ACCESS_KEY"),
	SecretKey: os.Getenv("AWS_S3_SECRET_KEY"),
	Region:    os.Getenv("AWS_S3_REGION"),
	Bucket:    os.Getenv("AWS_S3_BUCKET"),
	ObjectKey: "dir/config.json",
}), json.Parser()); err != nil {
	log.Fatalf("error loading config: %v", err)
}
```

### Reading raw bytes

The bundled `rawbytes` Provider can be used to read arbitrary bytes from a source, like a database or an HTTP call.

```go
package main

import (
	"fmt"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/rawbytes"
)

// Global koanf instance. Use . as the key path delimiter. This can be / or anything.
var k = koanf.New(".")

func main() {
	b := []byte(`{"type": "rawbytes", "parent1": {"child1": {"type": "rawbytes"}}}`)
	k.Load(rawbytes.Provider(b), json.Parser())
	fmt.Println("type is = ", k.String("parent1.child1.type"))
}
```

### Unmarshalling and marshalling
`Parser`s can be used to unmarshal and scan the values in a Koanf instance into a struct based on the field tags, and to marshal a Koanf instance back into serialized bytes, for example, back to JSON or YAML, to write back to files.

```go
package main

import (
	"fmt"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
)

// Global koanf instance. Use . as the key path delimiter. This can be / or anything.
var (
	k      = koanf.New(".")
	parser = json.Parser()
)

func main() {
	// Load JSON config.
	if err := k.Load(file.Provider("mock/mock.json"), parser); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Structure to unmarshal nested conf to.
	type childStruct struct {
		Name       string            `koanf:"name"`
		Type       string            `koanf:"type"`
		Empty      map[string]string `koanf:"empty"`
		GrandChild struct {
			Ids []int `koanf:"ids"`
			On  bool  `koanf:"on"`
		} `koanf:"grandchild1"`
	}

	var out childStruct

	// Quick unmarshal.
	k.Unmarshal("parent1.child1", &out)
	fmt.Println(out)

	// Unmarshal with advanced config.
	out = childStruct{}
	k.UnmarshalWithConf("parent1.child1", &out, koanf.UnmarshalConf{Tag: "koanf"})
	fmt.Println(out)

	// Marshal the instance back to JSON.
	// The paser instance can be anything, eg: json.Paser(), yaml.Parser() etc.
	b, _ := k.Marshal(parser)
	fmt.Println(string(b))
}
```

### Unmarshalling with flat paths

Sometimes it is necessary to unmarshal an assortment of keys from various nested structures into a flat target structure. This is possible with the `UnmarshalConf.FlatPaths` flag.

```go
package main

import (
	"fmt"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/providers/file"
)

// Global koanf instance. Use . as the key path delimiter. This can be / or anything.
var k = koanf.New(".")

func main() {
	// Load JSON config.
	if err := k.Load(file.Provider("mock/mock.json"), json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	type rootFlat struct {
		Type                        string            `koanf:"type"`
		Empty                       map[string]string `koanf:"empty"`
		Parent1Name                 string            `koanf:"parent1.name"`
		Parent1ID                   int               `koanf:"parent1.id"`
		Parent1Child1Name           string            `koanf:"parent1.child1.name"`
		Parent1Child1Type           string            `koanf:"parent1.child1.type"`
		Parent1Child1Empty          map[string]string `koanf:"parent1.child1.empty"`
		Parent1Child1Grandchild1IDs []int             `koanf:"parent1.child1.grandchild1.ids"`
		Parent1Child1Grandchild1On  bool              `koanf:"parent1.child1.grandchild1.on"`
	}

	// Unmarshal the whole root with FlatPaths: True.
	var o1 rootFlat
	k.UnmarshalWithConf("", &o1, koanf.UnmarshalConf{Tag: "koanf", FlatPaths: true})
	fmt.Println(o1)

	// Unmarshal a child structure of "parent1".
	type subFlat struct {
		Name                 string            `koanf:"name"`
		ID                   int               `koanf:"id"`
		Child1Name           string            `koanf:"child1.name"`
		Child1Type           string            `koanf:"child1.type"`
		Child1Empty          map[string]string `koanf:"child1.empty"`
		Child1Grandchild1IDs []int             `koanf:"child1.grandchild1.ids"`
		Child1Grandchild1On  bool              `koanf:"child1.grandchild1.on"`
	}

	var o2 subFlat
	k.UnmarshalWithConf("parent1", &o2, koanf.UnmarshalConf{Tag: "koanf", FlatPaths: true})
	fmt.Println(o2)
}
```

### Marshalling and writing config
It is possible to marshal and serialize the conf map into TOML, YAML etc.

### Setting default values.

koanf does not provide any special functions to set default values but uses the Provider interface to enable it.

#### From a map

The bundled `confmap` provider takes a `map[string]interface{}` that can be loaded into a koanf instance. 

```go
package main

import (
	"fmt"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
)

// Global koanf instance. Use "." as the key path delimiter. This can be "/" or any character.
var k = koanf.New(".")

func main() {
	// Load default values using the confmap provider.
	// We provide a flat map with the "." delimiter.
	// A nested map can be loaded by setting the delimiter to an empty string "".
	k.Load(confmap.Provider(map[string]interface{}{
		"parent1.name": "Default Name",
		"parent3.name": "New name here",
	}, "."), nil)

	// Load JSON config on top of the default values.
	if err := k.Load(file.Provider("mock/mock.json"), json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	// Load YAML config and merge into the previously loaded config (because we can).
	k.Load(file.Provider("mock/mock.yaml"), yaml.Parser())

	fmt.Println("parent's name is = ", k.String("parent1.name"))
	fmt.Println("parent's ID is = ", k.Int("parent1.id"))
}
```

#### From a struct 

The bundled `structs` provider can be used to read data from a struct to load into a koanf instance.

```go
package main

import (
	"fmt"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/providers/structs"
)

// Global koanf instance. Use "." as the key path delimiter. This can be "/" or any character.
var k = koanf.New(".")

type parentStruct struct {
	Name   string      `koanf:"name"`
	ID     int         `koanf:"id"`
	Child1 childStruct `koanf:"child1"`
}
type childStruct struct {
	Name        string            `koanf:"name"`
	Type        string            `koanf:"type"`
	Empty       map[string]string `koanf:"empty"`
	Grandchild1 grandchildStruct  `koanf:"grandchild1"`
}
type grandchildStruct struct {
	Ids []int `koanf:"ids"`
	On  bool  `koanf:"on"`
}
type sampleStruct struct {
	Type    string            `koanf:"type"`
	Empty   map[string]string `koanf:"empty"`
	Parent1 parentStruct      `koanf:"parent1"`
}

func main() {
	// Load default values using the structs provider.
	// We provide a struct along with the struct tag `koanf` to the
	// provider.
	k.Load(structs.Provider(sampleStruct{
		Type:  "json",
		Empty: make(map[string]string),
		Parent1: parentStruct{
			Name: "parent1",
			ID:   1234,
			Child1: childStruct{
				Name:  "child1",
				Type:  "json",
				Empty: make(map[string]string),
				Grandchild1: grandchildStruct{
					Ids: []int{1, 2, 3},
					On:  true,
				},
			},
		},
	}, "koanf"), nil)

	fmt.Printf("name is = `%s`\n", k.String("parent1.child1.name"))
}
```
### Merge behavior
#### Default behavior
The default behavior when you create Koanf this way is: `koanf.New(delim)` that the latest loaded configuration will
merge with the previous one.

For example:
`first.yml`
```yaml
key: [1,2,3]
```
`second.yml`
```yaml
key: 'string'
```
When `second.yml` is loaded it will override the type of the `first.yml`.

If this behavior is not desired, you can merge 'strictly'. In the same scenario, `Load` will return an error.

```go
package main

import (
	"errors"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
)

var conf = koanf.Conf{
	Delim:       ".",
	StrictMerge: true,
}
var k = koanf.NewWithConf(conf)

func main() {
	yamlPath := "mock/mock.yml"
	if err := k.Load(file.Provider(yamlPath), yaml.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	jsonPath := "mock/mock.json"
	if err := k.Load(file.Provider(jsonPath), json.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}
}
```
**Note:** When merging different extensions, each parser can treat his types differently,
 meaning even though you the load same types there is a probability that it will fail with `StrictMerge: true`.

For example: merging JSON and YAML will most likely fail because JSON treats integers as float64 and YAML treats them as integers.

### Order of merge and key case sensitivity

- Config keys are case-sensitive in koanf. For example, `app.server.port` and `APP.SERVER.port` are not the same.
- koanf does not impose any ordering on loading config from various providers. Every successive `Load()` or `Load()` merges new config into existing config. That means it is possible to load environment variables first, then files on top of it, and then command line variables on top of it, or any such order.

### Custom Providers and Parsers

A Provider can provide a nested map[string]interface{} config that can be loaded into koanf with `koanf.Load()` or raw bytes that can be parsed with a Parser (loaded using `koanf.Load()`.

Writing Providers and Parsers are easy. See the bundled implementations in the `providers` and `parses` directory.

### Custom merge strategies

By default, when merging two config sources using `Load()`, koanf recursively merges keys of nested maps (`map[string]interface{}`),
while static values are overwritten (slices, strings, etc). This behaviour can be changed by providing a custom merge function with the `WithMergeFunc` option.

```go
package main

import (
	"errors"
	"log"

	"github.com/knadh/koanf"
	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
)

var conf = koanf.Conf{
	Delim:       ".",
	StrictMerge: true,
}
var k = koanf.NewWithConf(conf)

func main() {
	yamlPath := "mock/mock.yml"
	if err := k.Load(file.Provider(yamlPath), yaml.Parser()); err != nil {
		log.Fatalf("error loading config: %v", err)
	}

	jsonPath := "mock/mock.json"
	if err := k.Load(file.Provider(jsonPath), json.Parser(), koanf.WithMergeFunc(func(src, dest map[string]interface{}) error {
     // Your custom logic, copying values from src into dst
     return nil
    })); err != nil {
		log.Fatalf("error loading config: %v", err)
	}
}
```

## API

### Instantiation methods
| Method                          | Description                                                                                                                                                                 | 
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `New(delim string) *Koanf`      | Returns a new instance of Koanf. delim is the delimiter to use when specifying config key paths, for instance a `.` for `parent.child.key` or a `/` for `parent/child/key`. |
| `NewWithConf(conf Conf) *Koanf` | Returns a new instance of Koanf depending on the `Koanf.Conf` configurations.                                                                                               |

### Structs
| Struct                          | Description                                                                                                      |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Koanf                           | Koanf is the configuration apparatus.                                                                            |
| Conf                            | Conf is the Koanf configuration, that includes `Delim` and `StrictMerge`.                                        |
| UnmarshalConf                   | UnmarshalConf represents configuration options used by `Unmarshal()` to unmarshal conf maps in arbitrary structs |

### Bundled providers

| Package             | Provider                                                      | Description                                                                                                                                                                           |
| ------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| providers/file      | `file.Provider(filepath string)`                              | Reads a file and returns the raw bytes to be parsed.                                                                                                                                  |
| providers/fs      | `fs.Provider(f fs.FS, filepath string)`                              | (**Experimental**) Reads a file from fs.FS and returns the raw bytes to be parsed. The provider requires `go v1.16` or higher.                                            |
| providers/basicflag | `basicflag.Provider(f *flag.FlagSet, delim string)`           | Takes an stdlib `flag.FlagSet`                                                                                                                                                        |
| providers/posflag   | `posflag.Provider(f *pflag.FlagSet, delim string)`            | Takes an `spft3/pflag.FlagSet` (advanced POSIX compatible flags with multiple types) and provides a nested config map based on delim.                                                 |
| providers/env       | `env.Provider(prefix, delim string, f func(s string) string)` | Takes an optional prefix to filter env variables by, an optional function that takes and returns a string to transform env variables, and returns a nested config map based on delim. |
| providers/confmap   | `confmap.Provider(mp map[string]interface{}, delim string)`   | Takes a premade `map[string]interface{}` conf map. If delim is provided, the keys are assumed to be flattened, thus unflattened using delim.                                          |
| providers/structs   | `structs.Provider(s interface{}, tag string)`                 | Takes a struct and struct tag.                                                                                                                                                        |
| providers/s3        | `s3.Provider(s3.S3Config{})`                                  | Takes a s3 config struct.                                                                                                                                                             |
| providers/rawbytes  | `rawbytes.Provider(b []byte)`                                 | Takes a raw `[]byte` slice to be parsed with a koanf.Parser                                                                                                                           |
| providers/vault     | `vault.Provider(vault.Config{})`                              | Hashicorp Vault provider                                                                                                                           |
| providers/appconfig     | `vault.AppConfig(appconfig.Config{})`                              | AWS AppConfig provider                                                                                                                           |

### Bundled parsers

| Package      | Parser                           | Description                                                                                                                                               |
| ------------ | -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| parsers/json       | `json.Parser()`                  | Parses JSON bytes into a nested map                                                                                                                       |
| parsers/yaml       | `yaml.Parser()`                  | Parses YAML bytes into a nested map                                                                                                                       |
| parsers/toml       | `toml.Parser()`                  | Parses TOML bytes into a nested map                                                                                                                       |
| parsers/dotenv     | `dotenv.Parser()`              | Parses DotEnv bytes into a flat map                                                                                                                       |
| parsers/hcl        | `hcl.Parser(flattenSlices bool)` | Parses Hashicorp HCL bytes into a nested map. `flattenSlices` is recommended to be set to true. [Read more](https://github.com/hashicorp/hcl/issues/162). |
| parsers/nestedtext | `nestedtext.Parser()`              | Parses NestedText bytes into a flat map                                                                                                                 |

### Instance functions

| Method                                                                 | Description                                                  |
|------------------------------------------------------------------------| ------------------------------------------------------------ |
| `Load(p Provider, pa Parser, opts ...Option) error`                    | Loads config from a Provider. If a koanf.Parser is provided, the config is assumed to be raw bytes that's then parsed with the Parser. |
| `Keys() []string`                                                      | Returns the list of flattened key paths that can be used to access config values |
| `KeyMap() map[string][]string`                                         | Returns a map of all possible key path combinations possible in the loaded nested conf map |
| `All() map[string]interface{}`                                         | Returns a flat map of flattened key paths and their corresponding config values |
| `Raw() map[string]interface{}`                                         | Returns a copy of the raw nested conf map                    |
| `Print()`                                                              | Prints a human readable copy of the flattened key paths and their values for debugging |
| `Sprint()`                                                             | Returns a human readable copy of the flattened key paths and their values for debugging |
| `Cut(path string) *Koanf`                                              | Cuts the loaded nested conf map at the given path and returns a new Koanf instance with the children |
| `Copy() *Koanf`                                                        | Returns a copy of the Koanf instance                         |
| `Merge(*Koanf)`                                                        | Merges the config map of a Koanf instance into the current instance |
| `Delete(path string)`                                                  | Delete the value at the given path, and does nothing if path doesn't exist. |
| `MergeAt(in *Koanf, path string)`                                      | Merges the config map of a Koanf instance into the current instance, at the given key path. |
| `Unmarshal(path string, o interface{}) error`                          | Scans the given nested key path into a given struct (like json.Unmarshal) where fields are denoted by the `koanf` tag |
| `UnmarshalWithConf(path string, o interface{}, c UnmarshalConf) error` | Like Unmarshal but with customizable options                 |

### Getter functions

|                                               |                                                                                                                                                                                            |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `Get(path string) interface{}`                | Returns the value for the given key path, and if it doesn’t exist, returns nil                                                                                                             |
| `Exists(path string) bool`                    | Returns true if the given key path exists in the conf map                                                                                                                                  |
| `Int64(path string) int64`                    |                                                                                                                                                                                            |
| `Int64s(path string) []int64`                 |                                                                                                                                                                                            |
| `Int64Map(path string) map[string]int64`      |                                                                                                                                                                                            |
| `Int(path string) int`                        |                                                                                                                                                                                            |
| `Ints(path string) []int`                     |                                                                                                                                                                                            |
| `IntMap(path string) map[string]int`          |                                                                                                                                                                                            |
| `Float64(path string) float64`                |                                                                                                                                                                                            |
| `Float64s(path string) []float64`             |                                                                                                                                                                                            |
| `Float64Map(path string) map[string]float64`  |                                                                                                                                                                                            |
| `Duration(path string) time.Duration`         | Returns the time.Duration value of the given key path if it’s numeric (attempts a parse+convert if string) or a string representation like "3s".                                           |
| `Time(path, layout string) time.Time`         | Parses the string value of the the given key path with the given layout format and returns time.Time. If the key path is numeric, treats it as a UNIX timestamp and returns its time.Time. |
| `String(path string) string`                  |                                                                                                                                                                                            |
| `Strings(path string) []string`               |                                                                                                                                                                                            |
| `StringMap(path string) map[string]string`    |                                                                                                                                                                                            |
| `StringsMap(path string) map[string][]string` |                                                                                                                                                                                            |
| `Byte(path string) []byte`                    |                                                                                                                                                                                            |
| `Bool(path string) bool`                      |                                                                                                                                                                                            |
| `Bools(path string) []bool`                   |                                                                                                                                                                                            |
| `BoolMap(path string) map[string]bool`        |                                                                                                                                                                                            |
| `MapKeys(path string) []string`               | Returns the list of keys in any map                                                                                                                                                        |
| `Slices(path string) []Koanf`                 | Returns `[]map[string]interface{}`, a slice of confmaps loaded into a slice of new Koanf instances.							                                                                           |

### Alternative to viper

koanf is a lightweight alternative to the popular [spf13/viper](https://github.com/spf13/viper). It was written as a result of multiple stumbling blocks encountered with some of viper's fundamental flaws.

- viper breaks JSON, YAML, TOML, HCL language specs by [forcibly lowercasing keys](https://github.com/spf13/viper/pull/635).
- Significantly bloats [build sizes](https://github.com/knadh/koanf/wiki/Comparison-with-spf13-viper).
- Tightly couples config parsing with file extensions.
- Has poor semantics and abstractions. Commandline, env, file etc. and various parses are hardcoded in the core. There are no primitives that can be extended.
- Pulls a large number of [third party dependencies](https://github.com/spf13/viper/issues/707) into the core package. For instance, even if you do not use YAML or flags, the dependencies are still pulled as a result of the coupling.
- Imposes arbitrary ordering conventions (eg: flag -> env -> config etc.)
- `Get()` returns references to slices and maps. Mutations made outside change the underlying values inside the conf map.
- Does non-idiomatic things such as [throwing away O(1) on flat maps](https://github.com/spf13/viper/blob/3b4aca75714a37276c4b1883630bd98c02498b73/viper.go#L1524).
- There are a large number of [open issues](https://github.com/spf13/viper/issues).
