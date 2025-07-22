package godo

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const (
	genAIBasePath                = "/v2/gen-ai/agents"
	agentModelBasePath           = "/v2/gen-ai/models"
	KnowledgeBasePath            = "/v2/gen-ai/knowledge_bases"
	KnowledgeBaseDataSourcesPath = KnowledgeBasePath + "/%s/data_sources"
	GetKnowledgeBaseByIDPath     = KnowledgeBasePath + "/%s"
	UpdateKnowledgeBaseByIDPath  = KnowledgeBasePath + "/%s"
	DeleteKnowledgeBaseByIDPath  = KnowledgeBasePath + "/%s"
	AgentKnowledgeBasePath       = "/v2/gen-ai/agents" + "/%s/knowledge_bases/%s"
	DeleteDataSourcePath         = KnowledgeBasePath + "/%s/data_sources/%s"
)

// GenAIService is an interface for interfacing with the Gen AI Agent endpoints
// of the DigitalOcean API.
// See https://docs.digitalocean.com/reference/api/digitalocean/#tag/GenAI-Platform-(Public-Preview) for more details.
type GenAIService interface {
	ListAgents(context.Context, *ListOptions) ([]*Agent, *Response, error)
	CreateAgent(context.Context, *AgentCreateRequest) (*Agent, *Response, error)
	ListAgentAPIKeys(context.Context, string, *ListOptions) ([]*ApiKeyInfo, *Response, error)
	CreateAgentAPIKey(context.Context, string, *AgentAPIKeyCreateRequest) (*ApiKeyInfo, *Response, error)
	UpdateAgentAPIKey(context.Context, string, string, *AgentAPIKeyUpdateRequest) (*ApiKeyInfo, *Response, error)
	DeleteAgentAPIKey(context.Context, string, string) (*ApiKeyInfo, *Response, error)
	RegenerateAgentAPIKey(context.Context, string, string) (*ApiKeyInfo, *Response, error)
	GetAgent(context.Context, string) (*Agent, *Response, error)
	UpdateAgent(context.Context, string, *AgentUpdateRequest) (*Agent, *Response, error)
	DeleteAgent(context.Context, string) (*Agent, *Response, error)
	UpdateAgentVisibility(context.Context, string, *AgentVisibilityUpdateRequest) (*Agent, *Response, error)
	ListModels(context.Context, *ListOptions) ([]*Model, *Response, error)
	ListKnowledgeBases(ctx context.Context, opt *ListOptions) ([]KnowledgeBase, *Response, error)
	CreateKnowledgeBase(ctx context.Context, knowledgeBaseCreate *KnowledgeBaseCreateRequest) (*KnowledgeBase, *Response, error)
	ListKnowledgeBaseDataSources(ctx context.Context, knowledgeBaseID string, opt *ListOptions) ([]KnowledgeBaseDataSource, *Response, error)
	AddKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, addDataSource *AddKnowledgeBaseDataSourceRequest) (*KnowledgeBaseDataSource, *Response, error)
	DeleteKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, dataSourceID string) (string, string, *Response, error)
	GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBase, string, *Response, error)
	UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, update *UpdateKnowledgeBaseRequest) (*KnowledgeBase, *Response, error)
	DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) (string, *Response, error)
	AttachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error)
	DetachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error)
}

var _ GenAIService = &GenAIServiceOp{}

// GenAIServiceOp interfaces with the Gen AI Service endpoints in the DigitalOcean API.
type GenAIServiceOp struct {
	client *Client
}

type genAIAgentsRoot struct {
	Agents []*Agent `json:"agents"`
	Links  *Links   `json:"links"`
	Meta   *Meta    `json:"meta"`
}

type genAIAgentRoot struct {
	Agent *Agent `json:"agent"`
}

type genAIModelsRoot struct {
	Models []*Model `json:"models"`
	Links  *Links   `json:"links"`
	Meta   *Meta    `json:"meta"`
}

