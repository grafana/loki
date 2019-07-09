package stages

import (
	"fmt"
	"strconv"
	"time"
)

// convertDateLayout converts pre-defined date format layout into date format
func convertDateLayout(predef string) parser {
	switch predef {
	case "ANSIC":
		return func(t string) (time.Time, error) {
			return time.Parse(time.ANSIC, t)
		}
	case "UnixDate":
		return func(t string) (time.Time, error) {
			return time.Parse(time.UnixDate, t)
		}
	case "RubyDate":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RubyDate, t)
		}
	case "RFC822":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC822, t)
		}
	case "RFC822Z":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC822Z, t)
		}
	case "RFC850":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC850, t)
		}
	case "RFC1123":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC1123, t)
		}
	case "RFC1123Z":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC1123Z, t)
		}
	case "RFC3339":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC3339, t)
		}
	case "RFC3339Nano":
		return func(t string) (time.Time, error) {
			return time.Parse(time.RFC3339Nano, t)
		}
	case "Unix":
		return func(t string) (time.Time, error) {
			i, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(i, 0), nil
		}
	case "UnixMs":
		return func(t string) (time.Time, error) {
			i, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(0, i*int64(time.Millisecond)), nil
		}
	case "UnixNs":
		return func(t string) (time.Time, error) {
			i, err := strconv.ParseInt(t, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(0, i), nil
		}
	default:
		return func(t string) (time.Time, error) {
			return time.Parse(predef, t)
		}
	}
}

// getString will convert the input variable to a string if possible
func getString(unk interface{}) (string, error) {

	switch i := unk.(type) {
	case float64:
		return fmt.Sprintf("%f", i), nil
	case float32:
		return fmt.Sprintf("%f", i), nil
	case int64:
		return strconv.FormatInt(i, 10), nil
	case int32:
		return strconv.FormatInt(int64(i), 10), nil
	case int:
		return strconv.Itoa(i), nil
	case uint64:
		return strconv.FormatUint(i, 10), nil
	case uint32:
		return strconv.FormatUint(uint64(i), 10), nil
	case uint:
		return strconv.FormatUint(uint64(i), 10), nil
	case string:
		return unk.(string), nil
	case bool:
		if i {
			return "true", nil
		}
		return "false", nil
	default:
		return "", fmt.Errorf("Can't convert %v to string", unk)
	}
}
