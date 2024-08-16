package core

// (C) Copyright IBM Corp. 2019.
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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// common HTTP methods
const (
	POST   = http.MethodPost
	GET    = http.MethodGet
	DELETE = http.MethodDelete
	PUT    = http.MethodPut
	PATCH  = http.MethodPatch
	HEAD   = http.MethodHead
)

// common headers
const (
	Accept                  = "Accept"
	APPLICATION_JSON        = "application/json"
	CONTENT_DISPOSITION     = "Content-Disposition"
	CONTENT_ENCODING        = "Content-Encoding"
	CONTENT_TYPE            = "Content-Type"
	FORM_URL_ENCODED_HEADER = "application/x-www-form-urlencoded"

	ERRORMSG_SERVICE_URL_MISSING = "service URL is empty"
	ERRORMSG_SERVICE_URL_INVALID = "error parsing service URL: %s"
	ERRORMSG_PATH_PARAM_EMPTY    = "path parameter '%s' is empty"
)

// FormData stores information for form data.
type FormData struct {
	fileName    string
	contentType string
	contents    interface{}
}

// RequestBuilder is used to build an HTTP Request instance.
type RequestBuilder struct {
	Method string
	URL    *url.URL
	Header http.Header
	Body   io.Reader
	Query  map[string][]string
	Form   map[string][]FormData

	// EnableGzipCompression indicates whether or not request bodies
	// should be gzip-compressed.
	// This field has no effect on response bodies.
	// If enabled, the Body field will be gzip-compressed and
	// the "Content-Encoding" header will be added to the request with the
	// value "gzip".
	EnableGzipCompression bool

	// RequestContext is an optional Context instance to be associated with the
	// http.Request that is constructed by the Build() method.
	ctx context.Context
}

// NewRequestBuilder initiates a new request.
func NewRequestBuilder(method string) *RequestBuilder {
	return &RequestBuilder{
		Method: method,
		Header: make(http.Header),
		Query:  make(map[string][]string),
		Form:   make(map[string][]FormData),
	}
}

// WithContext sets "ctx" as the Context to be associated with
// the http.Request instance that will be constructed by the Build() method.
func (requestBuilder *RequestBuilder) WithContext(ctx context.Context) *RequestBuilder {
	requestBuilder.ctx = ctx
	return requestBuilder
}

// ConstructHTTPURL creates a properly-encoded URL with path parameters.
// This function returns an error if the serviceURL is "" or is an
// invalid URL string (e.g. ":<badscheme>").
func (requestBuilder *RequestBuilder) ConstructHTTPURL(serviceURL string, pathSegments []string, pathParameters []string) (*RequestBuilder, error) {
	if serviceURL == "" {
		return requestBuilder, SDKErrorf(fmt.Errorf(ERRORMSG_SERVICE_URL_MISSING), "", "no-url", getComponentInfo())
	}
	var URL *url.URL

	URL, err := url.Parse(serviceURL)
	if err != nil {
		err := fmt.Errorf(ERRORMSG_SERVICE_URL_INVALID, err.Error())
		return requestBuilder, SDKErrorf(err, "", "bad-url", getComponentInfo())
	}

	for i, pathSegment := range pathSegments {
		if pathSegment != "" {
			URL.Path += "/" + pathSegment
		}

		if pathParameters != nil && i < len(pathParameters) {
			if pathParameters[i] == "" {
				err := fmt.Errorf(ERRORMSG_PATH_PARAM_EMPTY, fmt.Sprintf("[%d]", i))
				return requestBuilder, SDKErrorf(err, "", "empty-path-param", getComponentInfo())
			}
			URL.Path += "/" + pathParameters[i]
		}
	}
	requestBuilder.URL = URL
	return requestBuilder, nil
}

