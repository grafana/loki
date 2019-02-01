package restxml_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/signer/v4"
	"github.com/aws/aws-sdk-go/awstesting"
	"github.com/aws/aws-sdk-go/awstesting/unit"
	"github.com/aws/aws-sdk-go/private/protocol"
	"github.com/aws/aws-sdk-go/private/protocol/restxml"
	"github.com/aws/aws-sdk-go/private/protocol/xml/xmlutil"
	"github.com/aws/aws-sdk-go/private/util"
)

var _ bytes.Buffer // always import bytes
var _ http.Request
var _ json.Marshaler
var _ time.Time
var _ xmlutil.XMLNode
var _ xml.Attr
var _ = ioutil.Discard
var _ = util.Trim("")
var _ = url.Values{}
var _ = io.EOF
var _ = aws.String
var _ = fmt.Println
var _ = reflect.Value{}

func init() {
	protocol.RandReader = &awstesting.ZeroReader{}
}

// InputService1ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService1ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService1ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService1ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService1ProtocolTest client from just a session.
//     svc := inputservice1protocoltest.New(mySession)
//
//     // Create a InputService1ProtocolTest client with additional configuration
//     svc := inputservice1protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService1ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService1ProtocolTest {
	c := p.ClientConfig("inputservice1protocoltest", cfgs...)
	return newInputService1ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService1ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService1ProtocolTest {
	svc := &InputService1ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService1ProtocolTest",
				ServiceID:     "InputService1ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService1ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService1ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService1TestCaseOperation1 = "OperationName"

// InputService1TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation1 for more information on using the InputService1TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation1Request method.
//    req, resp := client.InputService1TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1Request(input *InputService1TestShapeInputService1TestCaseOperation1Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService1TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation1Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation1 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1(input *InputService1TestShapeInputService1TestCaseOperation1Input) (*InputService1TestShapeInputService1TestCaseOperation1Output, error) {
	req, out := c.InputService1TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation1WithContext is the same as InputService1TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation1WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation1Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation1Output, error) {
	req, out := c.InputService1TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService1TestCaseOperation2 = "OperationName"

// InputService1TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation2 for more information on using the InputService1TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation2Request method.
//    req, resp := client.InputService1TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2Request(input *InputService1TestShapeInputService1TestCaseOperation2Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService1TestCaseOperation2,
		HTTPMethod: "PUT",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation2Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation2 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2(input *InputService1TestShapeInputService1TestCaseOperation2Input) (*InputService1TestShapeInputService1TestCaseOperation2Output, error) {
	req, out := c.InputService1TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation2WithContext is the same as InputService1TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation2WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation2Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation2Output, error) {
	req, out := c.InputService1TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService1TestCaseOperation3 = "OperationName"

// InputService1TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService1TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService1TestCaseOperation3 for more information on using the InputService1TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService1TestCaseOperation3Request method.
//    req, resp := client.InputService1TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3Request(input *InputService1TestShapeInputService1TestCaseOperation3Input) (req *request.Request, output *InputService1TestShapeInputService1TestCaseOperation3Output) {
	op := &request.Operation{
		Name:       opInputService1TestCaseOperation3,
		HTTPMethod: "GET",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService1TestShapeInputService1TestCaseOperation3Input{}
	}

	output = &InputService1TestShapeInputService1TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService1TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService1TestCaseOperation3 for usage and error information.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3(input *InputService1TestShapeInputService1TestCaseOperation3Input) (*InputService1TestShapeInputService1TestCaseOperation3Output, error) {
	req, out := c.InputService1TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService1TestCaseOperation3WithContext is the same as InputService1TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService1TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService1ProtocolTest) InputService1TestCaseOperation3WithContext(ctx aws.Context, input *InputService1TestShapeInputService1TestCaseOperation3Input, opts ...request.Option) (*InputService1TestShapeInputService1TestCaseOperation3Output, error) {
	req, out := c.InputService1TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService1TestShapeInputService1TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Description *string `type:"string"`

	Name *string `type:"string"`
}

// SetDescription sets the Description field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation1Input) SetDescription(v string) *InputService1TestShapeInputService1TestCaseOperation1Input {
	s.Description = &v
	return s
}

// SetName sets the Name field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation1Input) SetName(v string) *InputService1TestShapeInputService1TestCaseOperation1Input {
	s.Name = &v
	return s
}

type InputService1TestShapeInputService1TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService1TestShapeInputService1TestCaseOperation2Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Description *string `type:"string"`

	Name *string `type:"string"`
}

// SetDescription sets the Description field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation2Input) SetDescription(v string) *InputService1TestShapeInputService1TestCaseOperation2Input {
	s.Description = &v
	return s
}

// SetName sets the Name field's value.
func (s *InputService1TestShapeInputService1TestCaseOperation2Input) SetName(v string) *InputService1TestShapeInputService1TestCaseOperation2Input {
	s.Name = &v
	return s
}

type InputService1TestShapeInputService1TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService1TestShapeInputService1TestCaseOperation3Input struct {
	_ struct{} `type:"structure"`
}

type InputService1TestShapeInputService1TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

// InputService2ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService2ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService2ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService2ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService2ProtocolTest client from just a session.
//     svc := inputservice2protocoltest.New(mySession)
//
//     // Create a InputService2ProtocolTest client with additional configuration
//     svc := inputservice2protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService2ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService2ProtocolTest {
	c := p.ClientConfig("inputservice2protocoltest", cfgs...)
	return newInputService2ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService2ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService2ProtocolTest {
	svc := &InputService2ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService2ProtocolTest",
				ServiceID:     "InputService2ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService2ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService2ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService2TestCaseOperation1 = "OperationName"

// InputService2TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService2TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService2TestCaseOperation1 for more information on using the InputService2TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService2TestCaseOperation1Request method.
//    req, resp := client.InputService2TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1Request(input *InputService2TestShapeInputService2TestCaseOperation1Input) (req *request.Request, output *InputService2TestShapeInputService2TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService2TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService2TestShapeInputService2TestCaseOperation1Input{}
	}

	output = &InputService2TestShapeInputService2TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService2TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService2TestCaseOperation1 for usage and error information.
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1(input *InputService2TestShapeInputService2TestCaseOperation1Input) (*InputService2TestShapeInputService2TestCaseOperation1Output, error) {
	req, out := c.InputService2TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService2TestCaseOperation1WithContext is the same as InputService2TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService2TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService2ProtocolTest) InputService2TestCaseOperation1WithContext(ctx aws.Context, input *InputService2TestShapeInputService2TestCaseOperation1Input, opts ...request.Option) (*InputService2TestShapeInputService2TestCaseOperation1Output, error) {
	req, out := c.InputService2TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService2TestShapeInputService2TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	First *bool `type:"boolean"`

	Fourth *int64 `type:"integer"`

	Second *bool `type:"boolean"`

	Third *float64 `type:"float"`
}

// SetFirst sets the First field's value.
func (s *InputService2TestShapeInputService2TestCaseOperation1Input) SetFirst(v bool) *InputService2TestShapeInputService2TestCaseOperation1Input {
	s.First = &v
	return s
}

// SetFourth sets the Fourth field's value.
func (s *InputService2TestShapeInputService2TestCaseOperation1Input) SetFourth(v int64) *InputService2TestShapeInputService2TestCaseOperation1Input {
	s.Fourth = &v
	return s
}

// SetSecond sets the Second field's value.
func (s *InputService2TestShapeInputService2TestCaseOperation1Input) SetSecond(v bool) *InputService2TestShapeInputService2TestCaseOperation1Input {
	s.Second = &v
	return s
}

// SetThird sets the Third field's value.
func (s *InputService2TestShapeInputService2TestCaseOperation1Input) SetThird(v float64) *InputService2TestShapeInputService2TestCaseOperation1Input {
	s.Third = &v
	return s
}

type InputService2TestShapeInputService2TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService3ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService3ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService3ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService3ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService3ProtocolTest client from just a session.
//     svc := inputservice3protocoltest.New(mySession)
//
//     // Create a InputService3ProtocolTest client with additional configuration
//     svc := inputservice3protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService3ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService3ProtocolTest {
	c := p.ClientConfig("inputservice3protocoltest", cfgs...)
	return newInputService3ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService3ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService3ProtocolTest {
	svc := &InputService3ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService3ProtocolTest",
				ServiceID:     "InputService3ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService3ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService3ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService3TestCaseOperation1 = "OperationName"

// InputService3TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService3TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService3TestCaseOperation1 for more information on using the InputService3TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService3TestCaseOperation1Request method.
//    req, resp := client.InputService3TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1Request(input *InputService3TestShapeInputService3TestCaseOperation1Input) (req *request.Request, output *InputService3TestShapeInputService3TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService3TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService3TestShapeInputService3TestCaseOperation1Input{}
	}

	output = &InputService3TestShapeInputService3TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService3TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService3TestCaseOperation1 for usage and error information.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1(input *InputService3TestShapeInputService3TestCaseOperation1Input) (*InputService3TestShapeInputService3TestCaseOperation1Output, error) {
	req, out := c.InputService3TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService3TestCaseOperation1WithContext is the same as InputService3TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService3TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation1WithContext(ctx aws.Context, input *InputService3TestShapeInputService3TestCaseOperation1Input, opts ...request.Option) (*InputService3TestShapeInputService3TestCaseOperation1Output, error) {
	req, out := c.InputService3TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService3TestCaseOperation2 = "OperationName"

// InputService3TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService3TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService3TestCaseOperation2 for more information on using the InputService3TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService3TestCaseOperation2Request method.
//    req, resp := client.InputService3TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2Request(input *InputService3TestShapeInputService3TestCaseOperation2Input) (req *request.Request, output *InputService3TestShapeInputService3TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService3TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService3TestShapeInputService3TestCaseOperation2Input{}
	}

	output = &InputService3TestShapeInputService3TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService3TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService3TestCaseOperation2 for usage and error information.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2(input *InputService3TestShapeInputService3TestCaseOperation2Input) (*InputService3TestShapeInputService3TestCaseOperation2Output, error) {
	req, out := c.InputService3TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService3TestCaseOperation2WithContext is the same as InputService3TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService3TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService3ProtocolTest) InputService3TestCaseOperation2WithContext(ctx aws.Context, input *InputService3TestShapeInputService3TestCaseOperation2Input, opts ...request.Option) (*InputService3TestShapeInputService3TestCaseOperation2Output, error) {
	req, out := c.InputService3TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService3TestShapeInputService3TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Description *string `type:"string"`

	SubStructure *InputService3TestShapeSubStructure `type:"structure"`
}

// SetDescription sets the Description field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation1Input) SetDescription(v string) *InputService3TestShapeInputService3TestCaseOperation1Input {
	s.Description = &v
	return s
}

// SetSubStructure sets the SubStructure field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation1Input) SetSubStructure(v *InputService3TestShapeSubStructure) *InputService3TestShapeInputService3TestCaseOperation1Input {
	s.SubStructure = v
	return s
}

type InputService3TestShapeInputService3TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService3TestShapeInputService3TestCaseOperation2Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Description *string `type:"string"`

	SubStructure *InputService3TestShapeSubStructure `type:"structure"`
}

// SetDescription sets the Description field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation2Input) SetDescription(v string) *InputService3TestShapeInputService3TestCaseOperation2Input {
	s.Description = &v
	return s
}

// SetSubStructure sets the SubStructure field's value.
func (s *InputService3TestShapeInputService3TestCaseOperation2Input) SetSubStructure(v *InputService3TestShapeSubStructure) *InputService3TestShapeInputService3TestCaseOperation2Input {
	s.SubStructure = v
	return s
}

type InputService3TestShapeInputService3TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService3TestShapeSubStructure struct {
	_ struct{} `type:"structure"`

	Bar *string `type:"string"`

	Foo *string `type:"string"`
}

// SetBar sets the Bar field's value.
func (s *InputService3TestShapeSubStructure) SetBar(v string) *InputService3TestShapeSubStructure {
	s.Bar = &v
	return s
}

// SetFoo sets the Foo field's value.
func (s *InputService3TestShapeSubStructure) SetFoo(v string) *InputService3TestShapeSubStructure {
	s.Foo = &v
	return s
}

// InputService4ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService4ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService4ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService4ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService4ProtocolTest client from just a session.
//     svc := inputservice4protocoltest.New(mySession)
//
//     // Create a InputService4ProtocolTest client with additional configuration
//     svc := inputservice4protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService4ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService4ProtocolTest {
	c := p.ClientConfig("inputservice4protocoltest", cfgs...)
	return newInputService4ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService4ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService4ProtocolTest {
	svc := &InputService4ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService4ProtocolTest",
				ServiceID:     "InputService4ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService4ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService4ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService4TestCaseOperation1 = "OperationName"

// InputService4TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService4TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService4TestCaseOperation1 for more information on using the InputService4TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService4TestCaseOperation1Request method.
//    req, resp := client.InputService4TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1Request(input *InputService4TestShapeInputService4TestCaseOperation1Input) (req *request.Request, output *InputService4TestShapeInputService4TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService4TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService4TestShapeInputService4TestCaseOperation1Input{}
	}

	output = &InputService4TestShapeInputService4TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService4TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService4TestCaseOperation1 for usage and error information.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1(input *InputService4TestShapeInputService4TestCaseOperation1Input) (*InputService4TestShapeInputService4TestCaseOperation1Output, error) {
	req, out := c.InputService4TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService4TestCaseOperation1WithContext is the same as InputService4TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService4TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService4ProtocolTest) InputService4TestCaseOperation1WithContext(ctx aws.Context, input *InputService4TestShapeInputService4TestCaseOperation1Input, opts ...request.Option) (*InputService4TestShapeInputService4TestCaseOperation1Output, error) {
	req, out := c.InputService4TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService4TestShapeInputService4TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Description *string `type:"string"`

	SubStructure *InputService4TestShapeSubStructure `type:"structure"`
}

// SetDescription sets the Description field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation1Input) SetDescription(v string) *InputService4TestShapeInputService4TestCaseOperation1Input {
	s.Description = &v
	return s
}

// SetSubStructure sets the SubStructure field's value.
func (s *InputService4TestShapeInputService4TestCaseOperation1Input) SetSubStructure(v *InputService4TestShapeSubStructure) *InputService4TestShapeInputService4TestCaseOperation1Input {
	s.SubStructure = v
	return s
}

type InputService4TestShapeInputService4TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService4TestShapeSubStructure struct {
	_ struct{} `type:"structure"`

	Bar *string `type:"string"`

	Foo *string `type:"string"`
}

// SetBar sets the Bar field's value.
func (s *InputService4TestShapeSubStructure) SetBar(v string) *InputService4TestShapeSubStructure {
	s.Bar = &v
	return s
}

// SetFoo sets the Foo field's value.
func (s *InputService4TestShapeSubStructure) SetFoo(v string) *InputService4TestShapeSubStructure {
	s.Foo = &v
	return s
}

