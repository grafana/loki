// Copyright 2014 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub // import "cloud.google.com/go/pubsub"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"time"

	"cloud.google.com/go/internal/detect"
	vkit "cloud.google.com/go/pubsub/apiv1"
	"cloud.google.com/go/pubsub/internal"
	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	// ScopePubSub grants permissions to view and manage Pub/Sub
	// topics and subscriptions.
	ScopePubSub = "https://www.googleapis.com/auth/pubsub"

	// ScopeCloudPlatform grants permissions to view and manage your data
	// across Google Cloud Platform services.
	ScopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"
)

// Client is a Google Pub/Sub client scoped to a single project.
//
// Clients should be reused rather than being created as needed.
// A Client may be shared by multiple goroutines.
type Client struct {
	projectID     string
	pubc          *vkit.PublisherClient
	subc          *vkit.SubscriberClient
	enableTracing bool
}

// ClientConfig has configurations for the client.
type ClientConfig struct {
	PublisherCallOptions  *vkit.PublisherCallOptions
	SubscriberCallOptions *vkit.SubscriberCallOptions

	// EnableOpenTelemetryTracing enables tracing for this client.
	// This option allows selectively disabling Pub/Sub traces.
	// This defaults to false.
	// OpenTelemetry tracing standards are in active development, and thus
	// attributes, links, and span names are EXPERIMENTAL and subject to
	// change or removal without notice.
	EnableOpenTelemetryTracing bool
}

// mergePublisherCallOptions merges two PublisherCallOptions into one and the first argument has
// a lower order of precedence than the second one. If either is nil, return the other.
func mergePublisherCallOptions(a *vkit.PublisherCallOptions, b *vkit.PublisherCallOptions) *vkit.PublisherCallOptions {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	res := &vkit.PublisherCallOptions{}
	resVal := reflect.ValueOf(res).Elem()
	aVal := reflect.ValueOf(a).Elem()
	bVal := reflect.ValueOf(b).Elem()

	t := aVal.Type()

	for i := 0; i < aVal.NumField(); i++ {
		fieldName := t.Field(i).Name

		aFieldVal := aVal.Field(i).Interface().([]gax.CallOption)
		bFieldVal := bVal.Field(i).Interface().([]gax.CallOption)

		merged := append(aFieldVal, bFieldVal...)
		resVal.FieldByName(fieldName).Set(reflect.ValueOf(merged))
	}
	return res
}

// mergeSubscribercallOptions merges two SubscriberCallOptions into one and the first argument has
// a lower order of precedence than the second one. If either is nil, the other is used.
func mergeSubscriberCallOptions(a *vkit.SubscriberCallOptions, b *vkit.SubscriberCallOptions) *vkit.SubscriberCallOptions {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	res := &vkit.SubscriberCallOptions{}
	resVal := reflect.ValueOf(res).Elem()
	aVal := reflect.ValueOf(a).Elem()
	bVal := reflect.ValueOf(b).Elem()

	t := aVal.Type()

	for i := 0; i < aVal.NumField(); i++ {
		fieldName := t.Field(i).Name

		aFieldVal := aVal.Field(i).Interface().([]gax.CallOption)
		bFieldVal := bVal.Field(i).Interface().([]gax.CallOption)

		merged := append(aFieldVal, bFieldVal...)
		resVal.FieldByName(fieldName).Set(reflect.ValueOf(merged))
	}
	return res
}

// DetectProjectID is a sentinel value that instructs NewClient to detect the
// project ID. It is given in place of the projectID argument. NewClient will
// use the project ID from the given credentials or the default credentials
// (https://developers.google.com/accounts/docs/application-default-credentials)
// if no credentials were provided. When providing credentials, not all
// options will allow NewClient to extract the project ID. Specifically a JWT
// does not have the project ID encoded.
const DetectProjectID = "*detect-project-id*"

