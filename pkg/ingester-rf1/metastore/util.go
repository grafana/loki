package metastore

import (
	"fmt"
	"os"
	"runtime"
)

const maxStacksize = 8 * 1024

func panicError(p interface{}) error {
	stack := make([]byte, maxStacksize)
	stack = stack[:runtime.Stack(stack, true)]
	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, stack)
	return fmt.Errorf("%v", p)
}
