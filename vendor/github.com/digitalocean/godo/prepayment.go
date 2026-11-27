package godo

import (
	"context"
	"net/http"
	"time"
)

// PrepaymentService is an interface for interfacing with the Prepayment
// endpoints of the DigitalOcean API.
type PrepaymentService interface {
	GetConfig(context.Context) (*PrepaymentConfigResponse, *Response, error)
	GetStatus(context.Context) (*PrepaymentStatusResponse, *Response, error)
}

// PrepaymentServiceOp handles communication with the Prepayment related methods of
// the DigitalOcean API.
type PrepaymentServiceOp struct {
	client *Client
}

var _ PrepaymentService = &PrepaymentServiceOp{}

// PrepaymentConfig represents prepayment gate configuration for an account.
type PrepaymentConfig struct {
	SpendLimit          string    `json:"spend_limit"`
	IsAutoPrepayEnabled bool      `json:"is_auto_prepay_enabled"`
	PrepayAmount        string    `json:"prepay_amount"`
	PrepayThreshold     string    `json:"prepay_threshold"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// PrepaymentStatus represents prepayment gate status for an account.
type PrepaymentStatus struct {
	Balance             string `json:"balance"`
	IsAutoPrepayEnabled bool   `json:"is_auto_prepay_enabled"`
	Blocked             bool   `json:"blocked"`
	Eligible            bool   `json:"eligible"`
	MonthToDateBalance  string `json:"month_to_date_balance"`
}

// PrepaymentConfigResponse is the response from the public prepayment config endpoint.
type PrepaymentConfigResponse struct {
	Config *PrepaymentConfig `json:"config"`
	Status *PrepaymentStatus `json:"status"`
}

func (r PrepaymentConfigResponse) String() string {
	return Stringify(r)
}

// PrepaymentStatusResponse is the response from the public prepayment status endpoint.
type PrepaymentStatusResponse struct {
	Status *PrepaymentStatus `json:"status"`
}

func (r PrepaymentStatusResponse) String() string {
	return Stringify(r)
}

// GetConfig retrieves the authenticated customer's prepayment configuration and status.
func (s *PrepaymentServiceOp) GetConfig(ctx context.Context) (*PrepaymentConfigResponse, *Response, error) {
	path := "v2/customers/my/prepayment_config"

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(PrepaymentConfigResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, err
}

// GetStatus retrieves the authenticated customer's prepayment status.
func (s *PrepaymentServiceOp) GetStatus(ctx context.Context) (*PrepaymentStatusResponse, *Response, error) {
	path := "v2/customers/my/prepayment_status"

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(PrepaymentStatusResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, err
}
