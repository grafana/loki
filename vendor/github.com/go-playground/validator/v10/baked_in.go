package validator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/sha3"
	"golang.org/x/text/language"

	urn "github.com/leodido/go-urn"
)

// Func accepts a FieldLevel interface for all validation needs. The return
// value should be true when validation succeeds.
type Func func(fl FieldLevel) bool

// FuncCtx accepts a context.Context and FieldLevel interface for all
// validation needs. The return value should be true when validation succeeds.
type FuncCtx func(ctx context.Context, fl FieldLevel) bool

// wrapFunc wraps noramal Func makes it compatible with FuncCtx
func wrapFunc(fn Func) FuncCtx {
	if fn == nil {
		return nil // be sure not to wrap a bad function.
	}
	return func(ctx context.Context, fl FieldLevel) bool {
		return fn(fl)
	}
}

var (
	restrictedTags = map[string]struct{}{
		diveTag:           {},
		keysTag:           {},
		endKeysTag:        {},
		structOnlyTag:     {},
		omitempty:         {},
		skipValidationTag: {},
		utf8HexComma:      {},
		utf8Pipe:          {},
		noStructLevelTag:  {},
		requiredTag:       {},
		isdefault:         {},
	}

	// bakedInAliases is a default mapping of a single validation tag that
	// defines a common or complex set of validation(s) to simplify
	// adding validation to structs.
	bakedInAliases = map[string]string{
		"iscolor":      "hexcolor|rgb|rgba|hsl|hsla",
		"country_code": "iso3166_1_alpha2|iso3166_1_alpha3|iso3166_1_alpha_numeric",
	}

	// bakedInValidators is the default map of ValidationFunc
	// you can add, remove or even replace items to suite your needs,
	// or even disregard and use your own map if so desired.
	bakedInValidators = map[string]Func{
		"required":                      hasValue,
		"required_if":                   requiredIf,
		"required_unless":               requiredUnless,
		"required_with":                 requiredWith,
		"required_with_all":             requiredWithAll,
		"required_without":              requiredWithout,
		"required_without_all":          requiredWithoutAll,
		"excluded_if":                   excludedIf,
		"excluded_unless":               excludedUnless,
		"excluded_with":                 excludedWith,
		"excluded_with_all":             excludedWithAll,
		"excluded_without":              excludedWithout,
		"excluded_without_all":          excludedWithoutAll,
		"isdefault":                     isDefault,
		"len":                           hasLengthOf,
		"min":                           hasMinOf,
		"max":                           hasMaxOf,
		"eq":                            isEq,
		"ne":                            isNe,
		"lt":                            isLt,
		"lte":                           isLte,
		"gt":                            isGt,
		"gte":                           isGte,
		"eqfield":                       isEqField,
		"eqcsfield":                     isEqCrossStructField,
		"necsfield":                     isNeCrossStructField,
		"gtcsfield":                     isGtCrossStructField,
		"gtecsfield":                    isGteCrossStructField,
		"ltcsfield":                     isLtCrossStructField,
		"ltecsfield":                    isLteCrossStructField,
		"nefield":                       isNeField,
		"gtefield":                      isGteField,
		"gtfield":                       isGtField,
		"ltefield":                      isLteField,
		"ltfield":                       isLtField,
		"fieldcontains":                 fieldContains,
		"fieldexcludes":                 fieldExcludes,
		"alpha":                         isAlpha,
		"alphanum":                      isAlphanum,
		"alphaunicode":                  isAlphaUnicode,
		"alphanumunicode":               isAlphanumUnicode,
		"boolean":                       isBoolean,
		"numeric":                       isNumeric,
		"number":                        isNumber,
		"hexadecimal":                   isHexadecimal,
		"hexcolor":                      isHEXColor,
		"rgb":                           isRGB,
		"rgba":                          isRGBA,
		"hsl":                           isHSL,
		"hsla":                          isHSLA,
		"e164":                          isE164,
		"email":                         isEmail,
		"url":                           isURL,
		"uri":                           isURI,
		"urn_rfc2141":                   isUrnRFC2141, // RFC 2141
		"file":                          isFile,
		"base64":                        isBase64,
		"base64url":                     isBase64URL,
		"contains":                      contains,
		"containsany":                   containsAny,
		"containsrune":                  containsRune,
		"excludes":                      excludes,
		"excludesall":                   excludesAll,
		"excludesrune":                  excludesRune,
		"startswith":                    startsWith,
		"endswith":                      endsWith,
		"startsnotwith":                 startsNotWith,
		"endsnotwith":                   endsNotWith,
		"isbn":                          isISBN,
		"isbn10":                        isISBN10,
		"isbn13":                        isISBN13,
		"eth_addr":                      isEthereumAddress,
		"btc_addr":                      isBitcoinAddress,
		"btc_addr_bech32":               isBitcoinBech32Address,
		"uuid":                          isUUID,
		"uuid3":                         isUUID3,
		"uuid4":                         isUUID4,
		"uuid5":                         isUUID5,
		"uuid_rfc4122":                  isUUIDRFC4122,
		"uuid3_rfc4122":                 isUUID3RFC4122,
		"uuid4_rfc4122":                 isUUID4RFC4122,
		"uuid5_rfc4122":                 isUUID5RFC4122,
		"ulid":                          isULID,
		"md4":                           isMD4,
		"md5":                           isMD5,
		"sha256":                        isSHA256,
		"sha384":                        isSHA384,
		"sha512":                        isSHA512,
		"ripemd128":                     isRIPEMD128,
		"ripemd160":                     isRIPEMD160,
		"tiger128":                      isTIGER128,
		"tiger160":                      isTIGER160,
		"tiger192":                      isTIGER192,
		"ascii":                         isASCII,
		"printascii":                    isPrintableASCII,
		"multibyte":                     hasMultiByteCharacter,
		"datauri":                       isDataURI,
		"latitude":                      isLatitude,
		"longitude":                     isLongitude,
		"ssn":                           isSSN,
		"ipv4":                          isIPv4,
		"ipv6":                          isIPv6,
		"ip":                            isIP,
		"cidrv4":                        isCIDRv4,
		"cidrv6":                        isCIDRv6,
		"cidr":                          isCIDR,
		"tcp4_addr":                     isTCP4AddrResolvable,
		"tcp6_addr":                     isTCP6AddrResolvable,
		"tcp_addr":                      isTCPAddrResolvable,
		"udp4_addr":                     isUDP4AddrResolvable,
		"udp6_addr":                     isUDP6AddrResolvable,
		"udp_addr":                      isUDPAddrResolvable,
		"ip4_addr":                      isIP4AddrResolvable,
		"ip6_addr":                      isIP6AddrResolvable,
		"ip_addr":                       isIPAddrResolvable,
		"unix_addr":                     isUnixAddrResolvable,
		"mac":                           isMAC,
		"hostname":                      isHostnameRFC952,  // RFC 952
		"hostname_rfc1123":              isHostnameRFC1123, // RFC 1123
		"fqdn":                          isFQDN,
		"unique":                        isUnique,
		"oneof":                         isOneOf,
		"html":                          isHTML,
		"html_encoded":                  isHTMLEncoded,
		"url_encoded":                   isURLEncoded,
		"dir":                           isDir,
		"json":                          isJSON,
		"jwt":                           isJWT,
		"hostname_port":                 isHostnamePort,
		"lowercase":                     isLowercase,
		"uppercase":                     isUppercase,
		"datetime":                      isDatetime,
		"timezone":                      isTimeZone,
		"iso3166_1_alpha2":              isIso3166Alpha2,
		"iso3166_1_alpha3":              isIso3166Alpha3,
		"iso3166_1_alpha_numeric":       isIso3166AlphaNumeric,
		"iso3166_2":                     isIso31662,
		"iso4217":                       isIso4217,
		"iso4217_numeric":               isIso4217Numeric,
		"bcp47_language_tag":            isBCP47LanguageTag,
		"postcode_iso3166_alpha2":       isPostcodeByIso3166Alpha2,
		"postcode_iso3166_alpha2_field": isPostcodeByIso3166Alpha2Field,
		"bic":                           isIsoBicFormat,
		"semver":                        isSemverFormat,
		"dns_rfc1035_label":             isDnsRFC1035LabelFormat,
		"credit_card":                   isCreditCard,
	}
)

