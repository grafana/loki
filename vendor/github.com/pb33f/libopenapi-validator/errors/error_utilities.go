package errors

import (
	"net/http"
)

// PopulateValidationErrors mutates the provided validation errors with additional useful error information, that is
// not necessarily available when the ValidationError was created and are standard for all errors.
// Specifically, the RequestPath, SpecPath and RequestMethod are populated.
func PopulateValidationErrors(validationErrors []*ValidationError, request *http.Request, path string) {
	for _, validationError := range validationErrors {
		validationError.SpecPath = path
		validationError.RequestMethod = request.Method
		validationError.RequestPath = request.URL.Path
	}
}
