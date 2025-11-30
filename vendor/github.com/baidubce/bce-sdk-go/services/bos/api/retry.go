package api

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/util/log"
)

var (
	DEFAULT_BOS_RETRY_POLICY = NewBosRetryPolicy(3, 20000, 300)
)

type BosRetryPolicy struct {
	maxErrorRetry        int
	maxDelayInMillis     int64
	baseIntervalInMillis int64
}

func (b *BosRetryPolicy) ShouldRetry(err bce.BceError, attempts int) bool {
	// Do not retry any more when retry the max times
	if attempts >= b.maxErrorRetry {
		return false
	}

	if err == nil {
		return true
	}

	// Always retry on IO error
	if _, ok := err.(net.Error); ok {
		// context canceled should not retry
		if strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "context canceled") {
			return false
		}
		return true
	}

	// Only retry on a service error
	if realErr, ok := err.(*bce.BceServiceError); ok {
		switch realErr.StatusCode {
		case http.StatusInternalServerError:
			log.Warn("retry for internal server error(500)")
			return true
		case http.StatusBadGateway:
			log.Warn("retry for bad gateway(502)")
			return true
		case http.StatusServiceUnavailable:
			log.Warn("retry for service unavailable(503)")
			return true
		case http.StatusForbidden:
			log.Warn("retry for service forbidden(403)")
			return true
		case http.StatusBadRequest:
			if realErr.Code != "Http400" {
				return false
			}
			log.Warn("retry for bad request(400)")
			return true
		}

		if realErr.Code == bce.EREQUEST_EXPIRED {
			log.Warn("retry for request expired")
			return true
		}
	}
	return false
}

func (b *BosRetryPolicy) GetDelayBeforeNextRetryInMillis(
	err bce.BceError, attempts int) time.Duration {
	if attempts < 0 {
		return 0 * time.Millisecond
	}
	delayInMillis := (1 << uint64(attempts)) * b.baseIntervalInMillis
	if delayInMillis > b.maxDelayInMillis {
		return time.Duration(b.maxDelayInMillis) * time.Millisecond
	}
	return time.Duration(delayInMillis) * time.Millisecond
}

func NewBosRetryPolicy(maxRetry int, maxDelay, base int64) *BosRetryPolicy {
	return &BosRetryPolicy{maxRetry, maxDelay, base}
}
