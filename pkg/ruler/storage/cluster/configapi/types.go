package configapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// APIResponse is the base object returned for any API call.
// The Data field will be set to either nil or a value of
// another *Response type value from this package.
type APIResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// WriteTo writes the response to the given ResponseWriter with the provided
// statusCode.
func (r *APIResponse) WriteTo(w http.ResponseWriter, statusCode int) error {
	bb, err := json.Marshal(r)
	if err != nil {
		// If we fail here, we should at least write a 500 back.
		w.WriteHeader(http.StatusInternalServerError)
		return err
	}

	w.WriteHeader(statusCode)
	n, err := w.Write(bb)
	if err != nil {
		return err
	} else if n != len(bb) {
		return fmt.Errorf("could not write full response. expected %d, wrote %d", len(bb), n)
	}

	return nil
}

// ErrorResponse is contained inside an APIResponse and returns
// an error string. Returned by any API call that can fail.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ListConfigurationsResponse is contained inside an APIResponse
// and provides the list of configurations known to the KV store.
// Returned by ListConfigurations.
type ListConfigurationsResponse struct {
	// Configs is the list of configuration names.
	Configs []string `json:"configs"`
}

// GetConfigurationResponse is contained inside an APIResponse
// and provides a single configuration known to the KV store.
// Returned by GetConfiguration.
type GetConfigurationResponse struct {
	// Value is the stringified YAML configuration.
	Value string `json:"value"`
}

// WriteResponse writes a response object to the provided ResponseWriter w and with a
// status code of statusCode. resp is marshaled to JSON.
func WriteResponse(w http.ResponseWriter, statusCode int, resp interface{}) error {
	apiResp := &APIResponse{Status: "success", Data: resp}
	return apiResp.WriteTo(w, statusCode)
}

// WriteError writes an error response back to the ResponseWriter.
func WriteError(w http.ResponseWriter, statusCode int, err error) error {
	resp := &APIResponse{Status: "error", Data: &ErrorResponse{Error: err.Error()}}
	return resp.WriteTo(w, statusCode)
}
