package parquet

import (
	"fmt"
	"strconv"
	"strings"
)

func parseIntBitWidthArgs(args string) (int, error) {
	if !strings.HasPrefix(args, "(") || !strings.HasSuffix(args, ")") {
		return 0, fmt.Errorf("malformed int bit width args: %s", args)
	}
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	bitWidth, err := strconv.Atoi(args)
	if err != nil {
		return 0, err
	}
	switch bitWidth {
	case 8, 16, 32, 64:
		return bitWidth, nil
	default:
		return 0, fmt.Errorf("invalid integer bit width: %d (must be 8, 16, 32, or 64)", bitWidth)
	}
}
