package magefile

import (
	"fmt"

	"github.com/magefile/mage/sh"
)

func revParse(args ...string) string {
	return stdout("git", append([]string{"rev-parse"}, args...)...)
}

func joinMap(mapping map[string]string, pattern string) (out []string) {
	for k, v := range mapping {
		out = append(out, fmt.Sprintf(pattern, k, v))
	}
	return
}

func stdout(cmd string, args ...string) string {
	out, err := sh.Output(cmd, args...)
	if err != nil {
		panic(err)
	}
	return out
}
