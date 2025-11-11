// Package geoip2 provides an easy-to-use API for the MaxMind GeoIP2 and
// GeoLite2 databases; this package does not support GeoIP Legacy databases.
//
// # Basic Usage
//
//	db, err := geoip2.Open("GeoIP2-City.mmdb")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer db.Close()
//
//	ip, err := netip.ParseAddr("81.2.69.142")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	record, err := db.City(ip)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	if !record.HasData() {
//		fmt.Println("No data found for this IP")
//		return
//	}
//
//	fmt.Printf("City: %v\n", record.City.Names.English)
//	fmt.Printf("Country: %v\n", record.Country.Names.English)
//
// # Database Types
//
// This library supports all MaxMind database types:
//   - City: Most comprehensive geolocation data
//   - Country: Country-level geolocation
//   - ASN: Autonomous system information
//   - AnonymousIP: Anonymous network detection
//   - Enterprise: Enhanced City data with additional fields
//   - ISP: Internet service provider information
//   - Domain: Second-level domain data
//   - ConnectionType: Connection type identification
//
// # Version 2.0 Features
//
// Version 2.0 introduces significant improvements:
//   - Modern API using netip.Addr instead of net.IP
//   - Network and IPAddress fields in all result structs
//   - HasData() method for data validation
//   - Structured Names type for localized names
//   - JSON serialization support
//
// See github.com/oschwald/maxminddb-golang/v2 for more advanced use cases.
package geoip2