var (
	oneofValsCache       = map[string][]string{}
	oneofValsCacheRWLock = sync.RWMutex{}
)

func parseOneOfParam2(s string) []string {
	oneofValsCacheRWLock.RLock()
	vals, ok := oneofValsCache[s]
	oneofValsCacheRWLock.RUnlock()
	if !ok {
		oneofValsCacheRWLock.Lock()
		vals = splitParamsRegex.FindAllString(s, -1)
		for i := 0; i < len(vals); i++ {
			vals[i] = strings.Replace(vals[i], "'", "", -1)
		}
		oneofValsCache[s] = vals
		oneofValsCacheRWLock.Unlock()
	}
	return vals
}

func isURLEncoded(fl FieldLevel) bool {
	return uRLEncodedRegex.MatchString(fl.Field().String())
}

func isHTMLEncoded(fl FieldLevel) bool {
	return hTMLEncodedRegex.MatchString(fl.Field().String())
}

func isHTML(fl FieldLevel) bool {
	return hTMLRegex.MatchString(fl.Field().String())
}

func isOneOf(fl FieldLevel) bool {
	vals := parseOneOfParam2(fl.Param())

	field := fl.Field()

	var v string
	switch field.Kind() {
	case reflect.String:
		v = field.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v = strconv.FormatInt(field.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v = strconv.FormatUint(field.Uint(), 10)
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}
	for i := 0; i < len(vals); i++ {
		if vals[i] == v {
			return true
		}
	}
	return false
}

// isUnique is the validation function for validating if each array|slice|map value is unique
func isUnique(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()
	v := reflect.ValueOf(struct{}{})

	switch field.Kind() {
	case reflect.Slice, reflect.Array:
		elem := field.Type().Elem()
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}

		if param == "" {
			m := reflect.MakeMap(reflect.MapOf(elem, v.Type()))

			for i := 0; i < field.Len(); i++ {
				m.SetMapIndex(reflect.Indirect(field.Index(i)), v)
			}
			return field.Len() == m.Len()
		}

		sf, ok := elem.FieldByName(param)
		if !ok {
			panic(fmt.Sprintf("Bad field name %s", param))
		}

		sfTyp := sf.Type
		if sfTyp.Kind() == reflect.Ptr {
			sfTyp = sfTyp.Elem()
		}

		m := reflect.MakeMap(reflect.MapOf(sfTyp, v.Type()))
		for i := 0; i < field.Len(); i++ {
			m.SetMapIndex(reflect.Indirect(reflect.Indirect(field.Index(i)).FieldByName(param)), v)
		}
		return field.Len() == m.Len()
	case reflect.Map:
		m := reflect.MakeMap(reflect.MapOf(field.Type().Elem(), v.Type()))

		for _, k := range field.MapKeys() {
			m.SetMapIndex(field.MapIndex(k), v)
		}
		return field.Len() == m.Len()
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}
}

// isMAC is the validation function for validating if the field's value is a valid MAC address.
func isMAC(fl FieldLevel) bool {
	_, err := net.ParseMAC(fl.Field().String())

	return err == nil
}

// isCIDRv4 is the validation function for validating if the field's value is a valid v4 CIDR address.
func isCIDRv4(fl FieldLevel) bool {
	ip, _, err := net.ParseCIDR(fl.Field().String())

	return err == nil && ip.To4() != nil
}

// isCIDRv6 is the validation function for validating if the field's value is a valid v6 CIDR address.
func isCIDRv6(fl FieldLevel) bool {
	ip, _, err := net.ParseCIDR(fl.Field().String())

	return err == nil && ip.To4() == nil
}

// isCIDR is the validation function for validating if the field's value is a valid v4 or v6 CIDR address.
func isCIDR(fl FieldLevel) bool {
	_, _, err := net.ParseCIDR(fl.Field().String())

	return err == nil
}

// isIPv4 is the validation function for validating if a value is a valid v4 IP address.
func isIPv4(fl FieldLevel) bool {
	ip := net.ParseIP(fl.Field().String())

	return ip != nil && ip.To4() != nil
}

// isIPv6 is the validation function for validating if the field's value is a valid v6 IP address.
func isIPv6(fl FieldLevel) bool {
	ip := net.ParseIP(fl.Field().String())

	return ip != nil && ip.To4() == nil
}

// isIP is the validation function for validating if the field's value is a valid v4 or v6 IP address.
func isIP(fl FieldLevel) bool {
	ip := net.ParseIP(fl.Field().String())

	return ip != nil
}

// isSSN is the validation function for validating if the field's value is a valid SSN.
func isSSN(fl FieldLevel) bool {
	field := fl.Field()

	if field.Len() != 11 {
		return false
	}

	return sSNRegex.MatchString(field.String())
}

