package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const dedicatedInferenceBasePath = "/v2/dedicated-inferences"

// DedicatedInferenceService is an interface for managing Dedicated Inference with the DigitalOcean API.
type DedicatedInferenceService interface {
	Create(context.Context, *DedicatedInferenceCreateRequest) (*DedicatedInference, *DedicatedInferenceToken, *Response, error)
	Get(context.Context, string) (*DedicatedInference, *Response, error)
	List(context.Context, *DedicatedInferenceListOptions) ([]DedicatedInferenceListItem, *Response, error)
	Delete(context.Context, string) (*Response, error)
	Update(context.Context, string, *DedicatedInferenceUpdateRequest) (*DedicatedInference, *Response, error)
	ListAccelerators(context.Context, string, *DedicatedInferenceListAcceleratorsOptions) ([]DedicatedInferenceAcceleratorInfo, *Response, error)
	CreateToken(context.Context, string, *DedicatedInferenceTokenCreateRequest) (*DedicatedInferenceToken, *Response, error)
	ListTokens(context.Context, string, *ListOptions) ([]DedicatedInferenceToken, *Response, error)
	RevokeToken(context.Context, string, string) (*Response, error)
	GetSizes(context.Context) (*DedicatedInferenceSizesResponse, *Response, error)
	GetGPUModelConfig(context.Context) (*DedicatedInferenceGPUModelConfigResponse, *Response, error)
}

// DedicatedInferenceServiceOp handles communication with Dedicated Inference methods of the DigitalOcean API.
type DedicatedInferenceServiceOp struct {
	client *Client
}

var _ DedicatedInferenceService = &DedicatedInferenceServiceOp{}

// DedicatedInferenceCreateRequest represents a request to create a Dedicated Inference.
type DedicatedInferenceCreateRequest struct {
	Spec    *DedicatedInferenceSpecRequest `json:"spec"`
	Secrets *DedicatedInferenceSecrets     `json:"secrets,omitempty"`
}

// DedicatedInferenceSpecRequest represents the deployment specification in a create/update request.
type DedicatedInferenceSpecRequest struct {
	Version              int                               `json:"version"`
	Name                 string                            `json:"name"`
	Region               string                            `json:"region"`
	EnablePublicEndpoint bool                              `json:"enable_public_endpoint"`
	VPC                  *DedicatedInferenceVPCRequest     `json:"vpc"`
	ModelDeployments     []*DedicatedInferenceModelRequest `json:"model_deployments"`
}

// DedicatedInferenceVPCRequest represents the VPC configuration in a request.
type DedicatedInferenceVPCRequest struct {
	UUID string `json:"uuid"`
}

// DedicatedInferenceModelRequest represents a model deployment in a request.
type DedicatedInferenceModelRequest struct {
	ModelID        string                                  `json:"model_id,omitempty"`
	ModelSlug      string                                  `json:"model_slug"`
	ModelProvider  string                                  `json:"model_provider"`
	WorkloadConfig *DedicatedInferenceWorkloadConfig       `json:"workload_config,omitempty"`
	Accelerators   []*DedicatedInferenceAcceleratorRequest `json:"accelerators"`
}

// DedicatedInferenceWorkloadConfig represents workload-specific configuration.
type DedicatedInferenceWorkloadConfig struct{}

// DedicatedInferenceAcceleratorRequest represents an accelerator in a request.
type DedicatedInferenceAcceleratorRequest struct {
	AcceleratorSlug string `json:"accelerator_slug"`
	Scale           uint64 `json:"scale"`
	Type            string `json:"type"`
}

// DedicatedInferenceSecrets represents secrets for external model providers.
type DedicatedInferenceSecrets struct {
	HuggingFaceToken string `json:"hugging_face_token,omitempty"`
}

// DedicatedInferenceListOptions specifies optional parameters for listing Dedicated Inferences.
type DedicatedInferenceListOptions struct {
	Region string `url:"region,omitempty"`
	Name   string `url:"name,omitempty"`
	ListOptions
}

