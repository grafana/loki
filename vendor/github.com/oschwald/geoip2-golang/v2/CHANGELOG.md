# Changes

## 2.0.1 - 2025-11-26

- Upgraded `github.com/oschwald/geoip2-golang/v2` to 2.1.1, which fixes an
  issue that prevented a unclosed memory-mapped file from being unmapped
  when the reader was garbage collected.

## 2.0.0 - 2025-10-19

- **BREAKING CHANGE**: Lookup methods now require `netip.Addr`, return typed
  `Names`, and provide `HasData()` helpers while always populating
  `Network`/`IPAddress` fields so network topology remains accessible.
- **BREAKING CHANGE**: Struct field casing now matches MaxMind responses (for
  example `IsoCode` → `ISOCode`), location coordinates use pointers, and JSON
  tags rely on Go 1.24 `omitzero` support—upgrade your toolchain before
  adopting v2.
- Added `MIGRATION.md` with detailed guidance for upgrading from v1.
- Updated dependency on `github.com/oschwald/maxminddb-golang/v2` to `v2.0.0`.
- Added configurable `Option` helpers so `Open` and `OpenBytes` can accept
  future options without forcing a v3 release.
- **BREAKING CHANGE**: Removed deprecated `FromBytes` method. Use `OpenBytes`
  instead.

## 2.0.0-beta.4 - 2025-08-23

- Updated maxminddb dependency to v2.0.0-beta.9.
- Added `OpenBytes` method to match the API changes in maxminddb v2.0.0-beta.9.
- Deprecated `FromBytes` method. Use `OpenBytes` instead. `FromBytes` will be
  removed in a future version.

## 2.0.0-beta.3 - 2025-07-07

- Added support for `GeoIP-City-Redacted-US` and `GeoIP-Enterprise-Redacted-US`.
  Requested by Tom Anderson. GitHub #134.
- Upgrade `github.com/oschwald/maxminddb-golang/v2` to `v2.0.0-beta.7`.

## 2.0.0-beta.2 - 2025-06-28

- **BREAKING CHANGE**: Replaced `IsZero()` methods with `HasData()` methods on
  all result structs (including Names). The new methods provide clearer
  semantics: `HasData()` returns `true` when GeoIP data is found and `false`
  when no data is available. Unlike `IsZero()`, `HasData()` excludes Network
  and IPAddress fields from validation, allowing users to access network
  topology information even when no GeoIP data is found. The Network and
  IPAddress fields are now always populated for all lookups, regardless of
  whether GeoIP data is available.
- **BREAKING CHANGE**: Replaced all anonymous nested structs with named types
  to improve struct initialization ergonomics. All result structs (Enterprise,
  City, Country) now use named types like `EnterpriseCityRecord`, `CityTraits`,
  `CountryRecord`, etc. This makes it much easier to initialize structs in user
  code while maintaining the same JSON serialization behavior.
- **BREAKING CHANGE**: Changed `Location.Latitude` and `Location.Longitude`
  from `float64` to `*float64` to properly distinguish between missing
  coordinates and the valid location (0, 0). Missing coordinates are now
  represented as `nil` and are omitted from JSON output, while valid zero
  coordinates are preserved. This fixes the ambiguity where (0, 0) was
  incorrectly treated as "no data". Added `Location.HasCoordinates()` method
  for safe coordinate access. Reported by Nick Bruun. GitHub #5.

## 2.0.0-beta.1 - 2025-06-22

- **BREAKING CHANGE**: Updated to use `maxminddb-golang/v2` which provides
  significant performance improvements and a more modern API.
- **BREAKING CHANGE**: All lookup methods now accept `netip.Addr` instead of
  `net.IP`. This provides better performance and aligns with modern Go
  networking practices.
- **BREAKING CHANGE**: Renamed `IsoCode` fields to `ISOCode` in all structs to
  follow proper capitalization for the ISO acronym. Closes GitHub issue #4.
- **BREAKING CHANGE**: Replaced `map[string]string` Names fields with
  structured `Names` type for significant performance improvements. This
  eliminates map allocation overhead, reducing memory usage by 34% and
  allocations by 56%.
- **BREAKING CHANGE**: Added JSON tags to all struct fields. JSON tags match
  the corresponding `maxminddb` tags where they exist. Custom fields
  (`IPAddress` and `Network`) use snake_case (`ip_address` and `network`).
- **BREAKING CHANGE**: Removed `IsAnonymousProxy` and `IsSatelliteProvider`
  fields from all Traits structs. These fields have been removed from MaxMind
  databases. Use the dedicated Anonymous IP database for anonymity detection
  instead.
- **BREAKING CHANGE**: Go 1.24 or greater is now required. This enables the use
  of `omitzero` in JSON tags to match MaxMind database behavior where empty
  values are not included.