// isLongitude is the validation function for validating if the field's value is a valid longitude coordinate.
func isLongitude(fl FieldLevel) bool {
	field := fl.Field()

	var v string
	switch field.Kind() {
	case reflect.String:
		v = field.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v = strconv.FormatInt(field.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v = strconv.FormatUint(field.Uint(), 10)
	case reflect.Float32:
		v = strconv.FormatFloat(field.Float(), 'f', -1, 32)
	case reflect.Float64:
		v = strconv.FormatFloat(field.Float(), 'f', -1, 64)
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}

	return longitudeRegex.MatchString(v)
}

// isLatitude is the validation function for validating if the field's value is a valid latitude coordinate.
func isLatitude(fl FieldLevel) bool {
	field := fl.Field()

	var v string
	switch field.Kind() {
	case reflect.String:
		v = field.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v = strconv.FormatInt(field.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v = strconv.FormatUint(field.Uint(), 10)
	case reflect.Float32:
		v = strconv.FormatFloat(field.Float(), 'f', -1, 32)
	case reflect.Float64:
		v = strconv.FormatFloat(field.Float(), 'f', -1, 64)
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}

	return latitudeRegex.MatchString(v)
}

// isDataURI is the validation function for validating if the field's value is a valid data URI.
func isDataURI(fl FieldLevel) bool {
	uri := strings.SplitN(fl.Field().String(), ",", 2)

	if len(uri) != 2 {
		return false
	}

	if !dataURIRegex.MatchString(uri[0]) {
		return false
	}

	return base64Regex.MatchString(uri[1])
}

// hasMultiByteCharacter is the validation function for validating if the field's value has a multi byte character.
func hasMultiByteCharacter(fl FieldLevel) bool {
	field := fl.Field()

	if field.Len() == 0 {
		return true
	}

	return multibyteRegex.MatchString(field.String())
}

// isPrintableASCII is the validation function for validating if the field's value is a valid printable ASCII character.
func isPrintableASCII(fl FieldLevel) bool {
	return printableASCIIRegex.MatchString(fl.Field().String())
}

// isASCII is the validation function for validating if the field's value is a valid ASCII character.
func isASCII(fl FieldLevel) bool {
	return aSCIIRegex.MatchString(fl.Field().String())
}

// isUUID5 is the validation function for validating if the field's value is a valid v5 UUID.
func isUUID5(fl FieldLevel) bool {
	return uUID5Regex.MatchString(fl.Field().String())
}

// isUUID4 is the validation function for validating if the field's value is a valid v4 UUID.
func isUUID4(fl FieldLevel) bool {
	return uUID4Regex.MatchString(fl.Field().String())
}

// isUUID3 is the validation function for validating if the field's value is a valid v3 UUID.
func isUUID3(fl FieldLevel) bool {
	return uUID3Regex.MatchString(fl.Field().String())
}

// isUUID is the validation function for validating if the field's value is a valid UUID of any version.
func isUUID(fl FieldLevel) bool {
	return uUIDRegex.MatchString(fl.Field().String())
}

// isUUID5RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v5 UUID.
func isUUID5RFC4122(fl FieldLevel) bool {
	return uUID5RFC4122Regex.MatchString(fl.Field().String())
}

// isUUID4RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v4 UUID.
func isUUID4RFC4122(fl FieldLevel) bool {
	return uUID4RFC4122Regex.MatchString(fl.Field().String())
}

// isUUID3RFC4122 is the validation function for validating if the field's value is a valid RFC4122 v3 UUID.
func isUUID3RFC4122(fl FieldLevel) bool {
	return uUID3RFC4122Regex.MatchString(fl.Field().String())
}

// isUUIDRFC4122 is the validation function for validating if the field's value is a valid RFC4122 UUID of any version.
func isUUIDRFC4122(fl FieldLevel) bool {
	return uUIDRFC4122Regex.MatchString(fl.Field().String())
}

// isULID is the validation function for validating if the field's value is a valid ULID.
func isULID(fl FieldLevel) bool {
	return uLIDRegex.MatchString(fl.Field().String())
}

// isMD4 is the validation function for validating if the field's value is a valid MD4.
func isMD4(fl FieldLevel) bool {
	return md4Regex.MatchString(fl.Field().String())
}

// isMD5 is the validation function for validating if the field's value is a valid MD5.
func isMD5(fl FieldLevel) bool {
	return md5Regex.MatchString(fl.Field().String())
}

// isSHA256 is the validation function for validating if the field's value is a valid SHA256.
func isSHA256(fl FieldLevel) bool {
	return sha256Regex.MatchString(fl.Field().String())
}

// isSHA384 is the validation function for validating if the field's value is a valid SHA384.
func isSHA384(fl FieldLevel) bool {
	return sha384Regex.MatchString(fl.Field().String())
}

// isSHA512 is the validation function for validating if the field's value is a valid SHA512.
func isSHA512(fl FieldLevel) bool {
	return sha512Regex.MatchString(fl.Field().String())
}

// isRIPEMD128 is the validation function for validating if the field's value is a valid PIPEMD128.
func isRIPEMD128(fl FieldLevel) bool {
	return ripemd128Regex.MatchString(fl.Field().String())
}

// isRIPEMD160 is the validation function for validating if the field's value is a valid PIPEMD160.
func isRIPEMD160(fl FieldLevel) bool {
	return ripemd160Regex.MatchString(fl.Field().String())
}

// isTIGER128 is the validation function for validating if the field's value is a valid TIGER128.
func isTIGER128(fl FieldLevel) bool {
	return tiger128Regex.MatchString(fl.Field().String())
}

// isTIGER160 is the validation function for validating if the field's value is a valid TIGER160.
func isTIGER160(fl FieldLevel) bool {
	return tiger160Regex.MatchString(fl.Field().String())
}

// isTIGER192 is the validation function for validating if the field's value is a valid isTIGER192.
func isTIGER192(fl FieldLevel) bool {
	return tiger192Regex.MatchString(fl.Field().String())
}

// isISBN is the validation function for validating if the field's value is a valid v10 or v13 ISBN.
func isISBN(fl FieldLevel) bool {
	return isISBN10(fl) || isISBN13(fl)
}

// isISBN13 is the validation function for validating if the field's value is a valid v13 ISBN.
func isISBN13(fl FieldLevel) bool {
	s := strings.Replace(strings.Replace(fl.Field().String(), "-", "", 4), " ", "", 4)

	if !iSBN13Regex.MatchString(s) {
		return false
	}

	var checksum int32
	var i int32

	factor := []int32{1, 3}

	for i = 0; i < 12; i++ {
		checksum += factor[i%2] * int32(s[i]-'0')
	}

	return (int32(s[12]-'0'))-((10-(checksum%10))%10) == 0
}

// isISBN10 is the validation function for validating if the field's value is a valid v10 ISBN.
func isISBN10(fl FieldLevel) bool {
	s := strings.Replace(strings.Replace(fl.Field().String(), "-", "", 3), " ", "", 3)

	if !iSBN10Regex.MatchString(s) {
		return false
	}

	var checksum int32
	var i int32

	for i = 0; i < 9; i++ {
		checksum += (i + 1) * int32(s[i]-'0')
	}

	if s[9] == 'X' {
		checksum += 10 * 10
	} else {
		checksum += 10 * int32(s[9]-'0')
	}

	return checksum%11 == 0
}

// isEthereumAddress is the validation function for validating if the field's value is a valid Ethereum address.
func isEthereumAddress(fl FieldLevel) bool {
	address := fl.Field().String()

	if !ethAddressRegex.MatchString(address) {
		return false
	}

	if ethAddressRegexUpper.MatchString(address) || ethAddressRegexLower.MatchString(address) {
		return true
	}

	// Checksum validation. Reference: https://github.com/ethereum/EIPs/blob/master/EIPS/eip-55.md
	address = address[2:] // Skip "0x" prefix.
	h := sha3.NewLegacyKeccak256()
	// hash.Hash's io.Writer implementation says it never returns an error. https://golang.org/pkg/hash/#Hash
	_, _ = h.Write([]byte(strings.ToLower(address)))
	hash := hex.EncodeToString(h.Sum(nil))

	for i := 0; i < len(address); i++ {
		if address[i] <= '9' { // Skip 0-9 digits: they don't have upper/lower-case.
			continue
		}
		if hash[i] > '7' && address[i] >= 'a' || hash[i] <= '7' && address[i] <= 'F' {
			return false
		}
	}

	return true
}

// isBitcoinAddress is the validation function for validating if the field's value is a valid btc address
func isBitcoinAddress(fl FieldLevel) bool {
	address := fl.Field().String()

	if !btcAddressRegex.MatchString(address) {
		return false
	}

	alphabet := []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

	decode := [25]byte{}

	for _, n := range []byte(address) {
		d := bytes.IndexByte(alphabet, n)

		for i := 24; i >= 0; i-- {
			d += 58 * int(decode[i])
			decode[i] = byte(d % 256)
			d /= 256
		}
	}

	h := sha256.New()
	_, _ = h.Write(decode[:21])
	d := h.Sum([]byte{})
	h = sha256.New()
	_, _ = h.Write(d)

	validchecksum := [4]byte{}
	computedchecksum := [4]byte{}

	copy(computedchecksum[:], h.Sum(d[:0]))
	copy(validchecksum[:], decode[21:])

	return validchecksum == computedchecksum
}

// isBitcoinBech32Address is the validation function for validating if the field's value is a valid bech32 btc address
func isBitcoinBech32Address(fl FieldLevel) bool {
	address := fl.Field().String()

	if !btcLowerAddressRegexBech32.MatchString(address) && !btcUpperAddressRegexBech32.MatchString(address) {
		return false
	}

	am := len(address) % 8

	if am == 0 || am == 3 || am == 5 {
		return false
	}

	address = strings.ToLower(address)

	alphabet := "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

	hr := []int{3, 3, 0, 2, 3} // the human readable part will always be bc
	addr := address[3:]
	dp := make([]int, 0, len(addr))

	for _, c := range addr {
		dp = append(dp, strings.IndexRune(alphabet, c))
	}

	ver := dp[0]

	if ver < 0 || ver > 16 {
		return false
	}

	if ver == 0 {
		if len(address) != 42 && len(address) != 62 {
			return false
		}
	}

	values := append(hr, dp...)

	GEN := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}

	p := 1

	for _, v := range values {
		b := p >> 25
		p = (p&0x1ffffff)<<5 ^ v

		for i := 0; i < 5; i++ {
			if (b>>uint(i))&1 == 1 {
				p ^= GEN[i]
			}
		}
	}

	if p != 1 {
		return false
	}

	b := uint(0)
	acc := 0
	mv := (1 << 5) - 1
	var sw []int

	for _, v := range dp[1 : len(dp)-6] {
		acc = (acc << 5) | v
		b += 5
		for b >= 8 {
			b -= 8
			sw = append(sw, (acc>>b)&mv)
		}
	}

	if len(sw) < 2 || len(sw) > 40 {
		return false
	}

	return true
}

// excludesRune is the validation function for validating that the field's value does not contain the rune specified within the param.
func excludesRune(fl FieldLevel) bool {
	return !containsRune(fl)
}

// excludesAll is the validation function for validating that the field's value does not contain any of the characters specified within the param.
func excludesAll(fl FieldLevel) bool {
	return !containsAny(fl)
}

// excludes is the validation function for validating that the field's value does not contain the text specified within the param.
func excludes(fl FieldLevel) bool {
	return !contains(fl)
}

// containsRune is the validation function for validating that the field's value contains the rune specified within the param.
func containsRune(fl FieldLevel) bool {
	r, _ := utf8.DecodeRuneInString(fl.Param())

	return strings.ContainsRune(fl.Field().String(), r)
}

// containsAny is the validation function for validating that the field's value contains any of the characters specified within the param.
func containsAny(fl FieldLevel) bool {
	return strings.ContainsAny(fl.Field().String(), fl.Param())
}

// contains is the validation function for validating that the field's value contains the text specified within the param.
func contains(fl FieldLevel) bool {
	return strings.Contains(fl.Field().String(), fl.Param())
}

// startsWith is the validation function for validating that the field's value starts with the text specified within the param.
func startsWith(fl FieldLevel) bool {
	return strings.HasPrefix(fl.Field().String(), fl.Param())
}

// endsWith is the validation function for validating that the field's value ends with the text specified within the param.
func endsWith(fl FieldLevel) bool {
	return strings.HasSuffix(fl.Field().String(), fl.Param())
}

// startsNotWith is the validation function for validating that the field's value does not start with the text specified within the param.
func startsNotWith(fl FieldLevel) bool {
	return !startsWith(fl)
}

// endsNotWith is the validation function for validating that the field's value does not end with the text specified within the param.
func endsNotWith(fl FieldLevel) bool {
	return !endsWith(fl)
}

// fieldContains is the validation function for validating if the current field's value contains the field specified by the param's value.
func fieldContains(fl FieldLevel) bool {
	field := fl.Field()

	currentField, _, ok := fl.GetStructFieldOK()

	if !ok {
		return false
	}

	return strings.Contains(field.String(), currentField.String())
}

// fieldExcludes is the validation function for validating if the current field's value excludes the field specified by the param's value.
func fieldExcludes(fl FieldLevel) bool {
	field := fl.Field()

	currentField, _, ok := fl.GetStructFieldOK()
	if !ok {
		return true
	}

	return !strings.Contains(field.String(), currentField.String())
}

// isNeField is the validation function for validating if the current field's value is not equal to the field specified by the param's value.
func isNeField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()

	if !ok || currentKind != kind {
		return true
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() != currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() != currentField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() != currentField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) != int64(currentField.Len())

	case reflect.Bool:
		return field.Bool() != currentField.Bool()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Interface().(time.Time)
			fieldTime := field.Interface().(time.Time)

			return !fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return true
		}
	}

	// default reflect.String:
	return field.String() != currentField.String()
}

