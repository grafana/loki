go.yaml.in/yaml
===============

YAML Support for the Go Language


## Introduction

The `yaml` package enables [Go](https://go.dev/) programs to comfortably encode
and decode [YAML](https://yaml.org/) values.

It was originally developed within [Canonical](https://www.canonical.com) as
part of the [juju](https://juju.ubuntu.com) project, and is based on a pure Go
port of the well-known [libyaml](http://pyyaml.org/wiki/LibYAML) C library to
parse and generate YAML data quickly and reliably.


## Project Status

This project started as a fork of the extremely popular [go-yaml](
https://github.com/go-yaml/yaml/)
project, and is being maintained by the official [YAML organization](
https://github.com/yaml/).

The YAML team took over ongoing maintenance and development of the project after
discussion with go-yaml's author, @niemeyer, following his decision to
[label the project repository as "unmaintained"](
https://github.com/go-yaml/yaml/blob/944c86a7d2/README.md) in April 2025.

We have put together a team of dedicated maintainers including representatives
of go-yaml's most important downstream projects.

We will strive to earn the trust of the various go-yaml forks to switch back to
this repository as their upstream.

Please [contact us](https://cloud-native.slack.com/archives/C08PPAT8PS7) if you
would like to contribute or be involved.


### Version Intentions

Versions `v1`, `v2`, and `v3` will remain as **frozen legacy**.
They will receive **security-fixes only** so that existing consumers keep
working without breaking changes.

All ongoing work, including new features and routine bug-fixes, will happen in
**`v4`**.
If you’re starting a new project or upgrading an existing one, please use the
`go.yaml.in/yaml/v4` import path.


## Compatibility

The `yaml` package supports most of YAML 1.2, but preserves some behavior from
1.1 for backwards compatibility.

Specifically, v3 of the `yaml` package:

* Supports YAML 1.1 bools (`yes`/`no`, `on`/`off`) as long as they are being
  decoded into a typed bool value.
  Otherwise they behave as a string.
  Booleans in YAML 1.2 are `true`/`false` only.
* Supports octals encoded and decoded as `0777` per YAML 1.1, rather than
  `0o777` as specified in YAML 1.2, because most parsers still use the old
  format.
  Octals in the `0o777` format are supported though, so new files work.
* Does not support base-60 floats.
  These are gone from YAML 1.2, and were actually never supported by this
  package as it's clearly a poor choice.


## Installation and Usage

The import path for the package is *go.yaml.in/yaml/v4*.

To install it, run:

```bash
go get go.yaml.in/yaml/v4
```


## API Documentation

See: <https://pkg.go.dev/go.yaml.in/yaml/v4>


## API Stability

The package API for yaml v3 will remain stable as described in [gopkg.in](
https://gopkg.in).


## Example

```go
package main

import (
	"fmt"
	"log"

	"go.yaml.in/yaml/v4"
)

var data = `
a: Easy!
b:
  c: 2
  d: [3, 4]
`

// Note: struct fields must be public in order for unmarshal to
// correctly populate the data.
type T struct {
	A string
	B struct {
		RenamedC int   `yaml:"c"`
		D	[]int `yaml:",flow"`
	}
}

func main() {
	t := T{}

	err := yaml.Unmarshal([]byte(data), &t)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Printf("--- t:\n%v\n\n", t)

	d, err := yaml.Marshal(&t)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Printf("--- t dump:\n%s\n\n", string(d))

	m := make(map[any]any)

	err = yaml.Unmarshal([]byte(data), &m)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Printf("--- m:\n%v\n\n", m)

	d, err = yaml.Marshal(&m)
	if err != nil {
		log.Fatalf("error: %v", err)
	}
	fmt.Printf("--- m dump:\n%s\n\n", string(d))
}
```

This example will generate the following output:

```
--- t:
{Easy! {2 [3 4]}}

--- t dump:
a: Easy!
b:
  c: 2
  d: [3, 4]


--- m:
map[a:Easy! b:map[c:2 d:[3 4]]]

--- m dump:
a: Easy!
b:
  c: 2
  d:
  - 3
  - 4
```


## Development and Testing with `make`

This project's makefile (`GNUmakefile`) is set up to support all of the
project's testing, automation and development tasks in a completely
deterministic way.

Some `make` commands are:

* `make test`
* `make lint tidy`
* `make test-shell`
* `make test v=1`
* `make test o='-foo --bar=baz'`  # Add extra CLI options
* `make test GO-VERSION=1.2.34`
* `make test GO_YAML_PATH=/usr/local/go/bin`
* `make shell`  # Start a shell with the local `go` environment
* `make shell GO-VERSION=1.2.34`
* `make distclean`  # Remove all generated files including `.cache/`


### Dependency Auto-install

By default, this makefile will not use your system's Go installation, or any
other system tools that it needs.

The only things from your system that it relies on are:
* Linux or macOS
* GNU `make` (3.81+)
* `git`
* `bash`
* `curl`

Everything else, including Go and Go utils, are installed and cached as they
are needed by the makefile (under `.cache/`).

> **Note**: Use `make shell` to get a subshell with the same environment that
> the makefile set up for its commands.


### Using your own Go

If you want to use your own Go installation and utils, export `GO_YAML_PATH` to
the directory containing the `go` binary.

Use something like this:

```
export GO_YAML_PATH=$(dirname "$(command -v go)")
make <rule>
# or:
make <rule> GO_YAML_PATH=$(dirname "$(command -v go)")
```

> **Note:** `GO-VERSION` and `GO_YAML_PATH` are mutually exclusive.
> When `GO_YAML_PATH` is set, the Makefile uses your own Go installation and
> ignores any `GO-VERSION` setting.


## The `go-yaml` CLI Tool

This repository includes a `go-yaml` CLI tool which can be used to understand
the internal stages and final results of YAML processing with the go-yaml
library.

We strongly encourage you to show pertinent output from this command when
reporting and discussing issues.

```bash
make go-yaml
./go-yaml --help
./go-yaml <<< '
foo: &a1 bar
*a1: baz
' -n        # Show value on decoded Node structs (formatted in YAML)
```

You can also install it with:

```bash
go install go.yaml.in/yaml/v4/cmd/go-yaml@latest
```


## License

The yaml package is licensed under the MIT and Apache License 2.0 licenses.
Please see the LICENSE file for details.
