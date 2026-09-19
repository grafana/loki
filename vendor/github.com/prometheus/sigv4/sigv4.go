// Copyright 2021 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sigv4

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
)

var sigv4HeaderDenylist = []string{
	"uber-trace-id",
}

type sigV4RoundTripper struct {
	region      string
	next        http.RoundTripper
	pool        sync.Pool
	creds       *aws.CredentialsCache
	serviceName string
	signer      *signer.Signer
}

// Option configures [NewSigV4RoundTripper].
type Option func(*options)

type options struct {
	ctx                 context.Context
	stsEndpointOverride string // for testing: override STS endpoint URL
}

// WithContext sets the context used during AWS configuration loading
// and credential retrieval.
func WithContext(ctx context.Context) Option {
	return func(o *options) {
		o.ctx = ctx
	}
}

// NewSigV4RoundTripper returns a new http.RoundTripper that will sign requests
// using Amazon's Signature Verification V4 signing procedure. The request will
// then be handed off to the next RoundTripper provided by next. If next is nil,
// http.DefaultTransport will be used.
//
// Credentials for signing are retrieved using the the default AWS credential
// chain. If credentials cannot be found, an error will be returned.
func NewSigV4RoundTripper(cfg *SigV4Config, next http.RoundTripper, opts ...Option) (http.RoundTripper, error) {
	o := options{ctx: context.Background()}
	for _, opt := range opts {
		opt(&o)
	}
	if next == nil {
		next = http.DefaultTransport
	}

	awsConfig := []func(*config.LoadOptions) error{}

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		awsConfig = append(awsConfig, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, string(cfg.SecretKey), ""),
		))
	}

	if cfg.UseFIPSSTSEndpoint {
		awsConfig = append(awsConfig, config.WithUseFIPSEndpoint(aws.FIPSEndpointStateEnabled))
	} else {
		awsConfig = append(awsConfig, config.WithUseFIPSEndpoint(aws.FIPSEndpointStateDisabled))
	}

	if cfg.Region != "" {
		awsConfig = append(awsConfig, config.WithRegion(cfg.Region))
	}

	if cfg.Profile != "" {
		awsConfig = append(awsConfig, config.WithSharedConfigProfile(cfg.Profile))
	}

	awscfg, err := config.LoadDefaultConfig(
		o.ctx,
		awsConfig...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create new AWS session: %w", err)
	}

	if _, err := awscfg.Credentials.Retrieve(o.ctx); err != nil {
		return nil, fmt.Errorf("could not get SigV4 credentials: %w", err)
	}

	if awscfg.Region == "" {
		return nil, fmt.Errorf("region not configured in sigv4 or in default credentials chain")
	}

	if cfg.RoleARN != "" {
		stsOpts := []func(*sts.Options){}
		if o.stsEndpointOverride != "" {
			overrideURL := o.stsEndpointOverride
			stsOpts = append(stsOpts, func(so *sts.Options) {
				so.BaseEndpoint = aws.String(overrideURL)
			})
		}
		awscfg.Credentials = stscreds.NewAssumeRoleProvider(
			sts.NewFromConfig(awscfg, stsOpts...),
			cfg.RoleARN,
			func(ao *stscreds.AssumeRoleOptions) {
				if cfg.ExternalID != "" {
					ao.ExternalID = aws.String(cfg.ExternalID)
				}
				if cfg.SessionName != "" {
					ao.RoleSessionName = cfg.SessionName
				}
				if len(cfg.Tags) > 0 {
					ao.Tags = make([]ststypes.Tag, 0, len(cfg.Tags))
					for k, v := range cfg.Tags {
						ao.Tags = append(ao.Tags, ststypes.Tag{
							Key:   aws.String(k),
							Value: aws.String(v),
						})
					}
				}
			},
		)
	}

	serviceName := "aps"

	if cfg.ServiceName != "" {
		serviceName = cfg.ServiceName
	}

	rt := &sigV4RoundTripper{
		region:      awscfg.Region,
		next:        next,
		creds:       aws.NewCredentialsCache(awscfg.Credentials, credentialCacheOptions),
		signer:      signer.NewSigner(),
		serviceName: serviceName,
	}
	rt.pool.New = rt.newBuf
	return rt, nil
}

func (rt *sigV4RoundTripper) newBuf() any {
	return bytes.NewBuffer(make([]byte, 0, 1024))
}

// pooledBody serves a request body from a pooled buffer. The downstream
// RoundTripper may read and close the body in a separate goroutine after
// RoundTrip returns, so the buffer is only reset and returned to the pool once
// Close is called, never while RoundTrip's defer runs.
type pooledBody struct {
	*bytes.Reader
	buf  *bytes.Buffer
	pool *sync.Pool
	once sync.Once
}

