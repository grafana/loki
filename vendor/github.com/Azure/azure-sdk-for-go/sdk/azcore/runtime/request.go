//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"path"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// Base64Encoding is usesd to specify which base-64 encoder/decoder to use when
// encoding/decoding a slice of bytes to/from a string.
type Base64Encoding = exported.Base64Encoding

const (
	// Base64StdFormat uses base64.StdEncoding for encoding and decoding payloads.
	Base64StdFormat Base64Encoding = exported.Base64StdFormat

	// Base64URLFormat uses base64.RawURLEncoding for encoding and decoding payloads.
	Base64URLFormat Base64Encoding = exported.Base64URLFormat
)

// NewRequest creates a new policy.Request with the specified input.
// The endpoint MUST be properly encoded before calling this function.
func NewRequest(ctx context.Context, httpMethod string, endpoint string) (*policy.Request, error) {
	return exported.NewRequest(ctx, httpMethod, endpoint)
}

// EncodeQueryParams will parse and encode any query parameters in the specified URL.
func EncodeQueryParams(u string) (string, error) {
	before, after, found := strings.Cut(u, "?")
	if !found {
		return u, nil
	}
	qp, err := url.ParseQuery(after)
	if err != nil {
		return "", err
	}
	return before + "?" + qp.Encode(), nil
}

// JoinPaths concatenates multiple URL path segments into one path,
// inserting path separation characters as required. JoinPaths will preserve
// query parameters in the root path
func JoinPaths(root string, paths ...string) string {
	if len(paths) == 0 {
		return root
	}

	qps := ""
	if strings.Contains(root, "?") {
		splitPath := strings.Split(root, "?")
		root, qps = splitPath[0], splitPath[1]
	}

	p := path.Join(paths...)
	// path.Join will remove any trailing slashes.
	// if one was provided, preserve it.
	if strings.HasSuffix(paths[len(paths)-1], "/") && !strings.HasSuffix(p, "/") {
		p += "/"
	}

	if qps != "" {
		p = p + "?" + qps
	}

	if strings.HasSuffix(root, "/") && strings.HasPrefix(p, "/") {
		root = root[:len(root)-1]
	} else if !strings.HasSuffix(root, "/") && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return root + p
}

// EncodeByteArray will base-64 encode the byte slice v.
func EncodeByteArray(v []byte, format Base64Encoding) string {
	return exported.EncodeByteArray(v, format)
}

// MarshalAsByteArray will base-64 encode the byte slice v, then calls SetBody.
// The encoded value is treated as a JSON string.
func MarshalAsByteArray(req *policy.Request, v []byte, format Base64Encoding) error {
	// send as a JSON string
	encode := fmt.Sprintf("\"%s\"", EncodeByteArray(v, format))
	// tsp generated code can set Content-Type so we must prefer that
	return exported.SetBody(req, exported.NopCloser(strings.NewReader(encode)), shared.ContentTypeAppJSON, false)
}

// MarshalAsJSON calls json.Marshal() to get the JSON encoding of v then calls SetBody.
func MarshalAsJSON(req *policy.Request, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("error marshalling type %T: %s", v, err)
	}
	// tsp generated code can set Content-Type so we must prefer that
	return exported.SetBody(req, exported.NopCloser(bytes.NewReader(b)), shared.ContentTypeAppJSON, false)
}

// MarshalAsXML calls xml.Marshal() to get the XML encoding of v then calls SetBody.
func MarshalAsXML(req *policy.Request, v interface{}) error {
	b, err := xml.Marshal(v)
	if err != nil {
		return fmt.Errorf("error marshalling type %T: %s", v, err)
	}
	// inclue the XML header as some services require it
	b = []byte(xml.Header + string(b))
	return req.SetBody(exported.NopCloser(bytes.NewReader(b)), shared.ContentTypeAppXML)
}

// SetMultipartFormData writes the specified keys/values as multi-part form
// fields with the specified value.  File content must be specified as a ReadSeekCloser.
// All other values are treated as string values.
func SetMultipartFormData(req *policy.Request, formData map[string]interface{}) error {
	body := bytes.Buffer{}
	writer := multipart.NewWriter(&body)

	writeContent := func(fieldname, filename string, src io.Reader) error {
		fd, err := writer.CreateFormFile(fieldname, filename)
		if err != nil {
			return err
		}
		// copy the data to the form file
		if _, err = io.Copy(fd, src); err != nil {
			return err
		}
		return nil
	}

	for k, v := range formData {
		if rsc, ok := v.(io.ReadSeekCloser); ok {
			if err := writeContent(k, k, rsc); err != nil {
				return err
			}
			continue
		} else if rscs, ok := v.([]io.ReadSeekCloser); ok {
			for _, rsc := range rscs {
				if err := writeContent(k, k, rsc); err != nil {
					return err
				}
			}
			continue
		}
		// ensure the value is in string format
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprintf("%v", v)
		}
		if err := writer.WriteField(k, s); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return req.SetBody(exported.NopCloser(bytes.NewReader(body.Bytes())), writer.FormDataContentType())
}

// SkipBodyDownload will disable automatic downloading of the response body.
func SkipBodyDownload(req *policy.Request) {
	req.SetOperationValue(bodyDownloadPolicyOpValues{Skip: true})
}

// CtxAPINameKey is used as a context key for adding/retrieving the API name.
type CtxAPINameKey = shared.CtxAPINameKey
