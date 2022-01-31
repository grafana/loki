package cloudflare

import (
	"context"
	"errors"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/stretchr/testify/mock"
)

type fakeCloudflareClient struct {
	mock.Mock
}

func (f *fakeCloudflareClient) CallCount() int {
	var actualCalls int
	for _, call := range f.Calls {
		if call.Method == "LogpullReceived" {
			actualCalls++
		}
	}
	return actualCalls
}

type fakeLogIterator struct {
	logs    []string
	current string

	err error
}

func (f *fakeLogIterator) Next() bool {
	if len(f.logs) == 0 {
		return false
	}
	f.current = f.logs[0]
	if f.current == `error` {
		f.err = errors.New("error")
		return false
	}
	f.logs = f.logs[1:]
	return true
}
func (f *fakeLogIterator) Err() error                         { return f.err }
func (f *fakeLogIterator) Line() []byte                       { return []byte(f.current) }
func (f *fakeLogIterator) Fields() (map[string]string, error) { return nil, nil }
func (f *fakeLogIterator) Close() error                       { return nil }

func newFakeCloudflareClient() *fakeCloudflareClient {
	return &fakeCloudflareClient{}
}

func (f *fakeCloudflareClient) LogpullReceived(ctx context.Context, start, end time.Time) (cloudflare.LogpullReceivedIterator, error) {
	r := f.Called(ctx, start, end)
	if r.Get(0) != nil {
		return r.Get(0).(cloudflare.LogpullReceivedIterator), nil
	}
	return nil, r.Error(1)
}