type agentAPIKeysRoot struct {
	ApiKeys []*ApiKeyInfo `json:"api_key_infos"`
	Links   *Links        `json:"links"`
	Meta    *Meta         `json:"meta"`
}

type agentAPIKeyRoot struct {
	ApiKey *ApiKeyInfo `json:"api_key_info,omitempty"`
}

// Agent represents a Gen AI Agent
type Agent struct {
	AnthropicApiKey    *AnthropicApiKeyInfo      `json:"anthropic_api_key,omitempty"`
	ApiKeyInfos        []*ApiKeyInfo             `json:"api_key_infos,omitempty"`
	ApiKeys            []*ApiKey                 `json:"api_keys,omitempty"`
	ChatBot            *ChatBot                  `json:"chatbot,omitempty"`
	ChatbotIdentifiers []*AgentChatbotIdentifier `json:"chatbot_identifiers,omitempty"`
	CreatedAt          *Timestamp                `json:"created_at,omitempty"`
	ChildAgents        []*Agent                  `json:"child_agents,omitempty"`
	Deployment         *AgentDeployment          `json:"deployment,omitempty"`
	Description        string                    `json:"description,omitempty"`
	UpdatedAt          *Timestamp                `json:"updated_at,omitempty"`
	Functions          []*AgentFunction          `json:"functions,omitempty"`
	Guardrails         []*AgentGuardrail         `json:"guardrails,omitempty"`
	IfCase             string                    `json:"if_case,omitempty"`
	Instruction        string                    `json:"instruction,omitempty"`
	K                  int                       `json:"k,omitempty"`
	KnowledgeBases     []*KnowledgeBase          `json:"knowledge_bases,omitempty"`
	MaxTokens          int                       `json:"max_tokens,omitempty"`
	Model              *Model                    `json:"model,omitempty"`
	Name               string                    `json:"name,omitempty"`
	OpenAiApiKey       *OpenAiApiKey             `json:"open_ai_api_key,omitempty"`
	ParentAgents       []*Agent                  `json:"parent_agents,omitempty"`
	ProjectId          string                    `json:"project_id,omitempty"`
	Region             string                    `json:"region,omitempty"`
	RetrievalMethod    string                    `json:"retrieval_method,omitempty"`
	RouteCreatedAt     *Timestamp                `json:"route_created_at,omitempty"`
	RouteCreatedBy     string                    `json:"route_created_by,omitempty"`
	RouteUuid          string                    `json:"route_uuid,omitempty"`
	RouteName          string                    `json:"route_name,omitempty"`
	Tags               []string                  `json:"tags,omitempty"`
	Template           *AgentTemplate            `json:"template,omitempty"`
	Temperature        float64                   `json:"temperature,omitempty"`
	TopP               float64                   `json:"top_p,omitempty"`
	Url                string                    `json:"url,omitempty"`
	UserId             string                    `json:"user_id,omitempty"`
	Uuid               string                    `json:"uuid,omitempty"`
}

// AgentFunction represents a Gen AI Agent Function
type AgentFunction struct {
	ApiKey        string     `json:"api_key,omitempty"`
	CreatedAt     *Timestamp `json:"created_at,omitempty"`
	Description   string     `json:"description,omitempty"`
	GuardrailUuid string     `json:"guardrail_uuid,omitempty"`
	FaasName      string     `json:"faas_name,omitempty"`
	FaasNamespace string     `json:"faas_namespace,omitempty"`
	Name          string     `json:"name,omitempty"`
	UpdatedAt     *Timestamp `json:"updated_at,omitempty"`
	Url           string     `json:"url,omitempty"`
	Uuid          string     `json:"uuid,omitempty"`
}

