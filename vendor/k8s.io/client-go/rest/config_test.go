/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rest

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/client-go/kubernetes/scheme"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/client-go/util/flowcontrol"

	fuzz "github.com/google/gofuzz"
	"github.com/stretchr/testify/assert"
)

func TestIsConfigTransportTLS(t *testing.T) {
	testCases := []struct {
		Config       *Config
		TransportTLS bool
	}{
		{
			Config:       &Config{},
			TransportTLS: false,
		},
		{
			Config: &Config{
				Host: "https://localhost",
			},
			TransportTLS: true,
		},
		{
			Config: &Config{
				Host: "localhost",
				TLSClientConfig: TLSClientConfig{
					CertFile: "foo",
				},
			},
			TransportTLS: true,
		},
		{
			Config: &Config{
				Host: "///:://localhost",
				TLSClientConfig: TLSClientConfig{
					CertFile: "foo",
				},
			},
			TransportTLS: false,
		},
		{
			Config: &Config{
				Host: "1.2.3.4:567",
				TLSClientConfig: TLSClientConfig{
					Insecure: true,
				},
			},
			TransportTLS: true,
		},
	}
	for _, testCase := range testCases {
		if err := SetKubernetesDefaults(testCase.Config); err != nil {
			t.Errorf("setting defaults failed for %#v: %v", testCase.Config, err)
			continue
		}
		useTLS := IsConfigTransportTLS(*testCase.Config)
		if testCase.TransportTLS != useTLS {
			t.Errorf("expected %v for %#v", testCase.TransportTLS, testCase.Config)
		}
	}
}

func TestSetKubernetesDefaultsUserAgent(t *testing.T) {
	config := &Config{}
	if err := SetKubernetesDefaults(config); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(config.UserAgent, "kubernetes/") {
		t.Errorf("no user agent set: %#v", config)
	}
}

func TestAdjustVersion(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("1.2.3", adjustVersion("1.2.3-alpha4"))
	assert.Equal("1.2.3", adjustVersion("1.2.3-alpha"))
	assert.Equal("1.2.3", adjustVersion("1.2.3"))
	assert.Equal("unknown", adjustVersion(""))
}

func TestAdjustCommit(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("1234567", adjustCommit("1234567890"))
	assert.Equal("123456", adjustCommit("123456"))
	assert.Equal("unknown", adjustCommit(""))
}

func TestAdjustCommand(t *testing.T) {
	assert := assert.New(t)
	assert.Equal("beans", adjustCommand(filepath.Join("home", "bob", "Downloads", "beans")))
	assert.Equal("beans", adjustCommand(filepath.Join(".", "beans")))
	assert.Equal("beans", adjustCommand("beans"))
	assert.Equal("unknown", adjustCommand(""))
}

func TestBuildUserAgent(t *testing.T) {
	assert.New(t).Equal(
		"lynx/nicest (beos/itanium) kubernetes/baaaaaaaaad",
		buildUserAgent(
			"lynx", "nicest",
			"beos", "itanium", "baaaaaaaaad"))
}

// This function untestable since it doesn't accept arguments.
func TestDefaultKubernetesUserAgent(t *testing.T) {
	assert.New(t).Contains(DefaultKubernetesUserAgent(), "kubernetes")
}

