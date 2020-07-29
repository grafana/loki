package events

import "encoding/json"

// AppSyncResolverTemplate represents the requests from AppSync to Lambda
type AppSyncResolverTemplate struct {
	Version   string           `json:"version"`
	Operation AppSyncOperation `json:"operation"`
	Payload   json.RawMessage  `json:"payload"`
}

// AppSyncIAMIdentity contains information about the caller authed via IAM.
type AppSyncIAMIdentity struct {
	AccountID             string   `json:"accountId"`
	CognitoIdentityPoolID string   `json:"cognitoIdentityPoolId"`
	CognitoIdentityID     string   `json:"cognitoIdentityId"`
	SourceIP              []string `json:"sourceIp"`
	Username              string   `json:"username"`
	UserARN               string   `json:"userArn"`
}

// AppSyncCognitoIdentity contains information about the caller authed via Cognito.
type AppSyncCognitoIdentity struct {
	Sub                 string                 `json:"sub"`
	Issuer              string                 `json:"issuer"`
	Username            string                 `json:"username"`
	Claims              map[string]interface{} `json:"claims"`
	SourceIP            []string               `json:"sourceIp"`
	DefaultAuthStrategy string                 `json:"defaultAuthStrategy"`
}

// AppSyncOperation specifies the operation type supported by Lambda operations
type AppSyncOperation string

const (
	// OperationInvoke lets AWS AppSync know to call your Lambda function for every GraphQL field resolver
	OperationInvoke AppSyncOperation = "Invoke"
	// OperationBatchInvoke instructs AWS AppSync to batch requests for the current GraphQL field
	OperationBatchInvoke AppSyncOperation = "BatchInvoke"
)
