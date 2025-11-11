# Migrating from v1 to v2

## Package import

```go
- import "github.com/oschwald/maxminddb-golang"
+ import "github.com/oschwald/maxminddb-golang/v2"
```

## Lookup API

- v1: `err := reader.Lookup(net.IP, &result)`
- v2: `err := reader.Lookup(netip.Addr).Decode(&result)`

### Migration tips

- Replace `net.IP` inputs with `net/netip`. Use `netip.ParseAddr` or
  `addr.AsSlice()` helpers when interoperating with code that still expects
  `net.IP`.
- The new `Result` type returned from `Lookup` exposes `Decode` and
  `DecodePath` methods. Update call sites to chain `Decode` or `DecodePath`
  instead of passing the destination pointer to `Lookup`.

## Manual decoding improvements

- Custom types can implement `UnmarshalMaxMindDB(d *maxminddb.Decoder) error`
  to skip reflection and zero allocations for hot paths.
- `maxminddb.Decoder` mirrors the APIs from `internal/decoder`, giving fine
  grained access to the underlying data section.
- `DecodePath` works on the result object, supporting nested lookups without
  decoding entire records.

## Opening databases from byte slice

- v1: `reader, err := maxminddb.FromBytes(databaseBytes)`
- v2: `reader, err := maxminddb.OpenBytes(databaseBytes)`

## Network iteration

- `Reader.Networks()` and `Reader.NetworksWithin()` now yield iterators that
  work efficiently with Go 1.23+ `range` syntax:

  ```go
  for result := range reader.Networks() {
  	var record struct {
  		ConnectionType string `maxminddb:"connection_type"`
  	}

  	if err := result.Decode(&record); err != nil {
  		return err
  	}
  	fmt.Println(result.Prefix(), record.ConnectionType)
  }
  ```

- Replace the v1 iterator pattern (`for networks.Next() { ... networks.Network(&record) }`)
  with the Go 1.23 iteration shown above. `Decode` returns any iterator or
  lookup error, so separate calls to `Result.Err()` are rarely needed.
- Options such as `SkipAliasedNetworks` now use an options pattern. Pass them as
  `Networks(SkipAliasedNetworks())` or `NetworksWithin(prefix, SkipEmptyValues())`.
- New helpers include `SkipEmptyValues()` to omit entries with empty maps and
  `IncludeNetworksWithoutData()` to keep networks that lack data records.

## Additional API additions

- `Reader.Verify()` validates the database structure and metadata, exposing
  precise `InvalidDatabaseError` messages when corruption is detected.
- `Metadata.BuildTime()` converts the build epoch to `time.Time`.
- `Result.DecodePath` now supports negative indices for arrays, matching
  Go slices: `result.DecodePath(&value, "array", -1)` fetches the last element.

## Error handling

- All decoder and verifier errors wrap `mmdberrors.InvalidDatabaseError`, which
  carries offset and JSON Pointer style path clues. Display those details to
  speed up debugging malformed databases.

For more background on the architectural changes, see the per-release notes in
`CHANGELOG.md`.