func TestRESTClientRequires(t *testing.T) {
	if _, err := RESTClientFor(&Config{Host: "127.0.0.1", ContentConfig: ContentConfig{NegotiatedSerializer: scheme.Codecs}}); err == nil {
		t.Errorf("unexpected non-error")
	}
	if _, err := RESTClientFor(&Config{Host: "127.0.0.1", ContentConfig: ContentConfig{GroupVersion: &v1.SchemeGroupVersion}}); err == nil {
		t.Errorf("unexpected non-error")
	}
	if _, err := RESTClientFor(&Config{Host: "127.0.0.1", ContentConfig: ContentConfig{GroupVersion: &v1.SchemeGroupVersion, NegotiatedSerializer: scheme.Codecs}}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

type fakeLimiter struct {
	FakeSaturation float64
	FakeQPS        float32
}

func (t *fakeLimiter) TryAccept() bool {
	return true
}

func (t *fakeLimiter) Saturation() float64 {
	return t.FakeSaturation
}

func (t *fakeLimiter) QPS() float32 {
	return t.FakeQPS
}

func (t *fakeLimiter) Stop() {}

func (t *fakeLimiter) Accept() {}

type fakeCodec struct{}

func (c *fakeCodec) Decode([]byte, *schema.GroupVersionKind, runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	return nil, nil, nil
}

func (c *fakeCodec) Encode(obj runtime.Object, stream io.Writer) error {
	return nil
}

type fakeRoundTripper struct{}

func (r *fakeRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

var fakeWrapperFunc = func(http.RoundTripper) http.RoundTripper {
	return &fakeRoundTripper{}
}

type fakeNegotiatedSerializer struct{}

func (n *fakeNegotiatedSerializer) SupportedMediaTypes() []runtime.SerializerInfo {
	return nil
}

func (n *fakeNegotiatedSerializer) EncoderForVersion(serializer runtime.Encoder, gv runtime.GroupVersioner) runtime.Encoder {
	return &fakeCodec{}
}

func (n *fakeNegotiatedSerializer) DecoderToVersion(serializer runtime.Decoder, gv runtime.GroupVersioner) runtime.Decoder {
	return &fakeCodec{}
}

var fakeDialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
	return nil, fakeDialerError
}
var fakeDialerError = errors.New("fakedialer")

type fakeAuthProviderConfigPersister struct{}

func (fakeAuthProviderConfigPersister) Persist(map[string]string) error {
	return fakeAuthProviderConfigPersisterError
}

var fakeAuthProviderConfigPersisterError = errors.New("fakeAuthProviderConfigPersisterError")

func TestAnonymousConfig(t *testing.T) {
	f := fuzz.New().NilChance(0.0).NumElements(1, 1)
	f.Funcs(
		func(r *runtime.Codec, f fuzz.Continue) {
			codec := &fakeCodec{}
			f.Fuzz(codec)
			*r = codec
		},
		func(r *http.RoundTripper, f fuzz.Continue) {
			roundTripper := &fakeRoundTripper{}
			f.Fuzz(roundTripper)
			*r = roundTripper
		},
		func(fn *func(http.RoundTripper) http.RoundTripper, f fuzz.Continue) {
			*fn = fakeWrapperFunc
		},
		func(r *runtime.NegotiatedSerializer, f fuzz.Continue) {
			serializer := &fakeNegotiatedSerializer{}
			f.Fuzz(serializer)
			*r = serializer
		},
		func(r *flowcontrol.RateLimiter, f fuzz.Continue) {
			limiter := &fakeLimiter{}
			f.Fuzz(limiter)
			*r = limiter
		},
		// Authentication does not require fuzzer
		func(r *AuthProviderConfigPersister, f fuzz.Continue) {},
		func(r *clientcmdapi.AuthProviderConfig, f fuzz.Continue) {
			r.Config = map[string]string{}
		},
		// Dial does not require fuzzer
		func(r *func(ctx context.Context, network, addr string) (net.Conn, error), f fuzz.Continue) {},
	)
	for i := 0; i < 20; i++ {
		original := &Config{}
		f.Fuzz(original)
		actual := AnonymousClientConfig(original)
		expected := *original

		// this is the list of known security related fields, add to this list if a new field
		// is added to Config, update AnonymousClientConfig to preserve the field otherwise.
		expected.Impersonate = ImpersonationConfig{}
		expected.BearerToken = ""
		expected.Username = ""
		expected.Password = ""
		expected.AuthProvider = nil
		expected.AuthConfigPersister = nil
		expected.ExecProvider = nil
		expected.TLSClientConfig.CertData = nil
		expected.TLSClientConfig.CertFile = ""
		expected.TLSClientConfig.KeyData = nil
		expected.TLSClientConfig.KeyFile = ""

		// The DeepEqual cannot handle the func comparison, so we just verify if the
		// function return the expected object.
		if actual.WrapTransport == nil || !reflect.DeepEqual(expected.WrapTransport(nil), &fakeRoundTripper{}) {
			t.Fatalf("AnonymousClientConfig dropped the WrapTransport field")
		} else {
			actual.WrapTransport = nil
			expected.WrapTransport = nil
		}
		if actual.Dial != nil {
			_, actualError := actual.Dial(context.Background(), "", "")
			_, expectedError := expected.Dial(context.Background(), "", "")
			if !reflect.DeepEqual(expectedError, actualError) {
				t.Fatalf("CopyConfig dropped the Dial field")
			}
		} else {
			actual.Dial = nil
			expected.Dial = nil
		}

		if !reflect.DeepEqual(*actual, expected) {
			t.Fatalf("AnonymousClientConfig dropped unexpected fields, identify whether they are security related or not: %s", diff.ObjectGoPrintDiff(expected, actual))
		}
	}
}

func TestCopyConfig(t *testing.T) {
	f := fuzz.New().NilChance(0.0).NumElements(1, 1)
	f.Funcs(
		func(r *runtime.Codec, f fuzz.Continue) {
			codec := &fakeCodec{}
			f.Fuzz(codec)
			*r = codec
		},
		func(r *http.RoundTripper, f fuzz.Continue) {
			roundTripper := &fakeRoundTripper{}
			f.Fuzz(roundTripper)
			*r = roundTripper
		},
		func(fn *func(http.RoundTripper) http.RoundTripper, f fuzz.Continue) {
			*fn = fakeWrapperFunc
		},
		func(r *runtime.NegotiatedSerializer, f fuzz.Continue) {
			serializer := &fakeNegotiatedSerializer{}
			f.Fuzz(serializer)
			*r = serializer
		},
		func(r *flowcontrol.RateLimiter, f fuzz.Continue) {
			limiter := &fakeLimiter{}
			f.Fuzz(limiter)
			*r = limiter
		},
		func(r *AuthProviderConfigPersister, f fuzz.Continue) {
			*r = fakeAuthProviderConfigPersister{}
		},
		func(r *func(ctx context.Context, network, addr string) (net.Conn, error), f fuzz.Continue) {
			*r = fakeDialFunc
		},
	)
	for i := 0; i < 20; i++ {
		original := &Config{}
		f.Fuzz(original)
		actual := CopyConfig(original)
		expected := *original

		// this is the list of known risky fields, add to this list if a new field
		// is added to Config, update CopyConfig to preserve the field otherwise.

		// The DeepEqual cannot handle the func comparison, so we just verify if the
		// function return the expected object.
		if actual.WrapTransport == nil || !reflect.DeepEqual(expected.WrapTransport(nil), &fakeRoundTripper{}) {
			t.Fatalf("CopyConfig dropped the WrapTransport field")
		} else {
			actual.WrapTransport = nil
			expected.WrapTransport = nil
		}
		if actual.Dial != nil {
			_, actualError := actual.Dial(context.Background(), "", "")
			_, expectedError := expected.Dial(context.Background(), "", "")
			if !reflect.DeepEqual(expectedError, actualError) {
				t.Fatalf("CopyConfig  dropped the Dial field")
			}
		}
		actual.Dial = nil
		expected.Dial = nil
		if actual.AuthConfigPersister != nil {
			actualError := actual.AuthConfigPersister.Persist(nil)
			expectedError := expected.AuthConfigPersister.Persist(nil)
			if !reflect.DeepEqual(expectedError, actualError) {
				t.Fatalf("CopyConfig  dropped the Dial field")
			}
		}
		actual.AuthConfigPersister = nil
		expected.AuthConfigPersister = nil

		if !reflect.DeepEqual(*actual, expected) {
			t.Fatalf("CopyConfig  dropped unexpected fields, identify whether they are security related or not: %s", diff.ObjectReflectDiff(expected, *actual))
		}
	}
}