// DedicatedInferenceListAcceleratorsOptions specifies optional parameters for listing accelerators.
type DedicatedInferenceListAcceleratorsOptions struct {
	Slug string `url:"slug,omitempty"`
	ListOptions
}

// DedicatedInferenceUpdateRequest represents a request to update a Dedicated Inference.
type DedicatedInferenceUpdateRequest struct {
	Spec    *DedicatedInferenceSpecRequest `json:"spec"`
	Secrets *DedicatedInferenceSecrets     `json:"secrets,omitempty"`
}

// DedicatedInferenceTokenCreateRequest represents a request to create an auth token.
type DedicatedInferenceTokenCreateRequest struct {
	Name string `json:"name"`
}

// -- Response types (what the API returns) --

// DedicatedInferenceListItem represents a Dedicated Inference item in a list response.
type DedicatedInferenceListItem struct {
	ID        string                       `json:"id"`
	Name      string                       `json:"name"`
	Region    string                       `json:"region"`
	Status    string                       `json:"status"`
	VPCUUID   string                       `json:"vpc_uuid"`
	Endpoints *DedicatedInferenceEndpoints `json:"endpoints,omitempty"`
	CreatedAt time.Time                    `json:"created_at,omitempty"`
	UpdatedAt time.Time                    `json:"updated_at,omitempty"`
}

// DedicatedInferenceAcceleratorInfo represents an accelerator in a list accelerators response.
type DedicatedInferenceAcceleratorInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// DedicatedInference represents a Dedicated Inference resource returned by the API.
type DedicatedInference struct {
	ID                    string                        `json:"id"`
	Name                  string                        `json:"name"`
	Region                string                        `json:"region"`
	Status                string                        `json:"status"`
	VPCUUID               string                        `json:"vpc_uuid"`
	Endpoints             *DedicatedInferenceEndpoints  `json:"endpoints,omitempty"`
	DeploymentSpec        *DedicatedInferenceDeployment `json:"spec,omitempty"`
	PendingDeploymentSpec *DedicatedInferenceDeployment `json:"pending_deployment_spec,omitempty"`
	CreatedAt             time.Time                     `json:"created_at,omitempty"`
	UpdatedAt             time.Time                     `json:"updated_at,omitempty"`
}

func (d DedicatedInference) String() string {
	return Stringify(d)
}

// DedicatedInferenceEndpoints represents the endpoints for a Dedicated Inference.
type DedicatedInferenceEndpoints struct {
	PublicEndpointFQDN  string `json:"public_endpoint_fqdn,omitempty"`
	PrivateEndpointFQDN string `json:"private_endpoint_fqdn,omitempty"`
}

// DedicatedInferenceDeployment represents a deployment spec in the API response.
type DedicatedInferenceDeployment struct {
	Version              uint64                               `json:"version"`
	ID                   string                               `json:"id"`
	DedicatedInferenceID string                               `json:"dedicated_inference_id"`
	State                string                               `json:"state"`
	EnablePublicEndpoint bool                                 `json:"enable_public_endpoint"`
	VPCConfig            *DedicatedInferenceVPCConfig         `json:"vpc_config,omitempty"`
	ModelDeployments     []*DedicatedInferenceModelDeployment `json:"model_deployments"`
	CreatedAt            time.Time                            `json:"created_at,omitempty"`
	UpdatedAt            time.Time                            `json:"updated_at,omitempty"`
}

// DedicatedInferenceVPCConfig represents the VPC config in an API response.
type DedicatedInferenceVPCConfig struct {
	VPCUUID string `json:"vpc_uuid"`
}

// DedicatedInferenceModelDeployment represents a model deployment in an API response.
type DedicatedInferenceModelDeployment struct {
	ModelID       string                           `json:"model_id"`
	ModelSlug     string                           `json:"model_slug"`
	ModelProvider string                           `json:"model_provider"`
	Accelerators  []*DedicatedInferenceAccelerator `json:"accelerators"`
}