// InputService5ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService5ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService5ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService5ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService5ProtocolTest client from just a session.
//     svc := inputservice5protocoltest.New(mySession)
//
//     // Create a InputService5ProtocolTest client with additional configuration
//     svc := inputservice5protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService5ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService5ProtocolTest {
	c := p.ClientConfig("inputservice5protocoltest", cfgs...)
	return newInputService5ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService5ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService5ProtocolTest {
	svc := &InputService5ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService5ProtocolTest",
				ServiceID:     "InputService5ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService5ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService5ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService5TestCaseOperation1 = "OperationName"

// InputService5TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService5TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService5TestCaseOperation1 for more information on using the InputService5TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService5TestCaseOperation1Request method.
//    req, resp := client.InputService5TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1Request(input *InputService5TestShapeInputService5TestCaseOperation1Input) (req *request.Request, output *InputService5TestShapeInputService5TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService5TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService5TestShapeInputService5TestCaseOperation1Input{}
	}

	output = &InputService5TestShapeInputService5TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService5TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService5TestCaseOperation1 for usage and error information.
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1(input *InputService5TestShapeInputService5TestCaseOperation1Input) (*InputService5TestShapeInputService5TestCaseOperation1Output, error) {
	req, out := c.InputService5TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService5TestCaseOperation1WithContext is the same as InputService5TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService5TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService5ProtocolTest) InputService5TestCaseOperation1WithContext(ctx aws.Context, input *InputService5TestShapeInputService5TestCaseOperation1Input, opts ...request.Option) (*InputService5TestShapeInputService5TestCaseOperation1Output, error) {
	req, out := c.InputService5TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService5TestShapeInputService5TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	ListParam []*string `type:"list"`
}

// SetListParam sets the ListParam field's value.
func (s *InputService5TestShapeInputService5TestCaseOperation1Input) SetListParam(v []*string) *InputService5TestShapeInputService5TestCaseOperation1Input {
	s.ListParam = v
	return s
}

type InputService5TestShapeInputService5TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService6ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService6ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService6ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService6ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService6ProtocolTest client from just a session.
//     svc := inputservice6protocoltest.New(mySession)
//
//     // Create a InputService6ProtocolTest client with additional configuration
//     svc := inputservice6protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService6ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService6ProtocolTest {
	c := p.ClientConfig("inputservice6protocoltest", cfgs...)
	return newInputService6ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService6ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService6ProtocolTest {
	svc := &InputService6ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService6ProtocolTest",
				ServiceID:     "InputService6ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService6ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService6ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService6TestCaseOperation1 = "OperationName"

// InputService6TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService6TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService6TestCaseOperation1 for more information on using the InputService6TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService6TestCaseOperation1Request method.
//    req, resp := client.InputService6TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1Request(input *InputService6TestShapeInputService6TestCaseOperation1Input) (req *request.Request, output *InputService6TestShapeInputService6TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService6TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService6TestShapeInputService6TestCaseOperation1Input{}
	}

	output = &InputService6TestShapeInputService6TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService6TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService6TestCaseOperation1 for usage and error information.
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1(input *InputService6TestShapeInputService6TestCaseOperation1Input) (*InputService6TestShapeInputService6TestCaseOperation1Output, error) {
	req, out := c.InputService6TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService6TestCaseOperation1WithContext is the same as InputService6TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService6TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService6ProtocolTest) InputService6TestCaseOperation1WithContext(ctx aws.Context, input *InputService6TestShapeInputService6TestCaseOperation1Input, opts ...request.Option) (*InputService6TestShapeInputService6TestCaseOperation1Output, error) {
	req, out := c.InputService6TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService6TestShapeInputService6TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	ListParam []*string `locationName:"AlternateName" locationNameList:"NotMember" type:"list"`
}

// SetListParam sets the ListParam field's value.
func (s *InputService6TestShapeInputService6TestCaseOperation1Input) SetListParam(v []*string) *InputService6TestShapeInputService6TestCaseOperation1Input {
	s.ListParam = v
	return s
}

type InputService6TestShapeInputService6TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService7ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService7ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService7ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService7ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService7ProtocolTest client from just a session.
//     svc := inputservice7protocoltest.New(mySession)
//
//     // Create a InputService7ProtocolTest client with additional configuration
//     svc := inputservice7protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService7ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService7ProtocolTest {
	c := p.ClientConfig("inputservice7protocoltest", cfgs...)
	return newInputService7ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService7ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService7ProtocolTest {
	svc := &InputService7ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService7ProtocolTest",
				ServiceID:     "InputService7ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService7ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService7ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService7TestCaseOperation1 = "OperationName"

// InputService7TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService7TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService7TestCaseOperation1 for more information on using the InputService7TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService7TestCaseOperation1Request method.
//    req, resp := client.InputService7TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1Request(input *InputService7TestShapeInputService7TestCaseOperation1Input) (req *request.Request, output *InputService7TestShapeInputService7TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService7TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService7TestShapeInputService7TestCaseOperation1Input{}
	}

	output = &InputService7TestShapeInputService7TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService7TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService7TestCaseOperation1 for usage and error information.
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1(input *InputService7TestShapeInputService7TestCaseOperation1Input) (*InputService7TestShapeInputService7TestCaseOperation1Output, error) {
	req, out := c.InputService7TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService7TestCaseOperation1WithContext is the same as InputService7TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService7TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService7ProtocolTest) InputService7TestCaseOperation1WithContext(ctx aws.Context, input *InputService7TestShapeInputService7TestCaseOperation1Input, opts ...request.Option) (*InputService7TestShapeInputService7TestCaseOperation1Output, error) {
	req, out := c.InputService7TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService7TestShapeInputService7TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	ListParam []*string `type:"list" flattened:"true"`
}

// SetListParam sets the ListParam field's value.
func (s *InputService7TestShapeInputService7TestCaseOperation1Input) SetListParam(v []*string) *InputService7TestShapeInputService7TestCaseOperation1Input {
	s.ListParam = v
	return s
}

type InputService7TestShapeInputService7TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService8ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService8ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService8ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService8ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService8ProtocolTest client from just a session.
//     svc := inputservice8protocoltest.New(mySession)
//
//     // Create a InputService8ProtocolTest client with additional configuration
//     svc := inputservice8protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService8ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService8ProtocolTest {
	c := p.ClientConfig("inputservice8protocoltest", cfgs...)
	return newInputService8ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService8ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService8ProtocolTest {
	svc := &InputService8ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService8ProtocolTest",
				ServiceID:     "InputService8ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService8ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService8ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService8TestCaseOperation1 = "OperationName"

// InputService8TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService8TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService8TestCaseOperation1 for more information on using the InputService8TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService8TestCaseOperation1Request method.
//    req, resp := client.InputService8TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1Request(input *InputService8TestShapeInputService8TestCaseOperation1Input) (req *request.Request, output *InputService8TestShapeInputService8TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService8TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService8TestShapeInputService8TestCaseOperation1Input{}
	}

	output = &InputService8TestShapeInputService8TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService8TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService8TestCaseOperation1 for usage and error information.
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1(input *InputService8TestShapeInputService8TestCaseOperation1Input) (*InputService8TestShapeInputService8TestCaseOperation1Output, error) {
	req, out := c.InputService8TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService8TestCaseOperation1WithContext is the same as InputService8TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService8TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService8ProtocolTest) InputService8TestCaseOperation1WithContext(ctx aws.Context, input *InputService8TestShapeInputService8TestCaseOperation1Input, opts ...request.Option) (*InputService8TestShapeInputService8TestCaseOperation1Output, error) {
	req, out := c.InputService8TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService8TestShapeInputService8TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	ListParam []*string `locationName:"item" type:"list" flattened:"true"`
}

// SetListParam sets the ListParam field's value.
func (s *InputService8TestShapeInputService8TestCaseOperation1Input) SetListParam(v []*string) *InputService8TestShapeInputService8TestCaseOperation1Input {
	s.ListParam = v
	return s
}

type InputService8TestShapeInputService8TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService9ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService9ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService9ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService9ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService9ProtocolTest client from just a session.
//     svc := inputservice9protocoltest.New(mySession)
//
//     // Create a InputService9ProtocolTest client with additional configuration
//     svc := inputservice9protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService9ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService9ProtocolTest {
	c := p.ClientConfig("inputservice9protocoltest", cfgs...)
	return newInputService9ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService9ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService9ProtocolTest {
	svc := &InputService9ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService9ProtocolTest",
				ServiceID:     "InputService9ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService9ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService9ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService9TestCaseOperation1 = "OperationName"

// InputService9TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService9TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService9TestCaseOperation1 for more information on using the InputService9TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService9TestCaseOperation1Request method.
//    req, resp := client.InputService9TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1Request(input *InputService9TestShapeInputService9TestCaseOperation1Input) (req *request.Request, output *InputService9TestShapeInputService9TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService9TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService9TestShapeInputService9TestCaseOperation1Input{}
	}

	output = &InputService9TestShapeInputService9TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService9TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService9TestCaseOperation1 for usage and error information.
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1(input *InputService9TestShapeInputService9TestCaseOperation1Input) (*InputService9TestShapeInputService9TestCaseOperation1Output, error) {
	req, out := c.InputService9TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService9TestCaseOperation1WithContext is the same as InputService9TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService9TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService9ProtocolTest) InputService9TestCaseOperation1WithContext(ctx aws.Context, input *InputService9TestShapeInputService9TestCaseOperation1Input, opts ...request.Option) (*InputService9TestShapeInputService9TestCaseOperation1Output, error) {
	req, out := c.InputService9TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService9TestShapeInputService9TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	ListParam []*InputService9TestShapeSingleFieldStruct `locationName:"item" type:"list" flattened:"true"`
}

// SetListParam sets the ListParam field's value.
func (s *InputService9TestShapeInputService9TestCaseOperation1Input) SetListParam(v []*InputService9TestShapeSingleFieldStruct) *InputService9TestShapeInputService9TestCaseOperation1Input {
	s.ListParam = v
	return s
}

type InputService9TestShapeInputService9TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService9TestShapeSingleFieldStruct struct {
	_ struct{} `type:"structure"`

	Element *string `locationName:"value" type:"string"`
}

// SetElement sets the Element field's value.
func (s *InputService9TestShapeSingleFieldStruct) SetElement(v string) *InputService9TestShapeSingleFieldStruct {
	s.Element = &v
	return s
}

// InputService10ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService10ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService10ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService10ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService10ProtocolTest client from just a session.
//     svc := inputservice10protocoltest.New(mySession)
//
//     // Create a InputService10ProtocolTest client with additional configuration
//     svc := inputservice10protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService10ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService10ProtocolTest {
	c := p.ClientConfig("inputservice10protocoltest", cfgs...)
	return newInputService10ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService10ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService10ProtocolTest {
	svc := &InputService10ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService10ProtocolTest",
				ServiceID:     "InputService10ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService10ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService10ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService10TestCaseOperation1 = "OperationName"

// InputService10TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService10TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService10TestCaseOperation1 for more information on using the InputService10TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService10TestCaseOperation1Request method.
//    req, resp := client.InputService10TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1Request(input *InputService10TestShapeInputService10TestCaseOperation1Input) (req *request.Request, output *InputService10TestShapeInputService10TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService10TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService10TestShapeInputService10TestCaseOperation1Input{}
	}

	output = &InputService10TestShapeInputService10TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService10TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService10TestCaseOperation1 for usage and error information.
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1(input *InputService10TestShapeInputService10TestCaseOperation1Input) (*InputService10TestShapeInputService10TestCaseOperation1Output, error) {
	req, out := c.InputService10TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService10TestCaseOperation1WithContext is the same as InputService10TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService10TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService10ProtocolTest) InputService10TestCaseOperation1WithContext(ctx aws.Context, input *InputService10TestShapeInputService10TestCaseOperation1Input, opts ...request.Option) (*InputService10TestShapeInputService10TestCaseOperation1Output, error) {
	req, out := c.InputService10TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService10TestShapeInputService10TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	StructureParam *InputService10TestShapeStructureShape `type:"structure"`
}

// SetStructureParam sets the StructureParam field's value.
func (s *InputService10TestShapeInputService10TestCaseOperation1Input) SetStructureParam(v *InputService10TestShapeStructureShape) *InputService10TestShapeInputService10TestCaseOperation1Input {
	s.StructureParam = v
	return s
}

type InputService10TestShapeInputService10TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService10TestShapeStructureShape struct {
	_ struct{} `type:"structure"`

	// B is automatically base64 encoded/decoded by the SDK.
	B []byte `locationName:"b" type:"blob"`
}

// SetB sets the B field's value.
func (s *InputService10TestShapeStructureShape) SetB(v []byte) *InputService10TestShapeStructureShape {
	s.B = v
	return s
}

// InputService11ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService11ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService11ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService11ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService11ProtocolTest client from just a session.
//     svc := inputservice11protocoltest.New(mySession)
//
//     // Create a InputService11ProtocolTest client with additional configuration
//     svc := inputservice11protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService11ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService11ProtocolTest {
	c := p.ClientConfig("inputservice11protocoltest", cfgs...)
	return newInputService11ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService11ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService11ProtocolTest {
	svc := &InputService11ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService11ProtocolTest",
				ServiceID:     "InputService11ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService11ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService11ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService11TestCaseOperation1 = "OperationName"

// InputService11TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService11TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService11TestCaseOperation1 for more information on using the InputService11TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService11TestCaseOperation1Request method.
//    req, resp := client.InputService11TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1Request(input *InputService11TestShapeInputService11TestCaseOperation1Input) (req *request.Request, output *InputService11TestShapeInputService11TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService11TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/2014-01-01/hostedzone",
	}

	if input == nil {
		input = &InputService11TestShapeInputService11TestCaseOperation1Input{}
	}

	output = &InputService11TestShapeInputService11TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService11TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService11TestCaseOperation1 for usage and error information.
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1(input *InputService11TestShapeInputService11TestCaseOperation1Input) (*InputService11TestShapeInputService11TestCaseOperation1Output, error) {
	req, out := c.InputService11TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService11TestCaseOperation1WithContext is the same as InputService11TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService11TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService11ProtocolTest) InputService11TestCaseOperation1WithContext(ctx aws.Context, input *InputService11TestShapeInputService11TestCaseOperation1Input, opts ...request.Option) (*InputService11TestShapeInputService11TestCaseOperation1Output, error) {
	req, out := c.InputService11TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService11TestShapeInputService11TestCaseOperation1Input struct {
	_ struct{} `locationName:"TimestampStructure" type:"structure" xmlURI:"https://foo/"`

	TimeArg *time.Time `type:"timestamp"`

	TimeArgInHeader *time.Time `location:"header" locationName:"x-amz-timearg" type:"timestamp"`

	TimeArgInQuery *time.Time `location:"querystring" locationName:"TimeQuery" type:"timestamp"`

	TimeCustom *time.Time `type:"timestamp" timestampFormat:"rfc822"`

	TimeCustomInHeader *time.Time `location:"header" locationName:"x-amz-timecustom-header" type:"timestamp" timestampFormat:"unixTimestamp"`

	TimeCustomInQuery *time.Time `location:"querystring" locationName:"TimeCustomQuery" type:"timestamp" timestampFormat:"unixTimestamp"`

	TimeFormat *time.Time `type:"timestamp" timestampFormat:"rfc822"`

	TimeFormatInHeader *time.Time `location:"header" locationName:"x-amz-timeformat-header" type:"timestamp" timestampFormat:"unixTimestamp"`

	TimeFormatInQuery *time.Time `location:"querystring" locationName:"TimeFormatQuery" type:"timestamp" timestampFormat:"unixTimestamp"`
}

// SetTimeArg sets the TimeArg field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeArg(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeArg = &v
	return s
}