// ResolveRequestURL creates a properly-encoded URL with path params.
// This function returns an error if the serviceURL is "" or is an
// invalid URL string (e.g. ":<badscheme>").
// Parameters:
// serviceURL - the base URL associated with the service endpoint (e.g. "https://myservice.cloud.ibm.com")
// path - the unresolved path string (e.g. "/resource/{resource_id}/type/{type_id}")
// pathParams - a map containing the path params, keyed by the path param base name
// (e.g. {"type_id": "type-1", "resource_id": "res-123-456-789-abc"})
// The resulting request URL: "https://myservice.cloud.ibm.com/resource/res-123-456-789-abc/type/type-1"
func (requestBuilder *RequestBuilder) ResolveRequestURL(serviceURL string, path string, pathParams map[string]string) (*RequestBuilder, error) {
	if serviceURL == "" {
		err := fmt.Errorf(ERRORMSG_SERVICE_URL_MISSING)
		return requestBuilder, SDKErrorf(err, "", "service-url-missing", getComponentInfo())
	}

	urlString := serviceURL

	// If we have a non-empty "path" input parameter, then process it for possible path param references.
	if path != "" {

		// Encode the unresolved path string.  This will convert all special characters to their
		// "%" encoding counterparts.  Then we need to revert the encodings for '/', '{' and '}' characters
		// to retain the original path segments and to make it easy to insert the encoded path param values below.
		path = url.PathEscape(path)
		path = strings.ReplaceAll(path, "%2F", "/")
		path = strings.ReplaceAll(path, "%7B", "{")
		path = strings.ReplaceAll(path, "%7D", "}")

		// If path parameter values were passed in, then for each one, replace any references to it
		// within "path" with the path parameter's encoded value.
		if len(pathParams) > 0 {
			for k, v := range pathParams {
				if v == "" {
					err := fmt.Errorf(ERRORMSG_PATH_PARAM_EMPTY, k)
					return requestBuilder, SDKErrorf(err, "", "empty-path-param", getComponentInfo())
				}
				encodedValue := url.PathEscape(v)
				ref := fmt.Sprintf("{%s}", k)
				path = strings.ReplaceAll(path, ref, encodedValue)
			}
		}

		// Next, we need to append "path" to "urlString".
		// We need to pay particular attention to any trailing slash on "urlString" and
		// a leading slash on "path".  Ultimately, we do not want a double slash.
		if strings.HasSuffix(urlString, "/") {
			// If urlString has a trailing slash, then make sure path does not have a leading slash.
			path = strings.TrimPrefix(path, "/")
		} else {
			// If urlString does not have a trailing slash and path does not have a
			// leading slash, then append a slash to urlString.
			if !strings.HasPrefix(path, "/") {
				urlString += "/"
			}
		}

		urlString += path
	}

	var URL *url.URL

	URL, err := url.Parse(urlString)
	if err != nil {
		err = fmt.Errorf(ERRORMSG_SERVICE_URL_INVALID, err.Error())
		return requestBuilder, SDKErrorf(err, "", "bad-url", getComponentInfo())
	}

	requestBuilder.URL = URL
	return requestBuilder, nil
}

// AddQuery adds a query parameter name and value to the request.
func (requestBuilder *RequestBuilder) AddQuery(name string, value string) *RequestBuilder {
	requestBuilder.Query[name] = append(requestBuilder.Query[name], value)
	return requestBuilder
}

// AddHeader adds a header name and value to the request.
func (requestBuilder *RequestBuilder) AddHeader(name string, value string) *RequestBuilder {
	requestBuilder.Header[name] = []string{value}
	return requestBuilder
}

// AddFormData adds a new mime part (constructed from the input parameters)
// to the request's multi-part form.
func (requestBuilder *RequestBuilder) AddFormData(fieldName string, fileName string, contentType string,
	contents interface{}) *RequestBuilder {
	if fileName == "" {
		if file, ok := contents.(*os.File); ok {
			if !((os.File{}) == *file) { // if file is not empty
				name := filepath.Base(file.Name())
				fileName = name
			}
		}
	}
	requestBuilder.Form[fieldName] = append(requestBuilder.Form[fieldName], FormData{
		fileName:    fileName,
		contentType: contentType,
		contents:    contents,
	})
	return requestBuilder
}