import (
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

type databaseType int

const (
	isAnonymousIP = 1 << iota
	isASN
	isCity
	isConnectionType
	isCountry
	isDomain
	isEnterprise
	isISP
)

// Reader holds the maxminddb.Reader struct. It can be created using the
// Open and OpenBytes functions.
type Reader struct {
	mmdbReader   *maxminddb.Reader
	databaseType databaseType
}

// Option configures Reader behavior.
type Option func(*readerOptions)

type readerOptions struct {
	maxminddb []maxminddb.ReaderOption
}

func applyOptions(options []Option) []maxminddb.ReaderOption {
	var opts readerOptions
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return opts.maxminddb
}

// InvalidMethodError is returned when a lookup method is called on a
// database that it does not support. For instance, calling the ISP method
// on a City database.
type InvalidMethodError struct {
	Method       string
	DatabaseType string
}

func (e InvalidMethodError) Error() string {
	return fmt.Sprintf(`geoip2: the %s method does not support the %s database`,
		e.Method, e.DatabaseType)
}

// UnknownDatabaseTypeError is returned when an unknown database type is
// opened.
type UnknownDatabaseTypeError struct {
	DatabaseType string
}

func (e UnknownDatabaseTypeError) Error() string {
	return fmt.Sprintf(`geoip2: reader does not support the %q database type`,
		e.DatabaseType)
}

// Open takes a string path to a file and returns a Reader struct or an error.
// The database file is opened using a memory map. Use the Close method on the
// Reader object to return the resources to the system.
func Open(file string, options ...Option) (*Reader, error) {
	reader, err := maxminddb.Open(file, applyOptions(options)...)
	if err != nil {
		return nil, err
	}
	dbType, err := getDBType(reader)
	return &Reader{reader, dbType}, err
}

// OpenBytes takes a byte slice corresponding to a GeoIP2/GeoLite2 database
// file and returns a Reader struct or an error. Note that the byte slice is
// used directly; any modification of it after opening the database will result
// in errors while reading from the database.
func OpenBytes(bytes []byte, options ...Option) (*Reader, error) {
	reader, err := maxminddb.OpenBytes(bytes, applyOptions(options)...)
	if err != nil {
		return nil, err
	}
	dbType, err := getDBType(reader)
	return &Reader{reader, dbType}, err
}

func getDBType(reader *maxminddb.Reader) (databaseType, error) {
	switch reader.Metadata.DatabaseType {
	case "GeoIP2-Anonymous-IP":
		return isAnonymousIP, nil
	case "DBIP-ASN-Lite (compat=GeoLite2-ASN)",
		"GeoLite2-ASN":
		return isASN, nil
	// We allow City lookups on Country for back compat
	case "DBIP-City-Lite",
		"DBIP-Country-Lite",
		"DBIP-Country",
		"DBIP-Location (compat=City)",
		"GeoLite2-City",
		"GeoIP-City-Redacted-US",
		"GeoIP2-City",
		"GeoIP2-City-Africa",
		"GeoIP2-City-Asia-Pacific",
		"GeoIP2-City-Europe",
		"GeoIP2-City-North-America",
		"GeoIP2-City-South-America",
		"GeoIP2-Precision-City",
		"GeoLite2-Country",
		"GeoIP2-Country":
		return isCity | isCountry, nil
	case "GeoIP2-Connection-Type":
		return isConnectionType, nil
	case "GeoIP2-Domain":
		return isDomain, nil
	case "DBIP-ISP (compat=Enterprise)",
		"DBIP-Location-ISP (compat=Enterprise)",
		"GeoIP-Enterprise-Redacted-US",
		"GeoIP2-Enterprise":
		return isEnterprise | isCity | isCountry, nil
	case "GeoIP2-ISP", "GeoIP2-Precision-ISP":
		return isISP | isASN, nil
	default:
		return 0, UnknownDatabaseTypeError{reader.Metadata.DatabaseType}
	}
}

// Enterprise takes an IP address as a netip.Addr and returns an Enterprise
// struct and/or an error. This is intended to be used with the GeoIP2
// Enterprise database.
func (r *Reader) Enterprise(ipAddress netip.Addr) (*Enterprise, error) {
	if isEnterprise&r.databaseType == 0 {
		return nil, InvalidMethodError{"Enterprise", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var enterprise Enterprise
	err := result.Decode(&enterprise)
	if err != nil {
		return &enterprise, err
	}
	enterprise.Traits.IPAddress = ipAddress
	enterprise.Traits.Network = result.Prefix()
	return &enterprise, nil
}

// City takes an IP address as a netip.Addr and returns a City struct
// and/or an error. Although this can be used with other databases, this
// method generally should be used with the GeoIP2 or GeoLite2 City databases.
func (r *Reader) City(ipAddress netip.Addr) (*City, error) {
	if isCity&r.databaseType == 0 {
		return nil, InvalidMethodError{"City", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var city City
	err := result.Decode(&city)
	if err != nil {
		return &city, err
	}
	city.Traits.IPAddress = ipAddress
	city.Traits.Network = result.Prefix()
	return &city, nil
}

// Country takes an IP address as a netip.Addr and returns a Country struct
// and/or an error. Although this can be used with other databases, this
// method generally should be used with the GeoIP2 or GeoLite2 Country
// databases.
func (r *Reader) Country(ipAddress netip.Addr) (*Country, error) {
	if isCountry&r.databaseType == 0 {
		return nil, InvalidMethodError{"Country", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var country Country
	err := result.Decode(&country)
	if err != nil {
		return &country, err
	}
	country.Traits.IPAddress = ipAddress
	country.Traits.Network = result.Prefix()
	return &country, nil
}

// AnonymousIP takes an IP address as a netip.Addr and returns a
// AnonymousIP struct and/or an error.
func (r *Reader) AnonymousIP(ipAddress netip.Addr) (*AnonymousIP, error) {
	if isAnonymousIP&r.databaseType == 0 {
		return nil, InvalidMethodError{"AnonymousIP", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var anonIP AnonymousIP
	err := result.Decode(&anonIP)
	if err != nil {
		return &anonIP, err
	}
	anonIP.IPAddress = ipAddress
	anonIP.Network = result.Prefix()
	return &anonIP, nil
}

// ASN takes an IP address as a netip.Addr and returns a ASN struct and/or
// an error.
func (r *Reader) ASN(ipAddress netip.Addr) (*ASN, error) {
	if isASN&r.databaseType == 0 {
		return nil, InvalidMethodError{"ASN", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var val ASN
	err := result.Decode(&val)
	if err != nil {
		return &val, err
	}
	val.IPAddress = ipAddress
	val.Network = result.Prefix()
	return &val, nil
}

// ConnectionType takes an IP address as a netip.Addr and returns a
// ConnectionType struct and/or an error.
func (r *Reader) ConnectionType(ipAddress netip.Addr) (*ConnectionType, error) {
	if isConnectionType&r.databaseType == 0 {
		return nil, InvalidMethodError{"ConnectionType", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var val ConnectionType
	err := result.Decode(&val)
	if err != nil {
		return &val, err
	}
	val.IPAddress = ipAddress
	val.Network = result.Prefix()
	return &val, nil
}

// Domain takes an IP address as a netip.Addr and returns a
// Domain struct and/or an error.
func (r *Reader) Domain(ipAddress netip.Addr) (*Domain, error) {
	if isDomain&r.databaseType == 0 {
		return nil, InvalidMethodError{"Domain", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var val Domain
	err := result.Decode(&val)
	if err != nil {
		return &val, err
	}
	val.IPAddress = ipAddress
	val.Network = result.Prefix()
	return &val, nil
}

// ISP takes an IP address as a netip.Addr and returns a ISP struct and/or
// an error.
func (r *Reader) ISP(ipAddress netip.Addr) (*ISP, error) {
	if isISP&r.databaseType == 0 {
		return nil, InvalidMethodError{"ISP", r.Metadata().DatabaseType}
	}
	result := r.mmdbReader.Lookup(ipAddress)
	var val ISP
	err := result.Decode(&val)
	if err != nil {
		return &val, err
	}
	val.IPAddress = ipAddress
	val.Network = result.Prefix()
	return &val, nil
}

// Metadata takes no arguments and returns a struct containing metadata about
// the MaxMind database in use by the Reader.
func (r *Reader) Metadata() maxminddb.Metadata {
	return r.mmdbReader.Metadata
}

// Close unmaps the database file from virtual memory and returns the
// resources to the system.
func (r *Reader) Close() error {
	return r.mmdbReader.Close()
}
