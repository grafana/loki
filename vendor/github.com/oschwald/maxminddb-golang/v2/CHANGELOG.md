# Changes

## 2.1.1 - 2025-11-26

- Fixed `runtime.AddCleanup` misuse that prevented the memory-mapped file from
  being unmapped when the `Reader` was garbage collected.

## 2.1.0 - 2025-11-04

- Updated `Offset` method on `Decoder` to return the resolved data offset
  when positioned at a pointer. This makes more useful for caching values,
  which is its stated purpose.

## 2.0.0 - 2025-10-18

- BREAKING CHANGE: Removed deprecated `FromBytes`. Use `OpenBytes` instead.
- Fixed verifier metadata error message to require a non-empty map for the
  database description. GitHub #187.
- Introduces the v2 API with `Reader.Lookup(ip).Decode(...)`, `netip.Addr`
  support, custom decoder interfaces, and richer error reporting.
- See MIGRATION.md for guidance on upgrading projects from v1 to v2.

## 2.0.0-beta.10 - 2025-08-23

- Replaced `runtime.SetFinalizer` with `runtime.AddCleanup` for resource
  cleanup in Go 1.24+. This provides more reliable finalization behavior and
  better garbage collection performance.

## 2.0.0-beta.9 - 2025-08-23

- **SECURITY**: Fixed integer overflow vulnerability in search tree size
  calculation that could potentially allow malformed databases to trigger
  security issues.
- **SECURITY**: Enhanced bounds checking in tree traversal functions to return
  proper errors instead of silent failures when encountering malformed
  databases.
- Added validation for invalid prefixes in `NetworksWithin` to prevent
  unexpected behavior with malformed input.
- Added `SkipEmptyValues()` option for `Networks` and `NetworksWithin` to skip
  networks whose data is an empty map or empty array. This is useful for
  databases that store empty maps or arrays for records without meaningful
  data. GitHub #172.
- Optimized custom unmarshaler type assertion to use Go 1.25's
  `reflect.TypeAssert` when available, reducing allocations in reflection code
  paths.
- Improved memory mapping implementation by using `SyscallConn()` instead of
  `Fd()` to avoid side effects and prepare for Go 1.25+ Windows I/O
  enhancements. Pull request by database64128. GitHub #179.
- Added `OpenBytes` function for better API discoverability and consistency
  with `Open()`. `FromBytes` is now deprecated and will be removed in a future
  version.

## 2.0.0-beta.8 - 2025-07-15

- Fixed "no next offset available" error that occurred when using custom
  unmarshalers that decode container types (maps, slices) in struct fields.
  The reflection decoder now correctly calculates field positions when
  advancing to the next field after custom unmarshaling.

## 2.0.0-beta.7 - 2025-07-07

* Update capitalization of "uint" in `ReadUInt*` to match `KindUint*` as well
  as the Go standard library.

## 2.0.0-beta.6 - 2025-07-07

* Invalid release with no code changes.

## 2.0.0-beta.5 - 2025-07-06

- Added `Offset()` method to `Decoder` to get the current database offset. This
  enables custom unmarshalers to implement caching for improved performance when
  loading databases with duplicate data structures.
- Fixed infinite recursion in pointer-to-pointer data structures, which are
  invalid per the MaxMind DB specification.

## 2.0.0-beta.4 - 2025-07-05

- **BREAKING CHANGE**: Removed experimental `deserializer` interface and
  supporting code. Applications using this interface should migrate to the
  `Unmarshaler` interface by implementing `UnmarshalMaxMindDB(d *Decoder) error`
  instead.
- `Open` and `FromBytes` now accept options.
- **BREAKING CHANGE**: `IncludeNetworksWithoutData` and `IncludeAliasedNetworks`
  now return a `NetworksOption` rather than being one themselves. These must now
  be called as functions: `Networks(IncludeAliasedNetworks())` instead of
  `Networks(IncludeAliasedNetworks)`. This was done to improve the documentation
  organization.
- Added `Unmarshaler` interface to allow custom decoding implementations for
  performance-critical applications. Types implementing
  `UnmarshalMaxMindDB(d *Decoder) error` will automatically use custom decoding
  logic instead of reflection, following the same pattern as
  `json.Unmarshaler`.
