package core

// (C) Copyright IBM Corp. 2019, 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// DetailedResponse holds the response information received from the server.
type DetailedResponse struct {

	// The HTTP status code associated with the response.
	StatusCode int `yaml:"status_code"`

	// The HTTP headers contained in the response.
	Headers http.Header `yaml:"headers"`

	// Result - this field will contain the result of the operation (obtained from the response body).
	//
	// If the operation was successful and the response body contains a JSON response, it is un-marshalled
	// into an object of the appropriate type (defined by the particular operation), and the Result field will contain
	// this response object.  If there was an error while un-marshalling the JSON response body, then the RawResult field
	// will be set to the byte array containing the response body.
	//
	// Alternatively, if the generated SDK code passes in a result object which is an io.ReadCloser instance,
	// the JSON un-marshalling step is bypassed and the response body is simply returned in the Result field.
	// This scenario would occur in a situation where the SDK would like to provide a streaming model for large JSON
	// objects.
	//
	// If the operation was successful and the response body contains a non-JSON response,
	// the Result field will be an instance of io.ReadCloser that can be used by generated SDK code
	// (or the application) to read the response data.
	//
	// If the operation was unsuccessful and the response body contains a JSON error response,
	// this field will contain an instance of map[string]interface{} which is the result of un-marshalling the
	// response body as a "generic" JSON object.
	// If the JSON response for an unsuccessful operation could not be properly un-marshalled, then the
	// RawResult field will contain the raw response body.
	Result interface{} `yaml:"result,omitempty"`

	// This field will contain the raw response body as a byte array under these conditions:
	// 1) there was a problem un-marshalling a JSON response body -
	// either for a successful or unsuccessful operation.
	// 2) the operation was unsuccessful, and the response body contains a non-JSON response.
	RawResult []byte `yaml:"raw_result,omitempty"`
}

// GetHeaders returns the headers
func (response *DetailedResponse) GetHeaders() http.Header {
	return response.Headers
}

// GetStatusCode returns the HTTP status code
func (response *DetailedResponse) GetStatusCode() int {
	return response.StatusCode
}

// GetResult returns the result from the service
func (response *DetailedResponse) GetResult() interface{} {
	return response.Result
}

// GetResultAsMap returns the result as a map (generic JSON object), if the
// DetailedResponse.Result field contains an instance of a map.
func (response *DetailedResponse) GetResultAsMap() (map[string]interface{}, bool) {
	m, ok := response.Result.(map[string]interface{})
	return m, ok
}

// GetRawResult returns the raw response body as a byte array.
func (response *DetailedResponse) GetRawResult() []byte {
	return response.RawResult
}

func (response *DetailedResponse) String() string {
	output, err := json.MarshalIndent(response, "", "    ")
	if err == nil {
		return fmt.Sprintf("%+v\n", string(output))
	}
	return fmt.Sprintf("Error marshalling DetailedResponse instance: %s", err.Error())
}