// AgentGuardrail represents a Guardrail attached to Gen AI Agent
type AgentGuardrail struct {
	AgentUuid       string     `json:"agent_uuid,omitempty"`
	CreatedAt       *Timestamp `json:"created_at,omitempty"`
	DefaultResponse string     `json:"default_response,omitempty"`
	Description     string     `json:"description,omitempty"`
	GuardrailUuid   string     `json:"guardrail_uuid,omitempty"`
	IsAttached      bool       `json:"is_attached,omitempty"`
	IsDefault       bool       `json:"is_default,omitempty"`
	Name            string     `json:"name,omitempty"`
	Priority        int        `json:"priority,omitempty"`
	Type            string     `json:"type,omitempty"`
	UpdatedAt       *Timestamp `json:"updated_at,omitempty"`
	Uuid            string     `json:"uuid,omitempty"`
}

type ApiKey struct {
	ApiKey string `json:"api_key,omitempty"`
}

// AnthropicApiKeyInfo represents the Anthropic API Key information
type AnthropicApiKeyInfo struct {
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	DeletedAt *Timestamp `json:"deleted_at,omitempty"`
	Name      string     `json:"name,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
	Uuid      string     `json:"uuid,omitempty"`
}

// ApiKeyInfo represents the information of an API key
type ApiKeyInfo struct {
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	DeletedAt *Timestamp `json:"deleted_at,omitempty"`
	Name      string     `json:"name,omitempty"`
	SecretKey string     `json:"secret_key,omitempty"`
	Uuid      string     `json:"uuid,omitempty"`
}

// OpenAiApiKey represents the OpenAI API Key information
type OpenAiApiKey struct {
	CreatedAt *Timestamp `json:"created_at,omitempty"`
	CreatedBy string     `json:"created_by,omitempty"`
	DeletedAt *Timestamp `json:"deleted_at,omitempty"`
	Models    []*Model   `json:"models,omitempty"`
	Name      string     `json:"name,omitempty"`
	UpdatedAt *Timestamp `json:"updated_at,omitempty"`
	Uuid      string     `json:"uuid,omitempty"`
}

// AgentVersionUpdateRequest represents the request to update the version of an agent
type AgentVisibilityUpdateRequest struct {
	Uuid       string `json:"uuid,omitempty"`
	Visibility string `json:"visibility,omitempty"`
}

// AgentTemplate represents the template of a Gen AI Agent
type AgentTemplate struct {
	CreatedAt      *Timestamp       `json:"created_at,omitempty"`
	Instruction    string           `json:"instruction,omitempty"`
	Description    string           `json:"description,omitempty"`
	K              int              `json:"k,omitempty"`
	KnowledgeBases []*KnowledgeBase `json:"knowledge_bases,omitempty"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	Model          *Model           `json:"model,omitempty"`
	Name           string           `json:"name,omitempty"`
	Temperature    float64          `json:"temperature,omitempty"`
	TopP           float64          `json:"top_p,omitempty"`
	UpdatedAt      *Timestamp       `json:"updated_at,omitempty"`
	Uuid           string           `json:"uuid,omitempty"`
}

// KnowledgeBase represents a Gen AI Knowledge Base
type KnowledgeBase struct {
	AddedToAgentAt     *Timestamp       `json:"added_to_agent_at,omitempty"`
	CreatedAt          *Timestamp       `json:"created_at,omitempty"`
	DatabaseId         string           `json:"database_id,omitempty"`
	EmbeddingModelUuid string           `json:"embedding_model_uuid,omitempty"`
	IsPublic           bool             `json:"is_public,omitempty"`
	LastIndexingJob    *LastIndexingJob `json:"last_indexing_job,omitempty"`
	Name               string           `json:"name,omitempty"`
	ProjectId          string           `json:"project_id,omitempty"`
	Region             string           `json:"region,omitempty"`
	Tags               []string         `json:"tags,omitempty"`
	UpdatedAt          *Timestamp       `json:"updated_at,omitempty"`
	UserId             string           `json:"user_id,omitempty"`
	Uuid               string           `json:"uuid,omitempty"`
}