// isNe is the validation function for validating that the field's value does not equal the provided param value.
func isNe(fl FieldLevel) bool {
	return !isEq(fl)
}

// isLteCrossStructField is the validation function for validating if the current field's value is less than or equal to the field, within a separate struct, specified by the param's value.
func isLteCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, topKind, ok := fl.GetStructFieldOK()
	if !ok || topKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() <= topField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() <= topField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() <= topField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) <= int64(topField.Len())

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			fieldTime := field.Convert(timeType).Interface().(time.Time)
			topTime := topField.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(topTime) || fieldTime.Equal(topTime)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return false
		}
	}

	// default reflect.String:
	return field.String() <= topField.String()
}

// isLtCrossStructField is the validation function for validating if the current field's value is less than the field, within a separate struct, specified by the param's value.
// NOTE: This is exposed for use within your own custom functions and not intended to be called directly.
func isLtCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, topKind, ok := fl.GetStructFieldOK()
	if !ok || topKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() < topField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() < topField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() < topField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) < int64(topField.Len())

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			fieldTime := field.Convert(timeType).Interface().(time.Time)
			topTime := topField.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(topTime)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return false
		}
	}

	// default reflect.String:
	return field.String() < topField.String()
}

// isGteCrossStructField is the validation function for validating if the current field's value is greater than or equal to the field, within a separate struct, specified by the param's value.
func isGteCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, topKind, ok := fl.GetStructFieldOK()
	if !ok || topKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() >= topField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() >= topField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() >= topField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) >= int64(topField.Len())

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			fieldTime := field.Convert(timeType).Interface().(time.Time)
			topTime := topField.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(topTime) || fieldTime.Equal(topTime)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return false
		}
	}

	// default reflect.String:
	return field.String() >= topField.String()
}