// SetTimeArgInHeader sets the TimeArgInHeader field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeArgInHeader(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeArgInHeader = &v
	return s
}

// SetTimeArgInQuery sets the TimeArgInQuery field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeArgInQuery(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeArgInQuery = &v
	return s
}

// SetTimeCustom sets the TimeCustom field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeCustom(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeCustom = &v
	return s
}

// SetTimeCustomInHeader sets the TimeCustomInHeader field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeCustomInHeader(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeCustomInHeader = &v
	return s
}

// SetTimeCustomInQuery sets the TimeCustomInQuery field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeCustomInQuery(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeCustomInQuery = &v
	return s
}

// SetTimeFormat sets the TimeFormat field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeFormat(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeFormat = &v
	return s
}

// SetTimeFormatInHeader sets the TimeFormatInHeader field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeFormatInHeader(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeFormatInHeader = &v
	return s
}

// SetTimeFormatInQuery sets the TimeFormatInQuery field's value.
func (s *InputService11TestShapeInputService11TestCaseOperation1Input) SetTimeFormatInQuery(v time.Time) *InputService11TestShapeInputService11TestCaseOperation1Input {
	s.TimeFormatInQuery = &v
	return s
}

type InputService11TestShapeInputService11TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService12ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService12ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService12ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService12ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService12ProtocolTest client from just a session.
//     svc := inputservice12protocoltest.New(mySession)
//
//     // Create a InputService12ProtocolTest client with additional configuration
//     svc := inputservice12protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService12ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService12ProtocolTest {
	c := p.ClientConfig("inputservice12protocoltest", cfgs...)
	return newInputService12ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService12ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService12ProtocolTest {
	svc := &InputService12ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService12ProtocolTest",
				ServiceID:     "InputService12ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService12ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService12ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService12TestCaseOperation1 = "OperationName"

// InputService12TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService12TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService12TestCaseOperation1 for more information on using the InputService12TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService12TestCaseOperation1Request method.
//    req, resp := client.InputService12TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1Request(input *InputService12TestShapeInputService12TestCaseOperation1Input) (req *request.Request, output *InputService12TestShapeInputService12TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService12TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService12TestShapeInputService12TestCaseOperation1Input{}
	}

	output = &InputService12TestShapeInputService12TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService12TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService12TestCaseOperation1 for usage and error information.
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1(input *InputService12TestShapeInputService12TestCaseOperation1Input) (*InputService12TestShapeInputService12TestCaseOperation1Output, error) {
	req, out := c.InputService12TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService12TestCaseOperation1WithContext is the same as InputService12TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService12TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService12ProtocolTest) InputService12TestCaseOperation1WithContext(ctx aws.Context, input *InputService12TestShapeInputService12TestCaseOperation1Input, opts ...request.Option) (*InputService12TestShapeInputService12TestCaseOperation1Output, error) {
	req, out := c.InputService12TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService12TestShapeInputService12TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	Foo map[string]*string `location:"headers" locationName:"x-foo-" type:"map"`
}

// SetFoo sets the Foo field's value.
func (s *InputService12TestShapeInputService12TestCaseOperation1Input) SetFoo(v map[string]*string) *InputService12TestShapeInputService12TestCaseOperation1Input {
	s.Foo = v
	return s
}

type InputService12TestShapeInputService12TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService13ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService13ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService13ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService13ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService13ProtocolTest client from just a session.
//     svc := inputservice13protocoltest.New(mySession)
//
//     // Create a InputService13ProtocolTest client with additional configuration
//     svc := inputservice13protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService13ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService13ProtocolTest {
	c := p.ClientConfig("inputservice13protocoltest", cfgs...)
	return newInputService13ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService13ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService13ProtocolTest {
	svc := &InputService13ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService13ProtocolTest",
				ServiceID:     "InputService13ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService13ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService13ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService13TestCaseOperation1 = "OperationName"

// InputService13TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService13TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService13TestCaseOperation1 for more information on using the InputService13TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService13TestCaseOperation1Request method.
//    req, resp := client.InputService13TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1Request(input *InputService13TestShapeInputService13TestCaseOperation1Input) (req *request.Request, output *InputService13TestShapeInputService13TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService13TestCaseOperation1,
		HTTPMethod: "GET",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService13TestShapeInputService13TestCaseOperation1Input{}
	}

	output = &InputService13TestShapeInputService13TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService13TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService13TestCaseOperation1 for usage and error information.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1(input *InputService13TestShapeInputService13TestCaseOperation1Input) (*InputService13TestShapeInputService13TestCaseOperation1Output, error) {
	req, out := c.InputService13TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService13TestCaseOperation1WithContext is the same as InputService13TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService13TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService13ProtocolTest) InputService13TestCaseOperation1WithContext(ctx aws.Context, input *InputService13TestShapeInputService13TestCaseOperation1Input, opts ...request.Option) (*InputService13TestShapeInputService13TestCaseOperation1Output, error) {
	req, out := c.InputService13TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService13TestShapeInputService13TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Items []*string `location:"querystring" locationName:"item" type:"list"`
}

// SetItems sets the Items field's value.
func (s *InputService13TestShapeInputService13TestCaseOperation1Input) SetItems(v []*string) *InputService13TestShapeInputService13TestCaseOperation1Input {
	s.Items = v
	return s
}

type InputService13TestShapeInputService13TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService14ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService14ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService14ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService14ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService14ProtocolTest client from just a session.
//     svc := inputservice14protocoltest.New(mySession)
//
//     // Create a InputService14ProtocolTest client with additional configuration
//     svc := inputservice14protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService14ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService14ProtocolTest {
	c := p.ClientConfig("inputservice14protocoltest", cfgs...)
	return newInputService14ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService14ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService14ProtocolTest {
	svc := &InputService14ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService14ProtocolTest",
				ServiceID:     "InputService14ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService14ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService14ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService14TestCaseOperation1 = "OperationName"

// InputService14TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService14TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService14TestCaseOperation1 for more information on using the InputService14TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService14TestCaseOperation1Request method.
//    req, resp := client.InputService14TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1Request(input *InputService14TestShapeInputService14TestCaseOperation1Input) (req *request.Request, output *InputService14TestShapeInputService14TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService14TestCaseOperation1,
		HTTPMethod: "GET",
		HTTPPath:   "/2014-01-01/jobsByPipeline/{PipelineId}",
	}

	if input == nil {
		input = &InputService14TestShapeInputService14TestCaseOperation1Input{}
	}

	output = &InputService14TestShapeInputService14TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService14TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService14TestCaseOperation1 for usage and error information.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1(input *InputService14TestShapeInputService14TestCaseOperation1Input) (*InputService14TestShapeInputService14TestCaseOperation1Output, error) {
	req, out := c.InputService14TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService14TestCaseOperation1WithContext is the same as InputService14TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService14TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService14ProtocolTest) InputService14TestCaseOperation1WithContext(ctx aws.Context, input *InputService14TestShapeInputService14TestCaseOperation1Input, opts ...request.Option) (*InputService14TestShapeInputService14TestCaseOperation1Output, error) {
	req, out := c.InputService14TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService14TestShapeInputService14TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	// PipelineId is a required field
	PipelineId *string `location:"uri" type:"string" required:"true"`

	QueryDoc map[string]*string `location:"querystring" type:"map"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService14TestShapeInputService14TestCaseOperation1Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService14TestShapeInputService14TestCaseOperation1Input"}
	if s.PipelineId == nil {
		invalidParams.Add(request.NewErrParamRequired("PipelineId"))
	}
	if s.PipelineId != nil && len(*s.PipelineId) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("PipelineId", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetPipelineId sets the PipelineId field's value.
func (s *InputService14TestShapeInputService14TestCaseOperation1Input) SetPipelineId(v string) *InputService14TestShapeInputService14TestCaseOperation1Input {
	s.PipelineId = &v
	return s
}

// SetQueryDoc sets the QueryDoc field's value.
func (s *InputService14TestShapeInputService14TestCaseOperation1Input) SetQueryDoc(v map[string]*string) *InputService14TestShapeInputService14TestCaseOperation1Input {
	s.QueryDoc = v
	return s
}

type InputService14TestShapeInputService14TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService15ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService15ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService15ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService15ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService15ProtocolTest client from just a session.
//     svc := inputservice15protocoltest.New(mySession)
//
//     // Create a InputService15ProtocolTest client with additional configuration
//     svc := inputservice15protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService15ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService15ProtocolTest {
	c := p.ClientConfig("inputservice15protocoltest", cfgs...)
	return newInputService15ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService15ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService15ProtocolTest {
	svc := &InputService15ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService15ProtocolTest",
				ServiceID:     "InputService15ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService15ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService15ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService15TestCaseOperation1 = "OperationName"

// InputService15TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService15TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService15TestCaseOperation1 for more information on using the InputService15TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService15TestCaseOperation1Request method.
//    req, resp := client.InputService15TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1Request(input *InputService15TestShapeInputService15TestCaseOperation1Input) (req *request.Request, output *InputService15TestShapeInputService15TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService15TestCaseOperation1,
		HTTPMethod: "GET",
		HTTPPath:   "/2014-01-01/jobsByPipeline/{PipelineId}",
	}

	if input == nil {
		input = &InputService15TestShapeInputService15TestCaseOperation1Input{}
	}

	output = &InputService15TestShapeInputService15TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService15TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService15TestCaseOperation1 for usage and error information.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1(input *InputService15TestShapeInputService15TestCaseOperation1Input) (*InputService15TestShapeInputService15TestCaseOperation1Output, error) {
	req, out := c.InputService15TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService15TestCaseOperation1WithContext is the same as InputService15TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService15TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService15ProtocolTest) InputService15TestCaseOperation1WithContext(ctx aws.Context, input *InputService15TestShapeInputService15TestCaseOperation1Input, opts ...request.Option) (*InputService15TestShapeInputService15TestCaseOperation1Output, error) {
	req, out := c.InputService15TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService15TestShapeInputService15TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	// PipelineId is a required field
	PipelineId *string `location:"uri" type:"string" required:"true"`

	QueryDoc map[string][]*string `location:"querystring" type:"map"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService15TestShapeInputService15TestCaseOperation1Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService15TestShapeInputService15TestCaseOperation1Input"}
	if s.PipelineId == nil {
		invalidParams.Add(request.NewErrParamRequired("PipelineId"))
	}
	if s.PipelineId != nil && len(*s.PipelineId) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("PipelineId", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetPipelineId sets the PipelineId field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation1Input) SetPipelineId(v string) *InputService15TestShapeInputService15TestCaseOperation1Input {
	s.PipelineId = &v
	return s
}

// SetQueryDoc sets the QueryDoc field's value.
func (s *InputService15TestShapeInputService15TestCaseOperation1Input) SetQueryDoc(v map[string][]*string) *InputService15TestShapeInputService15TestCaseOperation1Input {
	s.QueryDoc = v
	return s
}

type InputService15TestShapeInputService15TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService16ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService16ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService16ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService16ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService16ProtocolTest client from just a session.
//     svc := inputservice16protocoltest.New(mySession)
//
//     // Create a InputService16ProtocolTest client with additional configuration
//     svc := inputservice16protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService16ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService16ProtocolTest {
	c := p.ClientConfig("inputservice16protocoltest", cfgs...)
	return newInputService16ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService16ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService16ProtocolTest {
	svc := &InputService16ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService16ProtocolTest",
				ServiceID:     "InputService16ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService16ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService16ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService16TestCaseOperation1 = "OperationName"

// InputService16TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService16TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService16TestCaseOperation1 for more information on using the InputService16TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService16TestCaseOperation1Request method.
//    req, resp := client.InputService16TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1Request(input *InputService16TestShapeInputService16TestCaseOperation1Input) (req *request.Request, output *InputService16TestShapeInputService16TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService16TestCaseOperation1,
		HTTPMethod: "GET",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService16TestShapeInputService16TestCaseOperation1Input{}
	}

	output = &InputService16TestShapeInputService16TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService16TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService16TestCaseOperation1 for usage and error information.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1(input *InputService16TestShapeInputService16TestCaseOperation1Input) (*InputService16TestShapeInputService16TestCaseOperation1Output, error) {
	req, out := c.InputService16TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService16TestCaseOperation1WithContext is the same as InputService16TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService16TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation1WithContext(ctx aws.Context, input *InputService16TestShapeInputService16TestCaseOperation1Input, opts ...request.Option) (*InputService16TestShapeInputService16TestCaseOperation1Output, error) {
	req, out := c.InputService16TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService16TestCaseOperation2 = "OperationName"

// InputService16TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService16TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService16TestCaseOperation2 for more information on using the InputService16TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService16TestCaseOperation2Request method.
//    req, resp := client.InputService16TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2Request(input *InputService16TestShapeInputService16TestCaseOperation2Input) (req *request.Request, output *InputService16TestShapeInputService16TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService16TestCaseOperation2,
		HTTPMethod: "GET",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService16TestShapeInputService16TestCaseOperation2Input{}
	}

	output = &InputService16TestShapeInputService16TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService16TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService16TestCaseOperation2 for usage and error information.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2(input *InputService16TestShapeInputService16TestCaseOperation2Input) (*InputService16TestShapeInputService16TestCaseOperation2Output, error) {
	req, out := c.InputService16TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService16TestCaseOperation2WithContext is the same as InputService16TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService16TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService16ProtocolTest) InputService16TestCaseOperation2WithContext(ctx aws.Context, input *InputService16TestShapeInputService16TestCaseOperation2Input, opts ...request.Option) (*InputService16TestShapeInputService16TestCaseOperation2Output, error) {
	req, out := c.InputService16TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService16TestShapeInputService16TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	BoolQuery *bool `location:"querystring" locationName:"bool-query" type:"boolean"`
}

// SetBoolQuery sets the BoolQuery field's value.
func (s *InputService16TestShapeInputService16TestCaseOperation1Input) SetBoolQuery(v bool) *InputService16TestShapeInputService16TestCaseOperation1Input {
	s.BoolQuery = &v
	return s
}

type InputService16TestShapeInputService16TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService16TestShapeInputService16TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	BoolQuery *bool `location:"querystring" locationName:"bool-query" type:"boolean"`
}

// SetBoolQuery sets the BoolQuery field's value.
func (s *InputService16TestShapeInputService16TestCaseOperation2Input) SetBoolQuery(v bool) *InputService16TestShapeInputService16TestCaseOperation2Input {
	s.BoolQuery = &v
	return s
}

type InputService16TestShapeInputService16TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService17ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService17ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService17ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService17ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService17ProtocolTest client from just a session.
//     svc := inputservice17protocoltest.New(mySession)
//
//     // Create a InputService17ProtocolTest client with additional configuration
//     svc := inputservice17protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService17ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService17ProtocolTest {
	c := p.ClientConfig("inputservice17protocoltest", cfgs...)
	return newInputService17ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService17ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService17ProtocolTest {
	svc := &InputService17ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService17ProtocolTest",
				ServiceID:     "InputService17ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService17ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService17ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService17TestCaseOperation1 = "OperationName"

// InputService17TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService17TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService17TestCaseOperation1 for more information on using the InputService17TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService17TestCaseOperation1Request method.
//    req, resp := client.InputService17TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService17ProtocolTest) InputService17TestCaseOperation1Request(input *InputService17TestShapeInputService17TestCaseOperation1Input) (req *request.Request, output *InputService17TestShapeInputService17TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService17TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService17TestShapeInputService17TestCaseOperation1Input{}
	}

	output = &InputService17TestShapeInputService17TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService17TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService17TestCaseOperation1 for usage and error information.
func (c *InputService17ProtocolTest) InputService17TestCaseOperation1(input *InputService17TestShapeInputService17TestCaseOperation1Input) (*InputService17TestShapeInputService17TestCaseOperation1Output, error) {
	req, out := c.InputService17TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService17TestCaseOperation1WithContext is the same as InputService17TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService17TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService17ProtocolTest) InputService17TestCaseOperation1WithContext(ctx aws.Context, input *InputService17TestShapeInputService17TestCaseOperation1Input, opts ...request.Option) (*InputService17TestShapeInputService17TestCaseOperation1Output, error) {
	req, out := c.InputService17TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService17TestShapeInputService17TestCaseOperation1Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo *string `locationName:"foo" type:"string"`
}

// SetFoo sets the Foo field's value.
func (s *InputService17TestShapeInputService17TestCaseOperation1Input) SetFoo(v string) *InputService17TestShapeInputService17TestCaseOperation1Input {
	s.Foo = &v
	return s
}

type InputService17TestShapeInputService17TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService18ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService18ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService18ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService18ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService18ProtocolTest client from just a session.
//     svc := inputservice18protocoltest.New(mySession)
//
//     // Create a InputService18ProtocolTest client with additional configuration
//     svc := inputservice18protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService18ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService18ProtocolTest {
	c := p.ClientConfig("inputservice18protocoltest", cfgs...)
	return newInputService18ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService18ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService18ProtocolTest {
	svc := &InputService18ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService18ProtocolTest",
				ServiceID:     "InputService18ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService18ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService18ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService18TestCaseOperation1 = "OperationName"

// InputService18TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService18TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService18TestCaseOperation1 for more information on using the InputService18TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService18TestCaseOperation1Request method.
//    req, resp := client.InputService18TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService18ProtocolTest) InputService18TestCaseOperation1Request(input *InputService18TestShapeInputService18TestCaseOperation1Input) (req *request.Request, output *InputService18TestShapeInputService18TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService18TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService18TestShapeInputService18TestCaseOperation1Input{}
	}

	output = &InputService18TestShapeInputService18TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService18TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService18TestCaseOperation1 for usage and error information.
func (c *InputService18ProtocolTest) InputService18TestCaseOperation1(input *InputService18TestShapeInputService18TestCaseOperation1Input) (*InputService18TestShapeInputService18TestCaseOperation1Output, error) {
	req, out := c.InputService18TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService18TestCaseOperation1WithContext is the same as InputService18TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService18TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService18ProtocolTest) InputService18TestCaseOperation1WithContext(ctx aws.Context, input *InputService18TestShapeInputService18TestCaseOperation1Input, opts ...request.Option) (*InputService18TestShapeInputService18TestCaseOperation1Output, error) {
	req, out := c.InputService18TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService18TestCaseOperation2 = "OperationName"

// InputService18TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService18TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService18TestCaseOperation2 for more information on using the InputService18TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService18TestCaseOperation2Request method.
//    req, resp := client.InputService18TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService18ProtocolTest) InputService18TestCaseOperation2Request(input *InputService18TestShapeInputService18TestCaseOperation2Input) (req *request.Request, output *InputService18TestShapeInputService18TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService18TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService18TestShapeInputService18TestCaseOperation2Input{}
	}

	output = &InputService18TestShapeInputService18TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService18TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService18TestCaseOperation2 for usage and error information.
func (c *InputService18ProtocolTest) InputService18TestCaseOperation2(input *InputService18TestShapeInputService18TestCaseOperation2Input) (*InputService18TestShapeInputService18TestCaseOperation2Output, error) {
	req, out := c.InputService18TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService18TestCaseOperation2WithContext is the same as InputService18TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService18TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService18ProtocolTest) InputService18TestCaseOperation2WithContext(ctx aws.Context, input *InputService18TestShapeInputService18TestCaseOperation2Input, opts ...request.Option) (*InputService18TestShapeInputService18TestCaseOperation2Output, error) {
	req, out := c.InputService18TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService18TestShapeInputService18TestCaseOperation1Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo []byte `locationName:"foo" type:"blob"`
}

// SetFoo sets the Foo field's value.
func (s *InputService18TestShapeInputService18TestCaseOperation1Input) SetFoo(v []byte) *InputService18TestShapeInputService18TestCaseOperation1Input {
	s.Foo = v
	return s
}

type InputService18TestShapeInputService18TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService18TestShapeInputService18TestCaseOperation2Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo []byte `locationName:"foo" type:"blob"`
}