// LastIndexingJob represents the last indexing job description of a Gen AI Knowledge Base
type LastIndexingJob struct {
	CompletedDatasources int        `json:"completed_datasources,omitempty"`
	CreatedAt            *Timestamp `json:"created_at,omitempty"`
	DataSourceUuids      []string   `json:"data_source_uuids,omitempty"`
	FinishedAt           *Timestamp `json:"finished_at,omitempty"`
	KnowledgeBaseUuid    string     `json:"knowledge_base_uuid,omitempty"`
	Phase                string     `json:"phase,omitempty"`
	StartedAt            *Timestamp `json:"started_at,omitempty"`
	Tokens               int        `json:"tokens,omitempty"`
	TotalDatasources     int        `json:"total_datasources,omitempty"`
	UpdatedAt            *Timestamp `json:"updated_at,omitempty"`
	Uuid                 string     `json:"uuid,omitempty"`
}

type AgentChatbotIdentifier struct {
	AgentChatbotIdentifier string `json:"agent_chatbot_identifier,omitempty"`
}

// AgentDeployment represents the deployment information of a Gen AI Agent
type AgentDeployment struct {
	CreatedAt  *Timestamp `json:"created_at,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	UpdatedAt  *Timestamp `json:"updated_at,omitempty"`
	Url        string     `json:"url,omitempty"`
	Uuid       string     `json:"uuid,omitempty"`
	Visibility string     `json:"visibility,omitempty"`
}

// ChatBot represents the chatbot information of a Gen AI Agent
type ChatBot struct {
	ButtonBackgroundColor string `json:"button_background_color,omitempty"`
	Logo                  string `json:"logo,omitempty"`
	Name                  string `json:"name,omitempty"`
	PrimaryColor          string `json:"primary_color,omitempty"`
	SecondaryColor        string `json:"secondary_color,omitempty"`
	StartingMessage       string `json:"starting_message,omitempty"`
}

// Model represents a Gen AI Model
type Model struct {
	Agreement        *Agreement    `json:"agreement,omitempty"`
	CreatedAt        *Timestamp    `json:"created_at,omitempty"`
	InferenceName    string        `json:"inference_name,omitempty"`
	InferenceVersion string        `json:"inference_version,omitempty"`
	IsFoundational   bool          `json:"is_foundational,omitempty"`
	Name             string        `json:"name,omitempty"`
	ParentUuid       string        `json:"parent_uuid,omitempty"`
	Provider         string        `json:"provider,omitempty"`
	UpdatedAt        *Timestamp    `json:"updated_at,omitempty"`
	UploadComplete   bool          `json:"upload_complete,omitempty"`
	Url              string        `json:"url,omitempty"`
	Usecases         []string      `json:"usecases,omitempty"`
	Uuid             string        `json:"uuid,omitempty"`
	Version          *ModelVersion `json:"version,omitempty"`
}

// Agreement represents the agreement information of a Gen AI Model
type Agreement struct {
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	Url         string `json:"url,omitempty"`
	Uuid        string `json:"uuid,omitempty"`
}

type ModelVersion struct {
	Major int `json:"major,omitempty"`
	Minor int `json:"minor,omitempty"`
	Patch int `json:"patch,omitempty"`
}

// AgentCreateRequest represents the request to create a new Gen AI Agent
type AgentCreateRequest struct {
	AnthropicKeyUuid  string   `json:"anthropic_key_uuid,omitempty"`
	Description       string   `json:"description,omitempty"`
	Instruction       string   `json:"instruction,omitempty"`
	KnowledgeBaseUuid []string `json:"knowledge_base_uuid,omitempty"`
	ModelUuid         string   `json:"model_uuid,omitempty"`
	Name              string   `json:"name,omitempty"`
	OpenAiKeyUuid     string   `json:"open_ai_key_uuid,omitempty"`
	ProjectId         string   `json:"project_id,omitempty"`
	Region            string   `json:"region,omitempty"`
	Tags              []string `json:"tags,omitempty"`
}

// AgentAPIKeyCreateRequest represents the request to create a new Gen AI Agent API Key
type AgentAPIKeyCreateRequest struct {
	AgentUuid string `json:"agent_uuid,omitempty"`
	Name      string `json:"name,omitempty"`
}

// AgentUpdateRequest represents the request to update an existing Gen AI Agent
type AgentUpdateRequest struct {
	AnthropicKeyUuid string   `json:"anthropic_key_uuid,omitempty"`
	Description      string   `json:"description,omitempty"`
	Instruction      string   `json:"instruction,omitempty"`
	K                int      `json:"k,omitempty"`
	MaxTokens        int      `json:"max_tokens,omitempty"`
	ModelUuid        string   `json:"model_uuid,omitempty"`
	Name             string   `json:"name,omitempty"`
	OpenAiKeyUuid    string   `json:"open_ai_key_uuid,omitempty"`
	ProjectId        string   `json:"project_id,omitempty"`
	RetrievalMethod  string   `json:"retrieval_method,omitempty"`
	Region           string   `json:"region,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	Temperature      float64  `json:"temperature,omitempty"`
	TopP             float64  `json:"top_p,omitempty"`
	Uuid             string   `json:"uuid,omitempty"`
	ProvideCitations bool     `json:"provide_citations,omitempty"`
}

