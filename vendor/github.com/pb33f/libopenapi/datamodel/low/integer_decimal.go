package low

import (
	"math"
	"strconv"
	"strings"
)

// parseDecimalInt64 converts a syntactically valid decimal without rounding.
// It scans the mantissa once and retains at most 19 digits, regardless of exponent.
func parseDecimalInt64(value string) (int64, bool) {
	value = strings.ReplaceAll(value, "_", "")
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimLeft(value, "+-")
	mantissa := value
	exponent := int64(0)
	if i := strings.IndexAny(value, "eE"); i >= 0 {
		mantissa = value[:i]
		exponent, _ = strconv.ParseInt(value[i+1:], 10, 64)
	}
	var digits, leading, trailing, fractional int64
	nonzero, afterPoint := false, false
	for _, digit := range mantissa {
		if digit == '.' {
			afterPoint = true
			continue
		}
		digits++
		if afterPoint {
			fractional++
		}
		if digit == '0' {
			trailing++
			if !nonzero {
				leading++
			}
		} else {
			nonzero = true
			trailing = 0
		}
	}
	if !nonzero {
		return 0, true
	}
	limit := int64(math.MaxInt64)
	if negative {
		limit = math.MinInt64
	}
	// Bound the exponent before subtracting the mantissa's decimal places.
	if exponent > digits+19 {
		return limit, true
	}
	if exponent < -digits {
		return 0, false
	}
	scale := exponent - fractional
	if scale < -trailing {
		return 0, false
	}
	integerDigits := digits - leading + scale
	if integerDigits > 19 {
		return limit, true
	}
	maxMagnitude := uint64(math.MaxInt64)
	if negative {
		maxMagnitude++
	}
	var result uint64
	remaining := integerDigits
	for _, digit := range mantissa {
		if digit == '.' {
			continue
		}
		if leading > 0 {
			leading--
			continue
		}
		if remaining == 0 {
			break
		}
		result = result*10 + uint64(digit-'0')
		remaining--
	}
	for remaining > 0 {
		result *= 10
		remaining--
	}
	if result > maxMagnitude {
		return limit, true
	}
	if negative {
		return -int64(result), true
	}
	return int64(result), true
}