// SetFoo sets the Foo field's value.
func (s *InputService18TestShapeInputService18TestCaseOperation2Input) SetFoo(v []byte) *InputService18TestShapeInputService18TestCaseOperation2Input {
	s.Foo = v
	return s
}

type InputService18TestShapeInputService18TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService19ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService19ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService19ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService19ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService19ProtocolTest client from just a session.
//     svc := inputservice19protocoltest.New(mySession)
//
//     // Create a InputService19ProtocolTest client with additional configuration
//     svc := inputservice19protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService19ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService19ProtocolTest {
	c := p.ClientConfig("inputservice19protocoltest", cfgs...)
	return newInputService19ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService19ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService19ProtocolTest {
	svc := &InputService19ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService19ProtocolTest",
				ServiceID:     "InputService19ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService19ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService19ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService19TestCaseOperation1 = "OperationName"

// InputService19TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService19TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService19TestCaseOperation1 for more information on using the InputService19TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService19TestCaseOperation1Request method.
//    req, resp := client.InputService19TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService19ProtocolTest) InputService19TestCaseOperation1Request(input *InputService19TestShapeInputService19TestCaseOperation1Input) (req *request.Request, output *InputService19TestShapeInputService19TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService19TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService19TestShapeInputService19TestCaseOperation1Input{}
	}

	output = &InputService19TestShapeInputService19TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService19TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService19TestCaseOperation1 for usage and error information.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation1(input *InputService19TestShapeInputService19TestCaseOperation1Input) (*InputService19TestShapeInputService19TestCaseOperation1Output, error) {
	req, out := c.InputService19TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService19TestCaseOperation1WithContext is the same as InputService19TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService19TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation1WithContext(ctx aws.Context, input *InputService19TestShapeInputService19TestCaseOperation1Input, opts ...request.Option) (*InputService19TestShapeInputService19TestCaseOperation1Output, error) {
	req, out := c.InputService19TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService19TestCaseOperation2 = "OperationName"

// InputService19TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService19TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService19TestCaseOperation2 for more information on using the InputService19TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService19TestCaseOperation2Request method.
//    req, resp := client.InputService19TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService19ProtocolTest) InputService19TestCaseOperation2Request(input *InputService19TestShapeInputService19TestCaseOperation2Input) (req *request.Request, output *InputService19TestShapeInputService19TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService19TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService19TestShapeInputService19TestCaseOperation2Input{}
	}

	output = &InputService19TestShapeInputService19TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService19TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService19TestCaseOperation2 for usage and error information.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation2(input *InputService19TestShapeInputService19TestCaseOperation2Input) (*InputService19TestShapeInputService19TestCaseOperation2Output, error) {
	req, out := c.InputService19TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService19TestCaseOperation2WithContext is the same as InputService19TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService19TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation2WithContext(ctx aws.Context, input *InputService19TestShapeInputService19TestCaseOperation2Input, opts ...request.Option) (*InputService19TestShapeInputService19TestCaseOperation2Output, error) {
	req, out := c.InputService19TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService19TestCaseOperation3 = "OperationName"

// InputService19TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService19TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService19TestCaseOperation3 for more information on using the InputService19TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService19TestCaseOperation3Request method.
//    req, resp := client.InputService19TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService19ProtocolTest) InputService19TestCaseOperation3Request(input *InputService19TestShapeInputService19TestCaseOperation3Input) (req *request.Request, output *InputService19TestShapeInputService19TestCaseOperation3Output) {
	op := &request.Operation{
		Name:       opInputService19TestCaseOperation3,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService19TestShapeInputService19TestCaseOperation3Input{}
	}

	output = &InputService19TestShapeInputService19TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService19TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService19TestCaseOperation3 for usage and error information.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation3(input *InputService19TestShapeInputService19TestCaseOperation3Input) (*InputService19TestShapeInputService19TestCaseOperation3Output, error) {
	req, out := c.InputService19TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService19TestCaseOperation3WithContext is the same as InputService19TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService19TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation3WithContext(ctx aws.Context, input *InputService19TestShapeInputService19TestCaseOperation3Input, opts ...request.Option) (*InputService19TestShapeInputService19TestCaseOperation3Output, error) {
	req, out := c.InputService19TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService19TestCaseOperation4 = "OperationName"

// InputService19TestCaseOperation4Request generates a "aws/request.Request" representing the
// client's request for the InputService19TestCaseOperation4 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService19TestCaseOperation4 for more information on using the InputService19TestCaseOperation4
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService19TestCaseOperation4Request method.
//    req, resp := client.InputService19TestCaseOperation4Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService19ProtocolTest) InputService19TestCaseOperation4Request(input *InputService19TestShapeInputService19TestCaseOperation4Input) (req *request.Request, output *InputService19TestShapeInputService19TestCaseOperation4Output) {
	op := &request.Operation{
		Name:       opInputService19TestCaseOperation4,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService19TestShapeInputService19TestCaseOperation4Input{}
	}

	output = &InputService19TestShapeInputService19TestCaseOperation4Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService19TestCaseOperation4 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService19TestCaseOperation4 for usage and error information.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation4(input *InputService19TestShapeInputService19TestCaseOperation4Input) (*InputService19TestShapeInputService19TestCaseOperation4Output, error) {
	req, out := c.InputService19TestCaseOperation4Request(input)
	return out, req.Send()
}

// InputService19TestCaseOperation4WithContext is the same as InputService19TestCaseOperation4 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService19TestCaseOperation4 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService19ProtocolTest) InputService19TestCaseOperation4WithContext(ctx aws.Context, input *InputService19TestShapeInputService19TestCaseOperation4Input, opts ...request.Option) (*InputService19TestShapeInputService19TestCaseOperation4Output, error) {
	req, out := c.InputService19TestCaseOperation4Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService19TestShapeFooShape struct {
	_ struct{} `locationName:"foo" type:"structure"`

	Baz *string `locationName:"baz" type:"string"`
}

// SetBaz sets the Baz field's value.
func (s *InputService19TestShapeFooShape) SetBaz(v string) *InputService19TestShapeFooShape {
	s.Baz = &v
	return s
}

type InputService19TestShapeInputService19TestCaseOperation1Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo *InputService19TestShapeFooShape `locationName:"foo" type:"structure"`
}

// SetFoo sets the Foo field's value.
func (s *InputService19TestShapeInputService19TestCaseOperation1Input) SetFoo(v *InputService19TestShapeFooShape) *InputService19TestShapeInputService19TestCaseOperation1Input {
	s.Foo = v
	return s
}

type InputService19TestShapeInputService19TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService19TestShapeInputService19TestCaseOperation2Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo *InputService19TestShapeFooShape `locationName:"foo" type:"structure"`
}

// SetFoo sets the Foo field's value.
func (s *InputService19TestShapeInputService19TestCaseOperation2Input) SetFoo(v *InputService19TestShapeFooShape) *InputService19TestShapeInputService19TestCaseOperation2Input {
	s.Foo = v
	return s
}

type InputService19TestShapeInputService19TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService19TestShapeInputService19TestCaseOperation3Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo *InputService19TestShapeFooShape `locationName:"foo" type:"structure"`
}

// SetFoo sets the Foo field's value.
func (s *InputService19TestShapeInputService19TestCaseOperation3Input) SetFoo(v *InputService19TestShapeFooShape) *InputService19TestShapeInputService19TestCaseOperation3Input {
	s.Foo = v
	return s
}

type InputService19TestShapeInputService19TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

type InputService19TestShapeInputService19TestCaseOperation4Input struct {
	_ struct{} `type:"structure" payload:"Foo"`

	Foo *InputService19TestShapeFooShape `locationName:"foo" type:"structure"`
}

// SetFoo sets the Foo field's value.
func (s *InputService19TestShapeInputService19TestCaseOperation4Input) SetFoo(v *InputService19TestShapeFooShape) *InputService19TestShapeInputService19TestCaseOperation4Input {
	s.Foo = v
	return s
}

type InputService19TestShapeInputService19TestCaseOperation4Output struct {
	_ struct{} `type:"structure"`
}

// InputService20ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService20ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService20ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService20ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService20ProtocolTest client from just a session.
//     svc := inputservice20protocoltest.New(mySession)
//
//     // Create a InputService20ProtocolTest client with additional configuration
//     svc := inputservice20protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService20ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService20ProtocolTest {
	c := p.ClientConfig("inputservice20protocoltest", cfgs...)
	return newInputService20ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService20ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService20ProtocolTest {
	svc := &InputService20ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService20ProtocolTest",
				ServiceID:     "InputService20ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService20ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService20ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService20TestCaseOperation1 = "OperationName"

// InputService20TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService20TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService20TestCaseOperation1 for more information on using the InputService20TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService20TestCaseOperation1Request method.
//    req, resp := client.InputService20TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService20ProtocolTest) InputService20TestCaseOperation1Request(input *InputService20TestShapeInputService20TestCaseOperation1Input) (req *request.Request, output *InputService20TestShapeInputService20TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService20TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}

	if input == nil {
		input = &InputService20TestShapeInputService20TestCaseOperation1Input{}
	}

	output = &InputService20TestShapeInputService20TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService20TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService20TestCaseOperation1 for usage and error information.
func (c *InputService20ProtocolTest) InputService20TestCaseOperation1(input *InputService20TestShapeInputService20TestCaseOperation1Input) (*InputService20TestShapeInputService20TestCaseOperation1Output, error) {
	req, out := c.InputService20TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService20TestCaseOperation1WithContext is the same as InputService20TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService20TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService20ProtocolTest) InputService20TestCaseOperation1WithContext(ctx aws.Context, input *InputService20TestShapeInputService20TestCaseOperation1Input, opts ...request.Option) (*InputService20TestShapeInputService20TestCaseOperation1Output, error) {
	req, out := c.InputService20TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService20TestShapeGrant struct {
	_ struct{} `locationName:"Grant" type:"structure"`

	Grantee *InputService20TestShapeGrantee `type:"structure" xmlPrefix:"xsi" xmlURI:"http://www.w3.org/2001/XMLSchema-instance"`
}

// SetGrantee sets the Grantee field's value.
func (s *InputService20TestShapeGrant) SetGrantee(v *InputService20TestShapeGrantee) *InputService20TestShapeGrant {
	s.Grantee = v
	return s
}

type InputService20TestShapeGrantee struct {
	_ struct{} `type:"structure" xmlPrefix:"xsi" xmlURI:"http://www.w3.org/2001/XMLSchema-instance"`

	EmailAddress *string `type:"string"`

	Type *string `locationName:"xsi:type" type:"string" xmlAttribute:"true"`
}

// SetEmailAddress sets the EmailAddress field's value.
func (s *InputService20TestShapeGrantee) SetEmailAddress(v string) *InputService20TestShapeGrantee {
	s.EmailAddress = &v
	return s
}

// SetType sets the Type field's value.
func (s *InputService20TestShapeGrantee) SetType(v string) *InputService20TestShapeGrantee {
	s.Type = &v
	return s
}

type InputService20TestShapeInputService20TestCaseOperation1Input struct {
	_ struct{} `type:"structure" payload:"Grant"`

	Grant *InputService20TestShapeGrant `locationName:"Grant" type:"structure"`
}

// SetGrant sets the Grant field's value.
func (s *InputService20TestShapeInputService20TestCaseOperation1Input) SetGrant(v *InputService20TestShapeGrant) *InputService20TestShapeInputService20TestCaseOperation1Input {
	s.Grant = v
	return s
}

type InputService20TestShapeInputService20TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService21ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService21ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService21ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService21ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService21ProtocolTest client from just a session.
//     svc := inputservice21protocoltest.New(mySession)
//
//     // Create a InputService21ProtocolTest client with additional configuration
//     svc := inputservice21protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService21ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService21ProtocolTest {
	c := p.ClientConfig("inputservice21protocoltest", cfgs...)
	return newInputService21ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService21ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService21ProtocolTest {
	svc := &InputService21ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService21ProtocolTest",
				ServiceID:     "InputService21ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService21ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService21ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService21TestCaseOperation1 = "OperationName"

// InputService21TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService21TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService21TestCaseOperation1 for more information on using the InputService21TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService21TestCaseOperation1Request method.
//    req, resp := client.InputService21TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService21ProtocolTest) InputService21TestCaseOperation1Request(input *InputService21TestShapeInputService21TestCaseOperation1Input) (req *request.Request, output *InputService21TestShapeInputService21TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService21TestCaseOperation1,
		HTTPMethod: "GET",
		HTTPPath:   "/{Bucket}/{Key+}",
	}

	if input == nil {
		input = &InputService21TestShapeInputService21TestCaseOperation1Input{}
	}

	output = &InputService21TestShapeInputService21TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService21TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService21TestCaseOperation1 for usage and error information.
func (c *InputService21ProtocolTest) InputService21TestCaseOperation1(input *InputService21TestShapeInputService21TestCaseOperation1Input) (*InputService21TestShapeInputService21TestCaseOperation1Output, error) {
	req, out := c.InputService21TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService21TestCaseOperation1WithContext is the same as InputService21TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService21TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService21ProtocolTest) InputService21TestCaseOperation1WithContext(ctx aws.Context, input *InputService21TestShapeInputService21TestCaseOperation1Input, opts ...request.Option) (*InputService21TestShapeInputService21TestCaseOperation1Output, error) {
	req, out := c.InputService21TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService21TestShapeInputService21TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	// Bucket is a required field
	Bucket *string `location:"uri" type:"string" required:"true"`

	// Key is a required field
	Key *string `location:"uri" type:"string" required:"true"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService21TestShapeInputService21TestCaseOperation1Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService21TestShapeInputService21TestCaseOperation1Input"}
	if s.Bucket == nil {
		invalidParams.Add(request.NewErrParamRequired("Bucket"))
	}
	if s.Bucket != nil && len(*s.Bucket) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("Bucket", 1))
	}
	if s.Key == nil {
		invalidParams.Add(request.NewErrParamRequired("Key"))
	}
	if s.Key != nil && len(*s.Key) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("Key", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetBucket sets the Bucket field's value.
func (s *InputService21TestShapeInputService21TestCaseOperation1Input) SetBucket(v string) *InputService21TestShapeInputService21TestCaseOperation1Input {
	s.Bucket = &v
	return s
}

// SetKey sets the Key field's value.
func (s *InputService21TestShapeInputService21TestCaseOperation1Input) SetKey(v string) *InputService21TestShapeInputService21TestCaseOperation1Input {
	s.Key = &v
	return s
}

type InputService21TestShapeInputService21TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

// InputService22ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService22ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService22ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService22ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService22ProtocolTest client from just a session.
//     svc := inputservice22protocoltest.New(mySession)
//
//     // Create a InputService22ProtocolTest client with additional configuration
//     svc := inputservice22protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService22ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService22ProtocolTest {
	c := p.ClientConfig("inputservice22protocoltest", cfgs...)
	return newInputService22ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService22ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService22ProtocolTest {
	svc := &InputService22ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService22ProtocolTest",
				ServiceID:     "InputService22ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService22ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService22ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService22TestCaseOperation1 = "OperationName"

// InputService22TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService22TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService22TestCaseOperation1 for more information on using the InputService22TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService22TestCaseOperation1Request method.
//    req, resp := client.InputService22TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService22ProtocolTest) InputService22TestCaseOperation1Request(input *InputService22TestShapeInputService22TestCaseOperation1Input) (req *request.Request, output *InputService22TestShapeInputService22TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService22TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService22TestShapeInputService22TestCaseOperation1Input{}
	}

	output = &InputService22TestShapeInputService22TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService22TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService22TestCaseOperation1 for usage and error information.
func (c *InputService22ProtocolTest) InputService22TestCaseOperation1(input *InputService22TestShapeInputService22TestCaseOperation1Input) (*InputService22TestShapeInputService22TestCaseOperation1Output, error) {
	req, out := c.InputService22TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService22TestCaseOperation1WithContext is the same as InputService22TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService22TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService22ProtocolTest) InputService22TestCaseOperation1WithContext(ctx aws.Context, input *InputService22TestShapeInputService22TestCaseOperation1Input, opts ...request.Option) (*InputService22TestShapeInputService22TestCaseOperation1Output, error) {
	req, out := c.InputService22TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService22TestCaseOperation2 = "OperationName"

// InputService22TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService22TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService22TestCaseOperation2 for more information on using the InputService22TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService22TestCaseOperation2Request method.
//    req, resp := client.InputService22TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService22ProtocolTest) InputService22TestCaseOperation2Request(input *InputService22TestShapeInputService22TestCaseOperation2Input) (req *request.Request, output *InputService22TestShapeInputService22TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService22TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/path?abc=mno",
	}

	if input == nil {
		input = &InputService22TestShapeInputService22TestCaseOperation2Input{}
	}

	output = &InputService22TestShapeInputService22TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService22TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService22TestCaseOperation2 for usage and error information.
func (c *InputService22ProtocolTest) InputService22TestCaseOperation2(input *InputService22TestShapeInputService22TestCaseOperation2Input) (*InputService22TestShapeInputService22TestCaseOperation2Output, error) {
	req, out := c.InputService22TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService22TestCaseOperation2WithContext is the same as InputService22TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService22TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService22ProtocolTest) InputService22TestCaseOperation2WithContext(ctx aws.Context, input *InputService22TestShapeInputService22TestCaseOperation2Input, opts ...request.Option) (*InputService22TestShapeInputService22TestCaseOperation2Output, error) {
	req, out := c.InputService22TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService22TestShapeInputService22TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Foo *string `location:"querystring" locationName:"param-name" type:"string"`
}

// SetFoo sets the Foo field's value.
func (s *InputService22TestShapeInputService22TestCaseOperation1Input) SetFoo(v string) *InputService22TestShapeInputService22TestCaseOperation1Input {
	s.Foo = &v
	return s
}

type InputService22TestShapeInputService22TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService22TestShapeInputService22TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	Foo *string `location:"querystring" locationName:"param-name" type:"string"`
}

// SetFoo sets the Foo field's value.
func (s *InputService22TestShapeInputService22TestCaseOperation2Input) SetFoo(v string) *InputService22TestShapeInputService22TestCaseOperation2Input {
	s.Foo = &v
	return s
}

type InputService22TestShapeInputService22TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService23ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService23ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService23ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService23ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService23ProtocolTest client from just a session.
//     svc := inputservice23protocoltest.New(mySession)
//
//     // Create a InputService23ProtocolTest client with additional configuration
//     svc := inputservice23protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService23ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService23ProtocolTest {
	c := p.ClientConfig("inputservice23protocoltest", cfgs...)
	return newInputService23ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService23ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService23ProtocolTest {
	svc := &InputService23ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService23ProtocolTest",
				ServiceID:     "InputService23ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService23ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService23ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService23TestCaseOperation1 = "OperationName"

// InputService23TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation1 for more information on using the InputService23TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation1Request method.
//    req, resp := client.InputService23TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation1Request(input *InputService23TestShapeInputService23TestCaseOperation1Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation1Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation1 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation1(input *InputService23TestShapeInputService23TestCaseOperation1Input) (*InputService23TestShapeInputService23TestCaseOperation1Output, error) {
	req, out := c.InputService23TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation1WithContext is the same as InputService23TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation1WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation1Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation1Output, error) {
	req, out := c.InputService23TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService23TestCaseOperation2 = "OperationName"

// InputService23TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation2 for more information on using the InputService23TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation2Request method.
//    req, resp := client.InputService23TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation2Request(input *InputService23TestShapeInputService23TestCaseOperation2Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation2Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation2 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation2(input *InputService23TestShapeInputService23TestCaseOperation2Input) (*InputService23TestShapeInputService23TestCaseOperation2Output, error) {
	req, out := c.InputService23TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation2WithContext is the same as InputService23TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation2WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation2Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation2Output, error) {
	req, out := c.InputService23TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService23TestCaseOperation3 = "OperationName"

// InputService23TestCaseOperation3Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation3 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation3 for more information on using the InputService23TestCaseOperation3
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation3Request method.
//    req, resp := client.InputService23TestCaseOperation3Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation3Request(input *InputService23TestShapeInputService23TestCaseOperation3Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation3Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation3,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation3Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation3Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation3 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation3 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation3(input *InputService23TestShapeInputService23TestCaseOperation3Input) (*InputService23TestShapeInputService23TestCaseOperation3Output, error) {
	req, out := c.InputService23TestCaseOperation3Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation3WithContext is the same as InputService23TestCaseOperation3 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation3 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation3WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation3Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation3Output, error) {
	req, out := c.InputService23TestCaseOperation3Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService23TestCaseOperation4 = "OperationName"

// InputService23TestCaseOperation4Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation4 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation4 for more information on using the InputService23TestCaseOperation4
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation4Request method.
//    req, resp := client.InputService23TestCaseOperation4Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation4Request(input *InputService23TestShapeInputService23TestCaseOperation4Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation4Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation4,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation4Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation4Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation4 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation4 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation4(input *InputService23TestShapeInputService23TestCaseOperation4Input) (*InputService23TestShapeInputService23TestCaseOperation4Output, error) {
	req, out := c.InputService23TestCaseOperation4Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation4WithContext is the same as InputService23TestCaseOperation4 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation4 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation4WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation4Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation4Output, error) {
	req, out := c.InputService23TestCaseOperation4Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService23TestCaseOperation5 = "OperationName"

// InputService23TestCaseOperation5Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation5 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation5 for more information on using the InputService23TestCaseOperation5
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation5Request method.
//    req, resp := client.InputService23TestCaseOperation5Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation5Request(input *InputService23TestShapeInputService23TestCaseOperation5Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation5Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation5,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation5Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation5Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation5 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation5 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation5(input *InputService23TestShapeInputService23TestCaseOperation5Input) (*InputService23TestShapeInputService23TestCaseOperation5Output, error) {
	req, out := c.InputService23TestCaseOperation5Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation5WithContext is the same as InputService23TestCaseOperation5 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation5 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation5WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation5Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation5Output, error) {
	req, out := c.InputService23TestCaseOperation5Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService23TestCaseOperation6 = "OperationName"

// InputService23TestCaseOperation6Request generates a "aws/request.Request" representing the
// client's request for the InputService23TestCaseOperation6 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService23TestCaseOperation6 for more information on using the InputService23TestCaseOperation6
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService23TestCaseOperation6Request method.
//    req, resp := client.InputService23TestCaseOperation6Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService23ProtocolTest) InputService23TestCaseOperation6Request(input *InputService23TestShapeInputService23TestCaseOperation6Input) (req *request.Request, output *InputService23TestShapeInputService23TestCaseOperation6Output) {
	op := &request.Operation{
		Name:       opInputService23TestCaseOperation6,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService23TestShapeInputService23TestCaseOperation6Input{}
	}

	output = &InputService23TestShapeInputService23TestCaseOperation6Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService23TestCaseOperation6 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService23TestCaseOperation6 for usage and error information.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation6(input *InputService23TestShapeInputService23TestCaseOperation6Input) (*InputService23TestShapeInputService23TestCaseOperation6Output, error) {
	req, out := c.InputService23TestCaseOperation6Request(input)
	return out, req.Send()
}

// InputService23TestCaseOperation6WithContext is the same as InputService23TestCaseOperation6 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService23TestCaseOperation6 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService23ProtocolTest) InputService23TestCaseOperation6WithContext(ctx aws.Context, input *InputService23TestShapeInputService23TestCaseOperation6Input, opts ...request.Option) (*InputService23TestShapeInputService23TestCaseOperation6Output, error) {
	req, out := c.InputService23TestCaseOperation6Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService23TestShapeInputService23TestCaseOperation1Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation1Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation1Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeInputService23TestCaseOperation2Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation2Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation2Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeInputService23TestCaseOperation3Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation3Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation3Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation3Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeInputService23TestCaseOperation4Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation4Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation4Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation4Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeInputService23TestCaseOperation5Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation5Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation5Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation5Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeInputService23TestCaseOperation6Input struct {
	_ struct{} `locationName:"OperationRequest" type:"structure" xmlURI:"https://foo/"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeInputService23TestCaseOperation6Input) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeInputService23TestCaseOperation6Input {
	s.RecursiveStruct = v
	return s
}

type InputService23TestShapeInputService23TestCaseOperation6Output struct {
	_ struct{} `type:"structure"`
}

type InputService23TestShapeRecursiveStructType struct {
	_ struct{} `type:"structure"`

	NoRecurse *string `type:"string"`

	RecursiveList []*InputService23TestShapeRecursiveStructType `type:"list"`

	RecursiveMap map[string]*InputService23TestShapeRecursiveStructType `type:"map"`

	RecursiveStruct *InputService23TestShapeRecursiveStructType `type:"structure"`
}

// SetNoRecurse sets the NoRecurse field's value.
func (s *InputService23TestShapeRecursiveStructType) SetNoRecurse(v string) *InputService23TestShapeRecursiveStructType {
	s.NoRecurse = &v
	return s
}

// SetRecursiveList sets the RecursiveList field's value.
func (s *InputService23TestShapeRecursiveStructType) SetRecursiveList(v []*InputService23TestShapeRecursiveStructType) *InputService23TestShapeRecursiveStructType {
	s.RecursiveList = v
	return s
}

// SetRecursiveMap sets the RecursiveMap field's value.
func (s *InputService23TestShapeRecursiveStructType) SetRecursiveMap(v map[string]*InputService23TestShapeRecursiveStructType) *InputService23TestShapeRecursiveStructType {
	s.RecursiveMap = v
	return s
}

// SetRecursiveStruct sets the RecursiveStruct field's value.
func (s *InputService23TestShapeRecursiveStructType) SetRecursiveStruct(v *InputService23TestShapeRecursiveStructType) *InputService23TestShapeRecursiveStructType {
	s.RecursiveStruct = v
	return s
}

// InputService24ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService24ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService24ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService24ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService24ProtocolTest client from just a session.
//     svc := inputservice24protocoltest.New(mySession)
//
//     // Create a InputService24ProtocolTest client with additional configuration
//     svc := inputservice24protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService24ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService24ProtocolTest {
	c := p.ClientConfig("inputservice24protocoltest", cfgs...)
	return newInputService24ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService24ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService24ProtocolTest {
	svc := &InputService24ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService24ProtocolTest",
				ServiceID:     "InputService24ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService24ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService24ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService24TestCaseOperation1 = "OperationName"

// InputService24TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService24TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService24TestCaseOperation1 for more information on using the InputService24TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService24TestCaseOperation1Request method.
//    req, resp := client.InputService24TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService24ProtocolTest) InputService24TestCaseOperation1Request(input *InputService24TestShapeInputService24TestCaseOperation1Input) (req *request.Request, output *InputService24TestShapeInputService24TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService24TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService24TestShapeInputService24TestCaseOperation1Input{}
	}

	output = &InputService24TestShapeInputService24TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService24TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService24TestCaseOperation1 for usage and error information.
func (c *InputService24ProtocolTest) InputService24TestCaseOperation1(input *InputService24TestShapeInputService24TestCaseOperation1Input) (*InputService24TestShapeInputService24TestCaseOperation1Output, error) {
	req, out := c.InputService24TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService24TestCaseOperation1WithContext is the same as InputService24TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService24TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService24ProtocolTest) InputService24TestCaseOperation1WithContext(ctx aws.Context, input *InputService24TestShapeInputService24TestCaseOperation1Input, opts ...request.Option) (*InputService24TestShapeInputService24TestCaseOperation1Output, error) {
	req, out := c.InputService24TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService24TestCaseOperation2 = "OperationName"

// InputService24TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService24TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService24TestCaseOperation2 for more information on using the InputService24TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService24TestCaseOperation2Request method.
//    req, resp := client.InputService24TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService24ProtocolTest) InputService24TestCaseOperation2Request(input *InputService24TestShapeInputService24TestCaseOperation2Input) (req *request.Request, output *InputService24TestShapeInputService24TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService24TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService24TestShapeInputService24TestCaseOperation2Input{}
	}

	output = &InputService24TestShapeInputService24TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService24TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService24TestCaseOperation2 for usage and error information.
func (c *InputService24ProtocolTest) InputService24TestCaseOperation2(input *InputService24TestShapeInputService24TestCaseOperation2Input) (*InputService24TestShapeInputService24TestCaseOperation2Output, error) {
	req, out := c.InputService24TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService24TestCaseOperation2WithContext is the same as InputService24TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService24TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService24ProtocolTest) InputService24TestCaseOperation2WithContext(ctx aws.Context, input *InputService24TestShapeInputService24TestCaseOperation2Input, opts ...request.Option) (*InputService24TestShapeInputService24TestCaseOperation2Output, error) {
	req, out := c.InputService24TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService24TestShapeInputService24TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	Token *string `type:"string" idempotencyToken:"true"`
}

// SetToken sets the Token field's value.
func (s *InputService24TestShapeInputService24TestCaseOperation1Input) SetToken(v string) *InputService24TestShapeInputService24TestCaseOperation1Input {
	s.Token = &v
	return s
}

type InputService24TestShapeInputService24TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService24TestShapeInputService24TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	Token *string `type:"string" idempotencyToken:"true"`
}

// SetToken sets the Token field's value.
func (s *InputService24TestShapeInputService24TestCaseOperation2Input) SetToken(v string) *InputService24TestShapeInputService24TestCaseOperation2Input {
	s.Token = &v
	return s
}

type InputService24TestShapeInputService24TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

// InputService25ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService25ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService25ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService25ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService25ProtocolTest client from just a session.
//     svc := inputservice25protocoltest.New(mySession)
//
//     // Create a InputService25ProtocolTest client with additional configuration
//     svc := inputservice25protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService25ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService25ProtocolTest {
	c := p.ClientConfig("inputservice25protocoltest", cfgs...)
	return newInputService25ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService25ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService25ProtocolTest {
	svc := &InputService25ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService25ProtocolTest",
				ServiceID:     "InputService25ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService25ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService25ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService25TestCaseOperation1 = "OperationName"

// InputService25TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService25TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService25TestCaseOperation1 for more information on using the InputService25TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService25TestCaseOperation1Request method.
//    req, resp := client.InputService25TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService25ProtocolTest) InputService25TestCaseOperation1Request(input *InputService25TestShapeInputService25TestCaseOperation1Input) (req *request.Request, output *InputService25TestShapeInputService25TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService25TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/Enum/{URIEnum}",
	}

	if input == nil {
		input = &InputService25TestShapeInputService25TestCaseOperation1Input{}
	}

	output = &InputService25TestShapeInputService25TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService25TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService25TestCaseOperation1 for usage and error information.
func (c *InputService25ProtocolTest) InputService25TestCaseOperation1(input *InputService25TestShapeInputService25TestCaseOperation1Input) (*InputService25TestShapeInputService25TestCaseOperation1Output, error) {
	req, out := c.InputService25TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService25TestCaseOperation1WithContext is the same as InputService25TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService25TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService25ProtocolTest) InputService25TestCaseOperation1WithContext(ctx aws.Context, input *InputService25TestShapeInputService25TestCaseOperation1Input, opts ...request.Option) (*InputService25TestShapeInputService25TestCaseOperation1Output, error) {
	req, out := c.InputService25TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService25TestCaseOperation2 = "OperationName"

// InputService25TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService25TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService25TestCaseOperation2 for more information on using the InputService25TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService25TestCaseOperation2Request method.
//    req, resp := client.InputService25TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService25ProtocolTest) InputService25TestCaseOperation2Request(input *InputService25TestShapeInputService25TestCaseOperation2Input) (req *request.Request, output *InputService25TestShapeInputService25TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService25TestCaseOperation2,
		HTTPMethod: "POST",
		HTTPPath:   "/Enum/{URIEnum}",
	}

	if input == nil {
		input = &InputService25TestShapeInputService25TestCaseOperation2Input{}
	}

	output = &InputService25TestShapeInputService25TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	return
}

// InputService25TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService25TestCaseOperation2 for usage and error information.
func (c *InputService25ProtocolTest) InputService25TestCaseOperation2(input *InputService25TestShapeInputService25TestCaseOperation2Input) (*InputService25TestShapeInputService25TestCaseOperation2Output, error) {
	req, out := c.InputService25TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService25TestCaseOperation2WithContext is the same as InputService25TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService25TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService25ProtocolTest) InputService25TestCaseOperation2WithContext(ctx aws.Context, input *InputService25TestShapeInputService25TestCaseOperation2Input, opts ...request.Option) (*InputService25TestShapeInputService25TestCaseOperation2Output, error) {
	req, out := c.InputService25TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService25TestShapeInputService25TestCaseOperation1Input struct {
	_ struct{} `type:"structure"`

	FooEnum *string `type:"string" enum:"InputService25TestShapeEnumType"`

	HeaderEnum *string `location:"header" locationName:"x-amz-enum" type:"string" enum:"InputService25TestShapeEnumType"`

	ListEnums []*string `type:"list"`

	// URIFooEnum is a required field
	URIFooEnum *string `location:"uri" locationName:"URIEnum" type:"string" required:"true" enum:"InputService25TestShapeEnumType"`

	URIListEnums []*string `location:"querystring" locationName:"ListEnums" type:"list"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService25TestShapeInputService25TestCaseOperation1Input"}
	if s.URIFooEnum == nil {
		invalidParams.Add(request.NewErrParamRequired("URIFooEnum"))
	}
	if s.URIFooEnum != nil && len(*s.URIFooEnum) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("URIFooEnum", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetFooEnum sets the FooEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) SetFooEnum(v string) *InputService25TestShapeInputService25TestCaseOperation1Input {
	s.FooEnum = &v
	return s
}

// SetHeaderEnum sets the HeaderEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) SetHeaderEnum(v string) *InputService25TestShapeInputService25TestCaseOperation1Input {
	s.HeaderEnum = &v
	return s
}

// SetListEnums sets the ListEnums field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) SetListEnums(v []*string) *InputService25TestShapeInputService25TestCaseOperation1Input {
	s.ListEnums = v
	return s
}

// SetURIFooEnum sets the URIFooEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) SetURIFooEnum(v string) *InputService25TestShapeInputService25TestCaseOperation1Input {
	s.URIFooEnum = &v
	return s
}

// SetURIListEnums sets the URIListEnums field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation1Input) SetURIListEnums(v []*string) *InputService25TestShapeInputService25TestCaseOperation1Input {
	s.URIListEnums = v
	return s
}

type InputService25TestShapeInputService25TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService25TestShapeInputService25TestCaseOperation2Input struct {
	_ struct{} `type:"structure"`

	FooEnum *string `type:"string" enum:"InputService25TestShapeEnumType"`

	HeaderEnum *string `location:"header" locationName:"x-amz-enum" type:"string" enum:"InputService25TestShapeEnumType"`

	ListEnums []*string `type:"list"`

	// URIFooEnum is a required field
	URIFooEnum *string `location:"uri" locationName:"URIEnum" type:"string" required:"true" enum:"InputService25TestShapeEnumType"`

	URIListEnums []*string `location:"querystring" locationName:"ListEnums" type:"list"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService25TestShapeInputService25TestCaseOperation2Input"}
	if s.URIFooEnum == nil {
		invalidParams.Add(request.NewErrParamRequired("URIFooEnum"))
	}
	if s.URIFooEnum != nil && len(*s.URIFooEnum) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("URIFooEnum", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetFooEnum sets the FooEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) SetFooEnum(v string) *InputService25TestShapeInputService25TestCaseOperation2Input {
	s.FooEnum = &v
	return s
}

// SetHeaderEnum sets the HeaderEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) SetHeaderEnum(v string) *InputService25TestShapeInputService25TestCaseOperation2Input {
	s.HeaderEnum = &v
	return s
}

// SetListEnums sets the ListEnums field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) SetListEnums(v []*string) *InputService25TestShapeInputService25TestCaseOperation2Input {
	s.ListEnums = v
	return s
}

// SetURIFooEnum sets the URIFooEnum field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) SetURIFooEnum(v string) *InputService25TestShapeInputService25TestCaseOperation2Input {
	s.URIFooEnum = &v
	return s
}

// SetURIListEnums sets the URIListEnums field's value.
func (s *InputService25TestShapeInputService25TestCaseOperation2Input) SetURIListEnums(v []*string) *InputService25TestShapeInputService25TestCaseOperation2Input {
	s.URIListEnums = v
	return s
}

type InputService25TestShapeInputService25TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

const (
	// EnumTypeFoo is a InputService25TestShapeEnumType enum value
	EnumTypeFoo = "foo"

	// EnumTypeBar is a InputService25TestShapeEnumType enum value
	EnumTypeBar = "bar"

	// EnumType0 is a InputService25TestShapeEnumType enum value
	EnumType0 = "0"

	// EnumType1 is a InputService25TestShapeEnumType enum value
	EnumType1 = "1"
)

// InputService26ProtocolTest provides the API operation methods for making requests to
// . See this package's package overview docs
// for details on the service.
//
// InputService26ProtocolTest methods are safe to use concurrently. It is not safe to
// modify mutate any of the struct's properties though.
type InputService26ProtocolTest struct {
	*client.Client
}

// New creates a new instance of the InputService26ProtocolTest client with a session.
// If additional configuration is needed for the client instance use the optional
// aws.Config parameter to add your extra config.
//
// Example:
//     // Create a InputService26ProtocolTest client from just a session.
//     svc := inputservice26protocoltest.New(mySession)
//
//     // Create a InputService26ProtocolTest client with additional configuration
//     svc := inputservice26protocoltest.New(mySession, aws.NewConfig().WithRegion("us-west-2"))
func NewInputService26ProtocolTest(p client.ConfigProvider, cfgs ...*aws.Config) *InputService26ProtocolTest {
	c := p.ClientConfig("inputservice26protocoltest", cfgs...)
	return newInputService26ProtocolTestClient(*c.Config, c.Handlers, c.Endpoint, c.SigningRegion, c.SigningName)
}

// newClient creates, initializes and returns a new service client instance.
func newInputService26ProtocolTestClient(cfg aws.Config, handlers request.Handlers, endpoint, signingRegion, signingName string) *InputService26ProtocolTest {
	svc := &InputService26ProtocolTest{
		Client: client.New(
			cfg,
			metadata.ClientInfo{
				ServiceName:   "InputService26ProtocolTest",
				ServiceID:     "InputService26ProtocolTest",
				SigningName:   signingName,
				SigningRegion: signingRegion,
				Endpoint:      endpoint,
				APIVersion:    "2014-01-01",
			},
			handlers,
		),
	}

	// Handlers
	svc.Handlers.Sign.PushBackNamed(v4.SignRequestHandler)
	svc.Handlers.Build.PushBackNamed(restxml.BuildHandler)
	svc.Handlers.Unmarshal.PushBackNamed(restxml.UnmarshalHandler)
	svc.Handlers.UnmarshalMeta.PushBackNamed(restxml.UnmarshalMetaHandler)
	svc.Handlers.UnmarshalError.PushBackNamed(restxml.UnmarshalErrorHandler)

	return svc
}

// newRequest creates a new request for a InputService26ProtocolTest operation and runs any
// custom request initialization.
func (c *InputService26ProtocolTest) newRequest(op *request.Operation, params, data interface{}) *request.Request {
	req := c.NewRequest(op, params, data)

	return req
}

const opInputService26TestCaseOperation1 = "StaticOp"

// InputService26TestCaseOperation1Request generates a "aws/request.Request" representing the
// client's request for the InputService26TestCaseOperation1 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService26TestCaseOperation1 for more information on using the InputService26TestCaseOperation1
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService26TestCaseOperation1Request method.
//    req, resp := client.InputService26TestCaseOperation1Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService26ProtocolTest) InputService26TestCaseOperation1Request(input *InputService26TestShapeInputService26TestCaseOperation1Input) (req *request.Request, output *InputService26TestShapeInputService26TestCaseOperation1Output) {
	op := &request.Operation{
		Name:       opInputService26TestCaseOperation1,
		HTTPMethod: "POST",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService26TestShapeInputService26TestCaseOperation1Input{}
	}

	output = &InputService26TestShapeInputService26TestCaseOperation1Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	req.Handlers.Build.PushBackNamed(protocol.NewHostPrefixHandler("data-", nil))
	req.Handlers.Build.PushBackNamed(protocol.ValidateEndpointHostHandler)
	return
}