// isGtCrossStructField is the validation function for validating if the current field's value is greater than the field, within a separate struct, specified by the param's value.
func isGtCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, topKind, ok := fl.GetStructFieldOK()
	if !ok || topKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() > topField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() > topField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() > topField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) > int64(topField.Len())

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			fieldTime := field.Convert(timeType).Interface().(time.Time)
			topTime := topField.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(topTime)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return false
		}
	}

	// default reflect.String:
	return field.String() > topField.String()
}

// isNeCrossStructField is the validation function for validating that the current field's value is not equal to the field, within a separate struct, specified by the param's value.
func isNeCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return true
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return topField.Int() != field.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return topField.Uint() != field.Uint()

	case reflect.Float32, reflect.Float64:
		return topField.Float() != field.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(topField.Len()) != int64(field.Len())

	case reflect.Bool:
		return topField.Bool() != field.Bool()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			t := field.Convert(timeType).Interface().(time.Time)
			fieldTime := topField.Convert(timeType).Interface().(time.Time)

			return !fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return true
		}
	}

	// default reflect.String:
	return topField.String() != field.String()
}

// isEqCrossStructField is the validation function for validating that the current field's value is equal to the field, within a separate struct, specified by the param's value.
func isEqCrossStructField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	topField, topKind, ok := fl.GetStructFieldOK()
	if !ok || topKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return topField.Int() == field.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return topField.Uint() == field.Uint()

	case reflect.Float32, reflect.Float64:
		return topField.Float() == field.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(topField.Len()) == int64(field.Len())

	case reflect.Bool:
		return topField.Bool() == field.Bool()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && topField.Type().ConvertibleTo(timeType) {

			t := field.Convert(timeType).Interface().(time.Time)
			fieldTime := topField.Convert(timeType).Interface().(time.Time)

			return fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != topField.Type() {
			return false
		}
	}

	// default reflect.String:
	return topField.String() == field.String()
}

// isEqField is the validation function for validating if the current field's value is equal to the field specified by the param's value.
func isEqField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() == currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() == currentField.Uint()

	case reflect.Float32, reflect.Float64:
		return field.Float() == currentField.Float()

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) == int64(currentField.Len())

	case reflect.Bool:
		return field.Bool() == currentField.Bool()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String:
	return field.String() == currentField.String()
}

// isEq is the validation function for validating if the current field's value is equal to the param's value.
func isEq(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		return field.String() == param

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) == p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() == p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() == p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() == p

	case reflect.Bool:
		p := asBool(param)

		return field.Bool() == p
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isPostcodeByIso3166Alpha2 validates by value which is country code in iso 3166 alpha 2
// example: `postcode_iso3166_alpha2=US`
func isPostcodeByIso3166Alpha2(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	reg, found := postCodeRegexDict[param]
	if !found {
		return false
	}

	return reg.MatchString(field.String())
}

// isPostcodeByIso3166Alpha2Field validates by field which represents for a value of country code in iso 3166 alpha 2
// example: `postcode_iso3166_alpha2_field=CountryCode`
func isPostcodeByIso3166Alpha2Field(fl FieldLevel) bool {
	field := fl.Field()
	params := parseOneOfParam2(fl.Param())

	if len(params) != 1 {
		return false
	}

	currentField, kind, _, found := fl.GetStructFieldOKAdvanced2(fl.Parent(), params[0])
	if !found {
		return false
	}

	if kind != reflect.String {
		panic(fmt.Sprintf("Bad field type %T", currentField.Interface()))
	}

	reg, found := postCodeRegexDict[currentField.String()]
	if !found {
		return false
	}

	return reg.MatchString(field.String())
}

// isBase64 is the validation function for validating if the current field's value is a valid base 64.
func isBase64(fl FieldLevel) bool {
	return base64Regex.MatchString(fl.Field().String())
}

// isBase64URL is the validation function for validating if the current field's value is a valid base64 URL safe string.
func isBase64URL(fl FieldLevel) bool {
	return base64URLRegex.MatchString(fl.Field().String())
}

