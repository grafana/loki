package stages

import "time"

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