// AgentAPIKeyUpdateRequest represents the request to update an existing Gen AI Agent API Key
type AgentAPIKeyUpdateRequest struct {
	AgentUuid  string `json:"agent_uuid,omitempty"`
	APIKeyUuid string `json:"api_key_uuid,omitempty"`
	Name       string `json:"name,omitempty"`
}

type KnowledgeBaseCreateRequest struct {
	DatabaseID         string                    `json:"database_id"`
	DataSources        []KnowledgeBaseDataSource `json:"datasources"`
	EmbeddingModelUuid string                    `json:"embedding_model_uuid"`
	Name               string                    `json:"name"`
	ProjectID          string                    `json:"project_id"`
	Region             string                    `json:"region"`
	Tags               []string                  `json:"tags"`
	VPCUuid            string                    `json:"vpc_uuid"`
}

// KnowledgeBaseDataSource represents a Gen AI Knowledge Base Data Source
type KnowledgeBaseDataSource struct {
	CreatedAt            *Timestamp            `json:"created_at,omitempty"`
	FileUploadDataSource *FileUploadDataSource `json:"file_upload_data_source,omitempty"`
	LastIndexingJob      *LastIndexingJob      `json:"last_indexing_job,omitempty"`
	SpacesDataSource     *SpacesDataSource     `json:"spaces_data_source,omitempty"`
	UpdatedAt            *Timestamp            `json:"updated_at,omitempty"`
	Uuid                 string                `json:"uuid,omitempty"`
	WebCrawlerDataSource *WebCrawlerDataSource `json:"web_crawler_data_source,omitempty"`
}

// WebCrawlerDataSource represents the web crawler data source information
type WebCrawlerDataSource struct {
	BaseUrl        string `json:"base_url,omitempty"`
	CrawlingOption string `json:"crawling_option,omitempty"`
	EmbedMedia     bool   `json:"embed_media,omitempty"`
}

// SpacesDataSource represents the spaces data source information
type SpacesDataSource struct {
	BucketName string `json:"bucket_name,omitempty"`
	ItemPath   string `json:"item_path,omitempty"`
	Region     string `json:"region,omitempty"`
}

// FileUploadDataSource represents the file upload data source information
type FileUploadDataSource struct {
	OriginalFileName string `json:"original_file_name,omitempty"`
	Size             string `json:"size_in_bytes,omitempty"`
	StoredObjectKey  string `json:"stored_object_key,omitempty"`
}