// SetBodyContentJSON sets the body content from a JSON structure.
func (requestBuilder *RequestBuilder) SetBodyContentJSON(bodyContent interface{}) (*RequestBuilder, error) {
	requestBuilder.Body = new(bytes.Buffer)
	err := json.NewEncoder(requestBuilder.Body.(io.Writer)).Encode(bodyContent)
	if err != nil {
		err = fmt.Errorf("Could not encode JSON body:\n%s", err.Error())
		err = SDKErrorf(err, "", "bad-encode", getComponentInfo())
	}
	return requestBuilder, err
}

// SetBodyContentString sets the body content from a string.
func (requestBuilder *RequestBuilder) SetBodyContentString(bodyContent string) (*RequestBuilder, error) {
	requestBuilder.Body = strings.NewReader(bodyContent)
	return requestBuilder, nil
}

// SetBodyContentStream sets the body content from an io.Reader instance.
func (requestBuilder *RequestBuilder) SetBodyContentStream(bodyContent io.Reader) (*RequestBuilder, error) {
	requestBuilder.Body = bodyContent
	return requestBuilder, nil
}

// createFormFile is a convenience wrapper around CreatePart. It creates
// a new form-data header with the provided field name and file name and contentType.
func createFormFile(formWriter *multipart.Writer, fieldname string, filename string, contentType string) (io.Writer, error) {
	h := make(textproto.MIMEHeader)
	contentDisposition := fmt.Sprintf(`form-data; name="%s"`, fieldname)
	if filename != "" {
		contentDisposition += fmt.Sprintf(`; filename="%s"`, filename)
	}

	h.Set(CONTENT_DISPOSITION, contentDisposition)
	if contentType != "" {
		h.Set(CONTENT_TYPE, contentType)
	}

	res, err := formWriter.CreatePart(h)
	if err != nil {
		err = SDKErrorf(err, "", "create-part-error", getComponentInfo())
	}
	return res, err
}

// SetBodyContentForMultipart sets the body content for a part in a multi-part form.
func (requestBuilder *RequestBuilder) SetBodyContentForMultipart(contentType string, content interface{}, writer io.Writer) error {
	var err error
	if stream, ok := content.(io.Reader); ok {
		_, err = io.Copy(writer, stream)
		if err != nil {
			err = SDKErrorf(
				nil,
				fmt.Sprintf("Could not set body content in form:\n%s", err.Error()),
				"reader-error",
				getComponentInfo(),
			)
		}
	} else if stream, ok := content.(*io.ReadCloser); ok {
		_, err = io.Copy(writer, *stream)
		if err != nil {
			err = SDKErrorf(
				nil,
				fmt.Sprintf("Could not set body content in form:\n%s", err.Error()),
				"readcloser-error",
				getComponentInfo(),
			)
		}
	} else if IsJSONMimeType(contentType) || IsJSONPatchMimeType(contentType) {
		err = json.NewEncoder(writer).Encode(content)
		if err != nil {
			err = SDKErrorf(
				nil,
				fmt.Sprintf("Could not set body content in form:\n%s", err.Error()),
				"json-error",
				getComponentInfo(),
			)
		}
	} else if str, ok := content.(string); ok {
		_, err = writer.Write([]byte(str))
		if err != nil {
			err = SDKErrorf(
				nil,
				fmt.Sprintf("Could not set body content in form:\n%s", err.Error()),
				"string-error",
				getComponentInfo(),
			)
		}
	} else if strPtr, ok := content.(*string); ok {
		_, err = writer.Write([]byte(*strPtr))
		if err != nil {
			err = SDKErrorf(
				nil,
				fmt.Sprintf("Could not set body content in form:\n%s", err.Error()),
				"string-ptr-error",
				getComponentInfo(),
			)
		}
	} else {
		err = SDKErrorf(
			nil,
			"Error: unable to determine the type of 'content' provided",
			"undetermined-type",
			getComponentInfo(),
		)
	}

	return err
}