// DedicatedInferenceAccelerator represents an accelerator in an API response.
type DedicatedInferenceAccelerator struct {
	AcceleratorID   string `json:"accelerator_id"`
	AcceleratorSlug string `json:"accelerator_slug"`
	State           string `json:"state"`
	Type            string `json:"type"`
	Scale           uint64 `json:"scale"`
}

// DedicatedInferenceToken represents an auth token returned on create.
type DedicatedInferenceToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Value     string    `json:"value,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (t DedicatedInferenceToken) String() string {
	return Stringify(t)
}

// DedicatedInferenceSizesResponse represents the response from GetSizes.
type DedicatedInferenceSizesResponse struct {
	EnabledRegions []string                  `json:"enabled_regions"`
	Sizes          []*DedicatedInferenceSize `json:"sizes"`
}

// DedicatedInferenceSize represents a GPU size with pricing information.
type DedicatedInferenceSize struct {
	GPUSlug      string                          `json:"gpu_slug"`
	PricePerHour string                          `json:"price_per_hour"`
	Regions      []string                        `json:"regions"`
	Currency     string                          `json:"currency"`
	CPU          uint32                          `json:"cpu"`
	Memory       uint32                          `json:"memory"`
	GPU          *DedicatedInferenceSizeGPU      `json:"gpu"`
	SizeCategory *DedicatedInferenceSizeCategory `json:"size_category"`
	Disks        []*DedicatedInferenceSizeDisk   `json:"disks"`
}

// DedicatedInferenceSizeGPU represents GPU details in a size.
type DedicatedInferenceSizeGPU struct {
	Count  uint32 `json:"count"`
	VramGb uint32 `json:"vram_gb"`
	Slug   string `json:"slug"`
}

// DedicatedInferenceSizeCategory represents the category of a size.
type DedicatedInferenceSizeCategory struct {
	Name      string `json:"name"`
	FleetName string `json:"fleet_name"`
}

// DedicatedInferenceSizeDisk represents a disk in a size.
type DedicatedInferenceSizeDisk struct {
	Type   string `json:"type"`
	SizeGb uint64 `json:"size_gb"`
}

// DedicatedInferenceGPUModelConfigResponse represents the response from GetGPUModelConfig.
type DedicatedInferenceGPUModelConfigResponse struct {
	GPUModelConfigs []*DedicatedInferenceGPUModelConfig `json:"gpu_model_configs"`
}

// DedicatedInferenceGPUModelConfig represents a GPU model configuration.
type DedicatedInferenceGPUModelConfig struct {
	GPUSlugs     []string `json:"gpu_slugs"`
	ModelSlug    string   `json:"model_slug"`
	ModelName    string   `json:"model_name"`
	IsModelGated bool     `json:"is_model_gated"`
}

// -- Root types for JSON deserialization --

type dedicatedInferenceRoot struct {
	DedicatedInference *DedicatedInference      `json:"dedicated_inference"`
	Token              *DedicatedInferenceToken `json:"token,omitempty"`
}

type dedicatedInferencesRoot struct {
	DedicatedInferences []DedicatedInferenceListItem `json:"dedicated_inferences"`
	Links               *Links                       `json:"links"`
	Meta                *Meta                        `json:"meta"`
}

type dedicatedInferenceAcceleratorsRoot struct {
	Accelerators []DedicatedInferenceAcceleratorInfo `json:"accelerators"`
	Links        *Links                              `json:"links"`
	Meta         *Meta                               `json:"meta"`
}

type dedicatedInferenceTokenRoot struct {
	Token *DedicatedInferenceToken `json:"token"`
}

type dedicatedInferenceTokensRoot struct {
	Tokens []DedicatedInferenceToken `json:"tokens"`
	Links  *Links                    `json:"links"`
	Meta   *Meta                     `json:"meta"`
}

// -- Service methods --

// Create a new Dedicated Inference with the given configuration.
func (s *DedicatedInferenceServiceOp) Create(ctx context.Context, createRequest *DedicatedInferenceCreateRequest) (*DedicatedInference, *DedicatedInferenceToken, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodPost, dedicatedInferenceBasePath, createRequest)
	if err != nil {
		return nil, nil, nil, err
	}

	root := new(dedicatedInferenceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, resp, err
	}

	return root.DedicatedInference, root.Token, resp, nil
}

// Get an existing Dedicated Inference by its UUID.
func (s *DedicatedInferenceServiceOp) Get(ctx context.Context, id string) (*DedicatedInference, *Response, error) {
	path := fmt.Sprintf("%s/%s", dedicatedInferenceBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferenceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.DedicatedInference, resp, nil
}

// Delete an existing Dedicated Inference by its UUID.
func (s *DedicatedInferenceServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	path := fmt.Sprintf("%s/%s", dedicatedInferenceBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// Update an existing Dedicated Inference.
func (s *DedicatedInferenceServiceOp) Update(ctx context.Context, id string, updateRequest *DedicatedInferenceUpdateRequest) (*DedicatedInference, *Response, error) {
	path := fmt.Sprintf("%s/%s", dedicatedInferenceBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferenceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.DedicatedInference, resp, nil
}

// List all Dedicated Inferences.
func (s *DedicatedInferenceServiceOp) List(ctx context.Context, opt *DedicatedInferenceListOptions) ([]DedicatedInferenceListItem, *Response, error) {
	path, err := addOptions(dedicatedInferenceBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferencesRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.DedicatedInferences, resp, nil
}

// ListAccelerators lists accelerators for a Dedicated Inference.
func (s *DedicatedInferenceServiceOp) ListAccelerators(ctx context.Context, diID string, opt *DedicatedInferenceListAcceleratorsOptions) ([]DedicatedInferenceAcceleratorInfo, *Response, error) {
	basePath := fmt.Sprintf("%s/%s/accelerators", dedicatedInferenceBasePath, diID)
	path, err := addOptions(basePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferenceAcceleratorsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Accelerators, resp, nil
}

// CreateToken creates a new auth token for a Dedicated Inference.
func (s *DedicatedInferenceServiceOp) CreateToken(ctx context.Context, diID string, createRequest *DedicatedInferenceTokenCreateRequest) (*DedicatedInferenceToken, *Response, error) {
	path := fmt.Sprintf("%s/%s/tokens", dedicatedInferenceBasePath, diID)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferenceTokenRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Token, resp, nil
}

// ListTokens lists all auth tokens for a Dedicated Inference.
func (s *DedicatedInferenceServiceOp) ListTokens(ctx context.Context, diID string, opt *ListOptions) ([]DedicatedInferenceToken, *Response, error) {
	basePath := fmt.Sprintf("%s/%s/tokens", dedicatedInferenceBasePath, diID)
	path, err := addOptions(basePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(dedicatedInferenceTokensRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Tokens, resp, nil
}

// RevokeToken revokes (deletes) an auth token for a Dedicated Inference.
func (s *DedicatedInferenceServiceOp) RevokeToken(ctx context.Context, diID string, tokenID string) (*Response, error) {
	path := fmt.Sprintf("%s/%s/tokens/%s", dedicatedInferenceBasePath, diID, tokenID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// GetSizes returns available Dedicated Inference sizes and pricing.
func (s *DedicatedInferenceServiceOp) GetSizes(ctx context.Context) (*DedicatedInferenceSizesResponse, *Response, error) {
	path := fmt.Sprintf("%s/sizes", dedicatedInferenceBasePath)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(DedicatedInferenceSizesResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, nil
}

// GetGPUModelConfig returns supported GPU model configurations.
func (s *DedicatedInferenceServiceOp) GetGPUModelConfig(ctx context.Context) (*DedicatedInferenceGPUModelConfigResponse, *Response, error) {
	path := fmt.Sprintf("%s/gpu-model-config", dedicatedInferenceBasePath)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(DedicatedInferenceGPUModelConfigResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, nil
}