type KnowledgeBaseDataSourcesRoot struct {
	KnowledgeBaseDatasources []KnowledgeBaseDataSource `json:"knowledge_base_data_sources"`
	Links                    *Links                    `json:"links"`
	Meta                     *Meta                     `json:"meta"`
}
type SingleKnowledgeBaseDataSourceRoot struct {
	KnowledgeBaseDatasource *KnowledgeBaseDataSource `json:"knowledge_base_data_source"`
	Links                   *Links                   `json:"links"`
	Meta                    *Meta                    `json:"meta"`
}
type knowledgebasesRoot struct {
	KnowledgeBases []KnowledgeBase `json:"knowledge_bases"`
	Links          *Links          `json:"links"`
	Meta           *Meta           `json:"meta"`
}

type knowledgebaseRoot struct {
	KnowledgeBase  *KnowledgeBase `json:"knowledge_base"`
	DatabaseStatus string         `json:"database_status,omitempty"`
}

type DeleteDataSourceRoot struct {
	DataSourceUuid    string `json:"data_source_uuid"`
	KnowledgeBaseUuid string `json:"knowledge_base_uuid"`
}

type DeleteKnowledgeBaseRoot struct {
	KnowledgeBaseUuid string `json:"uuid"`
}

type DeletedKnowledgeBaseResponse struct {
	DataSourceUuid    string `json:"data_source_uuid"`
	KnowledgeBaseUuid string `json:"knowledge_base_uuid"`
}

type AddKnowledgeBaseDataSourceRequest struct {
	KnowledgeBaseUuid    string                `json:"knowledge_base_uuid"`
	SpacesDataSource     *SpacesDataSource     `json:"spaces_data_source"`
	WebCrawlerDataSource *WebCrawlerDataSource `json:"web_crawler_data_source"`
}

type UpdateKnowledgeBaseRequest struct {
	DatabaseID         string   `json:"database_id"`
	EmbeddingModelUuid string   `json:"embedding_model_uuid"`
	Name               string   `json:"name"`
	ProjectID          string   `json:"project_id"`
	Tags               []string `json:"tags"`
	KnowledgeBaseUUID  string   `json:"uuid"`
}

type genAIAgentKBRoot struct {
	Agent *Agent `json:"agent"`
}

// ListAgents returns a list of Gen AI Agents
func (s *GenAIServiceOp) ListAgents(ctx context.Context, opt *ListOptions) ([]*Agent, *Response, error) {
	path, err := addOptions(genAIBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentsRoot)
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
	return root.Agents, resp, nil
}

// CreateAgent creates a new Gen AI Agent by providing the AgentCreateRequest object
func (s *GenAIServiceOp) CreateAgent(ctx context.Context, create *AgentCreateRequest) (*Agent, *Response, error) {
	path := genAIBasePath
	if create.ProjectId == "" {
		return nil, nil, fmt.Errorf("Project ID is required")
	}
	if create.Region == "" {
		return nil, nil, fmt.Errorf("Region is required")
	}
	if create.Instruction == "" {
		return nil, nil, fmt.Errorf("Instruction is required")
	}
	if create.ModelUuid == "" {
		return nil, nil, fmt.Errorf("ModelUuid is required")
	}
	if create.Name == "" {
		return nil, nil, fmt.Errorf("Name is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentRoot)
	resp, err := s.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// ListAgentAPIKeys retrieves list of API Keys associated with the specified GenAI agent
func (s *GenAIServiceOp) ListAgentAPIKeys(ctx context.Context, agentId string, opt *ListOptions) ([]*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys", genAIBasePath, agentId)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentAPIKeysRoot)
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

	return root.ApiKeys, resp, nil
}

// CreateAgentAPIKey creates a new API key for the specified GenAI agent
func (s *GenAIServiceOp) CreateAgentAPIKey(ctx context.Context, agentId string, createRequest *AgentAPIKeyCreateRequest) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys", genAIBasePath, agentId)

	createRequest.AgentUuid = agentId

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.ApiKey, resp, err
}

// UpdateAgentAPIKey updates an existing API key for the specified GenAI agent
func (s *GenAIServiceOp) UpdateAgentAPIKey(ctx context.Context, agentId, apiKeyId string, updateRequest *AgentAPIKeyUpdateRequest) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s", genAIBasePath, agentId, apiKeyId)

	updateRequest.AgentUuid = agentId

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.ApiKey, resp, nil
}

// DeleteAgentAPIKey deletes an existing API key for the specified GenAI agent
func (s *GenAIServiceOp) DeleteAgentAPIKey(ctx context.Context, agentId, apiKeyId string) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s", genAIBasePath, agentId, apiKeyId)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.ApiKey, resp, nil
}

