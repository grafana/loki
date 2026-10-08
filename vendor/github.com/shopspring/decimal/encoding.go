package decimal

import (
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"math/big"
)

// UnmarshalJSON implements the json.Unmarshaler interface.
func (d *Decimal) UnmarshalJSON(decimalBytes []byte) error {
	if string(decimalBytes) == "null" {
		return nil
	}

	decimal, err := NewFromString(unquoteIfQuoted(string(decimalBytes)))
	*d = decimal
	if err != nil {
		return fmt.Errorf("error decoding string '%s': %s", string(decimalBytes), err)
	}
	return nil
}

// MarshalJSON implements the json.Marshaler interface.
func (d Decimal) MarshalJSON() ([]byte, error) {
	str := d.String()
	if MarshalJSONWithoutQuotes {
		return []byte(str), nil
	}
	b := make([]byte, 0, len(str)+2)
	b = append(b, '"')
	b = append(b, str...)
	return append(b, '"'), nil
}

// UnmarshalBinary implements the encoding.BinaryUnmarshaler interface. As a string representation
// is already used when encoding to text, this method stores that string as []byte
func (d *Decimal) UnmarshalBinary(data []byte) error {
	// Verify we have at least 4 bytes for the exponent. The GOB encoded value
	// may be empty.
	if len(data) < 4 {
		return fmt.Errorf("error decoding binary %v: expected at least 4 bytes, got %d", data, len(data))
	}

	// Extract the exponent
	exp := int32(binary.BigEndian.Uint32(data[:4]))
	if int64(exp) > int64(MaxDecodeExponent) || int64(exp) < -int64(MaxDecodeExponent) {
		return fmt.Errorf("error decoding binary: exponent %d exceeds MaxDecodeExponent (%d)", exp, MaxDecodeExponent)
	}
	d.exp = exp

	// Extract the value
	d.value = new(big.Int)
	if err := d.value.GobDecode(data[4:]); err != nil {
		return fmt.Errorf("error decoding binary %v: %s", data, err)
	}

	return nil
}

// MarshalBinary implements the encoding.BinaryMarshaler interface.
func (d Decimal) MarshalBinary() (data []byte, err error) {
	// exp is written first, but encode value first to know output size
	var valueData []byte
	if valueData, err = d.getValue().GobEncode(); err != nil {
		return nil, err
	}

	// Write the exponent in front, since it's a fixed size
	expData := make([]byte, 4, len(valueData)+4)
	binary.BigEndian.PutUint32(expData, uint32(d.exp))

	// Return the byte array
	return append(expData, valueData...), nil
}

// Scan implements the sql.Scanner interface for database deserialization.
func (d *Decimal) Scan(value interface{}) error {
	// first try to see if the data is stored in database as a Numeric datatype
	switch v := value.(type) {

	case float32:
		*d = NewFromFloat(float64(v))
		return nil

	case float64:
		// numeric in sqlite3 sends us float64
		*d = NewFromFloat(v)
		return nil

	case int64:
		// at least in sqlite3 when the value is 0 in db, the data is sent
		// to us as an int64 instead of a float64 ...
		*d = New(v, 0)
		return nil

	case uint64:
		// while clickhouse may send 0 in db as uint64
		*d = NewFromUint64(v)
		return nil

	case string:
		var err error
		*d, err = NewFromString(unquoteIfQuoted(v))
		return err

	case []byte:
		var err error
		*d, err = NewFromString(unquoteIfQuoted(string(v)))
		return err

	default:
		return fmt.Errorf("could not convert value '%+v' to any known type", value)
	}
}

// Value implements the driver.Valuer interface for database serialization.
func (d Decimal) Value() (driver.Value, error) {
	return d.String(), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for XML
// deserialization.
func (d *Decimal) UnmarshalText(text []byte) error {
	str := string(text)

	dec, err := NewFromString(str)
	*d = dec
	if err != nil {
		return fmt.Errorf("error decoding string '%s': %s", str, err)
	}

	return nil
}

// MarshalText implements the encoding.TextMarshaler interface for XML
// serialization.
func (d Decimal) MarshalText() (text []byte, err error) {
	return []byte(d.String()), nil
}

// GobEncode implements the gob.GobEncoder interface for gob serialization.
func (d Decimal) GobEncode() ([]byte, error) {
	return d.MarshalBinary()
}

// GobDecode implements the gob.GobDecoder interface for gob serialization.
func (d *Decimal) GobDecode(data []byte) error {
	return d.UnmarshalBinary(data)
}

// DecodeSpanner decodes a Spanner value into a Decimal
func (d *Decimal) DecodeSpanner(val interface{}) error {
	return d.Scan(val)
}

// EncodeSpanner encodes a Decimal into a Spanner value
func (d Decimal) EncodeSpanner() (interface{}, error) {
	return d.String(), nil
}

func unquoteIfQuoted(value string) string {
	// If the amount is quoted, strip the quotes
	if len(value) > 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}

	return value
}

// NullDecimal represents a nullable decimal with compatibility for
// scanning null values from the database.
type NullDecimal struct {
	Decimal Decimal
	Valid   bool
}

// NewNullDecimal returns a valid NullDecimal holding d.
func NewNullDecimal(d Decimal) NullDecimal {
	return NullDecimal{
		Decimal: d,
		Valid:   true,
	}
}

// Scan implements the sql.Scanner interface for database deserialization.
func (d *NullDecimal) Scan(value interface{}) error {
	if value == nil {
		d.Valid = false
		return nil
	}
	err := d.Decimal.Scan(value)
	d.Valid = err == nil
	return err
}

// Value implements the driver.Valuer interface for database serialization.
func (d NullDecimal) Value() (driver.Value, error) {
	if !d.Valid {
		return nil, nil
	}
	return d.Decimal.Value()
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (d *NullDecimal) UnmarshalJSON(decimalBytes []byte) error {
	if string(decimalBytes) == "null" {
		d.Valid = false
		return nil
	}
	err := d.Decimal.UnmarshalJSON(decimalBytes)
	d.Valid = err == nil
	return err
}

// MarshalJSON implements the json.Marshaler interface.
func (d NullDecimal) MarshalJSON() ([]byte, error) {
	if !d.Valid {
		return []byte("null"), nil
	}
	return d.Decimal.MarshalJSON()
}

// UnmarshalText implements the encoding.TextUnmarshaler interface for XML
// deserialization
func (d *NullDecimal) UnmarshalText(text []byte) error {
	str := string(text)

	// check for empty XML or XML without body e.g., <tag></tag>
	if str == "" {
		d.Valid = false
		return nil
	}
	if err := d.Decimal.UnmarshalText(text); err != nil {
		d.Valid = false
		return err
	}
	d.Valid = true
	return nil
}

// MarshalText implements the encoding.TextMarshaler interface for XML
// serialization.
func (d NullDecimal) MarshalText() (text []byte, err error) {
	if !d.Valid {
		return []byte{}, nil
	}
	return d.Decimal.MarshalText()
}

// DecodeSpanner decodes a Spanner value into a Decimal
func (d *NullDecimal) DecodeSpanner(value interface{}) error {
	switch t := value.(type) {
	case nil:
		d.Valid = false
		return nil
	case *string:
		if t == nil {
			d.Valid = false
			return nil
		}
		value = *t
	}

	err := d.Decimal.Scan(value)
	d.Valid = err == nil
	return err
}

// EncodeSpanner encodes a Decimal into a Spanner value
func (d NullDecimal) EncodeSpanner() (interface{}, error) {
	if !d.Valid {
		return nil, nil
	}
	return d.Decimal.String(), nil
}
