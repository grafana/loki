# GeoIP2 Reader for Go

[![PkgGoDev](https://pkg.go.dev/badge/github.com/oschwald/geoip2-golang/v2)](https://pkg.go.dev/github.com/oschwald/geoip2-golang/v2)

This library reads MaxMind
[GeoLite2](https://dev.maxmind.com/geoip/geoip2/geolite2/) and
[GeoIP2](https://www.maxmind.com/en/geolocation_landing) databases.

This library is built using
[the Go maxminddb reader](https://github.com/oschwald/maxminddb-golang). All
data for the database record is decoded using this library. Version 2.0
provides significant performance improvements with 56% fewer allocations and
34% less memory usage compared to v1. Version 2.0 also adds `Network` and
`IPAddress` fields to all result structs, and includes a `HasData()` method to
easily check if data was found. If you only need several fields, you may get
superior performance by using maxminddb's `Lookup` directly with a result
struct that only contains the required fields. (See
[example_test.go](https://github.com/oschwald/maxminddb-golang/blob/v2/example_test.go)
in the maxminddb repository for an example of this.)

## Installation

```
go get github.com/oschwald/geoip2-golang/v2
```

## New in v2

Version 2.0 includes several major improvements:

- **Performance**: 56% fewer allocations and 34% less memory usage
- **Modern API**: Uses `netip.Addr` instead of `net.IP` for better performance
- **Network Information**: All result structs now include `Network` and
  `IPAddress` fields
- **Data Validation**: New `HasData()` method to easily check if data was found
- **Structured Names**: Replaced `map[string]string` with typed `Names` struct
  for better performance
- **Go 1.24 Support**: Uses `omitzero` JSON tags to match MaxMind database
  behavior

## Migration

See [MIGRATION.md](MIGRATION.md) for step-by-step guidance on upgrading from
v1.

## Usage

[See GoDoc](https://pkg.go.dev/github.com/oschwald/geoip2-golang/v2) for
documentation and examples.

## Example

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-City.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	// If you are using strings that may be invalid, use netip.ParseAddr and check for errors
	ip, err := netip.ParseAddr("81.2.69.142")
	if err != nil {
		log.Fatal(err)
	}
	record, err := db.City(ip)
	if err != nil {
		log.Fatal(err)
	}
	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}
	fmt.Printf("Portuguese (BR) city name: %v\n", record.City.Names.BrazilianPortuguese)
	if len(record.Subdivisions) > 0 {
		fmt.Printf("English subdivision name: %v\n", record.Subdivisions[0].Names.English)
	}
	fmt.Printf("Russian country name: %v\n", record.Country.Names.Russian)
	fmt.Printf("ISO country code: %v\n", record.Country.ISOCode)
	fmt.Printf("Time zone: %v\n", record.Location.TimeZone)
	if record.Location.HasCoordinates() {
		fmt.Printf("Coordinates: %v, %v\n", *record.Location.Latitude, *record.Location.Longitude)
	}
	// Output:
	// Portuguese (BR) city name: Londres
	// English subdivision name: England
	// Russian country name: Великобритания
	// ISO country code: GB
	// Time zone: Europe/London
	// Coordinates: 51.5142, -0.0931
}

```

## Requirements

- Go 1.24 or later
- MaxMind GeoIP2 or GeoLite2 database files (.mmdb format)

## Getting Database Files

### GeoLite2 (Free)

Download free GeoLite2 databases from
[MaxMind's website](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data).
Registration required.

### GeoIP2 (Commercial)

Purchase GeoIP2 databases from
[MaxMind](https://www.maxmind.com/en/geoip2-databases) for enhanced accuracy
and additional features.

## Database Examples

This library supports all MaxMind GeoIP2 and GeoLite2 database types. Below are
examples for each database type:

### City Database

The City database provides the most comprehensive geolocation data, including
city, subdivision, country, and precise location information.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-City.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("128.101.101.101")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.City(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("City: %v\n", record.City.Names.English)
	fmt.Printf("Subdivision: %v\n", record.Subdivisions[0].Names.English)
	fmt.Printf("Country: %v (%v)\n", record.Country.Names.English, record.Country.ISOCode)
	fmt.Printf("Continent: %v (%v)\n", record.Continent.Names.English, record.Continent.Code)
	fmt.Printf("Postal Code: %v\n", record.Postal.Code)
	if record.Location.HasCoordinates() {
		fmt.Printf("Location: %v, %v\n", *record.Location.Latitude, *record.Location.Longitude)
	}
	fmt.Printf("Time Zone: %v\n", record.Location.TimeZone)
	fmt.Printf("Network: %v\n", record.Traits.Network)
	fmt.Printf("IP Address: %v\n", record.Traits.IPAddress)
}
```

### Country Database

The Country database provides country-level geolocation data.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-Country.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("81.2.69.142")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.Country(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("Country: %v (%v)\n", record.Country.Names.English, record.Country.ISOCode)
	fmt.Printf("Continent: %v (%v)\n", record.Continent.Names.English, record.Continent.Code)
	fmt.Printf("Is in EU: %v\n", record.Country.IsInEuropeanUnion)
	fmt.Printf("Network: %v\n", record.Traits.Network)
	fmt.Printf("IP Address: %v\n", record.Traits.IPAddress)

	if record.RegisteredCountry.Names.English != "" {
		fmt.Printf("Registered Country: %v (%v)\n",
			record.RegisteredCountry.Names.English, record.RegisteredCountry.ISOCode)
	}
}
```

### ASN Database

The ASN database provides Autonomous System Number and organization
information.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoLite2-ASN.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("1.128.0.0")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.ASN(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("ASN: %v\n", record.AutonomousSystemNumber)
	fmt.Printf("Organization: %v\n", record.AutonomousSystemOrganization)
	fmt.Printf("Network: %v\n", record.Network)
	fmt.Printf("IP Address: %v\n", record.IPAddress)
}
```

### Anonymous IP Database

The Anonymous IP database identifies various types of anonymous and proxy
networks.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-Anonymous-IP.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("81.2.69.142")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.AnonymousIP(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("Is Anonymous: %v\n", record.IsAnonymous)
	fmt.Printf("Is Anonymous VPN: %v\n", record.IsAnonymousVPN)
	fmt.Printf("Is Hosting Provider: %v\n", record.IsHostingProvider)
	fmt.Printf("Is Public Proxy: %v\n", record.IsPublicProxy)
	fmt.Printf("Is Residential Proxy: %v\n", record.IsResidentialProxy)
	fmt.Printf("Is Tor Exit Node: %v\n", record.IsTorExitNode)
	fmt.Printf("Network: %v\n", record.Network)
	fmt.Printf("IP Address: %v\n", record.IPAddress)
}
```

### Enterprise Database

The Enterprise database provides the most comprehensive data, including all
City database fields plus additional enterprise features.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-Enterprise.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("128.101.101.101")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.Enterprise(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	// Basic location information
	fmt.Printf("City: %v\n", record.City.Names.English)
	fmt.Printf("Country: %v (%v)\n", record.Country.Names.English, record.Country.ISOCode)
	if record.Location.HasCoordinates() {
		fmt.Printf("Location: %v, %v\n", *record.Location.Latitude, *record.Location.Longitude)
	}

	// Enterprise-specific fields
	fmt.Printf("ISP: %v\n", record.Traits.ISP)
	fmt.Printf("Organization: %v\n", record.Traits.Organization)
	fmt.Printf("ASN: %v (%v)\n", record.Traits.AutonomousSystemNumber,
		record.Traits.AutonomousSystemOrganization)
	fmt.Printf("Connection Type: %v\n", record.Traits.ConnectionType)
	fmt.Printf("Domain: %v\n", record.Traits.Domain)
	fmt.Printf("User Type: %v\n", record.Traits.UserType)
	fmt.Printf("Static IP Score: %v\n", record.Traits.StaticIPScore)
	fmt.Printf("Is Anycast: %v\n", record.Traits.IsAnycast)
	fmt.Printf("Is Legitimate Proxy: %v\n", record.Traits.IsLegitimateProxy)

	// Mobile carrier information (if available)
	if record.Traits.MobileCountryCode != "" {
		fmt.Printf("Mobile Country Code: %v\n", record.Traits.MobileCountryCode)
		fmt.Printf("Mobile Network Code: %v\n", record.Traits.MobileNetworkCode)
	}

	fmt.Printf("Network: %v\n", record.Traits.Network)
	fmt.Printf("IP Address: %v\n", record.Traits.IPAddress)
}
```

### ISP Database

The ISP database provides ISP, organization, and ASN information.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-ISP.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("1.128.0.0")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.ISP(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("ISP: %v\n", record.ISP)
	fmt.Printf("Organization: %v\n", record.Organization)
	fmt.Printf("ASN: %v (%v)\n", record.AutonomousSystemNumber,
		record.AutonomousSystemOrganization)

	// Mobile carrier information (if available)
	if record.MobileCountryCode != "" {
		fmt.Printf("Mobile Country Code: %v\n", record.MobileCountryCode)
		fmt.Printf("Mobile Network Code: %v\n", record.MobileNetworkCode)
	}

	fmt.Printf("Network: %v\n", record.Network)
	fmt.Printf("IP Address: %v\n", record.IPAddress)
}
```

### Domain Database

The Domain database provides the second-level domain associated with an IP
address.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-Domain.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("1.2.0.0")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.Domain(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("Domain: %v\n", record.Domain)
	fmt.Printf("Network: %v\n", record.Network)
	fmt.Printf("IP Address: %v\n", record.IPAddress)
}
```

### Connection Type Database

The Connection Type database identifies the connection type of an IP address.

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-Connection-Type.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("1.0.128.0")
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.ConnectionType(ip)
	if err != nil {
		log.Fatal(err)
	}

	if !record.HasData() {
		fmt.Println("No data found for this IP")
		return
	}

	fmt.Printf("Connection Type: %v\n", record.ConnectionType)
	fmt.Printf("Network: %v\n", record.Network)
	fmt.Printf("IP Address: %v\n", record.IPAddress)
}
```

### Error Handling

All database lookups can return errors and should be handled appropriately:

```go
package main

import (
	"fmt"
	"log"
	"net/netip"

	"github.com/oschwald/geoip2-golang/v2"
)

func main() {
	db, err := geoip2.Open("GeoIP2-City.mmdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ip, err := netip.ParseAddr("10.0.0.1") // Private IP
	if err != nil {
		log.Fatal(err)
	}

	record, err := db.City(ip)
	if err != nil {
		log.Fatal(err)
	}

	// Always check if data was found
	if !record.HasData() {
		fmt.Println("No data found for this IP address")
		return
	}

	// Check individual fields before using them
	if record.City.Names.English != "" {
		fmt.Printf("City: %v\n", record.City.Names.English)
	} else {
		fmt.Println("City name not available")
	}

	// Check array bounds for subdivisions
	if len(record.Subdivisions) > 0 {
		fmt.Printf("Subdivision: %v\n", record.Subdivisions[0].Names.English)
	} else {
		fmt.Println("No subdivision data available")
	}

	fmt.Printf("Country: %v\n", record.Country.Names.English)
}
```

## Performance Tips

### Database Reuse

Always reuse database instances across requests rather than opening/closing
repeatedly:

```go
// Good: Create once, use many times
db, err := geoip2.Open("GeoIP2-City.mmdb")
if err != nil {
    log.Fatal(err)
}
defer db.Close()

// Use db for multiple lookups...
```

### Memory Usage

For applications needing only specific fields, consider using the lower-level
maxminddb library with custom result structs to reduce memory allocation.

### Concurrent Usage

The Reader is safe for concurrent use by multiple goroutines.

### JSON Serialization

All result structs include JSON tags and support marshaling to JSON:

```go
record, err := db.City(ip)
if err != nil {
    log.Fatal(err)
}

jsonData, err := json.Marshal(record)
if err != nil {
    log.Fatal(err)
}
fmt.Println(string(jsonData))
```

## Migration from v1

### Breaking Changes

- **Import Path**: Change from `github.com/oschwald/geoip2-golang` to
  `github.com/oschwald/geoip2-golang/v2`
- **IP Type**: Use `netip.Addr` instead of `net.IP`
- **Field Names**: `IsoCode` → `ISOCode`
- **Names Access**: Use struct fields instead of map access
- **Data Validation**: Use `HasData()` method to check for data availability

### Migration Example

```go
// v1
import "github.com/oschwald/geoip2-golang"

ip := net.ParseIP("81.2.69.142")
record, err := db.City(ip)
cityName := record.City.Names["en"]

// v2
import "github.com/oschwald/geoip2-golang/v2"

ip, err := netip.ParseAddr("81.2.69.142")
if err != nil {
    // handle error
}
record, err := db.City(ip)
if !record.HasData() {
    // handle no data found
}
cityName := record.City.Names.English
```

## Troubleshooting

### Common Issues

**Database not found**: Ensure the .mmdb file path is correct and readable.

**No data returned**: Check if `HasData()` returns false - the IP may not be in
the database or may be a private/reserved IP.

**Performance issues**: Ensure you're reusing the database instance rather than
opening it for each lookup.

## Testing

Make sure you checked out test data submodule:

```
git submodule init
git submodule update
```

Execute test suite:

```
go test
```

## Contributing

Contributions welcome! Please fork the repository and open a pull request with
your changes.

## License

This is free software, licensed under the ISC license.
