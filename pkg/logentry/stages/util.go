package stages

import (
	"fmt"
	"strconv"
	"time"
)

// convertDateLayout converts pre-defined date format layout into date format
func convertDateLayout(predef string) string {
	switch predef {
	case "ANSIC":
		return time.ANSIC
	case "UnixDate":
		return time.UnixDate
	case "RubyDate":
		return time.RubyDate
	case "RFC822":
		return time.RFC822
	case "RFC822Z":
		return time.RFC822Z
	case "RFC850":
		return time.RFC850
	case "RFC1123":
		return time.RFC1123
	case "RFC1123Z":
		return time.RFC1123Z
	case "RFC3339":
		return time.RFC3339
	case "RFC3339Nano":
		return time.RFC3339Nano
	default:
		return predef
	}
}

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
		return "", fmt.Errorf("Can't convert %v to float64", unk)
	}
}
