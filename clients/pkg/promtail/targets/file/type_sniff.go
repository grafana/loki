package file

import (
	"bufio"
	"errors"
	"io"
	"net/http"
)

var binaryContentErr = errors.New("content is binary")

// checkIfBinary uses http.DetectContentType, which implements https://datatracker.ietf.org/doc/html/draft-ietf-websec-mime-sniff#page-9
func checkIfBinary(r io.Reader) error {
	reader := bufio.NewReader(r)

	// make a best-effort attempt to read into the buffer
	buf, _ := reader.Peek(512) //nolint:errcheck

	if http.DetectContentType(buf) == "application/octet-stream" {
		return binaryContentErr
	}

	return nil
}
