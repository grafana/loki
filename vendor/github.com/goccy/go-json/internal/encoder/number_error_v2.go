//go:build go1.27 && goexperiment.jsonv2

package encoder

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/goccy/go-json/internal/errors"
)

// invalidNumberError is the error of a json.Number which is not a JSON number, as encoding/json of Go 1.27
// reports it: an error of its method MarshalJSONTo, which shows the number with its quotes if it is quoted.
func invalidNumberError(n json.Number, quoted bool) error {
	s := string(n)
	if quoted {
		s = `"` + s + `"`
	}
	return errors.ErrMarshaler(reflect.TypeFor[*json.Number](),
		fmt.Errorf("cannot parse %q as JSON number: %w", s, strconv.ErrSyntax), "MarshalJSONTo")
}
