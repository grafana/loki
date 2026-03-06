package mmdbdata

// Unmarshaler is implemented by types that can unmarshal MaxMind DB data.
// This follows the same pattern as json.Unmarshaler and other Go standard library interfaces.
type Unmarshaler interface {
	UnmarshalMaxMindDB(d *Decoder) error
}
