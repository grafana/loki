package cos

import (
	"context"
	"encoding/xml"
	"net/http"
)

type BucketDomainRule struct {
	Status            string `xml:"Status,omitempty"`
	Name              string `xml:"Name,omitempty"`
	Type              string `xml:"Type,omitempty"`
	ForcedReplacement string `xml:"ForcedReplacement,omitempty"`
}

type BucketPutDomainOptions struct {
	XMLName xml.Name           `xml:"DomainConfiguration"`
	Rules   []BucketDomainRule `xml:"DomainRule,omitempty"`
}
type BucketGetDomainResult BucketPutDomainOptions

func (s *BucketService) PutDomain(ctx context.Context, opt *BucketPutDomainOptions) (*Response, error) {
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?domain",
		method:  http.MethodPut,
		body:    opt,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return resp, err
}

func (s *BucketService) GetDomain(ctx context.Context) (*BucketGetDomainResult, *Response, error) {
	var res BucketGetDomainResult
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?domain",
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return &res, resp, err
}

func (s *BucketService) DeleteDomain(ctx context.Context) (*Response, error) {
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?domain",
		method:  http.MethodDelete,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return resp, err
}