// ErrEmptyProjectID denotes that the project string passed into NewClient was empty.
// Please provide a valid project ID or use the DetectProjectID sentinel value to detect
// project ID from well defined sources.
var ErrEmptyProjectID = errors.New("pubsub: projectID string is empty")

// NewClient creates a new PubSub client. It uses a default configuration.
func NewClient(ctx context.Context, projectID string, opts ...option.ClientOption) (c *Client, err error) {
	return NewClientWithConfig(ctx, projectID, nil, opts...)
}

// NewClientWithConfig creates a new PubSub client.
func NewClientWithConfig(ctx context.Context, projectID string, config *ClientConfig, opts ...option.ClientOption) (*Client, error) {
	if projectID == "" {
		return nil, ErrEmptyProjectID
	}
	var o []option.ClientOption
	// Environment variables for gcloud emulator:
	// https://cloud.google.com/sdk/gcloud/reference/beta/emulators/pubsub/
	if addr := os.Getenv("PUBSUB_EMULATOR_HOST"); addr != "" {
		emulatorOpts := []option.ClientOption{
			option.WithEndpoint(addr),
			option.WithGRPCDialOption(grpc.WithInsecure()),
			option.WithoutAuthentication(),
			option.WithTelemetryDisabled(),
			internaloption.SkipDialSettingsValidation(),
		}
		opts = append(emulatorOpts, opts...)
	} else {
		numConns := runtime.GOMAXPROCS(0)
		if numConns > 4 {
			numConns = 4
		}
		o = []option.ClientOption{
			// Create multiple connections to increase throughput.
			option.WithGRPCConnectionPool(numConns),
			option.WithGRPCDialOption(grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time: 5 * time.Minute,
			})),
		}
	}
	o = append(o, opts...)
	pubc, err := vkit.NewPublisherClient(ctx, o...)
	if err != nil {
		return nil, fmt.Errorf("pubsub(publisher): %w", err)
	}
	subc, err := vkit.NewSubscriberClient(ctx, o...)
	if err != nil {
		return nil, fmt.Errorf("pubsub(subscriber): %w", err)
	}
	if config != nil {
		pubc.CallOptions = mergePublisherCallOptions(pubc.CallOptions, config.PublisherCallOptions)
		subc.CallOptions = mergeSubscriberCallOptions(subc.CallOptions, config.SubscriberCallOptions)
	}
	pubc.SetGoogleClientInfo("gccl", internal.Version)
	subc.SetGoogleClientInfo("gccl", internal.Version)

	// Handle project autodetection.
	projectID, err = detect.ProjectID(ctx, projectID, "", opts...)
	if err != nil {
		return nil, err
	}

	c := &Client{
		projectID: projectID,
		pubc:      pubc,
		subc:      subc,
	}
	if config != nil {
		c.enableTracing = config.EnableOpenTelemetryTracing
	}
	return c, nil
}

// Project returns the project ID or number for this instance of the client, which may have
// either been explicitly specified or autodetected.
func (c *Client) Project() string {
	return c.projectID
}

// Close releases any resources held by the client,
// such as memory and goroutines.
//
// If the client is available for the lifetime of the program, then Close need not be
// called at exit.
func (c *Client) Close() error {
	pubErr := c.pubc.Close()
	subErr := c.subc.Close()
	if pubErr != nil {
		return fmt.Errorf("pubsub publisher closing error: %w", pubErr)
	}
	if subErr != nil {
		// Suppress client connection closing errors. This will only happen
		// when using the client in conjunction with the Pub/Sub emulator
		// or fake (pstest). Closing both clients separately will never
		// return this error against the live Pub/Sub service.
		if strings.Contains(subErr.Error(), "the client connection is closing") {
			return nil
		}
		return fmt.Errorf("pubsub subscriber closing error: %w", subErr)
	}
	return nil
}

func (c *Client) fullyQualifiedProjectName() string {
	return fmt.Sprintf("projects/%s", c.projectID)
}
