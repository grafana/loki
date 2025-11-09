package geoip2

import "net/netip"

// Names contains localized names for geographic entities.
type Names struct {
	// German localized name
	German string `json:"de,omitzero"    maxminddb:"de"`
	// English localized name
	English string `json:"en,omitzero"    maxminddb:"en"`
	// Spanish localized name
	Spanish string `json:"es,omitzero"    maxminddb:"es"`
	// French localized name
	French string `json:"fr,omitzero"    maxminddb:"fr"`
	// Japanese localized name
	Japanese string `json:"ja,omitzero"    maxminddb:"ja"`
	// BrazilianPortuguese localized name (pt-BR)
	BrazilianPortuguese string `json:"pt-BR,omitzero" maxminddb:"pt-BR"` //nolint:tagliatelle,lll // pt-BR matches MMDB format
	// Russian localized name
	Russian string `json:"ru,omitzero"    maxminddb:"ru"`
	// SimplifiedChinese localized name (zh-CN)
	SimplifiedChinese string `json:"zh-CN,omitzero" maxminddb:"zh-CN"` //nolint:tagliatelle // zh-CN matches MMDB format
}

var (
	zeroNames                   Names
	zeroContinent               Continent
	zeroLocation                Location
	zeroRepresentedCountry      RepresentedCountry
	zeroCityRecord              CityRecord
	zeroCityPostal              CityPostal
	zeroCitySubdivision         CitySubdivision
	zeroCountryRecord           CountryRecord
	zeroEnterpriseCityRecord    EnterpriseCityRecord
	zeroEnterprisePostal        EnterprisePostal
	zeroEnterpriseSubdivision   EnterpriseSubdivision
	zeroEnterpriseCountryRecord EnterpriseCountryRecord
)

// HasData returns true if the Names struct has any localized names.
func (n Names) HasData() bool {
	return n != zeroNames
}

// Common types used across multiple database records

// Continent contains data for the continent record associated with an IP address.
type Continent struct {
	// Names contains localized names for the continent
	Names Names `json:"names,omitzero"      maxminddb:"names"`
	// Code is a two character continent code like "NA" (North America) or
	// "OC" (Oceania)
	Code string `json:"code,omitzero"       maxminddb:"code"`
	// GeoNameID for the continent
	GeoNameID uint `json:"geoname_id,omitzero" maxminddb:"geoname_id"`
}

// HasData returns true if the Continent has any data.
func (c Continent) HasData() bool {
	return c != zeroContinent
}

// Location contains data for the location record associated with an IP address.
type Location struct {
	// Latitude is the approximate latitude of the location associated with
	// the IP address. This value is not precise and should not be used to
	// identify a particular address or household. Will be nil if not present
	// in the database.
	Latitude *float64 `json:"latitude,omitzero"        maxminddb:"latitude"`
	// Longitude is the approximate longitude of the location associated with
	// the IP address. This value is not precise and should not be used to
	// identify a particular address or household. Will be nil if not present
	// in the database.
	Longitude *float64 `json:"longitude,omitzero"       maxminddb:"longitude"`
	// TimeZone is the time zone associated with location, as specified by
	// the IANA Time Zone Database (e.g., "America/New_York")
	TimeZone string `json:"time_zone,omitzero"       maxminddb:"time_zone"`
	// MetroCode is a metro code for targeting advertisements.
	//
	// Deprecated: Metro codes are no longer maintained and should not be used.
	MetroCode uint `json:"metro_code,omitzero"      maxminddb:"metro_code"`
	// AccuracyRadius is the approximate accuracy radius in kilometers around
	// the latitude and longitude. This is the radius where we have a 67%
	// confidence that the device using the IP address resides within the
	// circle.
	AccuracyRadius uint16 `json:"accuracy_radius,omitzero" maxminddb:"accuracy_radius"`
}

// HasData returns true if the Location has any data.
func (l Location) HasData() bool {
	return l != zeroLocation
}

// HasCoordinates returns true if both latitude and longitude are present.
func (l Location) HasCoordinates() bool {
	return l.Latitude != nil && l.Longitude != nil
}

