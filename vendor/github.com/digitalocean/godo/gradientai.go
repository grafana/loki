package godo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	gradientBasePath             = "/v2/gen-ai/agents"
	agentModelBasePath           = "/v2/gen-ai/models"
	datacenterRegionsPath        = "/v2/gen-ai/regions"
	agentRouteBasePath           = gradientBasePath + "/%s/child_agents/%s"
	KnowledgeBasePath            = "/v2/gen-ai/knowledge_bases"
	functionRouteBasePath        = gradientBasePath + "/%s/functions"
	KnowledgeBaseDataSourcesPath = KnowledgeBasePath + "/%s/data_sources"
	GetKnowledgeBaseByIDPath     = KnowledgeBasePath + "/%s"
	UpdateKnowledgeBaseByIDPath  = KnowledgeBasePath + "/%s"
	DeleteKnowledgeBaseByIDPath  = KnowledgeBasePath + "/%s"
	AgentKnowledgeBasePath       = "/v2/gen-ai/agents" + "/%s/knowledge_bases/%s"
	DeleteDataSourcePath         = KnowledgeBasePath + "/%s/data_sources/%s"
	IndexingJobsPath             = "/v2/gen-ai/indexing_jobs"
	IndexingJobByIDPath          = IndexingJobsPath + "/%s"
	IndexingJobCancelPath        = IndexingJobsPath + "/%s/cancel"
	IndexingJobDataSourcesPath   = IndexingJobsPath + "/%s/data_sources"
	AnthropicAPIKeysPath         = "/v2/gen-ai/anthropic/keys"
	AnthropicAPIKeyByIDPath      = AnthropicAPIKeysPath + "/%s"
	OpenAIAPIKeysPath            = "/v2/gen-ai/openai/keys"
	UpdateFunctionRoutePath      = functionRouteBasePath + "/%s"
	DeleteFunctionRoutePath      = functionRouteBasePath + "/%s"
)

// GradientAIService is an interface for interfacing with the Gradient AI Agent endpoints
// of the DigitalOcean API.
// See https://docs.digitalocean.com/reference/api/digitalocean/#tag/GradientAI-Platform for more details.
type GradientAIService interface {
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
	ListKnowledgeBases(ctx context.Context, opt *ListOptions) ([]KnowledgeBase, *Response, error)
	CreateKnowledgeBase(ctx context.Context, knowledgeBaseCreate *KnowledgeBaseCreateRequest) (*KnowledgeBase, *Response, error)
	ListKnowledgeBaseDataSources(ctx context.Context, knowledgeBaseID string, opt *ListOptions) ([]KnowledgeBaseDataSource, *Response, error)
	AddKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, addDataSource *AddKnowledgeBaseDataSourceRequest) (*KnowledgeBaseDataSource, *Response, error)
	DeleteKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, dataSourceID string) (string, string, *Response, error)
	GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBase, string, *Response, error)
	UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, update *UpdateKnowledgeBaseRequest) (*KnowledgeBase, *Response, error)
	DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) (string, *Response, error)
	ListIndexingJobs(ctx context.Context, opt *ListOptions) (*IndexingJobsResponse, *Response, error)
	GetIndexingJob(ctx context.Context, indexingJobUUID string) (*IndexingJobResponse, *Response, error)
	CancelIndexingJob(ctx context.Context, indexingJobUUID string) (*IndexingJobResponse, *Response, error)
	ListIndexingJobDataSources(ctx context.Context, indexingJobUUID string) (*IndexingJobDataSourcesResponse, *Response, error)
	AttachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error)
	DetachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error)
	AddAgentRoute(context.Context, string, string, *AgentRouteCreateRequest) (*AgentRouteResponse, *Response, error)
	UpdateAgentRoute(context.Context, string, string, *AgentRouteUpdateRequest) (*AgentRouteResponse, *Response, error)
	DeleteAgentRoute(context.Context, string, string) (*AgentRouteResponse, *Response, error)
	ListAgentVersions(context.Context, string, *ListOptions) ([]*AgentVersion, *Response, error)
	RollbackAgentVersion(context.Context, string, string) (string, *Response, error)
	ListAnthropicAPIKeys(context.Context, *ListOptions) ([]*AnthropicApiKeyInfo, *Response, error)
	CreateAnthropicAPIKey(ctx context.Context, anthropicAPIKeyCreateRequest *AnthropicAPIKeyCreateRequest) (*AnthropicApiKeyInfo, *Response, error)
	GetAnthropicAPIKey(ctx context.Context, id string) (*AnthropicApiKeyInfo, *Response, error)
	UpdateAnthropicAPIKey(ctx context.Context, id string, anthropicAPIKeyUpdateRequest *AnthropicAPIKeyUpdateRequest) (*AnthropicApiKeyInfo, *Response, error)
	DeleteAnthropicAPIKey(ctx context.Context, id string) (*AnthropicApiKeyInfo, *Response, error)
	ListAgentsByAnthropicAPIKey(ctx context.Context, id string, opt *ListOptions) ([]*Agent, *Response, error)
	ListOpenAIAPIKeys(context.Context, *ListOptions) ([]*OpenAiApiKey, *Response, error)
	CreateOpenAIAPIKey(ctx context.Context, openaiAPIKeyCreate *OpenAIAPIKeyCreateRequest) (*OpenAiApiKey, *Response, error)
	GetOpenAIAPIKey(ctx context.Context, openaiApiKeyId string) (*OpenAiApiKey, *Response, error)
	UpdateOpenAIAPIKey(ctx context.Context, openaiApiKeyId string, openaiAPIKeyUpdate *OpenAIAPIKeyUpdateRequest) (*OpenAiApiKey, *Response, error)
	DeleteOpenAIAPIKey(ctx context.Context, openaiApiKeyId string) (*OpenAiApiKey, *Response, error)
	ListAgentsByOpenAIAPIKey(ctx context.Context, openaiApiKeyId string, opt *ListOptions) ([]*Agent, *Response, error)
	CreateFunctionRoute(context.Context, string, *FunctionRouteCreateRequest) (*Agent, *Response, error)
	DeleteFunctionRoute(context.Context, string, string) (*Agent, *Response, error)
	UpdateFunctionRoute(context.Context, string, string, *FunctionRouteUpdateRequest) (*Agent, *Response, error)
	ListAvailableModels(context.Context, *ListOptions) ([]*Model, *Response, error)
	ListDatacenterRegions(context.Context, *bool, *bool) ([]*DatacenterRegions, *Response, error)
}