// isURI is the validation function for validating if the current field's value is a valid URI.
func isURI(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:

		s := field.String()

		// checks needed as of Go 1.6 because of change https://github.com/golang/go/commit/617c93ce740c3c3cc28cdd1a0d712be183d0b328#diff-6c2d018290e298803c0c9419d8739885L195
		// emulate browser and strip the '#' suffix prior to validation. see issue-#237
		if i := strings.Index(s, "#"); i > -1 {
			s = s[:i]
		}

		if len(s) == 0 {
			return false
		}

		_, err := url.ParseRequestURI(s)

		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isURL is the validation function for validating if the current field's value is a valid URL.
func isURL(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:

		var i int
		s := field.String()

		// checks needed as of Go 1.6 because of change https://github.com/golang/go/commit/617c93ce740c3c3cc28cdd1a0d712be183d0b328#diff-6c2d018290e298803c0c9419d8739885L195
		// emulate browser and strip the '#' suffix prior to validation. see issue-#237
		if i = strings.Index(s, "#"); i > -1 {
			s = s[:i]
		}

		if len(s) == 0 {
			return false
		}

		url, err := url.ParseRequestURI(s)

		if err != nil || url.Scheme == "" {
			return false
		}

		return true
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isUrnRFC2141 is the validation function for validating if the current field's value is a valid URN as per RFC 2141.
func isUrnRFC2141(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:

		str := field.String()

		_, match := urn.Parse([]byte(str))

		return match
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isFile is the validation function for validating if the current field's value is a valid file path.
func isFile(fl FieldLevel) bool {
	field := fl.Field()

	switch field.Kind() {
	case reflect.String:
		fileInfo, err := os.Stat(field.String())
		if err != nil {
			return false
		}

		return !fileInfo.IsDir()
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isE164 is the validation function for validating if the current field's value is a valid e.164 formatted phone number.
func isE164(fl FieldLevel) bool {
	return e164Regex.MatchString(fl.Field().String())
}

// isEmail is the validation function for validating if the current field's value is a valid email address.
func isEmail(fl FieldLevel) bool {
	return emailRegex.MatchString(fl.Field().String())
}

// isHSLA is the validation function for validating if the current field's value is a valid HSLA color.
func isHSLA(fl FieldLevel) bool {
	return hslaRegex.MatchString(fl.Field().String())
}

// isHSL is the validation function for validating if the current field's value is a valid HSL color.
func isHSL(fl FieldLevel) bool {
	return hslRegex.MatchString(fl.Field().String())
}

// isRGBA is the validation function for validating if the current field's value is a valid RGBA color.
func isRGBA(fl FieldLevel) bool {
	return rgbaRegex.MatchString(fl.Field().String())
}

// isRGB is the validation function for validating if the current field's value is a valid RGB color.
func isRGB(fl FieldLevel) bool {
	return rgbRegex.MatchString(fl.Field().String())
}

// isHEXColor is the validation function for validating if the current field's value is a valid HEX color.
func isHEXColor(fl FieldLevel) bool {
	return hexColorRegex.MatchString(fl.Field().String())
}

// isHexadecimal is the validation function for validating if the current field's value is a valid hexadecimal.
func isHexadecimal(fl FieldLevel) bool {
	return hexadecimalRegex.MatchString(fl.Field().String())
}

// isNumber is the validation function for validating if the current field's value is a valid number.
func isNumber(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return true
	default:
		return numberRegex.MatchString(fl.Field().String())
	}
}

// isNumeric is the validation function for validating if the current field's value is a valid numeric value.
func isNumeric(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return true
	default:
		return numericRegex.MatchString(fl.Field().String())
	}
}

// isAlphanum is the validation function for validating if the current field's value is a valid alphanumeric value.
func isAlphanum(fl FieldLevel) bool {
	return alphaNumericRegex.MatchString(fl.Field().String())
}

// isAlpha is the validation function for validating if the current field's value is a valid alpha value.
func isAlpha(fl FieldLevel) bool {
	return alphaRegex.MatchString(fl.Field().String())
}

// isAlphanumUnicode is the validation function for validating if the current field's value is a valid alphanumeric unicode value.
func isAlphanumUnicode(fl FieldLevel) bool {
	return alphaUnicodeNumericRegex.MatchString(fl.Field().String())
}

// isAlphaUnicode is the validation function for validating if the current field's value is a valid alpha unicode value.
func isAlphaUnicode(fl FieldLevel) bool {
	return alphaUnicodeRegex.MatchString(fl.Field().String())
}

// isBoolean is the validation function for validating if the current field's value is a valid boolean value or can be safely converted to a boolean value.
func isBoolean(fl FieldLevel) bool {
	switch fl.Field().Kind() {
	case reflect.Bool:
		return true
	default:
		_, err := strconv.ParseBool(fl.Field().String())
		return err == nil
	}
}

// isDefault is the opposite of required aka hasValue
func isDefault(fl FieldLevel) bool {
	return !hasValue(fl)
}

// hasValue is the validation function for validating if the current field's value is not the default static value.
func hasValue(fl FieldLevel) bool {
	field := fl.Field()
	switch field.Kind() {
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
		return !field.IsNil()
	default:
		if fl.(*validate).fldIsPointer && field.Interface() != nil {
			return true
		}
		return field.IsValid() && field.Interface() != reflect.Zero(field.Type()).Interface()
	}
}

// requireCheckField is a func for check field kind
func requireCheckFieldKind(fl FieldLevel, param string, defaultNotFoundValue bool) bool {
	field := fl.Field()
	kind := field.Kind()
	var nullable, found bool
	if len(param) > 0 {
		field, kind, nullable, found = fl.GetStructFieldOKAdvanced2(fl.Parent(), param)
		if !found {
			return defaultNotFoundValue
		}
	}
	switch kind {
	case reflect.Invalid:
		return defaultNotFoundValue
	case reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
		return field.IsNil()
	default:
		if nullable && field.Interface() != nil {
			return false
		}
		return field.IsValid() && field.Interface() == reflect.Zero(field.Type()).Interface()
	}
}

// requireCheckFieldValue is a func for check field value
func requireCheckFieldValue(fl FieldLevel, param string, value string, defaultNotFoundValue bool) bool {
	field, kind, _, found := fl.GetStructFieldOKAdvanced2(fl.Parent(), param)
	if !found {
		return defaultNotFoundValue
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int() == asInt(value)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint() == asUint(value)

	case reflect.Float32, reflect.Float64:
		return field.Float() == asFloat(value)

	case reflect.Slice, reflect.Map, reflect.Array:
		return int64(field.Len()) == asInt(value)

	case reflect.Bool:
		return field.Bool() == asBool(value)
	}

	// default reflect.String:
	return field.String() == value
}

// requiredIf is the validation function
// The field under validation must be present and not empty only if all the other specified fields are equal to the value following with the specified field.
func requiredIf(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for required_if %s", fl.FieldName()))
	}
	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedIf is the validation function
// The field under validation must not be present or is empty only if all the other specified fields are equal to the value following with the specified field.
func excludedIf(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for excluded_if %s", fl.FieldName()))
	}

	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return false
		}
	}
	return true
}

// requiredUnless is the validation function
// The field under validation must be present and not empty only unless all the other specified fields are equal to the value following with the specified field.
func requiredUnless(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for required_unless %s", fl.FieldName()))
	}

	for i := 0; i < len(params); i += 2 {
		if requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedUnless is the validation function
// The field under validation must not be present or is empty unless all the other specified fields are equal to the value following with the specified field.
func excludedUnless(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	if len(params)%2 != 0 {
		panic(fmt.Sprintf("Bad param number for excluded_unless %s", fl.FieldName()))
	}
	for i := 0; i < len(params); i += 2 {
		if !requireCheckFieldValue(fl, params[i], params[i+1], false) {
			return true
		}
	}
	return !hasValue(fl)
}

// excludedWith is the validation function
// The field under validation must not be present or is empty if any of the other specified fields are present.
func excludedWith(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return !hasValue(fl)
		}
	}
	return true
}

// requiredWith is the validation function
// The field under validation must be present and not empty only if any of the other specified fields are present.
func requiredWith(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return hasValue(fl)
		}
	}
	return true
}

// excludedWithAll is the validation function
// The field under validation must not be present or is empty if all of the other specified fields are present.
func excludedWithAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return !hasValue(fl)
}

// requiredWithAll is the validation function
// The field under validation must be present and not empty only if all of the other specified fields are present.
func requiredWithAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return hasValue(fl)
}

// excludedWithout is the validation function
// The field under validation must not be present or is empty when any of the other specified fields are not present.
func excludedWithout(fl FieldLevel) bool {
	if requireCheckFieldKind(fl, strings.TrimSpace(fl.Param()), true) {
		return !hasValue(fl)
	}
	return true
}

// requiredWithout is the validation function
// The field under validation must be present and not empty only when any of the other specified fields are not present.
func requiredWithout(fl FieldLevel) bool {
	if requireCheckFieldKind(fl, strings.TrimSpace(fl.Param()), true) {
		return hasValue(fl)
	}
	return true
}

// excludedWithoutAll is the validation function
// The field under validation must not be present or is empty when all of the other specified fields are not present.
func excludedWithoutAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return !hasValue(fl)
}

// requiredWithoutAll is the validation function
// The field under validation must be present and not empty only when all of the other specified fields are not present.
func requiredWithoutAll(fl FieldLevel) bool {
	params := parseOneOfParam2(fl.Param())
	for _, param := range params {
		if !requireCheckFieldKind(fl, param, true) {
			return true
		}
	}
	return hasValue(fl)
}