- Added public `Decoder` type and `Kind` constants in `mmdbdata` package for
  manual decoding. `Decoder` provides methods like `ReadMap()`, `ReadSlice()`,
  `ReadString()`, `ReadUInt32()`, `PeekKind()`, etc. `Kind` type includes
  helper methods `String()`, `IsContainer()`, and `IsScalar()` for type
  introspection. The main `maxminddb` package re-exports these types for
  backward compatibility. `NewDecoder()` supports an options pattern for
  future extensibility.
- Enhanced `UnmarshalMaxMindDB` to work with nested struct fields, slice
  elements, and map values. The custom unmarshaler is now called recursively
  for any type that implements the `Unmarshaler` interface, similar to
  `encoding/json`.
- Improved error messages to include byte offset information and, for the
  reflection-based API, path information for nested structures using JSON
  Pointer format. For example, errors may now show "at offset 1234, path
  /city/names/en" or "at offset 1234, path /list/0/name" instead of just the
  underlying error message.
- **PERFORMANCE**: Added string interning optimization that reduces allocations
  while maintaining thread safety. Reduces allocation count from 33 to 10 per
  operation in downstream libraries. Uses a fixed 512-entry cache with per-entry
  mutexes for bounded memory usage (~8KB) while minimizing lock contention.

## 2.0.0-beta.3 - 2025-02-16

- `Open` will now fall back to loading the database in memory if the
  file-system does not support `mmap`. Pull request by database64128. GitHub
  #163.
- Made significant improvements to the Windows memory-map handling. GitHub
  #162.
- Fix an integer overflow on large databases when using a 32-bit architecture.
  See ipinfo/mmdbctl#33.

## 2.0.0-beta.2 - 2024-11-14

- Allow negative indexes for arrays when using `DecodePath`. #152
- Add `IncludeNetworksWithoutData` option for `Networks` and `NetworksWithin`.
  #155 and #156

## 2.0.0-beta.1 - 2024-08-18

This is the first beta of the v2 releases. Go 1.23 is required. I don't expect
to do a final release until Go 1.24 is available. See #141 for the v2 roadmap.

Notable changes:

- `(*Reader).Lookup` now takes only the IP address and returns a `Result`.
  `Lookup(ip, &rec)` would now become `Lookup(ip).Decode(&rec)`.
- `(*Reader).LookupNetwork` has been removed. To get the network for a result,
  use `(Result).Prefix()`.
- `(*Reader).LookupOffset` now _takes_ an offset and returns a `Result`.
  `Result` has an `Offset()` method that returns the offset value.
  `(*Reader).Decode` has been removed.
- Use of `net.IP` and `*net.IPNet` have been replaced with `netip.Addr` and
  `netip.Prefix`.
- You may now decode a particular path within a database record using
  `(Result).DecodePath`. For instance, to decode just the country code in
  GeoLite2 Country to a string called `code`, you might do something like
  `Lookup(ip).DecodePath(&code, "country", "iso_code")`. Strings should be used
  for map keys and ints for array indexes.
- `(*Reader).Networks` and `(*Reader).NetworksWithin` now return a Go 1.23
  iterator of `Result` values. Aliased networks are now skipped by default. If
  you wish to include them, use the `IncludeAliasedNetworks` option.

## 1.13.1 - 2024-06-28

- Return the `*net.IPNet` in canonical form when using `NetworksWithin` to look
  up a network more specific than the one in the database. Previously, the `IP`
  field on the `*net.IPNet` would be set to the IP from the lookup network
  rather than the first IP of the network.
- `NetworksWithin` will now correctly handle an `*net.IPNet` parameter that is
  not in canonical form. This issue would only occur if the `*net.IPNet` was
  manually constructed, as `net.ParseCIDR` returns the value in canonical form
  even if the input string is not.

## 1.13.0 - 2024-06-03

- Go 1.21 or greater is now required.
- The error messages when decoding have been improved. #119

## 1.12.0 - 2023-08-01

- The `wasi` target is now built without memory-mapping support. Pull request
  by Alex Kashintsev. GitHub #114.
