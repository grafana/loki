package fakestorage

import (
	"encoding/xml"
	"net/http"
	"strings"
)

type xmlResponse struct {
	status       int
	header       http.Header
	data         any
	errorMessage string
}

type xmlResponseBody struct {
	XMLName xml.Name `xml:"PostResponse"`
	Bucket  string
	Etag    struct {
		Value string `xml:",innerxml"`
	}
	Key      string
	Location string
}

type ListBucketResult struct {
	XMLName        xml.Name       `xml:"ListBucketResult"`
	Name           string         `xml:"Name"`
	CommonPrefixes []CommonPrefix `xml:"CommonPrefixes,omitempty"`
	Delimiter      string         `xml:"Delimiter"`
	Prefix         string         `xml:"Prefix"`
	KeyCount       int            `xml:"KeyCount"`
	Contents       []Contents     `xml:"Contents"`
}

type Contents struct {
	XMLName      xml.Name `xml:"Contents"`
	Key          string   `xml:"Key"`
	Generation   int64    `xml:"Generation"`
	LastModified string   `xml:"LastModified"`
	ETag         ETag
	Size         int64 `xml:"Size"`
}

type CommonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type ETag struct {
	Value string `xml:",innerxml"`
}

func (e *ETag) Equals(etag string) bool {
	trim := func(s string) string {
		return strings.TrimPrefix(strings.TrimSuffix(s, "\""), "\"")
	}
	return trim(e.Value) == trim(etag)
}

type xmlHandler = func(r *http.Request) xmlResponse

func xmlToHTTPHandler(h xmlHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := h(r)
		w.Header().Set("Content-Type", "application/xml")
		for name, values := range resp.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}

		status := resp.getStatus()
		var data any
		if status > 399 {
			data = newErrorResponse(status, resp.getErrorMessage(status), nil)
		} else {
			data = resp.data
		}

		w.WriteHeader(status)

		dataBytes, ok := data.([]byte)
		if ok {
			w.Write(dataBytes)
		} else {
			xml.NewEncoder(w).Encode(data)
		}
	}
}

func createXmlResponseBody(bucketName, etag, key, location string) []byte {
	responseBody := xmlResponseBody{
		Bucket: bucketName,
		Etag: struct {
			Value string `xml:",innerxml"`
		}{etag},
		Location: location,
		Key:      key,
	}
	x, err := xml.Marshal(responseBody)
	if err != nil {
		return nil
	}

	return []byte(xml.Header + string(x))
}

func (r *xmlResponse) getStatus() int {
	if r.status > 0 {
		return r.status
	}
	if r.errorMessage != "" {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

func (r *xmlResponse) getErrorMessage(status int) string {
	if r.errorMessage != "" {
		return r.errorMessage
	}
	return http.StatusText(status)
}