// InputService26TestCaseOperation1 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService26TestCaseOperation1 for usage and error information.
func (c *InputService26ProtocolTest) InputService26TestCaseOperation1(input *InputService26TestShapeInputService26TestCaseOperation1Input) (*InputService26TestShapeInputService26TestCaseOperation1Output, error) {
	req, out := c.InputService26TestCaseOperation1Request(input)
	return out, req.Send()
}

// InputService26TestCaseOperation1WithContext is the same as InputService26TestCaseOperation1 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService26TestCaseOperation1 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService26ProtocolTest) InputService26TestCaseOperation1WithContext(ctx aws.Context, input *InputService26TestShapeInputService26TestCaseOperation1Input, opts ...request.Option) (*InputService26TestShapeInputService26TestCaseOperation1Output, error) {
	req, out := c.InputService26TestCaseOperation1Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

const opInputService26TestCaseOperation2 = "MemberRefOp"

// InputService26TestCaseOperation2Request generates a "aws/request.Request" representing the
// client's request for the InputService26TestCaseOperation2 operation. The "output" return
// value will be populated with the request's response once the request completes
// successfully.
//
// Use "Send" method on the returned Request to send the API call to the service.
// the "output" return value is not valid until after Send returns without error.
//
// See InputService26TestCaseOperation2 for more information on using the InputService26TestCaseOperation2
// API call, and error handling.
//
// This method is useful when you want to inject custom logic or configuration
// into the SDK's request lifecycle. Such as custom headers, or retry logic.
//
//
//    // Example sending a request using the InputService26TestCaseOperation2Request method.
//    req, resp := client.InputService26TestCaseOperation2Request(params)
//
//    err := req.Send()
//    if err == nil { // resp is now filled
//        fmt.Println(resp)
//    }
func (c *InputService26ProtocolTest) InputService26TestCaseOperation2Request(input *InputService26TestShapeInputService26TestCaseOperation2Input) (req *request.Request, output *InputService26TestShapeInputService26TestCaseOperation2Output) {
	op := &request.Operation{
		Name:       opInputService26TestCaseOperation2,
		HTTPMethod: "GET",
		HTTPPath:   "/path",
	}

	if input == nil {
		input = &InputService26TestShapeInputService26TestCaseOperation2Input{}
	}

	output = &InputService26TestShapeInputService26TestCaseOperation2Output{}
	req = c.newRequest(op, input, output)
	req.Handlers.Unmarshal.Swap(restxml.UnmarshalHandler.Name, protocol.UnmarshalDiscardBodyHandler)
	req.Handlers.Build.PushBackNamed(protocol.NewHostPrefixHandler("foo-{Name}.", input.hostLabels))
	req.Handlers.Build.PushBackNamed(protocol.ValidateEndpointHostHandler)
	return
}

