package token

import "encoding/json"

// IBMIAMToken holder for the IBM IAM token details
type Token struct {

	// Sets the access token
	AccessToken string `json:"access_token"`

	// Sets the refresh token
	RefreshToken string `json:"refresh_token"`

	// Sets the token type
	TokenType string `json:"token_type"`

	// Scope string `json:"scope"`

	// Sets the expiry timestamp
	ExpiresIn int64 `json:"expires_in"`

	// Sets the expiration timestamp
	Expiration int64 `json:"expiration"`
}

// Error type to help parse errors of IAM calls
type Error struct {
	Context      map[string]interface{} `json:"context"`
	ErrorCode    string                 `json:"errorCode"`
	ErrorMessage string                 `json:"errorMessage"`
}

// Error function
func (ie *Error) Error() string {
	bytes, err := json.Marshal(ie)
	if err != nil {
		return err.Error()
	}
	return string(bytes)
}
