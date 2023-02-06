package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	functionsBasePath      = "/v2/functions/namespaces"
	functionsNamespacePath = functionsBasePath + "/%s"
)

type FunctionsService interface {
	ListNamespaces(context.Context) ([]FunctionsNamespace, *Response, error)
	GetNamespace(context.Context, string) (*FunctionsNamespace, *Response, error)
	CreateNamespace(context.Context, *FunctionsNamespaceCreateRequest) (*FunctionsNamespace, *Response, error)
	DeleteNamespace(context.Context, string) (*Response, error)
}

type FunctionsServiceOp struct {
	client *Client
}

var _ FunctionsService = &FunctionsServiceOp{}

type namespacesRoot struct {
	Namespaces []FunctionsNamespace `json:"namespaces,omitempty"`
}

type namespaceRoot struct {
	Namespace *FunctionsNamespace `json:"namespace,omitempty"`
}

type FunctionsNamespace struct {
	ApiHost   string    `json:"api_host,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Label     string    `json:"label,omitempty"`
	Region    string    `json:"region,omitempty"`
	UUID      string    `json:"uuid,omitempty"`
	Key       string    `json:"key,omitempty"`
}

type FunctionsNamespaceCreateRequest struct {
	Label  string `json:"label"`
	Region string `json:"region"`
}

// Gets a list of namespaces
func (s *FunctionsServiceOp) ListNamespaces(ctx context.Context) ([]FunctionsNamespace, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, functionsBasePath, nil)
	if err != nil {
		return nil, nil, err
	}
	nsRoot := new(namespacesRoot)
	resp, err := s.client.Do(ctx, req, nsRoot)
	if err != nil {
		return nil, resp, err
	}
	return nsRoot.Namespaces, resp, nil
}

// Gets a single namespace
func (s *FunctionsServiceOp) GetNamespace(ctx context.Context, namespace string) (*FunctionsNamespace, *Response, error) {
	path := fmt.Sprintf(functionsNamespacePath, namespace)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	nsRoot := new(namespaceRoot)
	resp, err := s.client.Do(ctx, req, nsRoot)
	if err != nil {
		return nil, resp, err
	}
	return nsRoot.Namespace, resp, nil
}

// Creates a namespace
func (s *FunctionsServiceOp) CreateNamespace(ctx context.Context, opts *FunctionsNamespaceCreateRequest) (*FunctionsNamespace, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, functionsBasePath, opts)
	if err != nil {
		return nil, nil, err
	}
	nsRoot := new(namespaceRoot)
	resp, err := s.client.Do(ctx, req, nsRoot)
	if err != nil {
		return nil, resp, err
	}
	return nsRoot.Namespace, resp, nil
}

// Delete a namespace
func (s *FunctionsServiceOp) DeleteNamespace(ctx context.Context, namespace string) (*Response, error) {
	path := fmt.Sprintf(functionsNamespacePath, namespace)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}
	return resp, nil
}
