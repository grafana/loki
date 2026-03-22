package signer

import (
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/aws/signer/ibmiam"
	"github.com/IBM/ibm-cos-sdk-go/aws/signer/v4"
)

const (
	debugLog        = "<DEBUG>"
	signerRouterLog = "SignerRouter"
)

type requestSignerRouter struct {
	signers map[string]request.NamedHandler
}

// SignRequestHandler handler to route the request to a signer according the credential type
var SignRequestHandler = defaultRequestSignerRouter()

// DefaultSignerHandlerForProviderType a map with default handlers per credential type
var DefaultSignerHandlerForProviderType = map[string]request.NamedHandler{
	"":      v4.SignRequestHandler,
	"oauth": ibmiam.SignRequestHandler,
	"v4":    v4.SignRequestHandler,
}

func defaultRequestSignerRouter() request.NamedHandler {
	router := requestSignerRouter{signers: make(map[string]request.NamedHandler)}
	for k, v := range DefaultSignerHandlerForProviderType {
		router.signers[k] = v
	}
	return request.NamedHandler{
		Name: "signer.requestsignerrouter", Fn: router.delegateRequestToSigner,
	}
}

// to be as close as possible to aws template
// *** required make public the method SignSDKRequestWithCurrTime

// CustomRequestSignerRouter routes the request to a signer according to the current credentials type
func CustomRequestSignerRouter(opts ...func(*v4.Signer)) request.NamedHandler {

	router := requestSignerRouter{signers: make(map[string]request.NamedHandler)}
	for k, v := range DefaultSignerHandlerForProviderType {
		router.signers[k] = v
	}

	customV4Handler := request.NamedHandler{
		Name: v4.SignRequestHandler.Name,
		Fn: func(req *request.Request) {
			v4.SignSDKRequestWithCurrentTime(req, time.Now, opts...)
		},
	}

	router.signers[""] = customV4Handler
	router.signers["v4"] = customV4Handler

	return request.NamedHandler{
		Name: SignRequestHandler.Name, Fn: router.delegateRequestToSigner,
	}
}

// use req config to access config and logging stufff
func (r requestSignerRouter) delegateRequestToSigner(req *request.Request) {

	logger := req.Config.Logger
	if !req.Config.LogLevel.Matches(aws.LogDebug) {
		logger = nil
	}

	if req.Config.Credentials == credentials.AnonymousCredentials {
		if logger != nil {
			logger.Log(debugLog, signerRouterLog, "AnonymousCredentials")
		}
		return
	}

	value, err := req.Config.Credentials.Get()
	if err != nil {
		if logger != nil {
			logger.Log(debugLog, signerRouterLog, "CREDENTIAL GET ERROR", err)
		}
		req.Error = err
		req.SignedHeaderVals = nil
		return
	}

	if logger != nil {
		logger.Log(debugLog, signerRouterLog, "Provider Type", value.ProviderType)
	}

	if handler, ok := r.signers[value.ProviderType]; ok {
		if logger != nil {
			logger.Log(debugLog, signerRouterLog, "Delegating to", handler.Name)
		}
		handler.Fn(req)
	} else {
		err = awserr.New("SignerRouterMissingHandler", "No Handler Found for Type "+value.ProviderType, nil)
		if logger != nil {
			logger.Log(debugLog, signerRouterLog, "No Handler Found", err)
		}
		req.Error = err
		req.SignedHeaderVals = nil
		return
	}
}