func (b *pooledBody) Close() error {
	b.once.Do(func() {
		b.buf.Reset()
		b.pool.Put(b.buf)
	})
	return nil
}

func (rt *sigV4RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	buf := rt.pool.Get().(*bytes.Buffer)

	strHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// The buffer is returned to the pool when RoundTrip returns, unless its
	// ownership is handed to signReq.Body below, in which case the body's Close
	// recycles it instead. The downstream RoundTripper may read the body
	// asynchronously after RoundTrip returns, so the buffer must not be reset
	// here while it is still backing the body.
	bufOwnedByBody := false
	defer func() {
		if bufOwnedByBody {
			return
		}
		buf.Reset()
		rt.pool.Put(buf)
	}()

	// RoundTrip must not modify the caller's request, so clone it up front and
	// apply every change below to signReq only.
	signReq := req.Clone(req.Context())

	if req.Body != nil {
		// Read the body into the pooled buffer so it can be hashed and replayed
		// downstream. RoundTrip is allowed to consume and close the caller's
		// body, but must not reassign fields on the caller's request. With
		// GetBody we read a fresh copy and close req.Body to release its
		// resources (e.g. an open file); otherwise we consume req.Body directly.
		src := req.Body
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				if body != nil {
					_ = body.Close()
				}
				// RoundTrip must close the caller's body even on error.
				_ = req.Body.Close()
				return nil, err
			}
			if body == nil {
				// A non-nil Body must yield a non-nil copy; guard against a
				// misbehaving GetBody so io.Copy below can't read from nil.
				_ = req.Body.Close()
				return nil, fmt.Errorf("sigv4: request GetBody returned a nil body")
			}
			// GetBody returned an independent copy, so req.Body can be released.
			_ = req.Body.Close()
			src = body
		}

		if _, err := io.Copy(buf, src); err != nil {
			_ = src.Close()
			return nil, err
		}
		_ = src.Close()

		// Replay the body downstream from the pooled buffer. Empty body is a
		// valid situation. Ownership of the buffer moves to the body, which
		// recycles it on Close, so it stays valid for as long as the downstream
		// RoundTripper reads it.
		signReq.Body = &pooledBody{
			Reader: bytes.NewReader(buf.Bytes()),
			buf:    buf,
			pool:   &rt.pool,
		}
		bufOwnedByBody = true
		hash := sha256.Sum256(buf.Bytes())
		strHash = hex.EncodeToString(hash[:])
	}

	// Normalize the path as documented by AWS (path.Clean collapses duplicate
	// slashes and resolves dot segments), but don't let it turn an empty path
	// into "." or strip a trailing slash, which would corrupt the outgoing
	// path. This normalization applies to both the signed request and what is
	// sent downstream.
	// https://docs.aws.amazon.com/general/latest/gr/sigv4-create-canonical-request.html
	if cleaned := path.Clean(signReq.URL.Path); cleaned != "." {
		// Re-add the trailing slash path.Clean removed, to preserve the
		// caller's path.
		if cleaned != "/" && strings.HasSuffix(signReq.URL.Path, "/") {
			cleaned += "/"
		}
		signReq.URL.Path = cleaned
	}

	// Trim out headers that we don't want to sign.
	for _, header := range sigv4HeaderDenylist {
		signReq.Header.Del(header)
	}
	creds, err := rt.creds.Retrieve(req.Context())
	if err != nil {
		// signReq.Body owns the pooled buffer at this point but is never handed
		// to the downstream RoundTripper, so close it here to recycle it.
		if bufOwnedByBody {
			_ = signReq.Body.Close()
		}
		return nil, fmt.Errorf("error retrieving credentials: %w", err)
	}

	err = rt.signer.SignHTTP(
		req.Context(),
		creds,
		signReq,
		strHash,
		rt.serviceName,
		rt.region,
		time.Now().UTC(),
	)
	if err != nil {
		// As above, recycle the pooled buffer that signReq.Body owns since the
		// downstream RoundTripper will not receive it.
		if bufOwnedByBody {
			_ = signReq.Body.Close()
		}
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Set unsigned headers into the new req.
	for _, header := range sigv4HeaderDenylist {
		headerValue := req.Header.Get(header)
		if headerValue != "" {
			signReq.Header.Set(header, headerValue)
		}
	}

	return rt.next.RoundTrip(signReq)
}

func credentialCacheOptions(options *aws.CredentialsCacheOptions) {
	options.ExpiryWindow = 30 * time.Second
	options.ExpiryWindowJitterFrac = 0.5
}
