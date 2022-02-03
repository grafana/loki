//go:build go1.16
// +build go1.16

// Copyright 2017 Microsoft Corporation. All rights reserved.
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

/*
Package azcore implements an HTTP request/response middleware pipeline.

The middleware consists of three components.

   - One or more Policy instances.
   - A Transporter instance.
   - A Pipeline instance that combines the Policy and Transporter instances.

Implementing the Policy Interface

A Policy can be implemented in two ways; as a first-class function for a stateless Policy, or as
a method on a type for a stateful Policy.  Note that HTTP requests made via the same pipeline share
the same Policy instances, so if a Policy mutates its state it MUST be properly synchronized to
avoid race conditions.

A Policy's Do method is called when an HTTP request wants to be sent over the network. The Do method can
perform any operation(s) it desires. For example, it can log the outgoing request, mutate the URL, headers,
and/or query parameters, inject a failure, etc.  Once the Policy has successfully completed its request
work, it must call the Next() method on the *policy.Request instance in order to pass the request to the
next Policy in the chain.

When an HTTP response comes back, the Policy then gets a chance to process the response/error.  The Policy instance
can log the response, retry the operation if it failed due to a transient error or timeout, unmarshal the response
body, etc.  Once the Policy has successfully completed its response work, it must return the *http.Response
and error instances to its caller.

Template for implementing a stateless Policy:

   type policyFunc func(*policy.Request) (*http.Response, error)
   // Do implements the Policy interface on policyFunc.

   func (pf policyFunc) Do(req *policy.Request) (*http.Response, error) {
	   return pf(req)
   }

   func NewMyStatelessPolicy() policy.Policy {
      return policyFunc(func(req *policy.Request) (*http.Response, error) {
         // TODO: mutate/process Request here

         // forward Request to next Policy & get Response/error
         resp, err := req.Next()

         // TODO: mutate/process Response/error here

         // return Response/error to previous Policy
         return resp, err
      })
   }

Template for implementing a stateful Policy:

   type MyStatefulPolicy struct {
      // TODO: add configuration/setting fields here
   }

   // TODO: add initialization args to NewMyStatefulPolicy()
   func NewMyStatefulPolicy() policy.Policy {
      return &MyStatefulPolicy{
         // TODO: initialize configuration/setting fields here
      }
   }

   func (p *MyStatefulPolicy) Do(req *policy.Request) (resp *http.Response, err error) {
         // TODO: mutate/process Request here

         // forward Request to next Policy & get Response/error
         resp, err := req.Next()

         // TODO: mutate/process Response/error here

         // return Response/error to previous Policy
         return resp, err
   }

Implementing the Transporter Interface

The Transporter interface is responsible for sending the HTTP request and returning the corresponding
HTTP response or error.  The Transporter is invoked by the last Policy in the chain.  The default Transporter
implementation uses a shared http.Client from the standard library.

The same stateful/stateless rules for Policy implementations apply to Transporter implementations.

Using Policy and Transporter Instances Via a Pipeline

To use the Policy and Transporter instances, an application passes them to the runtime.NewPipeline function.

   func NewPipeline(transport Transporter, policies ...Policy) Pipeline

The specified Policy instances form a chain and are invoked in the order provided to NewPipeline
followed by the Transporter.

Once the Pipeline has been created, create a runtime.Request instance and pass it to Pipeline's Do method.

   func NewRequest(ctx context.Context, httpMethod string, endpoint string) (*Request, error)

   func (p Pipeline) Do(req *Request) (*http.Request, error)

The Pipeline.Do method sends the specified Request through the chain of Policy and Transporter
instances.  The response/error is then sent through the same chain of Policy instances in reverse
order.  For example, assuming there are Policy types PolicyA, PolicyB, and PolicyC along with
TransportA.

   pipeline := NewPipeline(TransportA, PolicyA, PolicyB, PolicyC)

The flow of Request and Response looks like the following:

   policy.Request -> PolicyA -> PolicyB -> PolicyC -> TransportA -----+
                                                                      |
                                                               HTTP(s) endpoint
                                                                      |
   caller <--------- PolicyA <- PolicyB <- PolicyC <- http.Response-+

Creating a Request Instance

The Request instance passed to Pipeline's Do method is a wrapper around an *http.Request.  It also
contains some internal state and provides various convenience methods.  You create a Request instance
by calling the runtime.NewRequest function:

   func NewRequest(ctx context.Context, httpMethod string, endpoint string) (*Request, error)

If the Request should contain a body, call the SetBody method.

   func (req *Request) SetBody(body ReadSeekCloser, contentType string) error

A seekable stream is required so that upon retry, the retry Policy instance can seek the stream
back to the beginning before retrying the network request and re-uploading the body.

Sending an Explicit Null

Operations like JSON-MERGE-PATCH send a JSON null to indicate a value should be deleted.

   {
      "delete-me": null
   }

This requirement conflicts with the SDK's default marshalling that specifies "omitempty" as
a means to resolve the ambiguity between a field to be excluded and its zero-value.

   type Widget struct {
      Name  *string `json:",omitempty"`
      Count *int    `json:",omitempty"`
   }

In the above example, Name and Count are defined as pointer-to-type to disambiguate between
a missing value (nil) and a zero-value (0) which might have semantic differences.

In a PATCH operation, any fields left as `nil` are to have their values preserved.  When updating
a Widget's count, one simply specifies the new value for Count, leaving Name nil.

To fulfill the requirement for sending a JSON null, the NullValue() function can be used.

   w := Widget{
      Count: azcore.NullValue(0).(*int),
   }

This sends an explict "null" for Count, indicating that any current value for Count should be deleted.

Processing the Response

When the HTTP response is received, the *http.Response is returned directly. Each Policy instance
can inspect/mutate the *http.Response.

Built-in Logging

To enable logging, set environment variable AZURE_SDK_GO_LOGGING to "all" before executing your program.

By default the logger writes to stderr.  This can be customized by calling log.SetListener, providing
a callback that writes to the desired location.  Any custom logging implementation MUST provide its
own synchronization to handle concurrent invocations.

See the docs for the log package for further details.
*/
package azcore
