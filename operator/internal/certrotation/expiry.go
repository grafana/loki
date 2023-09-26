package certrotation

import (
	"fmt"
	"strings"
)

// CertExpiredError contains information if a certificate expired
// and the reasons of expiry.
type CertExpiredError struct {
	Message string
	Reasons []string
}

func (e *CertExpiredError) Error() string {
	return fmt.Sprintf("%s for reasons: %s", e.Message, strings.Join(e.Reasons, ", "))
}
