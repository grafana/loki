package explain

import (
	"fmt"
	"strings"
)

func Pretty(level int, explain string) string {
	e := new(strings.Builder)
	for i := 0; i < level; i++ {
		fmt.Fprintf(e, "\t")
	}
	fmt.Fprintf(e, "%s", explain)
	return e.String()
}
