//go:build !go1.27 || !goexperiment.jsonv2

package encoder

import (
	"encoding/json"
	"fmt"
)

// invalidNumberError is the error of a json.Number which is not a JSON number, as encoding/json before Go 1.27
// reports it, whether it is quoted or not.
func invalidNumberError(n json.Number, _ bool) error {
	return fmt.Errorf("json: invalid number literal %q", n)
}