var _ GradientAIService = &GradientAIServiceOp{}

// GradientAIServiceOp interfaces with the Gradient AI Service endpoints in the DigitalOcean API.
type GradientAIServiceOp struct {
	client *Client
}

type gradientAgentsRoot struct {
	Agents []*Agent `json:"agents"`
	Links  *Links   `json:"links"`
	Meta   *Meta    `json:"meta"`
}

type gradientAgentRoot struct {
	Agent *Agent `json:"agent"`
}

type gradientModelsRoot struct {
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

type agentVersionsRoot struct {
	AgentVersions []*AgentVersion `json:"agent_versions,omitempty"`
	Links         *Links          `json:"links,omitempty"`
	Meta          *Meta           `json:"meta,omitempty"`
}

type anthropicAPIKeysRoot struct {
	AnthropicApiKeys []*AnthropicApiKeyInfo `json:"api_key_infos"`
	Links            *Links                 `json:"links"`
	Meta             *Meta                  `json:"meta"`
}

type openaiAPIKeysRoot struct {
	OpenAIApiKeys []*OpenAiApiKey `json:"api_key_infos"`
	Links         *Links          `json:"links"`
	Meta          *Meta           `json:"meta"`
}

type anthropicAPIKeyRoot struct {
	AnthropicApiKey *AnthropicApiKeyInfo `json:"api_key_info,omitempty"`
}
type openaiAPIKeyRoot struct {
	OpenAIAPIKey *OpenAiApiKey `json:"api_key_info,omitempty"`
}

// Agent represents a Gradient AI Agent
type Agent struct {
	AnthropicApiKey         *AnthropicApiKeyInfo      `json:"anthropic_api_key,omitempty"`
	ApiKeyInfos             []*ApiKeyInfo             `json:"api_key_infos,omitempty"`
	ApiKeys                 []*ApiKey                 `json:"api_keys,omitempty"`
	ChatBot                 *ChatBot                  `json:"chatbot,omitempty"`
	ChatbotIdentifiers      []*AgentChatbotIdentifier `json:"chatbot_identifiers,omitempty"`
	CreatedAt               *Timestamp                `json:"created_at,omitempty"`
	ChildAgents             []*Agent                  `json:"child_agents,omitempty"`
	ConversationLogsEnabled bool                      `json:"conversation_logs_enabled,omitempty"`
	Deployment              *AgentDeployment          `json:"deployment,omitempty"`
	Description             string                    `json:"description,omitempty"`
	Functions               []*AgentFunction          `json:"functions,omitempty"`
	Guardrails              []*AgentGuardrail         `json:"guardrails,omitempty"`
	IfCase                  string                    `json:"if_case,omitempty"`
	Instruction             string                    `json:"instruction,omitempty"`
	K                       int                       `json:"k,omitempty"`
	KnowledgeBases          []*KnowledgeBase          `json:"knowledge_bases,omitempty"`
	MaxTokens               int                       `json:"max_tokens,omitempty"`
	Model                   *Model                    `json:"model,omitempty"`
	Name                    string                    `json:"name,omitempty"`
	OpenAiApiKey            *OpenAiApiKey             `json:"open_ai_api_key,omitempty"`
	ParentAgents            []*Agent                  `json:"parent_agents,omitempty"`
	ProjectId               string                    `json:"project_id,omitempty"`
	ProvideCitations        bool                      `json:"provide_citations,omitempty"`
	Region                  string                    `json:"region,omitempty"`
	RetrievalMethod         string                    `json:"retrieval_method,omitempty"`
	RouteCreatedAt          *Timestamp                `json:"route_created_at,omitempty"`
	RouteCreatedBy          string                    `json:"route_created_by,omitempty"`
	RouteUuid               string                    `json:"route_uuid,omitempty"`
	RouteName               string                    `json:"route_name,omitempty"`
	Tags                    []string                  `json:"tags,omitempty"`
	Template                *AgentTemplate            `json:"template,omitempty"`
	Temperature             float64                   `json:"temperature,omitempty"`
	TopP                    float64                   `json:"top_p,omitempty"`
	UpdatedAt               *Timestamp                `json:"updated_at,omitempty"`
	Url                     string                    `json:"url,omitempty"`
	UserId                  string                    `json:"user_id,omitempty"`
	Uuid                    string                    `json:"uuid,omitempty"`
	VersionHash             string                    `json:"version_hash,omitempty"`
	VPCEgressIPs            []string                  `json:"vpc_egress_ips,omitempty"`
	VPCUuid                 string                    `json:"vpc_uuid,omitempty"`
	Workspace               Workspace                 `json:"workspace,omitempty"`
}

// AgentVersion represents a version of a Gradient AI Agent
type AgentVersion struct {
	AgentUuid              string                `json:"agent_uuid,omitempty"`
	AttachedChildAgents    []*AttachedChildAgent `json:"attached_child_agents,omitempty"`
	AttachedFunctions      []*AgentFunction      `json:"attached_functions,omitempty"`
	AttachedGuardrails     []*AgentGuardrail     `json:"attached_guardrails,omitempty"`
	AttachedKnowledgeBases []*KnowledgeBase      `json:"attached_knowledgebases,omitempty"`
	CanRollback            bool                  `json:"can_rollback,omitempty"`
	CreatedAt              *Timestamp            `json:"created_at,omitempty"`
	CreatedByEmail         string                `json:"created_by_email,omitempty"`
	CurrentlyApplied       bool                  `json:"currently_applied,omitempty"`
	Description            string                `json:"description,omitempty"`
	ID                     string                `json:"id,omitempty"`
	Instruction            string                `json:"instruction,omitempty"`
	K                      int64                 `json:"k,omitempty"`
	MaxTokens              int64                 `json:"max_tokens,omitempty"`
	ModelName              string                `json:"model_name,omitempty"`
	Name                   string                `json:"name,omitempty"`
	ProvideCitations       bool                  `json:"provide_citations,omitempty"`
	RetrievalMethod        string                `json:"retrieval_method,omitempty"`
	Tags                   []string              `json:"tags,omitempty"`
	Temperature            float64               `json:"temperature,omitempty"`
	TopP                   float64               `json:"top_p,omitempty"`
	TriggerAction          string                `json:"trigger_action,omitempty"`
	VersionHash            string                `json:"version_hash,omitempty"`
}

type AttachedChildAgent struct {
	AgentName      string `json:"agent_name,omitempty"`
	ChildAgentUuid string `json:"child_agent_uuid,omitempty"`
	IfCase         string `json:"if_case,omitempty"`
	IsDeleted      bool   `json:"is_deleted,omitempty"`
	RouteName      string `json:"route_name,omitempty"`
}

type auditResponse struct {
	AuditHeader AuditHeader `json:"audit_header,omitempty"`
	VersionHash string      `json:"version_hash,omitempty"`
}

// AuditHeader represents audit metadata for an action.
type AuditHeader struct {
	ActorID           string `json:"actor_id,omitempty"`
	ActorIP           string `json:"actor_ip,omitempty"`
	ActorUUID         string `json:"actor_uuid,omitempty"`
	ContextURN        string `json:"context_urn,omitempty"`
	OriginApplication string `json:"origin_application,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	UserUUID          string `json:"user_uuid,omitempty"`
}

// RollbackVersionRequest represents the request to rollback a Gradient AI Agent to a previous version
type RollbackVersionRequest struct {
	AgentUuid   string `json:"uuid,omitempty"`
	VersionHash string `json:"version_hash,omitempty"`
}

// AgentFunction represents a Gradient AI Agent Function
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
	IsDeleted     bool       `json:"is_deleted,omitempty"`
}

// AgentGuardrail represents a Guardrail attached to Gradient AI Agent
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
	IsDeleted       bool       `json:"is_deleted,omitempty"`
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

// AgentTemplate represents the template of a Gradient AI Agent
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

// EvaluationMetricType represents the type of evaluation metric.
type EvaluationMetricType string

const (
	MetricTypeUnspecified    EvaluationMetricType = "METRIC_TYPE_UNSPECIFIED"
	MetricTypeGeneralQuality EvaluationMetricType = "METRIC_TYPE_GENERAL_QUALITY"
	MetricTypeRAGAndTool     EvaluationMetricType = "METRIC_TYPE_RAG_AND_TOOL"
)

// EvaluationMetricValueType represents the value type of an evaluation metric.
type EvaluationMetricValueType string

const (
	MetricValueTypeUnspecified EvaluationMetricValueType = "METRIC_VALUE_TYPE_UNSPECIFIED"
	MetricValueTypeNumber      EvaluationMetricValueType = "METRIC_VALUE_TYPE_NUMBER"
	MetricValueTypeString      EvaluationMetricValueType = "METRIC_VALUE_TYPE_STRING"
	MetricValueTypePercentage  EvaluationMetricValueType = "METRIC_VALUE_TYPE_PERCENTAGE"
)

// EvaluationMetricCategory represents the category of an evaluation metric.
type EvaluationMetricCategory string

const (
	MetricCategoryUnspecified       EvaluationMetricCategory = "METRIC_CATEGORY_UNSPECIFIED"
	MetricCategoryCorrectness       EvaluationMetricCategory = "METRIC_CATEGORY_CORRECTNESS"
	MetricCategoryUserOutcomes      EvaluationMetricCategory = "METRIC_CATEGORY_USER_OUTCOMES"
	MetricCategorySafetyAndSecurity EvaluationMetricCategory = "METRIC_CATEGORY_SAFETY_AND_SECURITY"
	MetricCategoryContextQuality    EvaluationMetricCategory = "METRIC_CATEGORY_CONTEXT_QUALITY"
	MetricCategoryModelFit          EvaluationMetricCategory = "METRIC_CATEGORY_MODEL_FIT"
)

// Workspace represents a workspace containing agents and evaluation test cases.
type Workspace struct {
	UUID                string                `json:"uuid,omitempty"`
	Name                string                `json:"name,omitempty"`
	Description         string                `json:"description,omitempty"`
	CreatedByEmail      string                `json:"created_by_email,omitempty"`
	CreatedBy           string                `json:"created_by,omitempty"`
	CreatedAt           *Timestamp            `json:"created_at,omitempty"`
	UpdatedAt           *Timestamp            `json:"updated_at,omitempty"`
	DeletedAt           *Timestamp            `json:"deleted_at,omitempty"`
	Agents              []*Agent              `json:"agents,omitempty"`
	EvaluationTestCases []*EvaluationTestCase `json:"evaluation_test_cases,omitempty"`
}

// EvaluationTestCase represents an evaluation test case configuration.
type EvaluationTestCase struct {
	TestCaseUUID              string              `json:"test_case_uuid,omitempty"`
	Name                      string              `json:"name,omitempty"`
	Description               string              `json:"description,omitempty"`
	Version                   uint32              `json:"version,omitempty"`
	DatasetUUID               string              `json:"dataset_uuid,omitempty"` // Deprecated
	DatasetName               string              `json:"dataset_name,omitempty"` // Deprecated
	Metrics                   []*EvaluationMetric `json:"metrics,omitempty"`
	StarMetric                *StarMetric         `json:"star_metric,omitempty"`
	TotalRuns                 int32               `json:"total_runs,omitempty"`
	LatestVersionNumberOfRuns int32               `json:"latest_version_number_of_runs,omitempty"`
	UpdatedByUserID           uint64              `json:"updated_by_user_id,omitempty"`
	UpdatedByUserEmail        string              `json:"updated_by_user_email,omitempty"`
	CreatedByUserEmail        string              `json:"created_by_user_email,omitempty"`
	CreatedByUserID           uint64              `json:"created_by_user_id,omitempty"`
	CreatedAt                 *Timestamp          `json:"created_at,omitempty"`
	UpdatedAt                 *Timestamp          `json:"updated_at,omitempty"`
	ArchivedAt                *Timestamp          `json:"archived_at,omitempty"`
	Dataset                   *EvaluationDataset  `json:"dataset,omitempty"`
}

// EvaluationMetric represents an evaluation metric definition.
type EvaluationMetric struct {
	MetricUUID      string                    `json:"metric_uuid,omitempty"`
	MetricName      string                    `json:"metric_name,omitempty"`
	Description     string                    `json:"description,omitempty"`
	MetricType      EvaluationMetricType      `json:"metric_type,omitempty"`
	MetricValueType EvaluationMetricValueType `json:"metric_value_type,omitempty"`
	RangeMin        float32                   `json:"range_min,omitempty"`
	RangeMax        float32                   `json:"range_max,omitempty"`
	Inverted        bool                      `json:"inverted,omitempty"`
	Category        EvaluationMetricCategory  `json:"category,omitempty"`
	IsMetricGoal    bool                      `json:"is_metric_goal,omitempty"`
	MetricRank      uint32                    `json:"metric_rank,omitempty"`
}

// EvaluationDataset represents the dataset information for an evaluation.
type EvaluationDataset struct {
	DatasetUUID    string     `json:"dataset_uuid,omitempty"`
	DatasetName    string     `json:"dataset_name,omitempty"`
	RowCount       uint32     `json:"row_count,omitempty"`
	HasGroundTruth bool       `json:"has_ground_truth,omitempty"`
	FileSize       uint64     `json:"file_size,omitempty"`
	CreatedAt      *Timestamp `json:"created_at,omitempty"`
}

// StarMetric represents a star metric configuration.
type StarMetric struct {
	MetricUUID          string   `json:"metric_uuid,omitempty"`
	Name                string   `json:"name,omitempty"`
	SuccessThresholdPct *int32   `json:"success_threshold_pct,omitempty"` // Deprecated
	SuccessThreshold    *float32 `json:"success_threshold,omitempty"`
}

// KnowledgeBase represents a Gradient AI Knowledge Base
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
	IsDeleted          bool             `json:"is_deleted,omitempty"`
}

// LastIndexingJob represents the last indexing job description of a Gradient AI Knowledge Base
type LastIndexingJob struct {
	CompletedDatasources int        `json:"completed_datasources,omitempty"`
	CreatedAt            *Timestamp `json:"created_at,omitempty"`
	DataSourceUuids      []string   `json:"data_source_uuids,omitempty"`
	FinishedAt           *Timestamp `json:"finished_at,omitempty"`
	KnowledgeBaseUuid    string     `json:"knowledge_base_uuid,omitempty"`
	Phase                string     `json:"phase,omitempty"`
	StartedAt            *Timestamp `json:"started_at,omitempty"`
	Status               string     `json:"status,omitempty"`
	Tokens               int        `json:"tokens,omitempty"`
	TotalDatasources     int        `json:"total_datasources,omitempty"`
	TotalItemsFailed     string     `json:"total_items_failed,omitempty"`
	TotalItemsIndexed    string     `json:"total_items_indexed,omitempty"`
	TotalItemsSkipped    string     `json:"total_items_skipped,omitempty"`
	UpdatedAt            *Timestamp `json:"updated_at,omitempty"`
	Uuid                 string     `json:"uuid,omitempty"`
}

// IndexingJobsResponse represents the response from listing indexing jobs
type IndexingJobsResponse struct {
	Jobs  []LastIndexingJob `json:"jobs"`
	Links *Links            `json:"links,omitempty"`
	Meta  *Meta             `json:"meta,omitempty"`
}

// IndexingJobResponse represents the response from retrieving a single indexing job
type IndexingJobResponse struct {
	Job LastIndexingJob `json:"job"`
}

// CancelIndexingJobRequest represents the request payload for cancelling an indexing job
type CancelIndexingJobRequest struct {
	UUID string `json:"uuid"`
}

// IndexedDataSource represents a data source within an indexing job
type IndexedDataSource struct {
	CompletedAt       *Timestamp `json:"completed_at,omitempty"`
	DataSourceUuid    string     `json:"data_source_uuid,omitempty"`
	ErrorDetails      string     `json:"error_details,omitempty"`
	ErrorMsg          string     `json:"error_msg,omitempty"`
	FailedItemCount   string     `json:"failed_item_count,omitempty"`
	IndexedFileCount  string     `json:"indexed_file_count,omitempty"`
	IndexedItemCount  string     `json:"indexed_item_count,omitempty"`
	RemovedItemCount  string     `json:"removed_item_count,omitempty"`
	SkippedItemCount  string     `json:"skipped_item_count,omitempty"`
	StartedAt         *Timestamp `json:"started_at,omitempty"`
	Status            string     `json:"status,omitempty"`
	TotalBytes        string     `json:"total_bytes,omitempty"`
	TotalBytesIndexed string     `json:"total_bytes_indexed,omitempty"`
	TotalFileCount    string     `json:"total_file_count,omitempty"`
}

// IndexingJobDataSourcesResponse represents the response from listing data sources for an indexing job
type IndexingJobDataSourcesResponse struct {
	IndexedDataSources []IndexedDataSource `json:"indexed_data_sources"`
}

type AgentChatbotIdentifier struct {
	AgentChatbotIdentifier string `json:"agent_chatbot_identifier,omitempty"`
}

// AgentDeployment represents the deployment information of a Gradient AI Agent
type AgentDeployment struct {
	CreatedAt  *Timestamp `json:"created_at,omitempty"`
	Name       string     `json:"name,omitempty"`
	Status     string     `json:"status,omitempty"`
	UpdatedAt  *Timestamp `json:"updated_at,omitempty"`
	Url        string     `json:"url,omitempty"`
	Uuid       string     `json:"uuid,omitempty"`
	Visibility string     `json:"visibility,omitempty"`
}

// ChatBot represents the chatbot information of a Gradient AI Agent
type ChatBot struct {
	ButtonBackgroundColor string `json:"button_background_color,omitempty"`
	Logo                  string `json:"logo,omitempty"`
	Name                  string `json:"name,omitempty"`
	PrimaryColor          string `json:"primary_color,omitempty"`
	SecondaryColor        string `json:"secondary_color,omitempty"`
	StartingMessage       string `json:"starting_message,omitempty"`
}

// Model represents a Gradient AI Model
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

// Agreement represents the agreement information of a Gradient AI Model
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

// AgentCreateRequest represents the request to create a new Gradient AI Agent
type AgentCreateRequest struct {
	AnthropicKeyUuid     string   `json:"anthropic_key_uuid,omitempty"`
	Description          string   `json:"description,omitempty"`
	Instruction          string   `json:"instruction,omitempty"`
	KnowledgeBaseUuid    []string `json:"knowledge_base_uuid,omitempty"`
	ModelProviderKeyUuid string   `json:"model_provider_key_uuid,omitempty"`
	ModelUuid            string   `json:"model_uuid,omitempty"`
	Name                 string   `json:"name,omitempty"`
	OpenAiKeyUuid        string   `json:"open_ai_key_uuid,omitempty"`
	ProjectId            string   `json:"project_id,omitempty"`
	Region               string   `json:"region,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	WorkspaceUuid        string   `json:"workspace_uuid,omitempty"`
}

// AgentAPIKeyCreateRequest represents the request to create a new Gradient AI Agent API Key
type AgentAPIKeyCreateRequest struct {
	AgentUuid string `json:"agent_uuid,omitempty"`
	Name      string `json:"name,omitempty"`
}

// AgentUpdateRequest represents the request to update an existing Gradient AI Agent
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

// AgentAPIKeyUpdateRequest represents the request to update an existing Gradient AI Agent API Key
type AgentAPIKeyUpdateRequest struct {
	AgentUuid  string `json:"agent_uuid,omitempty"`
	APIKeyUuid string `json:"api_key_uuid,omitempty"`
	Name       string `json:"name,omitempty"`
}

type AnthropicAPIKeyCreateRequest struct {
	Name   string `json:"name,omitempty"`
	ApiKey string `json:"api_key,omitempty"`
}

type AnthropicAPIKeyUpdateRequest struct {
	Name       string `json:"name,omitempty"`
	ApiKey     string `json:"api_key,omitempty"`
	ApiKeyUuid string `json:"api_key_uuid,omitempty"`
}

type OpenAIAPIKeyCreateRequest struct {
	Name   string `json:"name,omitempty"`
	ApiKey string `json:"api_key,omitempty"`
}

type OpenAIAPIKeyUpdateRequest struct {
	Name       string `json:"name,omitempty"`
	ApiKey     string `json:"api_key,omitempty"`
	ApiKeyUuid string `json:"api_key_uuid,omitempty"`
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

// KnowledgeBaseDataSource represents a Gradient AI Knowledge Base Data Source
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

type indexingJobsRoot struct {
	Jobs  []LastIndexingJob `json:"jobs"`
	Links *Links            `json:"links"`
	Meta  *Meta             `json:"meta"`
}

type knowledgebaseRoot struct {
	KnowledgeBase  *KnowledgeBase `json:"knowledge_base"`
	DatabaseStatus string         `json:"database_status,omitempty"`
}

type DeleteDataSourceRoot struct {
	DataSourceUuid    string `json:"data_source_uuid"`
	KnowledgeBaseUuid string `json:"knowledge_base_uuid"`
}

type AgentRouteResponse struct {
	ChildAgentUuid  string `json:"child_agent_uuid,omitempty"`
	ParentAgentUuid string `json:"parent_agent_uuid,omitempty"`
	Rollback        bool   `json:"rollback,omitempty"`
	UUID            string `json:"uuid,omitempty"`
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

// AgentRouteCreateRequest represents a route between a parent and child agent.
type AgentRouteCreateRequest struct {
	ChildAgentUuid  string `json:"child_agent_uuid,omitempty"`
	IfCase          string `json:"if_case,omitempty"`
	ParentAgentUuid string `json:"parent_agent_uuid,omitempty"`
	RouteName       string `json:"route_name,omitempty"`
}

// AgentRouteUpdateRequest represents the request to update an existing route between a parent and child agent.
type AgentRouteUpdateRequest struct {
	ChildAgentUuid  string `json:"child_agent_uuid,omitempty"`
	IfCase          string `json:"if_case,omitempty"`
	ParentAgentUuid string `json:"parent_agent_uuid,omitempty"`
	RouteName       string `json:"route_name,omitempty"`
	UUID            string `json:"uuid,omitempty"`
}

type NestedSchema struct {
	Type        string                   `json:"type" validate:"required,oneof=string boolean number integer array object"`
	Items       *NestedSchema            `json:"items,omitempty"`
	Properties  map[string]*NestedSchema `json:"properties,omitempty"`
	Enum        []string                 `json:"enum,omitempty"`
	Description string                   `json:"description,omitempty"`
}

type OpenAPIParameterSchema struct {
	Name        string       `json:"name" validate:"required"`
	In          string       `json:"in" validate:"omitempty,oneof=query header path cookie"`
	Schema      NestedSchema `json:"schema" validate:"required"`
	Description string       `json:"description,omitempty"`
	Required    bool         `json:"required,omitempty"`
}

type FunctionInputSchema struct {
	Parameters []OpenAPIParameterSchema `json:"parameters" validate:"required,min=1,dive"`
}

type FunctionRouteCreateRequest struct {
	AgentUuid     string              `json:"agent_uuid,omitempty"`
	Description   string              `json:"description,omitempty"`
	FaasName      string              `json:"faas_name,omitempty"`
	FaasNamespace string              `json:"faas_namespace,omitempty"`
	FunctionName  string              `json:"function_name,omitempty"`
	InputSchema   FunctionInputSchema `json:"input_schema,omitempty"`
	OutputSchema  json.RawMessage     `json:"output_schema,omitempty"`
}

type FunctionRouteUpdateRequest struct {
	AgentUuid     string              `json:"agent_uuid,omitempty"`
	Description   string              `json:"description,omitempty"`
	FaasName      string              `json:"faas_name,omitempty"`
	FaasNamespace string              `json:"faas_namespace,omitempty"`
	FunctionName  string              `json:"function_name,omitempty"`
	FunctionUuid  string              `json:"function_uuid,omitempty"`
	InputSchema   FunctionInputSchema `json:"input_schema,omitempty"`
	OutputSchema  json.RawMessage     `json:"output_schema,omitempty"`
}

type DatacenterRegions struct {
	Region             string `json:"region"`
	InferenceUrl       string `json:"inference_url"`
	ServesBatch        bool   `json:"serves_batch"`
	ServesInference    bool   `json:"serves_inference"`
	StreamInferenceUrl string `json:"stream_inference_url"`
}

type datacenterRegionsRoot struct {
	DatacenterRegions []*DatacenterRegions `json:"regions"`
}

type gradientAgentKBRoot struct {
	Agent *Agent `json:"agent"`
}

// ListAgents returns a list of Gradient AI Agents
func (s *GradientAIServiceOp) ListAgents(ctx context.Context, opt *ListOptions) ([]*Agent, *Response, error) {
	path, err := addOptions(gradientBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentsRoot)
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

// CreateAgent creates a new Gradient AI Agent by providing the AgentCreateRequest object
func (s *GradientAIServiceOp) CreateAgent(ctx context.Context, create *AgentCreateRequest) (*Agent, *Response, error) {
	path := gradientBasePath
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

	root := new(gradientAgentRoot)
	resp, err := s.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// ListAgentAPIKeys retrieves list of API Keys associated with the specified Gradient AI agent
func (s *GradientAIServiceOp) ListAgentAPIKeys(ctx context.Context, agentId string, opt *ListOptions) ([]*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys", gradientBasePath, agentId)
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

// CreateAgentAPIKey creates a new API key for the specified Gradient AI agent
func (s *GradientAIServiceOp) CreateAgentAPIKey(ctx context.Context, agentId string, createRequest *AgentAPIKeyCreateRequest) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys", gradientBasePath, agentId)

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

// UpdateAgentAPIKey updates an existing API key for the specified Gradient AI agent
func (s *GradientAIServiceOp) UpdateAgentAPIKey(ctx context.Context, agentId, apiKeyId string, updateRequest *AgentAPIKeyUpdateRequest) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s", gradientBasePath, agentId, apiKeyId)

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

// DeleteAgentAPIKey deletes an existing API key for the specified Gradient AI agent
func (s *GradientAIServiceOp) DeleteAgentAPIKey(ctx context.Context, agentId, apiKeyId string) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s", gradientBasePath, agentId, apiKeyId)

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

// RegenerateAgentAPIKey regenerates an API key for the specified Gradient AI agent
func (s *GradientAIServiceOp) RegenerateAgentAPIKey(ctx context.Context, agentId, apiKeyId string) (*ApiKeyInfo, *Response, error) {
	path := fmt.Sprintf("%s/%s/api_keys/%s/regenerate", gradientBasePath, agentId, apiKeyId)

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

// GetAgent returns the details of a Gradient AI Agent based on the Agent UUID
func (s *GradientAIServiceOp) GetAgent(ctx context.Context, id string) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", gradientBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// UpdateAgent function updates a Gradient AI Agent properties for the given UUID
func (s *GradientAIServiceOp) UpdateAgent(ctx context.Context, id string, update *AgentUpdateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", gradientBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// DeleteAgent function deletes a Gradient AI Agent by its corresponding UUID
func (s *GradientAIServiceOp) DeleteAgent(ctx context.Context, id string) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s", gradientBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// UpdateAgentVisibility function updates a Gradient AI Agent status by changing visibility to public or private.
func (s *GradientAIServiceOp) UpdateAgentVisibility(ctx context.Context, id string, update *AgentVisibilityUpdateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s/deployment_visibility", gradientBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// List all knowledge bases
func (s *GradientAIServiceOp) ListKnowledgeBases(ctx context.Context, opt *ListOptions) ([]KnowledgeBase, *Response, error) {

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

// ListIndexingJobs returns a list of all indexing jobs for knowledge bases
func (s *GradientAIServiceOp) ListIndexingJobs(ctx context.Context, opt *ListOptions) (*IndexingJobsResponse, *Response, error) {
	path := IndexingJobsPath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(indexingJobsRoot)
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

	result := &IndexingJobsResponse{
		Jobs:  root.Jobs,
		Links: root.Links,
		Meta:  root.Meta,
	}

	return result, resp, err
}

// ListIndexingJobDataSources returns the data sources for a specific indexing job
func (s *GradientAIServiceOp) ListIndexingJobDataSources(ctx context.Context, indexingJobUUID string) (*IndexingJobDataSourcesResponse, *Response, error) {
	path := fmt.Sprintf(IndexingJobDataSourcesPath, indexingJobUUID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(IndexingJobDataSourcesResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, err
}

// GetIndexingJob retrieves the status of a specific indexing job for a knowledge base
func (s *GradientAIServiceOp) GetIndexingJob(ctx context.Context, indexingJobUUID string) (*IndexingJobResponse, *Response, error) {
	path := fmt.Sprintf(IndexingJobByIDPath, indexingJobUUID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(IndexingJobResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, err
}

// CancelIndexingJob cancels a specific indexing job for a knowledge base
func (s *GradientAIServiceOp) CancelIndexingJob(ctx context.Context, indexingJobUUID string) (*IndexingJobResponse, *Response, error) {
	path := fmt.Sprintf(IndexingJobCancelPath, indexingJobUUID)

	// Create the request payload
	cancelRequest := &CancelIndexingJobRequest{
		UUID: indexingJobUUID,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, cancelRequest)
	if err != nil {
		return nil, nil, err
	}

	result := new(IndexingJobResponse)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, err
}

// Create a knowledge base
func (s *GradientAIServiceOp) CreateKnowledgeBase(ctx context.Context, knowledgeBaseCreate *KnowledgeBaseCreateRequest) (*KnowledgeBase, *Response, error) {

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
func (s *GradientAIServiceOp) ListKnowledgeBaseDataSources(ctx context.Context, knowledgeBaseID string, opt *ListOptions) ([]KnowledgeBaseDataSource, *Response, error) {

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
func (s *GradientAIServiceOp) AddKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, addDataSource *AddKnowledgeBaseDataSourceRequest) (*KnowledgeBaseDataSource, *Response, error) {
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
func (s *GradientAIServiceOp) DeleteKnowledgeBaseDataSource(ctx context.Context, knowledgeBaseID string, dataSourceID string) (string, string, *Response, error) {

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
func (s *GradientAIServiceOp) GetKnowledgeBase(ctx context.Context, knowledgeBaseID string) (*KnowledgeBase, string, *Response, error) {
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
func (s *GradientAIServiceOp) UpdateKnowledgeBase(ctx context.Context, knowledgeBaseID string, update *UpdateKnowledgeBaseRequest) (*KnowledgeBase, *Response, error) {
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
func (s *GradientAIServiceOp) DeleteKnowledgeBase(ctx context.Context, knowledgeBaseID string) (string, *Response, error) {

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
func (s *GradientAIServiceOp) AttachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error) {

	path := fmt.Sprintf(AgentKnowledgeBasePath, agentID, knowledgeBaseID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentKBRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// Detach a knowledge base from an agent
func (s *GradientAIServiceOp) DetachKnowledgeBaseToAgent(ctx context.Context, agentID string, knowledgeBaseID string) (*Agent, *Response, error) {

	path := fmt.Sprintf(AgentKnowledgeBasePath, agentID, knowledgeBaseID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(gradientAgentKBRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Agent, resp, nil
}

// AddAgentRoute function adds a route between a parent and child agent.
func (s *GradientAIServiceOp) AddAgentRoute(ctx context.Context, parentId string, childId string, route *AgentRouteCreateRequest) (*AgentRouteResponse, *Response, error) {
	path := fmt.Sprintf(agentRouteBasePath, parentId, childId)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, route)
	if err != nil {
		return nil, nil, err
	}

	root := new(AgentRouteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, nil
}

// UpdateAgentRoute function updates a route between a parent and child agent.
func (s *GradientAIServiceOp) UpdateAgentRoute(ctx context.Context, parentId string, childId string, route *AgentRouteUpdateRequest) (*AgentRouteResponse, *Response, error) {
	path := fmt.Sprintf(agentRouteBasePath, parentId, childId)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, route)
	if err != nil {
		return nil, nil, err
	}

	root := new(AgentRouteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, nil
}

// DeleteAgentRoute function deletes a route between a parent and child agent.
func (s *GradientAIServiceOp) DeleteAgentRoute(ctx context.Context, parentId string, childId string) (*AgentRouteResponse, *Response, error) {
	path := fmt.Sprintf(agentRouteBasePath, parentId, childId)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(AgentRouteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, nil
}

// ListAgentVersions retrieves a list of versions for the specified GradientAI agent
func (s *GradientAIServiceOp) ListAgentVersions(ctx context.Context, agentId string, opt *ListOptions) ([]*AgentVersion, *Response, error) {
	path := fmt.Sprintf("%s/%s/versions", gradientBasePath, agentId)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(agentVersionsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AgentVersions, resp, nil
}

func (s *GradientAIServiceOp) RollbackAgentVersion(ctx context.Context, agentId string, versionId string) (string, *Response, error) {
	path := fmt.Sprintf("%s/%s/versions", gradientBasePath, agentId)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, RollbackVersionRequest{
		AgentUuid:   agentId,
		VersionHash: versionId,
	})
	if err != nil {
		return "", nil, err
	}

	root := new(auditResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return "", resp, err
	}

	return root.VersionHash, resp, nil
}

// ListAnthropicAPIKeys retrieves a list of Anthropic API Keys
func (s *GradientAIServiceOp) ListAnthropicAPIKeys(ctx context.Context, opt *ListOptions) ([]*AnthropicApiKeyInfo, *Response, error) {
	path := AnthropicAPIKeysPath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(anthropicAPIKeysRoot)
	resp, err := s.client.Do(ctx, req, &root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}
	return root.AnthropicApiKeys, resp, nil
}

func (s *GradientAIServiceOp) CreateAnthropicAPIKey(ctx context.Context, anthropicAPIKeyCreate *AnthropicAPIKeyCreateRequest) (*AnthropicApiKeyInfo, *Response, error) {
	path := AnthropicAPIKeysPath

	if anthropicAPIKeyCreate.Name == "" {
		return nil, nil, fmt.Errorf("Name is required")
	}
	if anthropicAPIKeyCreate.ApiKey == "" {
		return nil, nil, fmt.Errorf("ApiKey is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, anthropicAPIKeyCreate)
	if err != nil {
		return nil, nil, err
	}

	root := new(anthropicAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AnthropicApiKey, resp, nil
}

func (s *GradientAIServiceOp) GetAnthropicAPIKey(ctx context.Context, anthropicApiKeyId string) (*AnthropicApiKeyInfo, *Response, error) {
	path := AnthropicAPIKeysPath + "/" + anthropicApiKeyId

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(anthropicAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AnthropicApiKey, resp, nil
}

func (s *GradientAIServiceOp) UpdateAnthropicAPIKey(ctx context.Context, anthropicApiKeyId string, anthropicAPIKeyUpdate *AnthropicAPIKeyUpdateRequest) (*AnthropicApiKeyInfo, *Response, error) {
	path := AnthropicAPIKeysPath + "/" + anthropicApiKeyId

	if anthropicAPIKeyUpdate.ApiKeyUuid == "" {
		anthropicAPIKeyUpdate.ApiKeyUuid = anthropicApiKeyId
	}
	if anthropicAPIKeyUpdate.ApiKeyUuid == "" {
		return nil, nil, fmt.Errorf("ApiKeyUuid is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, anthropicAPIKeyUpdate)
	if err != nil {
		return nil, nil, err
	}

	root := new(anthropicAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AnthropicApiKey, resp, nil
}

func (s *GradientAIServiceOp) DeleteAnthropicAPIKey(ctx context.Context, anthropicApiKeyId string) (*AnthropicApiKeyInfo, *Response, error) {
	path := AnthropicAPIKeysPath + "/" + anthropicApiKeyId

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(anthropicAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AnthropicApiKey, resp, nil
}

func (s *GradientAIServiceOp) ListAgentsByAnthropicAPIKey(ctx context.Context, anthropicApiKeyId string, opt *ListOptions) ([]*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s/agents", AnthropicAPIKeysPath, anthropicApiKeyId)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentsRoot)
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

func (s *GradientAIServiceOp) ListOpenAIAPIKeys(ctx context.Context, opt *ListOptions) ([]*OpenAiApiKey, *Response, error) {
	path := OpenAIAPIKeysPath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(openaiAPIKeysRoot)
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
	return root.OpenAIApiKeys, resp, nil
}

func (s *GradientAIServiceOp) CreateOpenAIAPIKey(ctx context.Context, openaiAPIKeyCreate *OpenAIAPIKeyCreateRequest) (*OpenAiApiKey, *Response, error) {
	path := OpenAIAPIKeysPath

	if openaiAPIKeyCreate.Name == "" {
		return nil, nil, fmt.Errorf("Name is required")
	}
	if openaiAPIKeyCreate.ApiKey == "" {
		return nil, nil, fmt.Errorf("ApiKey is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, openaiAPIKeyCreate)
	if err != nil {
		return nil, nil, err
	}

	root := new(openaiAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.OpenAIAPIKey, resp, nil
}

func (s *GradientAIServiceOp) GetOpenAIAPIKey(ctx context.Context, openaiApiKeyId string) (*OpenAiApiKey, *Response, error) {
	path := OpenAIAPIKeysPath + "/" + openaiApiKeyId

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(openaiAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.OpenAIAPIKey, resp, nil
}

func (s *GradientAIServiceOp) UpdateOpenAIAPIKey(ctx context.Context, openaiApiKeyId string, openaiAPIKeyUpdate *OpenAIAPIKeyUpdateRequest) (*OpenAiApiKey, *Response, error) {
	path := OpenAIAPIKeysPath + "/" + openaiApiKeyId

	if openaiAPIKeyUpdate.ApiKeyUuid == "" {
		openaiAPIKeyUpdate.ApiKeyUuid = openaiApiKeyId
	}
	if openaiAPIKeyUpdate.ApiKeyUuid == "" {
		return nil, nil, fmt.Errorf("ApiKeyUuid is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, openaiAPIKeyUpdate)
	if err != nil {
		return nil, nil, err
	}

	root := new(openaiAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.OpenAIAPIKey, resp, nil
}

func (s *GradientAIServiceOp) DeleteOpenAIAPIKey(ctx context.Context, openaiApiKeyId string) (*OpenAiApiKey, *Response, error) {
	path := OpenAIAPIKeysPath + "/" + openaiApiKeyId

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(openaiAPIKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.OpenAIAPIKey, resp, nil
}

func (s *GradientAIServiceOp) ListAgentsByOpenAIAPIKey(ctx context.Context, openaiApiKeyId string, opt *ListOptions) ([]*Agent, *Response, error) {
	path := fmt.Sprintf("%s/%s/agents", OpenAIAPIKeysPath, openaiApiKeyId)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentsRoot)
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

// Attaches a functionroute to an agent.
func (g *GradientAIServiceOp) CreateFunctionRoute(ctx context.Context, id string, create *FunctionRouteCreateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf(functionRouteBasePath, id)

	if create.AgentUuid == "" {
		return nil, nil, fmt.Errorf("AgentUuid is required")
	}
	if create.Description == "" {
		return nil, nil, fmt.Errorf("Description is required")
	}
	if create.FaasName == "" {
		return nil, nil, fmt.Errorf("FaasName is required")
	}
	if create.FaasNamespace == "" {
		return nil, nil, fmt.Errorf("FaasNamespace is required")
	}
	if create.FunctionName == "" {
		return nil, nil, fmt.Errorf("FunctionName is required")
	}
	if len(create.InputSchema.Parameters) == 0 {
		return nil, nil, fmt.Errorf("InputSchema is required")
	}

	req, err := g.client.NewRequest(ctx, http.MethodPost, path, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := g.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// Deletes a functionroute to an agent.
func (g *GradientAIServiceOp) DeleteFunctionRoute(ctx context.Context, agent_id string, function_id string) (*Agent, *Response, error) {
	path := fmt.Sprintf(UpdateFunctionRoutePath, agent_id, function_id)
	req, err := g.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := g.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// Updates a functionroute to an agent.
func (g *GradientAIServiceOp) UpdateFunctionRoute(ctx context.Context, agent_id string, function_id string, update *FunctionRouteUpdateRequest) (*Agent, *Response, error) {
	path := fmt.Sprintf(UpdateFunctionRoutePath, agent_id, function_id)
	req, err := g.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientAgentRoot)
	resp, err := g.client.Do(ctx, req, root)

	if err != nil {
		return nil, resp, err
	}

	return root.Agent, resp, nil
}

// ListAvailableModels returns a list of available Gradient AI models
func (g *GradientAIServiceOp) ListAvailableModels(ctx context.Context, opt *ListOptions) ([]*Model, *Response, error) {
	path, err := addOptions(agentModelBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := g.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(gradientModelsRoot)
	resp, err := g.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}

	return root.Models, resp, nil
}

// ListDatacenterRegions returns a list of available datacenter regions for Gradient AI services
func (g *GradientAIServiceOp) ListDatacenterRegions(ctx context.Context, servesInference, servesBatch *bool) ([]*DatacenterRegions, *Response, error) {
	path := datacenterRegionsPath

	var params []string
	if servesInference != nil {
		params = append(params, fmt.Sprintf("serves_inference=%t", *servesInference))
	}
	if servesBatch != nil {
		params = append(params, fmt.Sprintf("serves_batch=%t", *servesBatch))
	}
	if len(params) > 0 {
		path = path + "?" + strings.Join(params, "&")
	}

	req, err := g.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(datacenterRegionsRoot)
	resp, err := g.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.DatacenterRegions, resp, nil
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

func (a AgentRouteResponse) String() string {
	return Stringify(a)
}

func (a AgentVersion) String() string {
	return Stringify(a)
}

func (a IndexingJobResponse) String() string {
	return Stringify(a)
}

func (a IndexingJobDataSourcesResponse) String() string {
	return Stringify(a)
}
