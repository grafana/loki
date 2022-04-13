# Logfmt Encoder

This package implements logfmt for
[zap](https://github.com/uber-go/zap).

## Usage

The encoder is simple to use.

```go
package main

import (
	"os"

	"github.com/jsternberg/zap-logfmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	config := zap.NewProductionEncoderConfig()
	logger := zap.New(zapcore.NewCore(
		zaplogfmt.NewEncoder(config),
		os.Stdout,
		zapcore.DebugLevel,
	))
	logger.Info("Hello World")
}
```

To use RFC3339 output for the time instead of an integer timestamp, you
can do this:

```go
package main

import (
	"os"

	"github.com/jsternberg/zap-logfmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = func(ts time.Time, encoder zapcore.PrimitiveArrayEncoder) {
		encoder.AppendString(ts.UTC().Format(time.RFC3339))
	}
	logger := zap.New(zapcore.NewCore(
		zaplogfmt.NewEncoder(config),
		os.Stdout,
		zapcore.DebugLevel,
	))
	logger.Info("Hello World")
}
```

## Limitations

It is not possible to log an array, channel, function, map, slice, or
struct. Functions and channels since they don't really have a suitable
representation to begin with. Logfmt does not have a method of
outputting arrays or maps so arrays, slices, maps, and structs cannot be
rendered.

## Namespaces

Namespaces are supported. If a namespace is opened, all of the keys will
be prepended with the namespace name. For example, with the namespace
`foo` and the key `bar`, you would get a key of `foo.bar`.
