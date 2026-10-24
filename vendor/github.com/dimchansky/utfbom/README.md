# utfbom [![Go Reference](https://pkg.go.dev/badge/github.com/dimchansky/utfbom.svg)](https://pkg.go.dev/github.com/dimchansky/utfbom) [![License](https://img.shields.io/:license-apache-blue.svg)](https://opensource.org/licenses/Apache-2.0) [![CI](https://github.com/dimchansky/utfbom/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/dimchansky/utfbom/actions/workflows/ci.yml) [![Coverage Status](https://coveralls.io/repos/github/dimchansky/utfbom/badge.svg?branch=master)](https://coveralls.io/github/dimchansky/utfbom?branch=master)

utfbom detects and removes a leading Unicode byte order mark (BOM) from an `io.Reader`. It can also report the encoding indicated by that BOM.

The remaining bytes pass through unchanged. The package does not decode text, convert UTF-16 or UTF-32 to UTF-8, validate Unicode, or guess an encoding when there is no BOM.

## Installation

    go get github.com/dimchansky/utfbom

Requires Go 1.19 or later. No dependencies outside the standard library.

## Example

```go
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/dimchansky/utfbom"
)

func main() {
	r, enc := utfbom.Skip(strings.NewReader("\xEF\xBB\xBFhello"))

	data, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s %q\n", enc, data) // UTF8 "hello"
}
```

If you don't need the encoding, use `SkipOnly`:

```go
data, err := io.ReadAll(utfbom.SkipOnly(file))
```

## Supported BOMs

| Encoding             | BOM bytes     | Constant            |
|----------------------|---------------|---------------------|
| UTF-8                | `EF BB BF`    | `UTF8`              |
| UTF-16 big-endian    | `FE FF`       | `UTF16BigEndian`    |
| UTF-16 little-endian | `FF FE`       | `UTF16LittleEndian` |
| UTF-32 big-endian    | `00 00 FE FF` | `UTF32BigEndian`    |
| UTF-32 little-endian | `FF FE 00 00` | `UTF32LittleEndian` |

`FF FE 00 00` is reported as UTF-32 little-endian, although the same bytes could be a UTF-16 little-endian BOM followed by a NUL character.

## Behavior

- At most one BOM is removed: the one at the current position of the reader you pass in. BOM-like bytes later in the stream are data and stay as they are.
- `Unknown` means no supported BOM was detected. Plain UTF-8 text without a BOM is also `Unknown`.
- After the BOM is removed, UTF-16 and UTF-32 data is still UTF-16 and UTF-32.
- `Skip` and `SkipOnly` read up to 4 bytes from the source before returning, so they can block.
- An error that occurs while reading those bytes is returned once by `Read`, after the bytes that remain after BOM removal. Later calls to `Read` go to the underlying reader.
- If probing stops with a non-EOF error (including `io.ErrNoProgress` after 100 consecutive reads returning `(0, nil)`) before 4 bytes are read, a UTF-16 BOM (`FE FF` / `FF FE`) is not recognized: the encoding is `Unknown` and the bytes are passed through. Complete UTF-8 and UTF-32 BOMs are still recognized.
- Calling `Skip` on a `*utfbom.Reader` returns that same reader and the encoding it detected. This does not apply to other readers that wrap a `*utfbom.Reader`.
- The returned reader does not close the source; close it yourself.

## Development

    make check   # go vet, golangci-lint, tests with -race
    make fmt     # format the code

`make lint` and `make fmt` need [golangci-lint](https://golangci-lint.run/) v2.15.0 or later.

## License

[Apache License 2.0](LICENSE)
