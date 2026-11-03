//go:build globdebug
// +build globdebug

package debug

import (
	"fmt"
)

const Enabled = true

func Printf(f string, args ...any) {
	fmt.Printf(f, args...)
}