// isGteField is the validation function for validating if the current field's value is greater than or equal to the field specified by the param's value.
func isGteField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() >= currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() >= currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() >= currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(t) || fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) >= len(currentField.String())
}

// isGtField is the validation function for validating if the current field's value is greater than the field specified by the param's value.
func isGtField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() > currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() > currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() > currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.After(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) > len(currentField.String())
}

// isGte is the validation function for validating if the current field's value is greater than or equal to the param's value.
func isGte(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) >= p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) >= p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() >= p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() >= p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() >= p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			now := time.Now().UTC()
			t := field.Convert(timeType).Interface().(time.Time)

			return t.After(now) || t.Equal(now)
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isGt is the validation function for validating if the current field's value is greater than the param's value.
func isGt(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) > p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) > p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() > p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() > p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() > p
	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			return field.Convert(timeType).Interface().(time.Time).After(time.Now().UTC())
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasLengthOf is the validation function for validating if the current field's value is equal to the param's value.
func hasLengthOf(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) == p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) == p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() == p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() == p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() == p
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasMinOf is the validation function for validating if the current field's value is greater than or equal to the param's value.
func hasMinOf(fl FieldLevel) bool {
	return isGte(fl)
}

// isLteField is the validation function for validating if the current field's value is less than or equal to the field specified by the param's value.
func isLteField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() <= currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() <= currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() <= currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(t) || fieldTime.Equal(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) <= len(currentField.String())
}

// isLtField is the validation function for validating if the current field's value is less than the field specified by the param's value.
func isLtField(fl FieldLevel) bool {
	field := fl.Field()
	kind := field.Kind()

	currentField, currentKind, ok := fl.GetStructFieldOK()
	if !ok || currentKind != kind {
		return false
	}

	switch kind {

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:

		return field.Int() < currentField.Int()

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:

		return field.Uint() < currentField.Uint()

	case reflect.Float32, reflect.Float64:

		return field.Float() < currentField.Float()

	case reflect.Struct:

		fieldType := field.Type()

		if fieldType.ConvertibleTo(timeType) && currentField.Type().ConvertibleTo(timeType) {

			t := currentField.Convert(timeType).Interface().(time.Time)
			fieldTime := field.Convert(timeType).Interface().(time.Time)

			return fieldTime.Before(t)
		}

		// Not Same underlying type i.e. struct and time
		if fieldType != currentField.Type() {
			return false
		}
	}

	// default reflect.String
	return len(field.String()) < len(currentField.String())
}

