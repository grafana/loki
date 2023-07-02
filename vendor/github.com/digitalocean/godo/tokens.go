package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	accessTokensBasePath = "v2/tokens"
	tokenScopesBasePath  = accessTokensBasePath + "/scopes"
)

// TokensService is an interface for managing DigitalOcean API access tokens.
// It is not currently generally available. Follow the release notes for
// updates: https://docs.digitalocean.com/release-notes/api/
type TokensService interface {
	List(context.Context, *ListOptions) ([]Token, *Response, error)
	Get(context.Context, int) (*Token, *Response, error)
	Create(context.Context, *TokenCreateRequest) (*Token, *Response, error)
	Update(context.Context, int, *TokenUpdateRequest) (*Token, *Response, error)
	Revoke(context.Context, int) (*Response, error)
	ListScopes(context.Context, *ListOptions) ([]TokenScope, *Response, error)
	ListScopesByNamespace(context.Context, string, *ListOptions) ([]TokenScope, *Response, error)
}

// TokensServiceOp handles communication with the tokens related methods of the
// DigitalOcean API.
type TokensServiceOp struct {
	client *Client
}

var _ TokensService = &TokensServiceOp{}

// Token represents a DigitalOcean API token.
type Token struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	Scopes        []string  `json:"scopes"`
	ExpirySeconds *int      `json:"expiry_seconds"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsedAt    string    `json:"last_used_at"`

	// AccessToken contains the actual Oauth token string. It is only included
	// in the create response.
	AccessToken string `json:"access_token,omitempty"`
}

// tokenRoot represents a response from the DigitalOcean API
type tokenRoot struct {
	Token *Token `json:"token"`
}

type tokensRoot struct {
	Tokens []Token `json:"tokens"`
	Links  *Links  `json:"links"`
	Meta   *Meta   `json:"meta"`
}

// TokenCreateRequest represents a request to create a token.
type TokenCreateRequest struct {
	Name          string   `json:"name"`
	Scopes        []string `json:"scopes"`
	ExpirySeconds *int     `json:"expiry_seconds,omitempty"`
}

// TokenUpdateRequest represents a request to update a token.
type TokenUpdateRequest struct {
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

// TokenScope is a representation of a scope for the public API.
type TokenScope struct {
	Name string `json:"name"`
}

type tokenScopesRoot struct {
	TokenScopes []TokenScope `json:"scopes"`
	Links       *Links       `json:"links"`
	Meta        *Meta        `json:"meta"`
}

type tokenScopeNamespaceParam struct {
	Namespace string `url:"namespace,omitempty"`
}

// List all DigitalOcean API access tokens.
func (c TokensServiceOp) List(ctx context.Context, opt *ListOptions) ([]Token, *Response, error) {
	path, err := addOptions(accessTokensBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := c.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(tokensRoot)
	resp, err := c.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Tokens, resp, err
}

// Get a specific DigitalOcean API access token.
func (c TokensServiceOp) Get(ctx context.Context, tokenID int) (*Token, *Response, error) {
	path := fmt.Sprintf("%s/%d", accessTokensBasePath, tokenID)
	req, err := c.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(tokenRoot)
	resp, err := c.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Token, resp, err
}

// Create a new DigitalOcean API access token.
func (c TokensServiceOp) Create(ctx context.Context, createRequest *TokenCreateRequest) (*Token, *Response, error) {
	req, err := c.client.NewRequest(ctx, http.MethodPost, accessTokensBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(tokenRoot)
	resp, err := c.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Token, resp, err
}

// Update the name or scopes of a specific DigitalOcean API access token.
func (c TokensServiceOp) Update(ctx context.Context, tokenID int, updateRequest *TokenUpdateRequest) (*Token, *Response, error) {
	path := fmt.Sprintf("%s/%d", accessTokensBasePath, tokenID)
	req, err := c.client.NewRequest(ctx, http.MethodPatch, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(tokenRoot)
	resp, err := c.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Token, resp, err
}

// Revoke a specific DigitalOcean API access token.
func (c TokensServiceOp) Revoke(ctx context.Context, tokenID int) (*Response, error) {
	path := fmt.Sprintf("%s/%d", accessTokensBasePath, tokenID)
	req, err := c.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(ctx, req, nil)

	return resp, err
}

// ListScopes lists all available scopes that can be granted to a token.
func (c TokensServiceOp) ListScopes(ctx context.Context, opt *ListOptions) ([]TokenScope, *Response, error) {
	path, err := addOptions(tokenScopesBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	return listTokenScopes(ctx, c, path)
}

// ListScopesByNamespace lists available scopes in a namespace that can be granted
// to a token (e.g. the namespace for the `droplet:read“ scope is `droplet`).
func (c TokensServiceOp) ListScopesByNamespace(ctx context.Context, namespace string, opt *ListOptions) ([]TokenScope, *Response, error) {
	path, err := addOptions(tokenScopesBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	namespaceOpt := tokenScopeNamespaceParam{
		Namespace: namespace,
	}

	path, err = addOptions(path, namespaceOpt)
	if err != nil {
		return nil, nil, err
	}

	return listTokenScopes(ctx, c, path)
}

func listTokenScopes(ctx context.Context, c TokensServiceOp, path string) ([]TokenScope, *Response, error) {
	req, err := c.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(tokenScopesRoot)
	resp, err := c.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.TokenScopes, resp, err
}
