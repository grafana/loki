// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package helpers

import (
	"mime"
	"net/http"

	"github.com/pb33f/libopenapi/datamodel/high/v3"
)

// ExtractOperation extracts the operation from the path item based on the request method. If there is no
// matching operation found, then nil is returned.
func ExtractOperation(request *http.Request, item *v3.PathItem) *v3.Operation {
	switch request.Method {
	case http.MethodGet:
		return item.Get
	case http.MethodPost:
		return item.Post
	case http.MethodPut:
		return item.Put
	case http.MethodDelete:
		return item.Delete
	case http.MethodOptions:
		return item.Options
	case http.MethodHead:
		return item.Head
	case http.MethodPatch:
		return item.Patch
	case http.MethodTrace:
		return item.Trace
	}
	return nil
}

// ExtractContentType extracts the content type from the request header. First return argument is the content type
// of the request.The second (optional) argument is the charset of the request. The third (optional)
// argument is the boundary of the type (only used with forms really).
func ExtractContentType(contentType string) (string, string, string) {
	// mime.ParseMediaType: "If there is an error parsing the optional parameter,
	// the media type will be returned along with the error ErrInvalidMediaParameter."
	ct, params, _ := mime.ParseMediaType(contentType)
	return ct, params["charset"], params["boundary"]
}