// RepresentedCountry contains data for the represented country associated
// with an IP address. The represented country is the country represented
// by something like a military base or embassy.
type RepresentedCountry struct {
	// Names contains localized names for the represented country
	Names Names `json:"names,omitzero"                maxminddb:"names"`
	// ISOCode is the two-character ISO 3166-1 alpha code for the represented
	// country. See https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2
	ISOCode string `json:"iso_code,omitzero"             maxminddb:"iso_code"`
	// Type is a string indicating the type of entity that is representing
	// the country. Currently this is only "military" but may expand in the future.
	Type string `json:"type,omitzero"                 maxminddb:"type"`
	// GeoNameID for the represented country
	GeoNameID uint `json:"geoname_id,omitzero"           maxminddb:"geoname_id"`
	// IsInEuropeanUnion is true if the represented country is a member
	// state of the European Union
	IsInEuropeanUnion bool `json:"is_in_european_union,omitzero" maxminddb:"is_in_european_union"`
}

// HasData returns true if the RepresentedCountry has any data.
func (r RepresentedCountry) HasData() bool {
	return r != zeroRepresentedCountry
}

// Enterprise-specific types

// EnterpriseCityRecord contains city data for Enterprise database records.
type EnterpriseCityRecord struct {
	// Names contains localized names for the city
	Names Names `json:"names,omitzero"      maxminddb:"names"`
	// GeoNameID for the city
	GeoNameID uint `json:"geoname_id,omitzero" maxminddb:"geoname_id"`
	// Confidence is a value from 0-100 indicating MaxMind's confidence that
	// the city is correct
	Confidence uint8 `json:"confidence,omitzero" maxminddb:"confidence"`
}

// HasData returns true if the EnterpriseCityRecord has any data.
func (c EnterpriseCityRecord) HasData() bool {
	return c != zeroEnterpriseCityRecord
}

// EnterprisePostal contains postal data for Enterprise database records.
type EnterprisePostal struct {
	// Code is the postal code of the location. Postal codes are not
	// available for all countries.
	// In some countries, this will only contain part of the postal code.
	Code string `json:"code,omitzero"       maxminddb:"code"`
	// Confidence is a value from 0-100 indicating MaxMind's confidence that
	// the postal code is correct
	Confidence uint8 `json:"confidence,omitzero" maxminddb:"confidence"`
}

// HasData returns true if the EnterprisePostal has any data.
func (p EnterprisePostal) HasData() bool {
	return p != zeroEnterprisePostal
}

// EnterpriseSubdivision contains subdivision data for Enterprise database records.
type EnterpriseSubdivision struct {
	// Names contains localized names for the subdivision
	Names Names `json:"names,omitzero"      maxminddb:"names"`
	// ISOCode is a string up to three characters long containing the
	// subdivision portion of the ISO 3166-2 code.
	// See https://en.wikipedia.org/wiki/ISO_3166-2
	ISOCode string `json:"iso_code,omitzero"   maxminddb:"iso_code"`
	// GeoNameID for the subdivision
	GeoNameID uint `json:"geoname_id,omitzero" maxminddb:"geoname_id"`
	// Confidence is a value from 0-100 indicating MaxMind's confidence that
	// the subdivision is correct
	Confidence uint8 `json:"confidence,omitzero" maxminddb:"confidence"`
}

// HasData returns true if the EnterpriseSubdivision has any data.
func (s EnterpriseSubdivision) HasData() bool {
	return s != zeroEnterpriseSubdivision
}

// EnterpriseCountryRecord contains country data for Enterprise database records.
type EnterpriseCountryRecord struct {
	// Names contains localized names for the country
	Names Names `json:"names,omitzero"                maxminddb:"names"`
	// ISOCode is the two-character ISO 3166-1 alpha code for the country.
	// See https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2
	ISOCode string `json:"iso_code,omitzero"             maxminddb:"iso_code"`
	// GeoNameID for the country
	GeoNameID uint `json:"geoname_id,omitzero"           maxminddb:"geoname_id"`
	// Confidence is a value from 0-100 indicating MaxMind's confidence that
	// the country is correct
	Confidence uint8 `json:"confidence,omitzero"           maxminddb:"confidence"`
	// IsInEuropeanUnion is true if the country is a member state of the
	// European Union
	IsInEuropeanUnion bool `json:"is_in_european_union,omitzero" maxminddb:"is_in_european_union"`
}

// HasData returns true if the EnterpriseCountryRecord has any data.
func (c EnterpriseCountryRecord) HasData() bool {
	return c != zeroEnterpriseCountryRecord
}

