package proto

import (
	"encoding"
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9/internal/util"
)

// Scan parses bytes `b` to `v` with appropriate type.
//
//nolint:gocyclo
func Scan(b []byte, v any) error {
	switch v := v.(type) {
	case nil:
		return fmt.Errorf("redis: Scan(nil)")
	case *string:
		*v = util.BytesToString(b)
		return nil
	case *[]byte:
		dest := make([]byte, len(b))
		copy(dest, b)
		*v = dest
		return nil
	case *int:
		var err error
		*v, err = util.Atoi(b)
		return err
	case *int8:
		n, err := util.ParseInt(b, 10, 8)
		if err != nil {
			return err
		}
		*v = int8(n)
		return nil
	case *int16:
		n, err := util.ParseInt(b, 10, 16)
		if err != nil {
			return err
		}
		*v = int16(n)
		return nil
	case *int32:
		n, err := util.ParseInt(b, 10, 32)
		if err != nil {
			return err
		}
		*v = int32(n)
		return nil
	case *int64:
		n, err := util.ParseInt(b, 10, 64)
		if err != nil {
			return err
		}
		*v = n
		return nil
	case *uint:
		n, err := util.ParseUint(b, 10, 64)
		if err != nil {
			return err
		}
		*v = uint(n)
		return nil
	case *uint8:
		n, err := util.ParseUint(b, 10, 8)
		if err != nil {
			return err
		}
		*v = uint8(n)
		return nil
	case *uint16:
		n, err := util.ParseUint(b, 10, 16)
		if err != nil {
			return err
		}
		*v = uint16(n)
		return nil
	case *uint32:
		n, err := util.ParseUint(b, 10, 32)
		if err != nil {
			return err
		}
		*v = uint32(n)
		return nil
	case *uint64:
		n, err := util.ParseUint(b, 10, 64)
		if err != nil {
			return err
		}
		*v = n
		return nil
	case *float32:
		n, err := util.ParseFloat(b, 32)
		if err != nil {
			return err
		}
		*v = float32(n)
		return err
	case *float64:
		var err error
		*v, err = util.ParseFloat(b, 64)
		return err
	case *bool:
		*v = len(b) == 1 && b[0] == '1'
		return nil
	case *time.Time:
		var err error
		*v, err = time.Parse(time.RFC3339Nano, util.BytesToString(b))
		return err
	case *time.Duration:
		n, err := util.ParseInt(b, 10, 64)
		if err != nil {
			return err
		}
		*v = time.Duration(n)
		return nil
	case encoding.BinaryUnmarshaler:
		dest := make([]byte, len(b))
		copy(dest, b)
		return v.UnmarshalBinary(dest)
	case *net.IP:
		dest := make(net.IP, len(b))
		copy(dest, b)
		*v = dest
		return nil
	default:
		return fmt.Errorf(
			"redis: can't unmarshal %T (consider implementing BinaryUnmarshaler)", v)
	}
}

func ScanSlice(data []string, slice any) error {
	v := reflect.ValueOf(slice)
	if !v.IsValid() {
		return fmt.Errorf("redis: ScanSlice(nil)")
	}
	if v.Kind() != reflect.Pointer {
		return fmt.Errorf("redis: ScanSlice(non-pointer %T)", slice)
	}
	v = v.Elem()
	if v.Kind() != reflect.Slice {
		return fmt.Errorf("redis: ScanSlice(non-slice %T)", slice)
	}

	next := makeSliceNextElemFunc(v)
	for i, s := range data {
		elem := next()
		if err := Scan(util.StringToBytes(s), elem.Addr().Interface()); err != nil {
			err = fmt.Errorf("redis: ScanSlice index=%d value=%q failed: %w", i, s, err)
			return err
		}
	}

	return nil
}

func makeSliceNextElemFunc(v reflect.Value) func() reflect.Value {
	elemType := v.Type().Elem()

	next := func() reflect.Value {
		if v.Len() == v.Cap() {
			v.Grow(1)
		}

		v.SetLen(v.Len() + 1)
		return v.Index(v.Len() - 1)
	}

	if elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
		return func() reflect.Value {
			elem := next()
			if elem.IsNil() {
				elem.Set(reflect.New(elemType))
			}
			return elem.Elem()
		}
	}

	return next
}
