package server

import (
	"fmt"
)

type errOpts struct {
	err error
}

// ErrorOption configures a NATS Error helper
type ErrorOption func(*errOpts)

// Unless ensures that if err is a ApiErr that err will be returned rather than the one being created via the helper
func Unless(err error) ErrorOption {
	return func(opts *errOpts) {
		opts.err = err
	}
}

func parseOpts(opts []ErrorOption) *errOpts {
	eopts := &errOpts{}
	for _, opt := range opts {
		opt(eopts)
	}
	return eopts
}

type ErrorIdentifier uint16

// IsNatsErr determines if an error matches ID, if multiple IDs are given if the error matches any of these the function will be true
func IsNatsErr(err error, ids ...ErrorIdentifier) bool {
	if err == nil {
		return false
	}

	ce, ok := err.(*ApiError)
	if !ok || ce == nil {
		return false
	}

	for _, id := range ids {
		ae, ok := ApiErrors[id]
		if !ok || ae == nil {
			continue
		}

		if ce.ErrCode == ae.ErrCode {
			return true
		}
	}

	return false
}

// ApiError is included in all responses if there was an error.
type ApiError struct {
	Code        int    `json:"code"`
	ErrCode     uint16 `json:"err_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// ErrorsData is the source data for generated errors as found in errors.json
type ErrorsData struct {
	Constant    string `json:"constant"`
	Code        int    `json:"code"`
	ErrCode     uint16 `json:"error_code"`
	Description string `json:"description"`
	Comment     string `json:"comment"`
	Help        string `json:"help"`
	URL         string `json:"url"`
	Deprecates  string `json:"deprecates"`
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("%s (%d)", e.Description, e.ErrCode)
}

func (e *ApiError) toReplacerArgs(replacements []interface{}) []string {
	var (
		ra  []string
		key string
	)

	for i, replacement := range replacements {
		if i%2 == 0 {
			key = replacement.(string)
			continue
		}

		switch v := replacement.(type) {
		case string:
			ra = append(ra, key, v)
		case error:
			ra = append(ra, key, v.Error())
		default:
			ra = append(ra, key, fmt.Sprintf("%v", v))
		}
	}

	return ra
}