// EnterpriseTraits contains traits data for Enterprise database records.
type EnterpriseTraits struct {
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// AutonomousSystemOrganization for the registered ASN
	AutonomousSystemOrganization string `json:"autonomous_system_organization,omitzero" maxminddb:"autonomous_system_organization"` //nolint:lll
	// ConnectionType indicates the connection type. May be Dialup,
	// Cable/DSL, Corporate, Cellular, or Satellite
	ConnectionType string `json:"connection_type,omitzero"                maxminddb:"connection_type"`
	// Domain is the second level domain associated with the IP address
	// (e.g., "example.com")
	Domain string `json:"domain,omitzero"                         maxminddb:"domain"`
	// ISP is the name of the ISP associated with the IP address
	ISP string `json:"isp,omitzero"                            maxminddb:"isp"`
	// MobileCountryCode is the mobile country code (MCC) associated with
	// the IP address and ISP.
	// See https://en.wikipedia.org/wiki/Mobile_country_code
	MobileCountryCode string `json:"mobile_country_code,omitzero"            maxminddb:"mobile_country_code"`
	// MobileNetworkCode is the mobile network code (MNC) associated with
	// the IP address and ISP.
	// See https://en.wikipedia.org/wiki/Mobile_network_code
	MobileNetworkCode string `json:"mobile_network_code,omitzero"            maxminddb:"mobile_network_code"`
	// Organization is the name of the organization associated with the IP
	// address
	Organization string `json:"organization,omitzero"                   maxminddb:"organization"`
	// UserType indicates the user type associated with the IP address
	// (business, cafe, cellular, college, etc.)
	UserType string `json:"user_type,omitzero"                      maxminddb:"user_type"`
	// StaticIPScore is an indicator of how static or dynamic an IP address
	// is, ranging from 0 to 99.99
	StaticIPScore float64 `json:"static_ip_score,omitzero"                maxminddb:"static_ip_score"`
	// AutonomousSystemNumber for the IP address
	AutonomousSystemNumber uint `json:"autonomous_system_number,omitzero"       maxminddb:"autonomous_system_number"`
	// IsAnycast is true if the IP address belongs to an anycast network.
	// See https://en.wikipedia.org/wiki/Anycast
	IsAnycast bool `json:"is_anycast,omitzero"                     maxminddb:"is_anycast"`
	// IsLegitimateProxy is true if MaxMind believes this IP address to be a
	// legitimate proxy, such as an internal VPN used by a corporation
	IsLegitimateProxy bool `json:"is_legitimate_proxy,omitzero"            maxminddb:"is_legitimate_proxy"`
}

// HasData returns true if the EnterpriseTraits has any data (excluding Network and IPAddress).
func (t EnterpriseTraits) HasData() bool {
	return t.AutonomousSystemOrganization != "" || t.ConnectionType != "" ||
		t.Domain != "" || t.ISP != "" || t.MobileCountryCode != "" ||
		t.MobileNetworkCode != "" || t.Organization != "" ||
		t.UserType != "" || t.StaticIPScore != 0 ||
		t.AutonomousSystemNumber != 0 || t.IsAnycast ||
		t.IsLegitimateProxy
}

// City/Country-specific types

// CityRecord contains city data for City database records.
type CityRecord struct {
	// Names contains localized names for the city
	Names Names `json:"names,omitzero"      maxminddb:"names"`
	// GeoNameID for the city
	GeoNameID uint `json:"geoname_id,omitzero" maxminddb:"geoname_id"`
}

// HasData returns true if the CityRecord has any data.
func (c CityRecord) HasData() bool {
	return c != zeroCityRecord
}

// CityPostal contains postal data for City database records.
type CityPostal struct {
	// Code is the postal code of the location. Postal codes are not
	// available for all countries.
	// In some countries, this will only contain part of the postal code.
	Code string `json:"code,omitzero" maxminddb:"code"`
}

// HasData returns true if the CityPostal has any data.
func (p CityPostal) HasData() bool {
	return p != zeroCityPostal
}

// CitySubdivision contains subdivision data for City database records.
type CitySubdivision struct {
	Names     Names  `json:"names,omitzero"      maxminddb:"names"`
	ISOCode   string `json:"iso_code,omitzero"   maxminddb:"iso_code"`
	GeoNameID uint   `json:"geoname_id,omitzero" maxminddb:"geoname_id"`
}

// HasData returns true if the CitySubdivision has any data.
func (s CitySubdivision) HasData() bool {
	return s != zeroCitySubdivision
}

