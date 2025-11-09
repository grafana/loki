# Migrating to geoip2-golang v2

Version 2.0 modernizes the public API, improves performance, and tightens
parity with current MaxMind databases. This guide highlights the most important
changes and shows how to update existing v1 code.

## Upgrade Checklist

1. **Update imports**

   ```go
   // v1
   import "github.com/oschwald/geoip2-golang"

   // v2
   import "github.com/oschwald/geoip2-golang/v2"
   ```

2. **Switch to `netip.Addr`**

   Lookup methods now take `netip.Addr` instead of `net.IP`. Use
   `netip.ParseAddr` (errors on invalid input) or `netip.MustParseAddr` for
   trusted literals. This avoids ambiguity around IPv4-in-IPv6 addresses and
   removes heap allocations for most lookups.

   ```go
   // v1
   ip := net.ParseIP("81.2.69.142")
   record, err := db.City(ip)

   // v2
   ip, err := netip.ParseAddr("81.2.69.142")
   if err != nil {
       return err
   }
   record, err := db.City(ip)
   ```

3. **Adopt the new `HasData()` helpers**

   All result structs have a `HasData()` method that ignores the always-filled
   `Network` and `IPAddress` fields. Use it instead of comparing against zero
   values.

   ```go
   record, err := db.City(ip)
   if err != nil {
       return err
   }
   if !record.HasData() {
       // handle missing GeoIP data
   }
   ```

4. **Use structured `Names` fields**

   Localized names are now strongly typed instead of map lookups. Access the
   language-specific struct field directly.

   ```go
   // v1
   cityEn := record.City.Names["en"]
   cityPtBr := record.City.Names["pt-BR"]

   // v2
   cityEn := record.City.Names.English
   cityPtBr := record.City.Names.BrazilianPortuguese
   ```

   Supported fields include `English`, `German`, `Spanish`, `French`,
   `Japanese`, `BrazilianPortuguese`, `Russian`, and `SimplifiedChinese`.

5. **Expect `Network` and `IPAddress` on every result**

   Each lookup populates the queried IP address and the enclosing network on
   every result struct, even if `HasData()` returns false. Existing code that
   depended on empty `Network` for “no data” should switch to `HasData()`.

6. **Adjust renamed fields**

   `IsoCode` is now `ISOCode` across all structs. Update code accordingly.

   ```go
   // v1
   code := record.Country.IsoCode

   // v2
   code := record.Country.ISOCode
   ```

7. **Build with Go 1.24 or newer**

   The module now depends on Go 1.24 features such as `omitzero` struct tags.
   Update your toolchain (`go env GOVERSION`) before upgrading.

## Additional Notes

- `Open` and `OpenBytes` accept optional `Option` values, allowing future
  extensions without new breaking releases.
- The legacy `FromBytes` helper is removed. Use `OpenBytes` instead.
- JSON output mirrors MaxMind database naming thanks to the new `omitzero` tags
  and typed structs.

Following the checklist above should cover most migrations. For edge cases,
review `models.go` for field definitions and `example_test.go` for idiomatic
usage with the v2 API.