// InputService26TestCaseOperation2 API operation for .
//
// Returns awserr.Error for service API and SDK errors. Use runtime type assertions
// with awserr.Error's Code and Message methods to get detailed information about
// the error.
//
// See the AWS API reference guide for 's
// API operation InputService26TestCaseOperation2 for usage and error information.
func (c *InputService26ProtocolTest) InputService26TestCaseOperation2(input *InputService26TestShapeInputService26TestCaseOperation2Input) (*InputService26TestShapeInputService26TestCaseOperation2Output, error) {
	req, out := c.InputService26TestCaseOperation2Request(input)
	return out, req.Send()
}

// InputService26TestCaseOperation2WithContext is the same as InputService26TestCaseOperation2 with the addition of
// the ability to pass a context and additional request options.
//
// See InputService26TestCaseOperation2 for details on how to use this API operation.
//
// The context must be non-nil and will be used for request cancellation. If
// the context is nil a panic will occur. In the future the SDK may create
// sub-contexts for http.Requests. See https://golang.org/pkg/context/
// for more information on using Contexts.
func (c *InputService26ProtocolTest) InputService26TestCaseOperation2WithContext(ctx aws.Context, input *InputService26TestShapeInputService26TestCaseOperation2Input, opts ...request.Option) (*InputService26TestShapeInputService26TestCaseOperation2Output, error) {
	req, out := c.InputService26TestCaseOperation2Request(input)
	req.SetContext(ctx)
	req.ApplyOptions(opts...)
	return out, req.Send()
}