// CountryRecord contains country data for City and Country database records.
type CountryRecord struct {
	// Names contains localized names for the country
	Names Names `json:"names,omitzero"                maxminddb:"names"`
	// ISOCode is the two-character ISO 3166-1 alpha code for the country.
	// See https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2
	ISOCode string `json:"iso_code,omitzero"             maxminddb:"iso_code"`
	// GeoNameID for the country
	GeoNameID uint `json:"geoname_id,omitzero"           maxminddb:"geoname_id"`
	// IsInEuropeanUnion is true if the country is a member state of the
	// European Union
	IsInEuropeanUnion bool `json:"is_in_european_union,omitzero" maxminddb:"is_in_european_union"`
}

// HasData returns true if the CountryRecord has any data.
func (c CountryRecord) HasData() bool {
	return c != zeroCountryRecord
}

// CityTraits contains traits data for City database records.
type CityTraits struct {
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the network prefix for this record. This is the largest
	// network where all of the fields besides IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// IsAnycast is true if the IP address belongs to an anycast network.
	// See https://en.wikipedia.org/wiki/Anycast
	IsAnycast bool `json:"is_anycast,omitzero" maxminddb:"is_anycast"`
}

// HasData returns true if the CityTraits has any data (excluding Network and IPAddress).
func (t CityTraits) HasData() bool {
	return t.IsAnycast
}

// CountryTraits contains traits data for Country database records.
type CountryTraits struct {
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// IsAnycast is true if the IP address belongs to an anycast network.
	// See https://en.wikipedia.org/wiki/Anycast
	IsAnycast bool `json:"is_anycast,omitzero" maxminddb:"is_anycast"`
}

// HasData returns true if the CountryTraits has any data (excluding Network and IPAddress).
func (t CountryTraits) HasData() bool {
	return t.IsAnycast
}

// The Enterprise struct corresponds to the data in the GeoIP2 Enterprise
// database.
type Enterprise struct {
	// Continent contains data for the continent record associated with the IP
	// address.
	Continent Continent `json:"continent,omitzero"           maxminddb:"continent"`
	// Subdivisions contains data for the subdivisions associated with the IP
	// address. The subdivisions array is ordered from largest to smallest. For
	// instance, the response for Oxford in the United Kingdom would have England
	// as the first element and Oxfordshire as the second element.
	Subdivisions []EnterpriseSubdivision `json:"subdivisions,omitzero"        maxminddb:"subdivisions"`
	// Postal contains data for the postal record associated with the IP address.
	Postal EnterprisePostal `json:"postal,omitzero"              maxminddb:"postal"`
	// RepresentedCountry contains data for the represented country associated
	// with the IP address. The represented country is the country represented
	// by something like a military base or embassy.
	RepresentedCountry RepresentedCountry `json:"represented_country,omitzero" maxminddb:"represented_country"`
	// Country contains data for the country record associated with the IP
	// address. This record represents the country where MaxMind believes the IP
	// is located.
	Country EnterpriseCountryRecord `json:"country,omitzero"             maxminddb:"country"`
	// RegisteredCountry contains data for the registered country associated
	// with the IP address. This record represents the country where the ISP has
	// registered the IP block and may differ from the user's country.
	RegisteredCountry CountryRecord `json:"registered_country,omitzero"  maxminddb:"registered_country"`
	// City contains data for the city record associated with the IP address.
	City EnterpriseCityRecord `json:"city,omitzero"                maxminddb:"city"`
	// Location contains data for the location record associated with the IP
	// address
	Location Location `json:"location,omitzero"            maxminddb:"location"`
	// Traits contains various traits associated with the IP address
	Traits EnterpriseTraits `json:"traits,omitzero"              maxminddb:"traits"`
}

// HasData returns true if any GeoIP data was found for the IP in the Enterprise database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (e Enterprise) HasData() bool {
	return e.Continent.HasData() || e.City.HasData() || e.Postal.HasData() ||
		e.hasSubdivisionsData() || e.RepresentedCountry.HasData() ||
		e.Country.HasData() || e.RegisteredCountry.HasData() ||
		e.Traits.HasData() || e.Location.HasData()
}

func (e Enterprise) hasSubdivisionsData() bool {
	for _, sub := range e.Subdivisions {
		if sub.HasData() {
			return true
		}
	}
	return false
}

