package labelaccess

import (
	"encoding/json"
	"net/http"
)

type ErrorAPI struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

func WritePromError(w http.ResponseWriter, errorMessage string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)

	defaultErrorMessage := `{"status": "error", "error": "` + errorMessage + `"}`
	errorAPI := &ErrorAPI{
		Status: "error",
		Error:  errorMessage,
	}
	errorAPIBytes, err := json.Marshal(errorAPI)
	if err != nil {
		errorAPIBytes = []byte(defaultErrorMessage)
	}
	w.Write(errorAPIBytes) // nolint:errcheck
}