// Build builds an HTTP Request object from this RequestBuilder instance.
func (requestBuilder *RequestBuilder) Build() (req *http.Request, err error) {

	// If the request builder contains a non-empty "Form" map, then we need to create
	// a form-based request body, with the specific flavor depending on the content type.
	if len(requestBuilder.Form) > 0 {
		contentType := requestBuilder.Header.Get(CONTENT_TYPE)
		if contentType == FORM_URL_ENCODED_HEADER {
			// Create a "application/x-www-form-urlencoded" request body.
			data := url.Values{}
			for fieldName, l := range requestBuilder.Form {
				for _, v := range l {
					data.Add(fieldName, v.contents.(string))
				}
			}
			// This function cannot actually return an error but check anyway
			_, err = requestBuilder.SetBodyContentString(data.Encode())
			if err != nil {
				err = RepurposeSDKProblem(err, "set-content-string-error")
				return
			}
		} else {
			// Create a "multipart/form-data" request body.
			var formBody io.ReadCloser
			formBody, contentType, err = requestBuilder.createMultipartFormRequestBody()
			if err != nil {
				err = RepurposeSDKProblem(err, "create-multipart-error")
				return
			}

			requestBuilder.Body = formBody
			requestBuilder.AddHeader("Content-Type", contentType)
		}
	}

	// If we have a request body and gzip is enabled, then wrap the body in a Gzip compression reader
	// and add the "Content-Encoding: gzip" request header.
	if !IsNil(requestBuilder.Body) && requestBuilder.EnableGzipCompression &&
		!SliceContains(requestBuilder.Header[CONTENT_ENCODING], "gzip") {
		newBody, err := NewGzipCompressionReader(requestBuilder.Body)
		if err != nil {
			err = RepurposeSDKProblem(err, "gzip-reader-error")
			return nil, err
		}
		requestBuilder.Body = newBody
		requestBuilder.Header.Add(CONTENT_ENCODING, "gzip")
	}

	// Create the request
	req, err = http.NewRequest(requestBuilder.Method, requestBuilder.URL.String(), requestBuilder.Body)
	if err != nil {
		err = SDKErrorf(err, fmt.Sprintf("Failed to build request:\n%s", err.Error()), "new-request-error", getComponentInfo())
		return
	}

	// Headers
	req.Header = requestBuilder.Header

	// If "Host" was specified as a header, we need to explicitly copy it
	// to the request's Host field since the "Host" header will be ignored by Request.Write().
	host := req.Header.Get("Host")
	if host != "" {
		req.Host = host
	}

	// Query
	query := req.URL.Query()
	for k, l := range requestBuilder.Query {
		for _, v := range l {
			query.Add(k, v)
		}
	}

	// Encode query
	req.URL.RawQuery = query.Encode()

	// Finally, if a Context should be associated with the new Request instance, then set it.
	if !IsNil(requestBuilder.ctx) {
		req = req.WithContext(requestBuilder.ctx)
	}

	return
}

