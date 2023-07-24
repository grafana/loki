package events

// IoTCustomAuthorizerRequest contains data coming in to a custom IoT device gateway authorizer function.
type IoTCustomAuthorizerRequest struct {
	HTTPContext        *IoTHTTPContext `json:"httpContext,omitempty"`
	MQTTContext        *IoTMQTTContext `json:"mqttContext,omitempty"`
	TLSContext         *IoTTLSContext  `json:"tlsContext,omitempty"`
	AuthorizationToken string          `json:"token"`
	TokenSignature     string          `json:"tokenSignature"`
}

type IoTHTTPContext struct {
	Headers     map[string]string `json:"headers,omitempty"`
	QueryString string            `json:"queryString"`
}

type IoTMQTTContext struct {
	ClientID string `json:"clientId"`
	Password []byte `json:"password"`
	Username string `json:"username"`
}

type IoTTLSContext struct {
	ServerName string `json:"serverName"`
}

// IoTCustomAuthorizerResponse represents the expected format of an IoT device gateway authorization response.
type IoTCustomAuthorizerResponse struct {
	IsAuthenticated          bool     `json:"isAuthenticated"`
	PrincipalID              string   `json:"principalId"`
	DisconnectAfterInSeconds int32    `json:"disconnectAfterInSeconds"`
	RefreshAfterInSeconds    int32    `json:"refreshAfterInSeconds"`
	PolicyDocuments          []string `json:"policyDocuments"`
}
