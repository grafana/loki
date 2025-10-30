// Copyright 2016 Google LLC
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

package pubsub

import (
	"math"
	"strings"
	"time"

	gax "github.com/googleapis/gax-go/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxPayload is the maximum number of bytes to devote to the
// encoded AcknowledgementRequest / ModifyAckDeadline proto message.
//
// With gRPC there is no way for the client to know the server's max message size (it is
// configurable on the server). We know from experience that it
// it 512K.
const (
	maxPayload       = 512 * 1024
	maxSendRecvBytes = 20 * 1024 * 1024 // 20M
)

func trunc32(i int64) int32 {
	if i > math.MaxInt32 {
		i = math.MaxInt32
	}
	return int32(i)
}

type defaultRetryer struct {
	bo gax.Backoff
}

// Logic originally from
// https://github.com/googleapis/java-pubsub/blob/main/google-cloud-pubsub/src/main/java/com/google/cloud/pubsub/v1/StatusUtil.java
func (r *defaultRetryer) Retry(err error) (pause time.Duration, shouldRetry bool) {
	s, ok := status.FromError(err)
	if !ok { // includes io.EOF, normal stream close, which causes us to reopen
		return r.bo.Pause(), true
	}
	switch s.Code() {
	case codes.DeadlineExceeded, codes.Internal, codes.ResourceExhausted, codes.Aborted:
		return r.bo.Pause(), true
	case codes.Unavailable:
		c := strings.Contains(s.Message(), "Server shutdownNow invoked")
		if !c {
			return r.bo.Pause(), true
		}
		return 0, false
	case codes.Unknown:
		// Retry GOAWAY, see https://github.com/googleapis/google-cloud-go/issues/4257.
		isGoaway := strings.Contains(s.Message(), "received prior goaway: code: NO_ERROR")
		if isGoaway {
			return r.bo.Pause(), true
		}
		return 0, false
	default:
		return 0, false
	}
}

type streamingPullRetryer struct {
	defaultRetryer gax.Retryer
}

// Does not retry ResourceExhausted. See: https://github.com/GoogleCloudPlatform/google-cloud-go/issues/1166#issuecomment-443744705
func (r *streamingPullRetryer) Retry(err error) (pause time.Duration, shouldRetry bool) {
	s, ok := status.FromError(err)
	if !ok { // call defaultRetryer so that its backoff can be used
		return r.defaultRetryer.Retry(err)
	}
	switch s.Code() {
	case codes.ResourceExhausted:
		return 0, false
	default:
		return r.defaultRetryer.Retry(err)
	}
}

type publishRetryer struct {
	defaultRetryer gax.Retryer
}

func (r *publishRetryer) Retry(err error) (pause time.Duration, shouldRetry bool) {
	s, ok := status.FromError(err)
	if !ok {
		return r.defaultRetryer.Retry(err)
	}
	if s.Code() == codes.Internal && strings.Contains(s.Message(), "string field contains invalid UTF-8") {
		return 0, false
	}
	return r.defaultRetryer.Retry(err)
}

var (
	exactlyOnceDeliveryTemporaryRetryErrors = map[codes.Code]struct{}{
		codes.DeadlineExceeded:  {},
		codes.ResourceExhausted: {},
		codes.Aborted:           {},
		codes.Internal:          {},
		codes.Unavailable:       {},
	}
)

// contains checks if grpc code v is in t, a set of retryable error codes.
func contains(v codes.Code, t map[codes.Code]struct{}) bool {
	_, ok := t[v]
	return ok
}

func newExactlyOnceBackoff() gax.Backoff {
	return gax.Backoff{
		Initial:    1 * time.Second,
		Max:        64 * time.Second,
		Multiplier: 2,
	}
}

// parseResourceName parses the project and resource ID from a fully qualified name.
// For example, "projects/p/topics/my-topic" -> "p", "my-topic".
// Returns empty strings if the input is misformatted.
func parseResourceName(fqn string) (string, string) {
	s := strings.Split(fqn, "/")
	if len(s) != 4 {
		return "", ""
	}
	return s[1], s[len(s)-1]
}