- Added `IsZero()` method to all result structs (City, Country, Enterprise,
  ASN, etc.) to easily check whether any data was found for the queried IP
  address. Requested by Salim Alami. GitHub
  [#32](https://github.com/oschwald/geoip2-golang/issues/32).
- Added `Network` and `IPAddress` fields to all result structs. The `Network`
  field exposes the network prefix from the MaxMind database lookup, and the
  `IPAddress` field contains the IP address used during the lookup. These
  fields are only populated when data is found for the IP address. For flat
  record types (ASN, ConnectionType, Domain, ISP, AnonymousIP), the fields are
  named `Network` and `IPAddress`. For complex types (City, Country,
  Enterprise), the fields are located at `.Traits.Network` and
  `.Traits.IPAddress`. Requested by Aaron Bishop. GitHub
  [#128](https://github.com/oschwald/geoip2-golang/issues/128).
- Updated module path to `github.com/oschwald/geoip2-golang/v2` to follow Go's
  semantic versioning guidelines for breaking changes.
- Updated examples and documentation to demonstrate proper error handling with
  `netip.ParseAddr()`.
- Updated linting rules to support both v1 and v2 import paths during the
  transition period.

### Migration Guide

To migrate from v1 to v2:

1. Update your import path:

   ```go
   // Old
   import "github.com/oschwald/geoip2-golang"

   // New
   import "github.com/oschwald/geoip2-golang/v2"
   ```

2. Replace `net.IP` with `netip.Addr`:

   ```go
   // Old
   ip := net.ParseIP("81.2.69.142")
   record, err := db.City(ip)

   // New
   ip, err := netip.ParseAddr("81.2.69.142")
   if err != nil {
       // handle error
   }
   record, err := db.City(ip)
   ```

3. Update field names from `IsoCode` to `ISOCode`:

   ```go
   // Old
   countryCode := record.Country.IsoCode
   subdivisionCode := record.Subdivisions[0].IsoCode

   // New
   countryCode := record.Country.ISOCode
   subdivisionCode := record.Subdivisions[0].ISOCode
   ```

4. Replace map-based Names access with struct fields:

   ```go
   // Old
   cityName := record.City.Names["en"]
   countryName := record.Country.Names["pt-BR"]
   continentName := record.Continent.Names["zh-CN"]

   // New
   cityName := record.City.Names.English
   countryName := record.Country.Names.BrazilianPortuguese
   continentName := record.Continent.Names.SimplifiedChinese
   ```

   Available Names struct fields:
   - `English` (en)
   - `German` (de)
   - `Spanish` (es)
   - `French` (fr)
   - `Japanese` (ja)
   - `BrazilianPortuguese` (pt-BR)
   - `Russian` (ru)
   - `SimplifiedChinese` (zh-CN)

5. Check if data was found using the new `IsZero()` method:
   ```go
   record, err := db.City(ip)
   if err != nil {
       // handle error
   }
   if record.IsZero() {
       fmt.Println("No data found for this IP")
   } else {
       fmt.Printf("City: %s\n", record.City.Names.English)
   }
   ```

## 1.11.0 - 2024-06-03

- Go 1.21 or greater is now required.
- The new `is_anycast` output is now supported on the GeoIP2 Country, City, and
  Enterprise databases.
  [#119](https://github.com/oschwald/geoip2-golang/issues/119).

Note: 1.10.0 was accidentally skipped.

## 1.9.0 - 2023-06-18

- Rearrange fields in structs to reduce memory usage. Although this does reduce
  readability, these structs are often created at very rates, making the
  trade-off worth it.

## 1.8.0 - 2022-08-07

- Set Go version to 1.18 in go.mod.

## 1.7.0 - 2022-03-26

- Set the minimum Go version in the go.mod file to 1.17.
- Updated dependencies.

## 1.6.1 - 2022-01-28

- This is a re-release with the changes that were supposed to be in 1.6.0.

## 1.6.0 - 2022-01-28

- Add support for new `mobile_country_code` and `mobile_network_code` outputs
  on GeoIP2 ISP and GeoIP2 Enterprise.

## 1.5.0 - 2021-02-20

- Add `StaticIPScore` field to Enterprise. Pull request by Pierre Bonzel.
  GitHub [#54](https://github.com/oschwald/geoip2-golang/issues/54).
- Add `IsResidentialProxy` field to `AnonymousIP`. Pull request by Brendan
  Boyle. GitHub [#72](https://github.com/oschwald/geoip2-golang/issues/72).
- Support DBIP-ASN-Lite database. Requested by Muhammad Hussein Fattahizadeh.
  GitHub [#69](https://github.com/oschwald/geoip2-golang/issues/69).

## 1.4.0 - 2019-12-25

- This module now uses Go modules. Requested by Axel Etcheverry. GitHub
  [#52](https://github.com/oschwald/geoip2-golang/issues/52).
- DBIP databases are now supported. Requested by jaw0. GitHub
  [#45](https://github.com/oschwald/geoip2-golang/issues/45).
- Allow using the ASN method with the GeoIP2 ISP database. Pull request by
  lspgn. GitHub [#47](https://github.com/oschwald/geoip2-golang/issues/47).
- The example in the `README.md` now checks the length of the subdivision slice
  before using it. GitHub
  [#51](https://github.com/oschwald/geoip2-golang/issues/51).

## 1.3.0 - 2019-08-28

- Added support for the GeoIP2 Enterprise database.

## 1.2.1 - 2018-02-25

- HTTPS is now used for the test data submodule rather than the Git protocol

## 1.2.0 - 2018-02-19

- The country structs for `geoip2.City` and `geoip2.Country` now have an
  `IsInEuropeanUnion` boolean field. This is true when the associated country
  is a member state of the European Union. This requires a database built on or
  after February 13, 2018.
- Switch from Go Check to Testify. Closes
  [#27](https://github.com/oschwald/geoip2-golang/issues/27)

## 1.1.0 - 2017-04-23

- Add support for the GeoLite2 ASN database.
- Add support for the GeoIP2 City by Continent databases. GitHub
  [#26](https://github.com/oschwald/geoip2-golang/issues/26).

## 1.0.0 - 2016-11-09

New release for those using tagged releases. Closes
[#21](https://github.com/oschwald/geoip2-golang/issues/21).