// The City struct corresponds to the data in the GeoIP2/GeoLite2 City
// databases.
type City struct {
	// Traits contains various traits associated with the IP address
	Traits CityTraits `json:"traits,omitzero"              maxminddb:"traits"`
	// Postal contains data for the postal record associated with the IP address
	Postal CityPostal `json:"postal,omitzero"              maxminddb:"postal"`
	// Continent contains data for the continent record associated with the IP address
	Continent Continent `json:"continent,omitzero"           maxminddb:"continent"`
	// City contains data for the city record associated with the IP address
	City CityRecord `json:"city,omitzero"                maxminddb:"city"`
	// Subdivisions contains data for the subdivisions associated with the IP
	// address. The subdivisions array is ordered from largest to smallest. For
	// instance, the response for Oxford in the United Kingdom would have England
	// as the first element and Oxfordshire as the second element.
	Subdivisions []CitySubdivision `json:"subdivisions,omitzero"        maxminddb:"subdivisions"`
	// RepresentedCountry contains data for the represented country associated
	// with the IP address. The represented country is the country represented
	// by something like a military base or embassy.
	RepresentedCountry RepresentedCountry `json:"represented_country,omitzero" maxminddb:"represented_country"`
	// Country contains data for the country record associated with the IP
	// address. This record represents the country where MaxMind believes the IP
	// is located.
	Country CountryRecord `json:"country,omitzero"             maxminddb:"country"`
	// RegisteredCountry contains data for the registered country associated
	// with the IP address. This record represents the country where the ISP has
	// registered the IP block and may differ from the user's country.
	RegisteredCountry CountryRecord `json:"registered_country,omitzero"  maxminddb:"registered_country"`
	// Location contains data for the location record associated with the IP
	// address
	Location Location `json:"location,omitzero"            maxminddb:"location"`
}

// HasData returns true if any GeoIP data was found for the IP in the City database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (c City) HasData() bool {
	return c.Traits.HasData() || c.Postal.HasData() || c.Continent.HasData() ||
		c.City.HasData() || c.hasSubdivisionsData() || c.RepresentedCountry.HasData() ||
		c.Country.HasData() || c.RegisteredCountry.HasData() || c.Location.HasData()
}

func (c City) hasSubdivisionsData() bool {
	for _, sub := range c.Subdivisions {
		if sub.HasData() {
			return true
		}
	}
	return false
}

// The Country struct corresponds to the data in the GeoIP2/GeoLite2
// Country databases.
type Country struct {
	// Traits contains various traits associated with the IP address
	Traits CountryTraits `json:"traits,omitzero"              maxminddb:"traits"`
	// Continent contains data for the continent record associated with the IP address
	Continent Continent `json:"continent,omitzero"           maxminddb:"continent"`
	// RepresentedCountry contains data for the represented country associated
	// with the IP address. The represented country is the country represented
	// by something like a military base or embassy.
	RepresentedCountry RepresentedCountry `json:"represented_country,omitzero" maxminddb:"represented_country"`
	// Country contains data for the country record associated with the IP
	// address. This record represents the country where MaxMind believes the IP
	// is located.
	Country CountryRecord `json:"country,omitzero"             maxminddb:"country"`
	// RegisteredCountry contains data for the registered country associated
	// with the IP address. This record represents the country where the ISP has
	// registered the IP block and may differ from the user's country.
	RegisteredCountry CountryRecord `json:"registered_country,omitzero"  maxminddb:"registered_country"`
}

// HasData returns true if any GeoIP data was found for the IP in the Country database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (c Country) HasData() bool {
	return c.Continent.HasData() || c.RepresentedCountry.HasData() ||
		c.Country.HasData() || c.RegisteredCountry.HasData() || c.Traits.HasData()
}

// The AnonymousIP struct corresponds to the data in the GeoIP2
// Anonymous IP database.
type AnonymousIP struct {
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// IsAnonymous is true if the IP address belongs to any sort of anonymous network.
	IsAnonymous bool `json:"is_anonymous,omitzero"         maxminddb:"is_anonymous"`
	// IsAnonymousVPN is true if the IP address is registered to an anonymous
	// VPN provider. If a VPN provider does not register subnets under names
	// associated with them, we will likely only flag their IP ranges using the
	// IsHostingProvider attribute.
	IsAnonymousVPN bool `json:"is_anonymous_vpn,omitzero"     maxminddb:"is_anonymous_vpn"`
	// IsHostingProvider is true if the IP address belongs to a hosting or VPN provider
	// (see description of IsAnonymousVPN attribute).
	IsHostingProvider bool `json:"is_hosting_provider,omitzero"  maxminddb:"is_hosting_provider"`
	// IsPublicProxy is true if the IP address belongs to a public proxy.
	IsPublicProxy bool `json:"is_public_proxy,omitzero"      maxminddb:"is_public_proxy"`
	// IsResidentialProxy is true if the IP address is on a suspected
	// anonymizing network and belongs to a residential ISP.
	IsResidentialProxy bool `json:"is_residential_proxy,omitzero" maxminddb:"is_residential_proxy"`
	// IsTorExitNode is true if the IP address is a Tor exit node.
	IsTorExitNode bool `json:"is_tor_exit_node,omitzero"     maxminddb:"is_tor_exit_node"`
}

