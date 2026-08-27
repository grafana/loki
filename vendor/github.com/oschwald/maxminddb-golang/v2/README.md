# MaxMind DB Reader for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/oschwald/maxminddb-golang/v2.svg)](https://pkg.go.dev/github.com/oschwald/maxminddb-golang/v2)

This is a Go reader for the MaxMind DB format. Although this can be used to
read [GeoLite2](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data)
and [GeoIP2](https://www.maxmind.com/en/geoip2-databases) databases,
[geoip2](https://github.com/oschwald/geoip2-golang) provides a higher-level API
for doing so.

This is not an official MaxMind API.

## Installation

```bash
go get github.com/oschwald/maxminddb-golang/v2
```

## Version 2 Features

Version 2 includes significant improvements:

- **Modern API**: Uses `netip.Addr` instead of `net.IP` for better performance
- **Custom Unmarshaling**: Implement `CursorUnmarshaler` for
  reflection-free custom decoding
- **Network Iteration**: Iterate over all networks in a database with
  `Networks()` and `NetworksWithin()`
- **Enhanced Performance**: Optimized data structures and decoding paths
- **Better Error Handling**: More detailed error types and improved debugging
- **Integrity Checks**: Validate databases with `Reader.Verify()` and access
  metadata helpers such as `Metadata.BuildTime()`

See [MIGRATION.md](MIGRATION.md) for guidance on updating existing v1 code.

## Quick Start

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

func main() {
	db, err := maxminddb.Open("GeoLite2-City.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("81.2.69.142")
	if err != nil {
		log.Fatal(err)
	}

	var record struct {
		Country struct {
			ISOCode string            `maxminddb:"iso_code"`
			Names   map[string]string `maxminddb:"names"`
		} `maxminddb:"country"`
		City struct {
			Names map[string]string `maxminddb:"names"`
		} `maxminddb:"city"`
	}

	err = db.Lookup(ip).Decode(&record)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Country: %s (%s)\n", record.Country.Names["en"], record.Country.ISOCode)
	fmt.Printf("City: %s\n", record.City.Names["en"])
}
```

## Usage Patterns

### Basic Lookup

```go
db, err := maxminddb.Open("GeoLite2-City.mmdb")
if err != nil {
	log.Fatal(err)
}
defer db.Close()

var record any
ip := netip.MustParseAddr("1.2.3.4")
err = db.Lookup(ip).Decode(&record)
```

### Custom Struct Decoding

```go
type City struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
		Names   struct {
			English string `maxminddb:"en"`
			German  string `maxminddb:"de"`
		} `maxminddb:"names"`
	} `maxminddb:"country"`
}

var city City
err = db.Lookup(ip).Decode(&city)
```

### High-Performance Custom Unmarshaling

For application-owned structs, `maxminddb-gen` can generate an
`UnmarshalMaxMindDBCursor` method that avoids reflection. The generator is
versioned with this module and remains optional; types with neither generated
nor handwritten custom unmarshaling methods continue to use reflection.

Add the tool to the consuming module's `go.mod` and add a generation directive
in the package that owns the target types:

```go.mod
tool github.com/oschwald/maxminddb-golang/v2/maxminddb-gen
```

```go
//go:generate go tool maxminddb-gen $GOFILE
```

This discovers the exported structs declared in the directive's source file.
For `models.go`, it writes `models_maxminddb.go`; recognized build suffixes and
source build constraints are preserved. Constrained inputs must match the
generation environment; multiple inputs share the intersection of their
constraints. Use `-output` to override the default. Run `go generate ./...` and
check the generated file into source control. See
[`maxminddb-gen/README.md`](maxminddb-gen/README.md) for supported types,
diagnostics, and reproducible CI usage.

For new handwritten decoders, implement `mmdbdata.CursorUnmarshaler`. Cursor
reads return an opaque successor positioned after the decoded value, allowing
nested custom decoding to continue without rescanning it.

The older `UnmarshalMaxMindDB(*mmdbdata.Decoder) error` callback is deprecated.
It remains supported throughout v2 but is planned for removal in v3; see
[GitHub #224](https://github.com/oschwald/maxminddb-golang/issues/224). When a
type implements both callbacks, `UnmarshalMaxMindDBCursor` takes precedence.

```go
type Label string

func (label *Label) UnmarshalMaxMindDBCursor(
	cursor mmdbdata.Cursor,
) (mmdbdata.Cursor, error) {
	value, next, err := cursor.ReadString()
	if err != nil {
		return mmdbdata.Cursor{}, mmdbdata.NormalizeUnmarshalError[Label](err)
	}
	*label = Label(value)
	return next, nil
}
```

### Network Iteration

```go
// Iterate over all networks in the database
for result := range db.Networks() {
	var record struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
	}
	err := result.Decode(&record)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %s\n", result.Prefix(), record.Country.ISOCode)
}

// Iterate over networks within a specific prefix
prefix := netip.MustParsePrefix("192.168.0.0/16")
for result := range db.NetworksWithin(prefix) {
	// Process networks within 192.168.0.0/16
}
```

### Path-Based Decoding

```go
var countryCode string
err = db.Lookup(ip).DecodePath(&countryCode, "country", "iso_code")

var cityName string
err = db.Lookup(ip).DecodePath(&cityName, "city", "names", "en")
```

## Supported Database Types

This library supports **all MaxMind DB (.mmdb) format databases**, including:

**MaxMind Official Databases:**

- **GeoLite/GeoIP City**: Comprehensive location data including city, country,
  subdivisions
- **GeoLite/GeoIP Country**: Country-level geolocation data
- **GeoLite ASN**: Autonomous System Number and organization data
- **GeoIP Anonymous IP**: Anonymous network and proxy detection
- **GeoIP Enterprise**: Enhanced City data with additional business fields
- **GeoIP ISP**: Internet service provider information
- **GeoIP Domain**: Second-level domain data
- **GeoIP Connection Type**: Connection type identification

**Third-Party Databases:**

- **DB-IP databases**: Compatible with DB-IP's .mmdb format databases
- **IPinfo databases**: Works with IPinfo's MaxMind DB format files
- **Custom databases**: Any database following the MaxMind DB file format
  specification

The library is format-agnostic and will work with any valid .mmdb file
regardless of the data provider.

## Performance Tips

1. **Reuse Reader instances**: Lookups, decoding, and iteration are safe to run
   concurrently. `Close` invalidates outstanding results, Reader-backed cursors,
   and their derived traversal handles; it must not run concurrently with their
   use and should run only after readers are done.
2. **Use specific structs**: Only decode the fields you need rather than using
   `any`
3. **Generate a decoder**: For high-throughput applications, use
   `maxminddb-gen`, or implement `CursorUnmarshaler` for custom decoding
4. **Consider caching**: Use `Result.Offset()` as a cache key for database
   records

## Getting Database Files

### Free GeoLite2 Databases

Download from
[MaxMind's GeoLite page](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data).

## Documentation

- [Go Reference](https://pkg.go.dev/github.com/oschwald/maxminddb-golang/v2)
- [MaxMind DB File Format Specification](https://maxmind.github.io/MaxMind-DB/)

## Requirements

- Go 1.25 or later
- MaxMind DB file in .mmdb format

## Contributing

Contributions welcome! Please fork the repository and open a pull request with
your changes.

## License

This is free software, licensed under the ISC License.