// RegenerateAgentAPIKey regenerates an API key for the specified GenAI agent
func (s *GenAIServiceOp) RegenerateAgentAPIKey(ctx context.Context, agentId, apiKeyId string) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s/regenerate", genAIBasePath, agentId, apiKeyId)

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.ApiKey, resp, nil
}

// GetAgent returns the details of a Gen AI Agent based on the Agent UUID
func (s *GenAIServiceOp) GetAgent(ctx context.Context, id string) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", genAIBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// UpdateAgent function updates a Gen AI Agent properties for the given UUID
func (s *GenAIServiceOp) UpdateAgent(ctx context.Context, id string, update *AgentUpdateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", genAIBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// DeleteAgent function deletes a Gen AI Agent by its corresponding UUID
func (s *GenAIServiceOp) DeleteAgent(ctx context.Context, id string) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", genAIBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// UpdateAgentVisibility function updates a Gen AI Agent status by changing visibility to public or private.
func (s *GenAIServiceOp) UpdateAgentVisibility(ctx context.Context, id string, update *AgentVisibilityUpdateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s/deployment_visibility", genAIBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// ListModels function returns a list of Gen AI Models
func (s *GenAIServiceOp) ListModels(ctx context.Context, opt *ListOptions) ([]*Model, *Response, error) {
	path, err := addOptions(agentModelBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(genAIModelsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}

	return root.Models, resp, nil
}

// List all knowledge bases
func (s *GenAIServiceOp) ListKnowledgeBases(ctx context.Context, opt *ListOptions) ([]KnowledgeBase, *Response, error) {

	path := KnowledgeBasePath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(knowledgebasesRoot)
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
	return root.KnowledgeBases, resp, err
}

// Create a knowledge base
func (s *GenAIServiceOp) CreateKnowledgeBase(ctx context.Context, knowledgeBaseCreate *KnowledgeBaseCreateRequest) (*KnowledgeBase, *Response, error) {

	path := KnowledgeBasePath

	if knowledgeBaseCreate.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if strings.Contains(knowledgeBaseCreate.Name, " ") {
		return nil, nil, fmt.Errorf("name cannot contain spaces")
	}
	if len(knowledgeBaseCreate.DataSources) == 0 {
		return nil, nil, fmt.Errorf("at least one datasource is required")
	}
	// TODO: Remove this check when additional regions are supported.
	if knowledgeBaseCreate.Region == "" {
		knowledgeBaseCreate.Region = "tor1"
	}
	if knowledgeBaseCreate.Region != "tor1" {
		return nil, nil, fmt.Errorf("currently only region 'tor1' is supported")
	}

	if knowledgeBaseCreate.EmbeddingModelUuid == "" {
		return nil, nil, fmt.Errorf("EmbeddingModelUuid ID is required")
	}
	if knowledgeBaseCreate.ProjectID == "" {
		return nil, nil, fmt.Errorf("Project ID is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, knowledgeBaseCreate)
	if err != nil {
		return nil, nil, err
	}
	root := new(knowledgebaseRoot)
	resp, err := s.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.KnowledgeBase, resp, err
}

// List Data Sources for a Knowledge Base
func (s *GenAIServiceOp) ListKnowledgeBaseDataSources(ctx context.Context, knowledgeBaseID string, opt *ListOptions) ([]KnowledgeBaseDataSource, *Response, error) {

	path := fmt.Sprintf(KnowledgeBaseDataSourcesPath, knowledgeBaseID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(KnowledgeBaseDataSourcesRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}
	return root.KnowledgeBaseDatasources, resp, err
}

// Add Data Source to a Knowledge Base
func (s *GenAIServiceOp) AddKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, addDataSource *AddKnowledgeBaseDataSourceRequest) (*KnowledgeBaseDataSource, *Response, error) {
	path := fmt.Sprintf(KnowledgeBaseDataSourcesPath, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, addDataSource)
	if err != nil {
		return nil, nil, err
	}
	root := new(SingleKnowledgeBaseDataSourceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.KnowledgeBaseDatasource, resp, err
}

// Deletes data source from a knowledge base
func (s *GenAIServiceOp) DeleteKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, dataSourceID string) (string, string, *Response, error) {

	path := fmt.Sprintf(DeleteDataSourcePath, knowledgeBaseID, dataSourceID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)

	if err != nil {
		return "", "", nil, err
	}

	root := new(DeleteDataSourceRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return "", "", resp, err

	}
	return root.KnowledgeBaseUuid, root.DataSourceUuid, resp, nil
}

// Get information about a KnowledgeBase and its Database status
// Database status can be "CREATING","ONLINE","POWEROFF","REBUILDING","REBALANCING","DECOMMISSIONED","FORKING","MIGRATING","RESIZING","RESTORING","POWERING_ON","UNHEALTHY"
func (s *GenAIServiceOp) GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBase, string, *Response, error) {
	path := fmt.Sprintf(GetKnowledgeBaseByIDPath, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)

	if err != nil {
		return nil, "", nil, err
	}
	root := new(knowledgebaseRoot)
	resp, err := s.client.Do(ctx, req, root)

	if err != nil {
		return nil, "", resp, err
	}
	return root.KnowledgeBase, root.DatabaseStatus, resp, nil
}

// Update a knowledge base
func (s *GenAIServiceOp) UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, update *UpdateKnowledgeBaseRequest) (*KnowledgeBase, *Response, error) {
	path := fmt.Sprintf(UpdateKnowledgeBaseByIDPath, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(knowledgebaseRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.KnowledgeBase, resp, nil
}

// Deletes a knowledge base by its corresponding UUID and returns the UUID of the deleted knowledge base
func (s *GenAIServiceOp) DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) (string, *Response, error) {

	path := fmt.Sprintf(DeleteKnowledgeBaseByIDPath, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	fmt.Print(path)
	if err != nil {
		return "", nil, err
	}
	root := new(DeleteKnowledgeBaseRoot)
	resp, err := s.client.Do(ctx, req, root)

	if err != nil {
		return "", resp, err
	}
	return root.KnowledgeBaseUuid, resp, nil
}

// Attach a knowledge base to an agent
func (s *GenAIServiceOp) AttachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error) {

	path := fmt.Sprintf(AgentKnowledgeBasePath, agentID, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(genAIAgentKBRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// Detach a knowledge base from an agent
func (s *GenAIServiceOp) DetachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error) {

	path := fmt.Sprintf(AgentKnowledgeBasePath, agentID, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(genAIAgentKBRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Agent, resp, nil
}

func (a Agent) String() string {
	return Stringify(a)
}

func (m Model) String() string {
	return Stringify(m)
}

func (a KnowledgeBase) String() string {
	return Stringify(a)
}

func (a KnowledgeBaseDataSource) String() string {
	return Stringify(a)
}

func (a ApiKeyInfo) String() string {
	return Stringify(a)
}
