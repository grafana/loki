package plumbing

import "strconv"

type OptionalInt struct {
	Value int64
	Valid bool
}

func (o *OptionalInt) String() string {
	if o.Valid {
		return strconv.FormatInt(o.Value, 10)
	}

	return ""
}

func (o *OptionalInt) Set(v string) error {
	if v == "" {
		return nil
	}

	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return err
	}

	o.Value = i
	o.Valid = true

	return nil
}