// isLte is the validation function for validating if the current field's value is less than or equal to the param's value.
func isLte(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) <= p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) <= p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() <= p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() <= p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() <= p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			now := time.Now().UTC()
			t := field.Convert(timeType).Interface().(time.Time)

			return t.Before(now) || t.Equal(now)
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isLt is the validation function for validating if the current field's value is less than the param's value.
func isLt(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	switch field.Kind() {

	case reflect.String:
		p := asInt(param)

		return int64(utf8.RuneCountInString(field.String())) < p

	case reflect.Slice, reflect.Map, reflect.Array:
		p := asInt(param)

		return int64(field.Len()) < p

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		p := asIntFromType(field.Type(), param)

		return field.Int() < p

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		p := asUint(param)

		return field.Uint() < p

	case reflect.Float32, reflect.Float64:
		p := asFloat(param)

		return field.Float() < p

	case reflect.Struct:

		if field.Type().ConvertibleTo(timeType) {

			return field.Convert(timeType).Interface().(time.Time).Before(time.Now().UTC())
		}
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// hasMaxOf is the validation function for validating if the current field's value is less than or equal to the param's value.
func hasMaxOf(fl FieldLevel) bool {
	return isLte(fl)
}

// isTCP4AddrResolvable is the validation function for validating if the field's value is a resolvable tcp4 address.
func isTCP4AddrResolvable(fl FieldLevel) bool {
	if !isIP4Addr(fl) {
		return false
	}

	_, err := net.ResolveTCPAddr("tcp4", fl.Field().String())
	return err == nil
}

// isTCP6AddrResolvable is the validation function for validating if the field's value is a resolvable tcp6 address.
func isTCP6AddrResolvable(fl FieldLevel) bool {
	if !isIP6Addr(fl) {
		return false
	}

	_, err := net.ResolveTCPAddr("tcp6", fl.Field().String())

	return err == nil
}

// isTCPAddrResolvable is the validation function for validating if the field's value is a resolvable tcp address.
func isTCPAddrResolvable(fl FieldLevel) bool {
	if !isIP4Addr(fl) && !isIP6Addr(fl) {
		return false
	}

	_, err := net.ResolveTCPAddr("tcp", fl.Field().String())

	return err == nil
}

// isUDP4AddrResolvable is the validation function for validating if the field's value is a resolvable udp4 address.
func isUDP4AddrResolvable(fl FieldLevel) bool {
	if !isIP4Addr(fl) {
		return false
	}

	_, err := net.ResolveUDPAddr("udp4", fl.Field().String())

	return err == nil
}

// isUDP6AddrResolvable is the validation function for validating if the field's value is a resolvable udp6 address.
func isUDP6AddrResolvable(fl FieldLevel) bool {
	if !isIP6Addr(fl) {
		return false
	}

	_, err := net.ResolveUDPAddr("udp6", fl.Field().String())

	return err == nil
}

// isUDPAddrResolvable is the validation function for validating if the field's value is a resolvable udp address.
func isUDPAddrResolvable(fl FieldLevel) bool {
	if !isIP4Addr(fl) && !isIP6Addr(fl) {
		return false
	}

	_, err := net.ResolveUDPAddr("udp", fl.Field().String())

	return err == nil
}

// isIP4AddrResolvable is the validation function for validating if the field's value is a resolvable ip4 address.
func isIP4AddrResolvable(fl FieldLevel) bool {
	if !isIPv4(fl) {
		return false
	}

	_, err := net.ResolveIPAddr("ip4", fl.Field().String())

	return err == nil
}

// isIP6AddrResolvable is the validation function for validating if the field's value is a resolvable ip6 address.
func isIP6AddrResolvable(fl FieldLevel) bool {
	if !isIPv6(fl) {
		return false
	}

	_, err := net.ResolveIPAddr("ip6", fl.Field().String())

	return err == nil
}

// isIPAddrResolvable is the validation function for validating if the field's value is a resolvable ip address.
func isIPAddrResolvable(fl FieldLevel) bool {
	if !isIP(fl) {
		return false
	}

	_, err := net.ResolveIPAddr("ip", fl.Field().String())

	return err == nil
}

// isUnixAddrResolvable is the validation function for validating if the field's value is a resolvable unix address.
func isUnixAddrResolvable(fl FieldLevel) bool {
	_, err := net.ResolveUnixAddr("unix", fl.Field().String())

	return err == nil
}

func isIP4Addr(fl FieldLevel) bool {
	val := fl.Field().String()

	if idx := strings.LastIndex(val, ":"); idx != -1 {
		val = val[0:idx]
	}

	ip := net.ParseIP(val)

	return ip != nil && ip.To4() != nil
}

func isIP6Addr(fl FieldLevel) bool {
	val := fl.Field().String()

	if idx := strings.LastIndex(val, ":"); idx != -1 {
		if idx != 0 && val[idx-1:idx] == "]" {
			val = val[1 : idx-1]
		}
	}

	ip := net.ParseIP(val)

	return ip != nil && ip.To4() == nil
}

func isHostnameRFC952(fl FieldLevel) bool {
	return hostnameRegexRFC952.MatchString(fl.Field().String())
}

func isHostnameRFC1123(fl FieldLevel) bool {
	return hostnameRegexRFC1123.MatchString(fl.Field().String())
}

func isFQDN(fl FieldLevel) bool {
	val := fl.Field().String()

	if val == "" {
		return false
	}

	return fqdnRegexRFC1123.MatchString(val)
}

// isDir is the validation function for validating if the current field's value is a valid directory.
func isDir(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		fileInfo, err := os.Stat(field.String())
		if err != nil {
			return false
		}

		return fileInfo.IsDir()
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isJSON is the validation function for validating if the current field's value is a valid json string.
func isJSON(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		val := field.String()
		return json.Valid([]byte(val))
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isJWT is the validation function for validating if the current field's value is a valid JWT string.
func isJWT(fl FieldLevel) bool {
	return jWTRegex.MatchString(fl.Field().String())
}

// isHostnamePort validates a <dns>:<port> combination for fields typically used for socket address.
func isHostnamePort(fl FieldLevel) bool {
	val := fl.Field().String()
	host, port, err := net.SplitHostPort(val)
	if err != nil {
		return false
	}
	// Port must be a iny <= 65535.
	if portNum, err := strconv.ParseInt(port, 10, 32); err != nil || portNum > 65535 || portNum < 1 {
		return false
	}

	// If host is specified, it should match a DNS name
	if host != "" {
		return hostnameRegexRFC1123.MatchString(host)
	}
	return true
}

// isLowercase is the validation function for validating if the current field's value is a lowercase string.
func isLowercase(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		if field.String() == "" {
			return false
		}
		return field.String() == strings.ToLower(field.String())
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isUppercase is the validation function for validating if the current field's value is an uppercase string.
func isUppercase(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		if field.String() == "" {
			return false
		}
		return field.String() == strings.ToUpper(field.String())
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isDatetime is the validation function for validating if the current field's value is a valid datetime string.
func isDatetime(fl FieldLevel) bool {
	field := fl.Field()
	param := fl.Param()

	if field.Kind() == reflect.String {
		_, err := time.Parse(param, field.String())

		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isTimeZone is the validation function for validating if the current field's value is a valid time zone string.
func isTimeZone(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		// empty value is converted to UTC by time.LoadLocation but disallow it as it is not a valid time zone name
		if field.String() == "" {
			return false
		}

		// Local value is converted to the current system time zone by time.LoadLocation but disallow it as it is not a valid time zone name
		if strings.ToLower(field.String()) == "local" {
			return false
		}

		_, err := time.LoadLocation(field.String())
		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isIso3166Alpha2 is the validation function for validating if the current field's value is a valid iso3166-1 alpha-2 country code.
func isIso3166Alpha2(fl FieldLevel) bool {
	val := fl.Field().String()
	return iso3166_1_alpha2[val]
}

// isIso3166Alpha2 is the validation function for validating if the current field's value is a valid iso3166-1 alpha-3 country code.
func isIso3166Alpha3(fl FieldLevel) bool {
	val := fl.Field().String()
	return iso3166_1_alpha3[val]
}

// isIso3166Alpha2 is the validation function for validating if the current field's value is a valid iso3166-1 alpha-numeric country code.
func isIso3166AlphaNumeric(fl FieldLevel) bool {
	field := fl.Field()

	var code int
	switch field.Kind() {
	case reflect.String:
		i, err := strconv.Atoi(field.String())
		if err != nil {
			return false
		}
		code = i % 1000
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		code = int(field.Int() % 1000)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		code = int(field.Uint() % 1000)
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}
	return iso3166_1_alpha_numeric[code]
}

// isIso31662 is the validation function for validating if the current field's value is a valid iso3166-2 code.
func isIso31662(fl FieldLevel) bool {
	val := fl.Field().String()
	return iso3166_2[val]
}

// isIso4217 is the validation function for validating if the current field's value is a valid iso4217 currency code.
func isIso4217(fl FieldLevel) bool {
	val := fl.Field().String()
	return iso4217[val]
}

// isIso4217Numeric is the validation function for validating if the current field's value is a valid iso4217 numeric currency code.
func isIso4217Numeric(fl FieldLevel) bool {
	field := fl.Field()

	var code int
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		code = int(field.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		code = int(field.Uint())
	default:
		panic(fmt.Sprintf("Bad field type %T", field.Interface()))
	}
	return iso4217_numeric[code]
}

// isBCP47LanguageTag is the validation function for validating if the current field's value is a valid BCP 47 language tag, as parsed by language.Parse
func isBCP47LanguageTag(fl FieldLevel) bool {
	field := fl.Field()

	if field.Kind() == reflect.String {
		_, err := language.Parse(field.String())
		return err == nil
	}

	panic(fmt.Sprintf("Bad field type %T", field.Interface()))
}

// isIsoBicFormat is the validation function for validating if the current field's value is a valid Business Identifier Code (SWIFT code), defined in ISO 9362
func isIsoBicFormat(fl FieldLevel) bool {
	bicString := fl.Field().String()

	return bicRegex.MatchString(bicString)
}

// isSemverFormat is the validation function for validating if the current field's value is a valid semver version, defined in Semantic Versioning 2.0.0
func isSemverFormat(fl FieldLevel) bool {
	semverString := fl.Field().String()

	return semverRegex.MatchString(semverString)
}

// isDnsRFC1035LabelFormat is the validation function
// for validating if the current field's value is
// a valid dns RFC 1035 label, defined in RFC 1035.
func isDnsRFC1035LabelFormat(fl FieldLevel) bool {
	val := fl.Field().String()
	return dnsRegexRFC1035Label.MatchString(val)
}

// isCreditCard is the validation function for validating if the current field's value is a valid credit card number
func isCreditCard(fl FieldLevel) bool {
	val := fl.Field().String()
	var creditCard bytes.Buffer
	segments := strings.Split(val, " ")
	for _, segment := range segments {
		if len(segment) < 3 {
			return false
		}
		creditCard.WriteString(segment)
	}

	ccDigits := strings.Split(creditCard.String(), "")
	size := len(ccDigits)
	if size < 12 || size > 19 {
		return false
	}

	sum := 0
	for i, digit := range ccDigits {
		value, err := strconv.Atoi(digit)
		if err != nil {
			return false
		}
		if size%2 == 0 && i%2 == 0 || size%2 == 1 && i%2 == 1 {
			v := value * 2
			if v >= 10 {
				sum += 1 + (v % 10)
			} else {
				sum += v
			}
		} else {
			sum += value
		}
	}
	return (sum % 10) == 0
}