type InputService26TestShapeInputService26TestCaseOperation1Input struct {
	_ struct{} `locationName:"StaticOpRequest" type:"structure"`

	Name *string `type:"string"`
}

// SetName sets the Name field's value.
func (s *InputService26TestShapeInputService26TestCaseOperation1Input) SetName(v string) *InputService26TestShapeInputService26TestCaseOperation1Input {
	s.Name = &v
	return s
}

type InputService26TestShapeInputService26TestCaseOperation1Output struct {
	_ struct{} `type:"structure"`
}

type InputService26TestShapeInputService26TestCaseOperation2Input struct {
	_ struct{} `locationName:"MemberRefOpRequest" type:"structure"`

	// Name is a required field
	Name *string `type:"string" required:"true"`
}

// Validate inspects the fields of the type to determine if they are valid.
func (s *InputService26TestShapeInputService26TestCaseOperation2Input) Validate() error {
	invalidParams := request.ErrInvalidParams{Context: "InputService26TestShapeInputService26TestCaseOperation2Input"}
	if s.Name == nil {
		invalidParams.Add(request.NewErrParamRequired("Name"))
	}
	if s.Name != nil && len(*s.Name) < 1 {
		invalidParams.Add(request.NewErrParamMinLen("Name", 1))
	}

	if invalidParams.Len() > 0 {
		return invalidParams
	}
	return nil
}

// SetName sets the Name field's value.
func (s *InputService26TestShapeInputService26TestCaseOperation2Input) SetName(v string) *InputService26TestShapeInputService26TestCaseOperation2Input {
	s.Name = &v
	return s
}

func (s *InputService26TestShapeInputService26TestCaseOperation2Input) hostLabels() map[string]string {
	return map[string]string{
		"Name": aws.StringValue(s.Name),
	}
}

type InputService26TestShapeInputService26TestCaseOperation2Output struct {
	_ struct{} `type:"structure"`
}

//
// Tests begin here
//