// HasData returns true if any data was found for the IP in the AnonymousIP database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (a AnonymousIP) HasData() bool {
	return a.IsAnonymous || a.IsAnonymousVPN || a.IsHostingProvider ||
		a.IsPublicProxy || a.IsResidentialProxy || a.IsTorExitNode
}

// The ASN struct corresponds to the data in the GeoLite2 ASN database.
type ASN struct {
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// AutonomousSystemOrganization for the registered autonomous system number.
	AutonomousSystemOrganization string `json:"autonomous_system_organization,omitzero" maxminddb:"autonomous_system_organization"` //nolint:lll
	// AutonomousSystemNumber for the IP address.
	AutonomousSystemNumber uint `json:"autonomous_system_number,omitzero"       maxminddb:"autonomous_system_number"` //nolint:lll
}

// HasData returns true if any data was found for the IP in the ASN database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (a ASN) HasData() bool {
	return a.AutonomousSystemNumber != 0 || a.AutonomousSystemOrganization != ""
}

// The ConnectionType struct corresponds to the data in the GeoIP2
// Connection-Type database.
type ConnectionType struct {
	// ConnectionType indicates the connection type. May be Dialup, Cable/DSL,
	// Corporate, Cellular, or Satellite. Additional values may be added in the
	// future.
	ConnectionType string `json:"connection_type,omitzero" maxminddb:"connection_type"`
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
}

// HasData returns true if any data was found for the IP in the ConnectionType database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (c ConnectionType) HasData() bool {
	return c.ConnectionType != ""
}

// The Domain struct corresponds to the data in the GeoIP2 Domain database.
type Domain struct {
	// Domain is the second level domain associated with the IP address
	// (e.g., "example.com")
	Domain string `json:"domain,omitzero"     maxminddb:"domain"`
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
}

// HasData returns true if any data was found for the IP in the Domain database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (d Domain) HasData() bool {
	return d.Domain != ""
}

// The ISP struct corresponds to the data in the GeoIP2 ISP database.
type ISP struct {
	// Network is the largest network prefix where all fields besides
	// IPAddress have the same value.
	Network netip.Prefix `json:"network,omitzero"`
	// IPAddress is the IP address used during the lookup
	IPAddress netip.Addr `json:"ip_address,omitzero"`
	// AutonomousSystemOrganization for the registered ASN
	AutonomousSystemOrganization string `json:"autonomous_system_organization,omitzero" maxminddb:"autonomous_system_organization"` //nolint:lll
	// ISP is the name of the ISP associated with the IP address
	ISP string `json:"isp,omitzero"                            maxminddb:"isp"`
	// MobileCountryCode is the mobile country code (MCC) associated with the IP address and ISP.
	// See https://en.wikipedia.org/wiki/Mobile_country_code
	MobileCountryCode string `json:"mobile_country_code,omitzero"            maxminddb:"mobile_country_code"`
	// MobileNetworkCode is the mobile network code (MNC) associated with the IP address and ISP.
	// See https://en.wikipedia.org/wiki/Mobile_network_code
	MobileNetworkCode string `json:"mobile_network_code,omitzero"            maxminddb:"mobile_network_code"`
	// Organization is the name of the organization associated with the IP address
	Organization string `json:"organization,omitzero"                   maxminddb:"organization"`
	// AutonomousSystemNumber for the IP address
	AutonomousSystemNumber uint `json:"autonomous_system_number,omitzero"       maxminddb:"autonomous_system_number"`
}

// HasData returns true if any data was found for the IP in the ISP database.
// This excludes the Network and IPAddress fields which are always populated for found IPs.
func (i ISP) HasData() bool {
	return i.AutonomousSystemOrganization != "" || i.ISP != "" ||
		i.MobileCountryCode != "" || i.MobileNetworkCode != "" ||
		i.Organization != "" || i.AutonomousSystemNumber != 0
}
