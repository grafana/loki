/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2025 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"context"
	"encoding/xml"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
	"github.com/minio/minio-go/v7/pkg/signer"
)

// SessionMode - session mode type there are only two types
type SessionMode string

// Session constants
const (
	SessionReadWrite SessionMode = "ReadWrite"
	SessionReadOnly  SessionMode = "ReadOnly"
)

type createSessionResult struct {
	XMLName     xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ CreateSessionResult"`
	Credentials struct {
		AccessKey    string    `xml:"AccessKeyId" json:"accessKey,omitempty"`
		SecretKey    string    `xml:"SecretAccessKey" json:"secretKey,omitempty"`
		SessionToken string    `xml:"SessionToken" json:"sessionToken,omitempty"`
		Expiration   time.Time `xml:"Expiration" json:"expiration,omitempty"`
	} `xml:",omitempty"`
}

// CreateSession - https://docs.aws.amazon.com/AmazonS3/latest/API/API_CreateSession.html
// the returning credentials may be cached depending on the expiration of the original
// credential, credentials will get renewed 10 secs earlier than when its gonna expire
// allowing for some leeway in the renewal process.
func (c *Client) CreateSession(ctx context.Context, bucketName string, sessionMode SessionMode) (cred credentials.Value, err error) {
	if err := s3utils.CheckValidBucketNameS3Express(bucketName); err != nil {
		return credentials.Value{}, err
	}

	v, ok := c.bucketSessionCache.Get(bucketName)
	if ok && v.Expiration.After(time.Now().Add(10*time.Second)) {
		// Verify if the credentials will not expire
		// in another 10 seconds, if not we renew it again.
		return v, nil
	}

	req, err := c.createSessionRequest(ctx, bucketName, sessionMode)
	if err != nil {
		return credentials.Value{}, err
	}

	resp, err := c.do(req)
	defer closeResponse(resp)
	if err != nil {
		return credentials.Value{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return credentials.Value{}, httpRespToErrorResponse(resp, bucketName, "")
	}

	credSession := &createSessionResult{}
	dec := xml.NewDecoder(resp.Body)
	if err = dec.Decode(credSession); err != nil {
		return credentials.Value{}, err
	}

	defer c.bucketSessionCache.Set(bucketName, cred)

	return credentials.Value{
		AccessKeyID:     credSession.Credentials.AccessKey,
		SecretAccessKey: credSession.Credentials.SecretKey,
		SessionToken:    credSession.Credentials.SessionToken,
		Expiration:      credSession.Credentials.Expiration,
	}, nil
}

// createSessionRequest - Wrapper creates a new CreateSession request.
func (c *Client) createSessionRequest(ctx context.Context, bucketName string, sessionMode SessionMode) (*http.Request, error) {
	// Set location query.
	urlValues := make(url.Values)
	urlValues.Set("session", "")

	// Set get bucket location always as path style.
	targetURL := *c.endpointURL

	// Fetch new host based on the bucket location.
	host := getS3ExpressEndpoint(c.region, s3utils.IsS3ExpressBucket(bucketName))

	// as it works in makeTargetURL method from api.go file
	if h, p, err := net.SplitHostPort(host); err == nil {
		if targetURL.Scheme == "http" && p == "80" || targetURL.Scheme == "https" && p == "443" {
			host = h
			if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
				host = "[" + h + "]"
			}
		}
	}

	isVirtualStyle := c.isVirtualHostStyleRequest(targetURL, bucketName)

	var urlStr string

	if isVirtualStyle {
		urlStr = c.endpointURL.Scheme + "://" + bucketName + "." + host + "/?session"
	} else {
		targetURL.Path = path.Join(bucketName, "") + "/"
		targetURL.RawQuery = urlValues.Encode()
		urlStr = targetURL.String()
	}

	// Get a new HTTP request for the method.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}

	// Set UserAgent for the request.
	c.setUserAgent(req)

	// Get credentials from the configured credentials provider.
	value, err := c.credsProvider.GetWithContext(c.CredContext())
	if err != nil {
		return nil, err
	}

	var (
		signerType      = value.SignerType
		accessKeyID     = value.AccessKeyID
		secretAccessKey = value.SecretAccessKey
		sessionToken    = value.SessionToken
	)

	// Custom signer set then override the behavior.
	if c.overrideSignerType != credentials.SignatureDefault {
		signerType = c.overrideSignerType
	}

	// If signerType returned by credentials helper is anonymous,
	// then do not sign regardless of signerType override.
	if value.SignerType == credentials.SignatureAnonymous {
		signerType = credentials.SignatureAnonymous
	}

	if signerType.IsAnonymous() || signerType.IsV2() {
		return req, errors.New("Only signature v4 is supported for CreateSession() API")
	}

	// Set sha256 sum for signature calculation only with signature version '4'.
	contentSha256 := emptySHA256Hex
	if c.secure {
		contentSha256 = unsignedPayload
	}

	req.Header.Set("X-Amz-Content-Sha256", contentSha256)
	req.Header.Set("x-amz-create-session-mode", string(sessionMode))
	req = signer.SignV4Express(*req, accessKeyID, secretAccessKey, sessionToken, c.region)
	return req, nil
}