func TestInputService1ProtocolTestBasicXMLSerializationCase1(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation1Input{
		Description: aws.String("bar"),
		Name:        aws.String("foo"),
	}
	req, _ := svc.InputService1TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><Description xmlns="https://foo/">bar</Description><Name xmlns="https://foo/">foo</Name></OperationRequest>`, util.Trim(string(body)), InputService1TestShapeInputService1TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService1ProtocolTestBasicXMLSerializationCase2(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation2Input{
		Description: aws.String("bar"),
		Name:        aws.String("foo"),
	}
	req, _ := svc.InputService1TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><Description xmlns="https://foo/">bar</Description><Name xmlns="https://foo/">foo</Name></OperationRequest>`, util.Trim(string(body)), InputService1TestShapeInputService1TestCaseOperation2Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService1ProtocolTestBasicXMLSerializationCase3(t *testing.T) {
	svc := NewInputService1ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService1TestShapeInputService1TestCaseOperation3Input{}
	req, _ := svc.InputService1TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService2ProtocolTestSerializeOtherScalarTypesCase1(t *testing.T) {
	svc := NewInputService2ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService2TestShapeInputService2TestCaseOperation1Input{
		First:  aws.Bool(true),
		Fourth: aws.Int64(3),
		Second: aws.Bool(false),
		Third:  aws.Float64(1.2),
	}
	req, _ := svc.InputService2TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><First xmlns="https://foo/">true</First><Fourth xmlns="https://foo/">3</Fourth><Second xmlns="https://foo/">false</Second><Third xmlns="https://foo/">1.2</Third></OperationRequest>`, util.Trim(string(body)), InputService2TestShapeInputService2TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService3ProtocolTestNestedStructuresCase1(t *testing.T) {
	svc := NewInputService3ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService3TestShapeInputService3TestCaseOperation1Input{
		Description: aws.String("baz"),
		SubStructure: &InputService3TestShapeSubStructure{
			Bar: aws.String("b"),
			Foo: aws.String("a"),
		},
	}
	req, _ := svc.InputService3TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><Description xmlns="https://foo/">baz</Description><SubStructure xmlns="https://foo/"><Bar xmlns="https://foo/">b</Bar><Foo xmlns="https://foo/">a</Foo></SubStructure></OperationRequest>`, util.Trim(string(body)), InputService3TestShapeInputService3TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService3ProtocolTestNestedStructuresCase2(t *testing.T) {
	svc := NewInputService3ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService3TestShapeInputService3TestCaseOperation2Input{
		Description: aws.String("baz"),
		SubStructure: &InputService3TestShapeSubStructure{
			Foo: aws.String("a"),
		},
	}
	req, _ := svc.InputService3TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><Description xmlns="https://foo/">baz</Description><SubStructure xmlns="https://foo/"><Foo xmlns="https://foo/">a</Foo></SubStructure></OperationRequest>`, util.Trim(string(body)), InputService3TestShapeInputService3TestCaseOperation2Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService4ProtocolTestNestedStructuresCase1(t *testing.T) {
	svc := NewInputService4ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService4TestShapeInputService4TestCaseOperation1Input{
		Description:  aws.String("baz"),
		SubStructure: &InputService4TestShapeSubStructure{},
	}
	req, _ := svc.InputService4TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><Description xmlns="https://foo/">baz</Description><SubStructure xmlns="https://foo/"></SubStructure></OperationRequest>`, util.Trim(string(body)), InputService4TestShapeInputService4TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService5ProtocolTestNonFlattenedListsCase1(t *testing.T) {
	svc := NewInputService5ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService5TestShapeInputService5TestCaseOperation1Input{
		ListParam: []*string{
			aws.String("one"),
			aws.String("two"),
			aws.String("three"),
		},
	}
	req, _ := svc.InputService5TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><ListParam xmlns="https://foo/"><member xmlns="https://foo/">one</member><member xmlns="https://foo/">two</member><member xmlns="https://foo/">three</member></ListParam></OperationRequest>`, util.Trim(string(body)), InputService5TestShapeInputService5TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService6ProtocolTestNonFlattenedListsWithLocationNameCase1(t *testing.T) {
	svc := NewInputService6ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService6TestShapeInputService6TestCaseOperation1Input{
		ListParam: []*string{
			aws.String("one"),
			aws.String("two"),
			aws.String("three"),
		},
	}
	req, _ := svc.InputService6TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><AlternateName xmlns="https://foo/"><NotMember xmlns="https://foo/">one</NotMember><NotMember xmlns="https://foo/">two</NotMember><NotMember xmlns="https://foo/">three</NotMember></AlternateName></OperationRequest>`, util.Trim(string(body)), InputService6TestShapeInputService6TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService7ProtocolTestFlattenedListsCase1(t *testing.T) {
	svc := NewInputService7ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService7TestShapeInputService7TestCaseOperation1Input{
		ListParam: []*string{
			aws.String("one"),
			aws.String("two"),
			aws.String("three"),
		},
	}
	req, _ := svc.InputService7TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><ListParam xmlns="https://foo/">one</ListParam><ListParam xmlns="https://foo/">two</ListParam><ListParam xmlns="https://foo/">three</ListParam></OperationRequest>`, util.Trim(string(body)), InputService7TestShapeInputService7TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService8ProtocolTestFlattenedListsWithLocationNameCase1(t *testing.T) {
	svc := NewInputService8ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService8TestShapeInputService8TestCaseOperation1Input{
		ListParam: []*string{
			aws.String("one"),
			aws.String("two"),
			aws.String("three"),
		},
	}
	req, _ := svc.InputService8TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><item xmlns="https://foo/">one</item><item xmlns="https://foo/">two</item><item xmlns="https://foo/">three</item></OperationRequest>`, util.Trim(string(body)), InputService8TestShapeInputService8TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService9ProtocolTestListOfStructuresCase1(t *testing.T) {
	svc := NewInputService9ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService9TestShapeInputService9TestCaseOperation1Input{
		ListParam: []*InputService9TestShapeSingleFieldStruct{
			{
				Element: aws.String("one"),
			},
			{
				Element: aws.String("two"),
			},
			{
				Element: aws.String("three"),
			},
		},
	}
	req, _ := svc.InputService9TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><item xmlns="https://foo/"><value xmlns="https://foo/">one</value></item><item xmlns="https://foo/"><value xmlns="https://foo/">two</value></item><item xmlns="https://foo/"><value xmlns="https://foo/">three</value></item></OperationRequest>`, util.Trim(string(body)), InputService9TestShapeInputService9TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService10ProtocolTestBlobShapesCase1(t *testing.T) {
	svc := NewInputService10ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService10TestShapeInputService10TestCaseOperation1Input{
		StructureParam: &InputService10TestShapeStructureShape{
			B: []byte("foo"),
		},
	}
	req, _ := svc.InputService10TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><StructureParam xmlns="https://foo/"><b xmlns="https://foo/">Zm9v</b></StructureParam></OperationRequest>`, util.Trim(string(body)), InputService10TestShapeInputService10TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone", r.URL.String())

	// assert headers

}

func TestInputService11ProtocolTestTimestampShapesCase1(t *testing.T) {
	svc := NewInputService11ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService11TestShapeInputService11TestCaseOperation1Input{
		TimeArg:            aws.Time(time.Unix(1422172800, 0)),
		TimeArgInHeader:    aws.Time(time.Unix(1422172800, 0)),
		TimeArgInQuery:     aws.Time(time.Unix(1422172800, 0)),
		TimeCustom:         aws.Time(time.Unix(1422172800, 0)),
		TimeCustomInHeader: aws.Time(time.Unix(1422172800, 0)),
		TimeCustomInQuery:  aws.Time(time.Unix(1422172800, 0)),
		TimeFormat:         aws.Time(time.Unix(1422172800, 0)),
		TimeFormatInHeader: aws.Time(time.Unix(1422172800, 0)),
		TimeFormatInQuery:  aws.Time(time.Unix(1422172800, 0)),
	}
	req, _ := svc.InputService11TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<TimestampStructure xmlns="https://foo/"><TimeArg xmlns="https://foo/">2015-01-25T08:00:00Z</TimeArg><TimeCustom xmlns="https://foo/">Sun, 25 Jan 2015 08:00:00 GMT</TimeCustom><TimeFormat xmlns="https://foo/">Sun, 25 Jan 2015 08:00:00 GMT</TimeFormat></TimestampStructure>`, util.Trim(string(body)), InputService11TestShapeInputService11TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/hostedzone?TimeQuery=2015-01-25T08%3A00%3A00Z&TimeCustomQuery=1422172800&TimeFormatQuery=1422172800", r.URL.String())

	// assert headers
	if e, a := "Sun, 25 Jan 2015 08:00:00 GMT", r.Header.Get("x-amz-timearg"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}
	if e, a := "1422172800", r.Header.Get("x-amz-timecustom-header"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}
	if e, a := "1422172800", r.Header.Get("x-amz-timeformat-header"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}

}

func TestInputService12ProtocolTestHeaderMapsCase1(t *testing.T) {
	svc := NewInputService12ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService12TestShapeInputService12TestCaseOperation1Input{
		Foo: map[string]*string{
			"a": aws.String("b"),
			"c": aws.String("d"),
		},
	}
	req, _ := svc.InputService12TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers
	if e, a := "b", r.Header.Get("x-foo-a"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}
	if e, a := "d", r.Header.Get("x-foo-c"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}

}

func TestInputService13ProtocolTestQuerystringListOfStringsCase1(t *testing.T) {
	svc := NewInputService13ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService13TestShapeInputService13TestCaseOperation1Input{
		Items: []*string{
			aws.String("value1"),
			aws.String("value2"),
		},
	}
	req, _ := svc.InputService13TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/path?item=value1&item=value2", r.URL.String())

	// assert headers

}

func TestInputService14ProtocolTestStringToStringMapsInQuerystringCase1(t *testing.T) {
	svc := NewInputService14ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService14TestShapeInputService14TestCaseOperation1Input{
		PipelineId: aws.String("foo"),
		QueryDoc: map[string]*string{
			"bar":  aws.String("baz"),
			"fizz": aws.String("buzz"),
		},
	}
	req, _ := svc.InputService14TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/jobsByPipeline/foo?bar=baz&fizz=buzz", r.URL.String())

	// assert headers

}

func TestInputService15ProtocolTestStringToStringListMapsInQuerystringCase1(t *testing.T) {
	svc := NewInputService15ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService15TestShapeInputService15TestCaseOperation1Input{
		PipelineId: aws.String("id"),
		QueryDoc: map[string][]*string{
			"fizz": {
				aws.String("buzz"),
				aws.String("pop"),
			},
			"foo": {
				aws.String("bar"),
				aws.String("baz"),
			},
		},
	}
	req, _ := svc.InputService15TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/2014-01-01/jobsByPipeline/id?foo=bar&foo=baz&fizz=buzz&fizz=pop", r.URL.String())

	// assert headers

}

func TestInputService16ProtocolTestBooleanInQuerystringCase1(t *testing.T) {
	svc := NewInputService16ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService16TestShapeInputService16TestCaseOperation1Input{
		BoolQuery: aws.Bool(true),
	}
	req, _ := svc.InputService16TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/path?bool-query=true", r.URL.String())

	// assert headers

}

func TestInputService16ProtocolTestBooleanInQuerystringCase2(t *testing.T) {
	svc := NewInputService16ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService16TestShapeInputService16TestCaseOperation2Input{
		BoolQuery: aws.Bool(false),
	}
	req, _ := svc.InputService16TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/path?bool-query=false", r.URL.String())

	// assert headers

}

func TestInputService17ProtocolTestStringPayloadCase1(t *testing.T) {
	svc := NewInputService17ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService17TestShapeInputService17TestCaseOperation1Input{
		Foo: aws.String("bar"),
	}
	req, _ := svc.InputService17TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	if e, a := "bar", util.Trim(string(body)); e != a {
		t.Errorf("expect %v, got %v", e, a)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService18ProtocolTestBlobPayloadCase1(t *testing.T) {
	svc := NewInputService18ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService18TestShapeInputService18TestCaseOperation1Input{
		Foo: []byte("bar"),
	}
	req, _ := svc.InputService18TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	if e, a := "bar", util.Trim(string(body)); e != a {
		t.Errorf("expect %v, got %v", e, a)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService18ProtocolTestBlobPayloadCase2(t *testing.T) {
	svc := NewInputService18ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService18TestShapeInputService18TestCaseOperation2Input{}
	req, _ := svc.InputService18TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService19ProtocolTestStructurePayloadCase1(t *testing.T) {
	svc := NewInputService19ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService19TestShapeInputService19TestCaseOperation1Input{
		Foo: &InputService19TestShapeFooShape{
			Baz: aws.String("bar"),
		},
	}
	req, _ := svc.InputService19TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<foo><baz>bar</baz></foo>`, util.Trim(string(body)), InputService19TestShapeInputService19TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService19ProtocolTestStructurePayloadCase2(t *testing.T) {
	svc := NewInputService19ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService19TestShapeInputService19TestCaseOperation2Input{}
	req, _ := svc.InputService19TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService19ProtocolTestStructurePayloadCase3(t *testing.T) {
	svc := NewInputService19ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService19TestShapeInputService19TestCaseOperation3Input{
		Foo: &InputService19TestShapeFooShape{},
	}
	req, _ := svc.InputService19TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<foo></foo>`, util.Trim(string(body)), InputService19TestShapeInputService19TestCaseOperation3Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService19ProtocolTestStructurePayloadCase4(t *testing.T) {
	svc := NewInputService19ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService19TestShapeInputService19TestCaseOperation4Input{}
	req, _ := svc.InputService19TestCaseOperation4Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService20ProtocolTestXMLAttributeCase1(t *testing.T) {
	svc := NewInputService20ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService20TestShapeInputService20TestCaseOperation1Input{
		Grant: &InputService20TestShapeGrant{
			Grantee: &InputService20TestShapeGrantee{
				EmailAddress: aws.String("foo@example.com"),
				Type:         aws.String("CanonicalUser"),
			},
		},
	}
	req, _ := svc.InputService20TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<Grant xmlns:_xmlns="xmlns" _xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:XMLSchema-instance="http://www.w3.org/2001/XMLSchema-instance" XMLSchema-instance:type="CanonicalUser"><Grantee><EmailAddress>foo@example.com</EmailAddress></Grantee></Grant>`, util.Trim(string(body)), InputService20TestShapeInputService20TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/", r.URL.String())

	// assert headers

}

func TestInputService21ProtocolTestGreedyKeysCase1(t *testing.T) {
	svc := NewInputService21ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService21TestShapeInputService21TestCaseOperation1Input{
		Bucket: aws.String("my/bucket"),
		Key:    aws.String("testing /123"),
	}
	req, _ := svc.InputService21TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/my%2Fbucket/testing%20/123", r.URL.String())

	// assert headers

}

func TestInputService22ProtocolTestOmitsNullQueryParamsButSerializesEmptyStringsCase1(t *testing.T) {
	svc := NewInputService22ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService22TestShapeInputService22TestCaseOperation1Input{}
	req, _ := svc.InputService22TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService22ProtocolTestOmitsNullQueryParamsButSerializesEmptyStringsCase2(t *testing.T) {
	svc := NewInputService22ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService22TestShapeInputService22TestCaseOperation2Input{
		Foo: aws.String(""),
	}
	req, _ := svc.InputService22TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/path?abc=mno&param-name=", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase1(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation1Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			NoRecurse: aws.String("foo"),
		},
	}
	req, _ := svc.InputService23TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase2(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation2Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			RecursiveStruct: &InputService23TestShapeRecursiveStructType{
				NoRecurse: aws.String("foo"),
			},
		},
	}
	req, _ := svc.InputService23TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></RecursiveStruct></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation2Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase3(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation3Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			RecursiveStruct: &InputService23TestShapeRecursiveStructType{
				RecursiveStruct: &InputService23TestShapeRecursiveStructType{
					RecursiveStruct: &InputService23TestShapeRecursiveStructType{
						NoRecurse: aws.String("foo"),
					},
				},
			},
		},
	}
	req, _ := svc.InputService23TestCaseOperation3Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></RecursiveStruct></RecursiveStruct></RecursiveStruct></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation3Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase4(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation4Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			RecursiveList: []*InputService23TestShapeRecursiveStructType{
				{
					NoRecurse: aws.String("foo"),
				},
				{
					NoRecurse: aws.String("bar"),
				},
			},
		},
	}
	req, _ := svc.InputService23TestCaseOperation4Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveList xmlns="https://foo/"><member xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></member><member xmlns="https://foo/"><NoRecurse xmlns="https://foo/">bar</NoRecurse></member></RecursiveList></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation4Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase5(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation5Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			RecursiveList: []*InputService23TestShapeRecursiveStructType{
				{
					NoRecurse: aws.String("foo"),
				},
				{
					RecursiveStruct: &InputService23TestShapeRecursiveStructType{
						NoRecurse: aws.String("bar"),
					},
				},
			},
		},
	}
	req, _ := svc.InputService23TestCaseOperation5Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveList xmlns="https://foo/"><member xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></member><member xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><NoRecurse xmlns="https://foo/">bar</NoRecurse></RecursiveStruct></member></RecursiveList></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation5Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService23ProtocolTestRecursiveShapesCase6(t *testing.T) {
	svc := NewInputService23ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService23TestShapeInputService23TestCaseOperation6Input{
		RecursiveStruct: &InputService23TestShapeRecursiveStructType{
			RecursiveMap: map[string]*InputService23TestShapeRecursiveStructType{
				"bar": {
					NoRecurse: aws.String("bar"),
				},
				"foo": {
					NoRecurse: aws.String("foo"),
				},
			},
		},
	}
	req, _ := svc.InputService23TestCaseOperation6Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<OperationRequest xmlns="https://foo/"><RecursiveStruct xmlns="https://foo/"><RecursiveMap xmlns="https://foo/"><entry xmlns="https://foo/"><key xmlns="https://foo/">foo</key><value xmlns="https://foo/"><NoRecurse xmlns="https://foo/">foo</NoRecurse></value></entry><entry xmlns="https://foo/"><key xmlns="https://foo/">bar</key><value xmlns="https://foo/"><NoRecurse xmlns="https://foo/">bar</NoRecurse></value></entry></RecursiveMap></RecursiveStruct></OperationRequest>`, util.Trim(string(body)), InputService23TestShapeInputService23TestCaseOperation6Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService24ProtocolTestIdempotencyTokenAutoFillCase1(t *testing.T) {
	svc := NewInputService24ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService24TestShapeInputService24TestCaseOperation1Input{
		Token: aws.String("abc123"),
	}
	req, _ := svc.InputService24TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<InputShape><Token>abc123</Token></InputShape>`, util.Trim(string(body)), InputService24TestShapeInputService24TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService24ProtocolTestIdempotencyTokenAutoFillCase2(t *testing.T) {
	svc := NewInputService24ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService24TestShapeInputService24TestCaseOperation2Input{}
	req, _ := svc.InputService24TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<InputShape><Token>00000000-0000-4000-8000-000000000000</Token></InputShape>`, util.Trim(string(body)), InputService24TestShapeInputService24TestCaseOperation2Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/path", r.URL.String())

	// assert headers

}

func TestInputService25ProtocolTestEnumCase1(t *testing.T) {
	svc := NewInputService25ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService25TestShapeInputService25TestCaseOperation1Input{
		FooEnum:    aws.String("foo"),
		HeaderEnum: aws.String("baz"),
		ListEnums: []*string{
			aws.String("foo"),
			aws.String(""),
			aws.String("bar"),
		},
		URIFooEnum: aws.String("bar"),
		URIListEnums: []*string{
			aws.String("0"),
			aws.String(""),
			aws.String("1"),
		},
	}
	req, _ := svc.InputService25TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<InputShape><FooEnum>foo</FooEnum><ListEnums><member>foo</member><member></member><member>bar</member></ListEnums></InputShape>`, util.Trim(string(body)), InputService25TestShapeInputService25TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://test/Enum/bar?ListEnums=0&ListEnums=&ListEnums=1", r.URL.String())

	// assert headers
	if e, a := "baz", r.Header.Get("x-amz-enum"); e != a {
		t.Errorf("expect %v to be %v", e, a)
	}

}

func TestInputService25ProtocolTestEnumCase2(t *testing.T) {
	svc := NewInputService25ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://test")})
	input := &InputService25TestShapeInputService25TestCaseOperation2Input{
		URIFooEnum: aws.String("bar"),
	}
	req, _ := svc.InputService25TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert URL
	awstesting.AssertURL(t, "https://test/Enum/bar", r.URL.String())

	// assert headers

}

func TestInputService26ProtocolTestEndpointHostTraitCase1(t *testing.T) {
	svc := NewInputService26ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://service.region.amazonaws.com")})
	input := &InputService26TestShapeInputService26TestCaseOperation1Input{
		Name: aws.String("myname"),
	}
	req, _ := svc.InputService26TestCaseOperation1Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<StaticOpRequest><Name>myname</Name></StaticOpRequest>`, util.Trim(string(body)), InputService26TestShapeInputService26TestCaseOperation1Input{})

	// assert URL
	awstesting.AssertURL(t, "https://data-service.region.amazonaws.com/path", r.URL.String())

	// assert headers

}

func TestInputService26ProtocolTestEndpointHostTraitCase2(t *testing.T) {
	svc := NewInputService26ProtocolTest(unit.Session, &aws.Config{Endpoint: aws.String("https://service.region.amazonaws.com")})
	input := &InputService26TestShapeInputService26TestCaseOperation2Input{
		Name: aws.String("myname"),
	}
	req, _ := svc.InputService26TestCaseOperation2Request(input)
	r := req.HTTPRequest

	// build request
	req.Build()
	if req.Error != nil {
		t.Errorf("expect no error, got %v", req.Error)
	}

	// assert body
	if r.Body == nil {
		t.Errorf("expect body not to be nil")
	}
	body := util.SortXML(r.Body)
	awstesting.AssertXML(t, `<MemberRefOpRequest><Name>myname</Name></MemberRefOpRequest>`, util.Trim(string(body)), InputService26TestShapeInputService26TestCaseOperation2Input{})

	// assert URL
	awstesting.AssertURL(t, "https://foo-myname.service.region.amazonaws.com/path", r.URL.String())

	// assert headers

}
