package fakestorage

import (
	"encoding/xml"
	"net/http"
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
		if status == 201 {
			dataBytes, _ := data.([]byte)
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