- When decoding to a map of non-scalar, non-interface types such as a
  `map[string]map[string]any`, the decoder failed to zero out the value for the
  map elements, which could result in incorrect decoding. Reported by JT Olio.
  GitHub #115.

## 1.11.0 - 2023-06-18

- `wasm` and `wasip1` targets are now built without memory-mapping support.
  Pull request by Randy Reddig. GitHub #110.

**Full Changelog**:
https://github.com/oschwald/maxminddb-golang/compare/v1.10.0...v1.11.0

## 1.10.0 - 2022-08-07

- Set Go version in go.mod file to 1.18.

## 1.9.0 - 2022-03-26

- Set the minimum Go version in the go.mod file to 1.17.
- Updated dependencies.
- Minor performance improvements to the custom deserializer feature added in
  1.8.0.

## 1.8.0 - 2020-11-23

- Added `maxminddb.SkipAliasedNetworks` option to `Networks` and
  `NetworksWithin` methods. When set, this option will cause the iterator to
  skip networks that are aliases of the IPv4 tree.
- Added experimental custom deserializer support. This allows much more control
  over the deserialization. The API is subject to change and you should use at
  your own risk.

## 1.7.0 - 2020-06-13

- Add `NetworksWithin` method. This returns an iterator that traverses all
  networks in the database that are contained in the given network. Pull
  request by Olaf Alders. GitHub #65.

## 1.6.0 - 2019-12-25

- This module now uses Go modules. Requested by Matthew Rothenberg. GitHub #49.
- Plan 9 is now supported. Pull request by Jacob Moody. GitHub #61.
- Documentation fixes. Pull request by Olaf Alders. GitHub #62.
- Thread-safety is now mentioned in the documentation. Requested by Ken
  Sedgwick. GitHub #39.
- Fix off-by-one error in file offset safety check. Reported by Will Storey.
  GitHub #63.

## 1.5.0 - 2019-09-11

- Drop support for Go 1.7 and 1.8.
- Minor performance improvements.

## 1.4.0 - 2019-08-28

- Add the method `LookupNetwork`. This returns the network that the record
  belongs to as well as a boolean indicating whether there was a record for the
  IP address in the database. GitHub #59.
- Improve performance.

## 1.3.1 - 2019-08-28

- Fix issue with the finalizer running too early on Go 1.12 when using the
  Verify method. Reported by Robert-André Mauchin. GitHub #55.
- Remove unnecessary call to reflect.ValueOf. PR by SenseyeDeveloper. GitHub
  #53.

## 1.3.0 - 2018-02-25

- The methods on the `maxminddb.Reader` struct now return an error if called on
  a closed database reader. Previously, this could cause a segmentation
  violation when using a memory-mapped file.
- The `Close` method on the `maxminddb.Reader` struct now sets the underlying
  buffer to nil, even when using `FromBytes` or `Open` on Google App Engine.
- No longer uses constants from `syscall`

## 1.2.1 - 2018-01-03

- Fix incorrect index being used when decoding into anonymous struct fields. PR
  #42 by Andy Bursavich.

## 1.2.0 - 2017-05-05

- The database decoder now does bound checking when decoding data from the
  database. This is to help ensure that the reader does not panic when given a
  corrupt database to decode. Closes #37.
- The reader will now return an error on a data structure with a depth greater
  than 512. This is done to prevent the possibility of a stack overflow on a
  cyclic data structure in a corrupt database. This matches the maximum depth
  allowed by `libmaxminddb`. All MaxMind databases currently have a depth of
  less than five.

## 1.1.0 - 2016-12-31

- Added appengine build tag for Windows. When enabled, memory-mapping will be
  disabled in the Windows build as it is for the non-Windows build. Pull
  request #35 by Ingo Oeser.
- SetFinalizer is now used to unmap files if the user fails to close the
  reader. Using `r.Close()` is still recommended for most use cases.
- Previously, an unsafe conversion between `[]byte` and string was used to
  avoid unnecessary allocations when decoding struct keys. The decoder now
  relies on a compiler optimization on `string([]byte)` map lookups to achieve
  this rather than using `unsafe`.

## 1.0.0 - 2016-11-09

New release for those using tagged releases.