// createMultipartFormRequestBody will create a request body (a multi-part form) from the parts contained
// in the request builder's "Form" map, which is a map of FormData values keyed by field name (part name).
func (requestBuilder *RequestBuilder) createMultipartFormRequestBody() (bodyReader io.ReadCloser, contentType string, err error) {
	var bodyWriter io.WriteCloser

	// We'll use a pipe so that we can hand the Request object a body (bodyReader below) that can be
	// read in parallel with the function that is writing it, thus saving us from having to make an entire copy
	// of the contents of each form part.
	bodyReader, bodyWriter = io.Pipe()
	formWriter := multipart.NewWriter(bodyWriter)

	go func() {
		defer bodyWriter.Close() // #nosec G307

		// Create a form part from each entry found in the request body's Form map.
		// Note: each entry will actually be a slice of values, and we'll create a separate
		// mime part for each element of the slice.
		for fieldName, formPartList := range requestBuilder.Form {
			for _, formPart := range formPartList {
				var partWriter io.Writer

				// Use the form writer to create the form part within the request body we're creating.
				partWriter, err = createFormFile(formWriter, fieldName, formPart.fileName, formPart.contentType)
				if err != nil {
					return
				}

				// If the part's content is a ReadCloser, we'll need to close it when we're done.
				if stream, ok := formPart.contents.(io.ReadCloser); ok {
					defer stream.Close() // #nosec G307
				} else if stream, ok := formPart.contents.(*io.ReadCloser); ok {
					defer (*stream).Close()
				}

				// Copy the contents of the part to the form part within the request body.
				if err = requestBuilder.SetBodyContentForMultipart(formPart.contentType, formPart.contents, partWriter); err != nil {
					return
				}
			}
		}

		// We're done adding parts to the form, so close the form writer.
		if err = formWriter.Close(); err != nil {
			err = SDKErrorf(err, err.Error(), "form-close-error", getComponentInfo())
			return
		}

	}()

	// Grab the Content-Type from the form writer (it will also contain the boundary string)
	contentType = formWriter.FormDataContentType()

	return
}

// SetBodyContent sets the body content from one of three different sources.
func (requestBuilder *RequestBuilder) SetBodyContent(contentType string, jsonContent interface{}, jsonPatchContent interface{},
	nonJSONContent interface{}) (builder *RequestBuilder, err error) {
	if !IsNil(jsonContent) {
		builder, err = requestBuilder.SetBodyContentJSON(jsonContent)
		if err != nil {
			err = RepurposeSDKProblem(err, "set-json-body-error")
			return
		}
	} else if !IsNil(jsonPatchContent) {
		builder, err = requestBuilder.SetBodyContentJSON(jsonPatchContent)
		if err != nil {
			err = RepurposeSDKProblem(err, "set-json-patch-body-error")
			return
		}
	} else if !IsNil(nonJSONContent) {
		// Set the non-JSON body content based on the type of value passed in,
		// which should be a "string", "*string" or an "io.Reader"
		if str, ok := nonJSONContent.(string); ok {
			builder, err = requestBuilder.SetBodyContentString(str)
			err = RepurposeSDKProblem(err, "set-body-string-error")
		} else if strPtr, ok := nonJSONContent.(*string); ok {
			builder, err = requestBuilder.SetBodyContentString(*strPtr)
			err = RepurposeSDKProblem(err, "set-body-strptr-error")
		} else if stream, ok := nonJSONContent.(io.Reader); ok {
			builder, err = requestBuilder.SetBodyContentStream(stream)
			err = RepurposeSDKProblem(err, "set-body-reader-error")
		} else if stream, ok := nonJSONContent.(*io.ReadCloser); ok {
			builder, err = requestBuilder.SetBodyContentStream(*stream)
			err = RepurposeSDKProblem(err, "set-body-readerptr-error")
		} else {
			builder = requestBuilder
			err = fmt.Errorf("Invalid type for non-JSON body content: %s", reflect.TypeOf(nonJSONContent).String())
			err = SDKErrorf(err, "", "bad-nonjson-body-content", getComponentInfo())
		}
	} else {
		builder = requestBuilder
		err = fmt.Errorf("No body content provided")
		err = SDKErrorf(err, "", "no-body-content", getComponentInfo())
	}

	return
}

// AddQuerySlice converts the passed in slice 'slice' by calling the ConverSlice method,
// and adds the converted slice to the request's query string. An error is returned when
// conversion fails.
func (requestBuilder *RequestBuilder) AddQuerySlice(param string, slice interface{}) (err error) {
	convertedSlice, err := ConvertSlice(slice)
	if err != nil {
		err = RepurposeSDKProblem(err, "convert-slice-error")
		return
	}

	requestBuilder.AddQuery(param, strings.Join(convertedSlice, ","))

	return
}
