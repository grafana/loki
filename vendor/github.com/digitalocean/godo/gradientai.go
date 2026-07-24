package godo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	gradientBasePath                         = "/v2/gen-ai/agents"
	agentModelBasePath                       = "/v2/gen-ai/models"
	inferenceRoutersBasePath                 = agentModelBasePath + "/routers"
	inferenceRouterTaskPresetsPath           = inferenceRoutersBasePath + "/tasks/presets"
	datacenterRegionsPath                    = "/v2/gen-ai/regions"
	agentRouteBasePath                       = gradientBasePath + "/%s/child_agents/%s"
	KnowledgeBasePath                        = "/v2/gen-ai/knowledge_bases"
	functionRouteBasePath                    = gradientBasePath + "/%s/functions"
	KnowledgeBaseDataSourcesPath             = KnowledgeBasePath + "/%s/data_sources"
	GetKnowledgeBaseByIDPath                 = KnowledgeBasePath + "/%s"
	UpdateKnowledgeBaseByIDPath              = KnowledgeBasePath + "/%s"
	DeleteKnowledgeBaseByIDPath              = KnowledgeBasePath + "/%s"
	AgentKnowledgeBasePath                   = "/v2/gen-ai/agents" + "/%s/knowledge_bases/%s"
	DeleteDataSourcePath                     = KnowledgeBasePath + "/%s/data_sources/%s"
	IndexingJobsPath                         = "/v2/gen-ai/indexing_jobs"
	IndexingJobByIDPath                      = IndexingJobsPath + "/%s"
	IndexingJobCancelPath                    = IndexingJobsPath + "/%s/cancel"
	IndexingJobDataSourcesPath               = IndexingJobsPath + "/%s/data_sources"
	AnthropicAPIKeysPath                     = "/v2/gen-ai/anthropic/keys"
	AnthropicAPIKeyByIDPath                  = AnthropicAPIKeysPath + "/%s"
	OpenAIAPIKeysPath                        = "/v2/gen-ai/openai/keys"
	UpdateFunctionRoutePath                  = functionRouteBasePath + "/%s"
	DeleteFunctionRoutePath                  = functionRouteBasePath + "/%s"
	customModelsBasePath                     = "/v2/gen-ai/custom_models"
	customModelImportPath                    = customModelsBasePath + "/import"
	customModelByIDPath                      = customModelsBasePath + "/%s"
	customModelMetadataPath                  = customModelsBasePath + "/%s/metadata"
	customEvaluationMetricsPath              = "/v2/gen-ai/custom_evaluation_metrics"
	customEvaluationMetricByIDPath           = customEvaluationMetricsPath + "/%s"
	modelEvaluationRunsBasePath              = "/v2/gen-ai/model_evaluation_runs"
	modelEvaluationRunByIDPath               = modelEvaluationRunsBasePath + "/%s"
	modelEvaluationRunCancelPath             = modelEvaluationRunsBasePath + "/%s/cancel"
	modelEvaluationRunResultsDownloadURLPath = modelEvaluationRunsBasePath + "/%s/results/download_url"
	modelEvaluationPresetsBasePath           = "/v2/gen-ai/model_evaluation_presets"
	modelEvaluationPresetByIDPath            = modelEvaluationPresetsBasePath + "/%s"
	modelEvaluationMetricsBasePath           = "/v2/gen-ai/model_evaluation_metrics"
	modelEvaluationDatasetUploadURLsPath     = "/v2/gen-ai/model_evaluation/datasets/file_upload_presigned_urls"
	evaluationDatasetsBasePath               = "/v2/gen-ai/evaluation_datasets"
	evaluationDatasetByIDPath                = evaluationDatasetsBasePath + "/%s"
	UpdateModelEvaluationRunPath             = modelEvaluationRunsBasePath + "/%s"
)

// CustomModelStatus represents the status of a custom model.
type CustomModelStatus string

const (
	CustomModelStatusUnspecified CustomModelStatus = "STATUS_UNSPECIFIED"
	CustomModelStatusImporting   CustomModelStatus = "STATUS_IMPORTING"
	CustomModelStatusReady       CustomModelStatus = "STATUS_READY"
	CustomModelStatusFailed      CustomModelStatus = "STATUS_FAILED"
	CustomModelStatusDeleted     CustomModelStatus = "STATUS_DELETED"
)

// CustomModelSourceType represents the source from which a custom model was imported.
type CustomModelSourceType string

const (
	CustomModelSourceTypeUnspecified  CustomModelSourceType = "SOURCE_TYPE_UNSPECIFIED"
	CustomModelSourceTypeHuggingFace  CustomModelSourceType = "SOURCE_TYPE_HUGGINGFACE"
	CustomModelSourceTypeSpacesBucket CustomModelSourceType = "SOURCE_TYPE_SPACES_BUCKET"
	CustomModelSourceTypeSDKUpload    CustomModelSourceType = "SOURCE_TYPE_SDK_UPLOAD"
	CustomModelSourceTypeFineTuning   CustomModelSourceType = "SOURCE_TYPE_FINE_TUNING"
)

// CustomModelSourceRefAccessType represents the access level required for a custom model source repository.
type CustomModelSourceRefAccessType string

const (
	CustomModelSourceRefAccessTypeUnspecified CustomModelSourceRefAccessType = "ACCESS_TYPE_UNSPECIFIED"
	CustomModelSourceRefAccessTypePublic      CustomModelSourceRefAccessType = "ACCESS_TYPE_PUBLIC"
	CustomModelSourceRefAccessTypePrivate     CustomModelSourceRefAccessType = "ACCESS_TYPE_PRIVATE"
	CustomModelSourceRefAccessTypeGated       CustomModelSourceRefAccessType = "ACCESS_TYPE_GATED"
)

// DeleteCustomModelStatus represents the status of a delete custom model operation.
type DeleteCustomModelStatus string

const (
	DeleteCustomModelStatusUnspecified DeleteCustomModelStatus = "DELETE_CUSTOM_MODEL_STATUS_UNSPECIFIED"
	DeleteCustomModelStatusSuccess     DeleteCustomModelStatus = "DELETE_CUSTOM_MODEL_STATUS_SUCCESS"
	DeleteCustomModelStatusFail        DeleteCustomModelStatus = "DELETE_CUSTOM_MODEL_STATUS_FAIL"
)

// DeleteModelEvaluationRunStatus represents the status of a delete model evaluation run operation.
type DeleteModelEvaluationRunStatus string

const (
	DeleteModelEvaluationRunStatusUnspecified DeleteModelEvaluationRunStatus = "DELETE_MODEL_EVALUATION_RUN_STATUS_UNSPECIFIED"
	DeleteModelEvaluationRunStatusSuccess     DeleteModelEvaluationRunStatus = "DELETE_MODEL_EVALUATION_RUN_STATUS_SUCCESS"
	DeleteModelEvaluationRunStatusFail        DeleteModelEvaluationRunStatus = "DELETE_MODEL_EVALUATION_RUN_STATUS_FAIL"
)

// ModelEvaluationRunStatus represents the lifecycle status of a model evaluation run.
type ModelEvaluationRunStatus string

const (
	ModelEvaluationRunStatusUnspecified   ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_STATUS_UNSPECIFIED"
	ModelEvaluationRunQueued              ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_QUEUED"
	ModelEvaluationRunRunningDataset      ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_RUNNING_DATASET"
	ModelEvaluationRunEvaluatingResults   ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_EVALUATING_RESULTS"
	ModelEvaluationRunCancelling          ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_CANCELLING"
	ModelEvaluationRunCancelled           ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_CANCELLED"
	ModelEvaluationRunSuccessful          ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_SUCCESSFUL"
	ModelEvaluationRunPartiallySuccessful ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_PARTIALLY_SUCCESSFUL"
	ModelEvaluationRunFailed              ModelEvaluationRunStatus = "MODEL_EVALUATION_RUN_FAILED"
)

// CandidateModelSource indicates whether evaluation inference runs against the
// serverless platform, a dedicated deployment, or a model router.
type CandidateModelSource string

const (
	CandidateModelSourceServerless CandidateModelSource = "CANDIDATE_MODEL_SOURCE_SERVERLESS"
	CandidateModelSourceDedicated  CandidateModelSource = "CANDIDATE_MODEL_SOURCE_DEDICATED"
	CandidateModelSourceRouter     CandidateModelSource = "CANDIDATE_MODEL_SOURCE_ROUTER"
)

// ModelEvaluationRunSortField is the field used to sort model evaluation run
// list results.
type ModelEvaluationRunSortField string

const (
	ModelEvaluationRunSortFieldUnspecified ModelEvaluationRunSortField = "MODEL_EVALUATION_RUN_SORT_FIELD_UNSPECIFIED"
	ModelEvaluationRunSortFieldCreatedAt   ModelEvaluationRunSortField = "MODEL_EVALUATION_RUN_SORT_FIELD_CREATED_AT"
	ModelEvaluationRunSortFieldStatus      ModelEvaluationRunSortField = "MODEL_EVALUATION_RUN_SORT_FIELD_STATUS"
)

// ModelEvaluationRunSortDirection is the sort direction for model evaluation
// run list results.
type ModelEvaluationRunSortDirection string

const (
	ModelEvaluationRunSortDirectionUnspecified ModelEvaluationRunSortDirection = "SORT_DIRECTION_UNSPECIFIED"
	ModelEvaluationRunSortDirectionAsc         ModelEvaluationRunSortDirection = "SORT_DIRECTION_ASC"
	ModelEvaluationRunSortDirectionDesc        ModelEvaluationRunSortDirection = "SORT_DIRECTION_DESC"
)

// EvaluationDatasetType represents the type of an evaluation dataset.
type EvaluationDatasetType string

const (
	EvaluationDatasetTypeUnknown EvaluationDatasetType = "EVALUATION_DATASET_TYPE_UNKNOWN"
	EvaluationDatasetTypeADK     EvaluationDatasetType = "EVALUATION_DATASET_TYPE_ADK"
	EvaluationDatasetTypeNonADK  EvaluationDatasetType = "EVALUATION_DATASET_TYPE_NON_ADK"
	EvaluationDatasetTypeModel   EvaluationDatasetType = "EVALUATION_DATASET_TYPE_MODEL"
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
	ListInferenceRouters(context.Context, *ListOptions) ([]*InferenceRouterSummary, *Response, error)
	GetInferenceRouter(context.Context, string) (*InferenceRouter, *Response, error)
	CreateInferenceRouter(context.Context, *InferenceRouterCreateRequest) (*InferenceRouter, *Response, error)
	UpdateInferenceRouter(context.Context, string, *InferenceRouterUpdateRequest) (*InferenceRouter, *Response, error)
	DeleteInferenceRouter(context.Context, string) (*InferenceRouterDeleteResponse, *Response, error)
	ListInferenceRouterTaskPresets(context.Context, *ListOptions) ([]*InferenceRouterTaskPreset, *Response, error)
	ListAvailableModels(context.Context, *ListOptions) ([]*Model, *Response, error)
	SearchModels(context.Context, string) ([]string, *Response, error)
	GetModelByUUID(context.Context, string) (*Model, *Response, error)
	ListDatacenterRegions(context.Context, *bool, *bool) ([]*DatacenterRegions, *Response, error)
	ListCustomModels(ctx context.Context, opt *CustomModelListOptions) (*CustomModelListResponse, *Response, error)
	GetCustomModel(ctx context.Context, uuid string) (*CustomModel, *Response, error)
	ImportCustomModel(ctx context.Context, importRequest *CustomModelImportRequest) (*CustomModelImportResponse, *Response, error)
	DeleteCustomModel(ctx context.Context, uuid string) (*CustomModelDeleteResponse, *Response, error)
	UpdateCustomModelMetadata(ctx context.Context, uuid string, updateRequest *CustomModelMetadataUpdateRequest) (*CustomModel, *Response, error)
	CreateCustomEvaluationMetric(ctx context.Context, createRequest *CreateCustomEvaluationMetricRequest) (*EvaluationMetric, *Response, error)
	UpdateCustomEvaluationMetric(ctx context.Context, metricUUID string, updateRequest *UpdateCustomEvaluationMetricRequest) (*EvaluationMetric, *Response, error)
	DeleteCustomEvaluationMetric(ctx context.Context, metricUUID string) (*Response, error)
	DeleteModelEvaluationRun(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunDeleteResponse, *Response, error)
	DeleteModelEvaluationPreset(ctx context.Context, evalPresetUUID string) (*ModelEvaluationPresetDeleteResponse, *Response, error)
	CancelModelEvaluationRun(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunCancelResponse, *Response, error)
	CreateModelEvaluationRun(ctx context.Context, createRequest *CreateModelEvaluationRunRequest) (*ModelEvaluationRunCreateResponse, *Response, error)
	UpdateModelEvaluationRun(ctx context.Context, evalRunUUID string, updateRequest *UpdateModelEvaluationRunRequest) (*ModelEvaluationRunUpdateResponse, *Response, error)
	CreateModelEvalDatasetUploadPresignedURLs(ctx context.Context, createRequest *CreateModelEvalDatasetUploadPresignedURLsRequest) (*CreateModelEvalDatasetUploadPresignedURLsResponse, *Response, error)
	GetModelEvaluationRun(ctx context.Context, evalRunUUID string, opt *ModelEvaluationRunGetOptions) (*ModelEvaluationRunGetResponse, *Response, error)
	GetModelEvaluationPreset(ctx context.Context, evalPresetUUID string) (*ModelEvaluationPresetGetResponse, *Response, error)
	GetModelEvaluationRunResultsDownloadURL(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunResultsDownloadURLResponse, *Response, error)
	ListModelEvaluationRuns(ctx context.Context, opt *ModelEvaluationRunListOptions) (*ModelEvaluationRunListResponse, *Response, error)
	ListModelEvaluationPresets(ctx context.Context) (*ModelEvaluationPresetListResponse, *Response, error)
	ListModelEvaluationMetrics(ctx context.Context) (*ModelEvaluationMetricListResponse, *Response, error)
	ListEvaluationDatasets(ctx context.Context, opt *EvaluationDatasetListOptions) (*EvaluationDatasetListResponse, *Response, error)
	DeleteEvaluationDataset(ctx context.Context, datasetUUID string) (*EvaluationDatasetDeleteResponse, *Response, error)
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

// EvaluationMetricSource distinguishes platform catalog metrics from user-defined LLM-as-judge metrics.
type EvaluationMetricSource string

const (
	EvaluationMetricSourceUnspecified EvaluationMetricSource = "EVALUATION_METRIC_SOURCE_UNSPECIFIED"
	EvaluationMetricSourceBuiltin     EvaluationMetricSource = "EVALUATION_METRIC_SOURCE_BUILTIN"
	EvaluationMetricSourceCustom      EvaluationMetricSource = "EVALUATION_METRIC_SOURCE_CUSTOM"
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
	MetricUUID        string                            `json:"metric_uuid,omitempty"`
	MetricName        string                            `json:"metric_name,omitempty"`
	Description       string                            `json:"description,omitempty"`
	MetricType        EvaluationMetricType              `json:"metric_type,omitempty"`
	MetricValueType   EvaluationMetricValueType         `json:"metric_value_type,omitempty"`
	RangeMin          float32                           `json:"range_min,omitempty"`
	RangeMax          float32                           `json:"range_max,omitempty"`
	Inverted          bool                              `json:"inverted,omitempty"`
	Category          EvaluationMetricCategory          `json:"category,omitempty"`
	IsMetricGoal      bool                              `json:"is_metric_goal,omitempty"`
	MetricRank        uint32                            `json:"metric_rank,omitempty"`
	Source            EvaluationMetricSource            `json:"source,omitempty"`
	CustomEvalConfig  *CustomEvaluationMetricConfig     `json:"custom_eval_config,omitempty"`
	AssociatedPresets []AssociatedModelEvaluationPreset `json:"associated_presets,omitempty"`
}

// CustomEvaluationMetricConfig represents the LLM-as-judge scoring configuration for custom evaluation metrics.
type CustomEvaluationMetricConfig struct {
	RequiresGroundTruth bool       `json:"requires_ground_truth,omitempty"`
	ScoringPrompt       string     `json:"scoring_prompt,omitempty"`
	CreatedAt           *Timestamp `json:"created_at,omitempty"`
	UpdatedAt           *Timestamp `json:"updated_at,omitempty"`
	DeletedAt           *Timestamp `json:"deleted_at,omitempty"`
}

// AssociatedModelEvaluationPreset identifies a saved model evaluation preset that references a custom metric.
type AssociatedModelEvaluationPreset struct {
	EvalPresetUUID string `json:"eval_preset_uuid,omitempty"`
	Name           string `json:"name,omitempty"`
}

// CreateCustomEvaluationMetricRequest is the request payload for creating a custom evaluation metric.
type CreateCustomEvaluationMetricRequest struct {
	MetricName  string                        `json:"metric_name,omitempty"`
	Description string                        `json:"description,omitempty"`
	Config      *CustomEvaluationMetricConfig `json:"config,omitempty"`
}

// UpdateCustomEvaluationMetricRequest is the request payload for updating a custom evaluation metric.
type UpdateCustomEvaluationMetricRequest struct {
	MetricUUID  string                        `json:"metric_uuid,omitempty"`
	MetricName  string                        `json:"metric_name,omitempty"`
	Description string                        `json:"description,omitempty"`
	Config      *CustomEvaluationMetricConfig `json:"config,omitempty"`
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
	Agreement         *Agreement       `json:"agreement,omitempty"`
	BenchmarkScore    json.RawMessage  `json:"benchmark_score,omitempty"`
	Capabilities      []string         `json:"capabilities,omitempty"`
	ContextWindow     string           `json:"context_window,omitempty"`
	CreatedAt         *Timestamp       `json:"created_at,omitempty"`
	Description       string           `json:"description,omitempty"`
	InferenceName     string           `json:"inference_name,omitempty"`
	InferenceVersion  string           `json:"inference_version,omitempty"`
	IsFoundational    bool             `json:"is_foundational,omitempty"`
	ModelAvailability string           `json:"model_availability,omitempty"`
	Modalities        *ModelModalities `json:"modalities,omitempty"`
	Name              string           `json:"name,omitempty"`
	ParameterCount    float64          `json:"parameter_count,omitempty"`
	ParentUuid        string           `json:"parent_uuid,omitempty"`
	Pricing           *ModelPricing    `json:"pricing,omitempty"`
	Provider          string           `json:"provider,omitempty"`
	Type              string           `json:"type,omitempty"`
	UpdatedAt         *Timestamp       `json:"updated_at,omitempty"`
	UploadComplete    bool             `json:"upload_complete,omitempty"`
	Url               string           `json:"url,omitempty"`
	Usecases          []string         `json:"usecases,omitempty"`
	Uuid              string           `json:"uuid,omitempty"`
	Version           *ModelVersion    `json:"version,omitempty"`
}

// ModelModalities represents the input and output modalities supported by a model
type ModelModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
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

// ModelPricing represents token- and unit-based pricing for a model.
type ModelPricing struct {
	InputPricePerMillion               float64 `json:"input_price_per_million,omitempty"`
	OutputPricePerMillion              float64 `json:"output_price_per_million,omitempty"`
	PricePerImage                      float64 `json:"price_per_image,omitempty"`
	PricePerMegapixel                  float64 `json:"price_per_megapixel,omitempty"`
	PricePerSecond                     float64 `json:"price_per_second,omitempty"`
	PricePerVideo                      float64 `json:"price_per_video,omitempty"`
	PricePerAudio                      float64 `json:"price_per_audio,omitempty"`
	PricePerThousandCharacters         float64 `json:"price_per_thousand_characters,omitempty"`
	TextInputPricePerMillion           float64 `json:"text_input_price_per_million,omitempty"`
	TextOutputPricePerMillion          float64 `json:"text_output_price_per_million,omitempty"`
	TextCacheReadInputPricePerMillion  float64 `json:"text_cache_read_input_price_per_million,omitempty"`
	ImageInputPricePerMillion          float64 `json:"image_input_price_per_million,omitempty"`
	ImageOutputPricePerMillion         float64 `json:"image_output_price_per_million,omitempty"`
	ImageCacheReadInputPricePerMillion float64 `json:"image_cache_read_input_price_per_million,omitempty"`
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

// InferenceRouterSummary is a compact inference router returned from list operations.
type InferenceRouterSummary struct {
	UUID        string `json:"uuid,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// InferenceRouterConfig holds routing configuration for an inference router.
type InferenceRouterConfig struct {
	FallbackModels []string        `json:"fallback_models,omitempty"`
	Policies       json.RawMessage `json:"policies,omitempty"`
}

// InferenceRouter represents a GenAI inference router resource.
type InferenceRouter struct {
	UUID        string                 `json:"uuid,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	CreatedAt   *Timestamp             `json:"created_at,omitempty"`
	UpdatedAt   *Timestamp             `json:"updated_at,omitempty"`
	Config      *InferenceRouterConfig `json:"config,omitempty"`
}

// InferenceRouterCreateRequest defines the body for creating an inference router.
// Field names match POST /v2/gen-ai/models/routers (see DigitalOcean API / pydo.genai.create_model_router).
// FallbackModels must contain at least one non-empty model name (enforced by CreateInferenceRouter).
type InferenceRouterCreateRequest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Policies       json.RawMessage `json:"policies,omitempty"`
	FallbackModels []string        `json:"fallback_models"`
}

// InferenceRouterUpdateRequest defines the body for updating an inference router.
type InferenceRouterUpdateRequest struct {
	Name           string           `json:"name,omitempty"`
	Description    string           `json:"description,omitempty"`
	Policies       *json.RawMessage `json:"policies,omitempty"`
	FallbackModels []string         `json:"fallback_models,omitempty"`
}

// InferenceRouterDeleteResponse is returned when deleting an inference router.
type InferenceRouterDeleteResponse struct {
	UUID string `json:"uuid,omitempty"`
}

// InferenceRouterTaskPreset is a catalog preset task for inference router policies (use TaskSlug in policy JSON).
// See GET /v2/gen-ai/models/routers/tasks/presets.
type InferenceRouterTaskPreset struct {
	TaskSlug    string   `json:"task_slug,omitempty"`
	Name        string   `json:"name,omitempty"`
	Category    string   `json:"category,omitempty"`
	Description string   `json:"description,omitempty"`
	Models      []string `json:"models,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type inferenceRouterTaskPresetsRoot struct {
	Tasks []*InferenceRouterTaskPreset `json:"tasks"`
	Links *Links                       `json:"links,omitempty"`
	Meta  *Meta                        `json:"meta,omitempty"`
}

type inferenceRoutersRoot struct {
	Routers []*InferenceRouterSummary `json:"model_routers"`
	Links   *Links                    `json:"links,omitempty"`
	Meta    *Meta                     `json:"meta,omitempty"`
}

type inferenceRouterRoot struct {
	Router *InferenceRouter `json:"model_router"`
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

// ListInferenceRouters returns a page of inference routers.
func (s *GradientAIServiceOp) ListInferenceRouters(ctx context.Context, opt *ListOptions) ([]*InferenceRouterSummary, *Response, error) {
	path, err := addOptions(inferenceRoutersBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(inferenceRoutersRoot)
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

	return root.Routers, resp, nil
}

// ListInferenceRouterTaskPresets returns a page of preset tasks for building inference router policies.
func (s *GradientAIServiceOp) ListInferenceRouterTaskPresets(ctx context.Context, opt *ListOptions) ([]*InferenceRouterTaskPreset, *Response, error) {
	path, err := addOptions(inferenceRouterTaskPresetsPath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(inferenceRouterTaskPresetsRoot)
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

	return root.Tasks, resp, nil
}

// GetInferenceRouter retrieves an inference router by UUID.
func (s *GradientAIServiceOp) GetInferenceRouter(ctx context.Context, uuid string) (*InferenceRouter, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}

	path := fmt.Sprintf("%s/%s", inferenceRoutersBasePath, uuid)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(inferenceRouterRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Router, resp, nil
}

// CreateInferenceRouter creates a new inference router.
func (s *GradientAIServiceOp) CreateInferenceRouter(ctx context.Context, create *InferenceRouterCreateRequest) (*InferenceRouter, *Response, error) {
	if create == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}
	if create.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if len(create.FallbackModels) == 0 {
		return nil, nil, fmt.Errorf("fallback_models is required")
	}
	for i, m := range create.FallbackModels {
		if strings.TrimSpace(m) == "" {
			return nil, nil, fmt.Errorf("fallback_models[%d] must not be empty", i)
		}
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, inferenceRoutersBasePath, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(inferenceRouterRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Router, resp, nil
}

// UpdateInferenceRouter updates an inference router. At least one field in the update request must be set.
func (s *GradientAIServiceOp) UpdateInferenceRouter(ctx context.Context, uuid string, update *InferenceRouterUpdateRequest) (*InferenceRouter, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}
	if update == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}
	if update.Name == "" && update.Description == "" && update.Policies == nil && len(update.FallbackModels) == 0 {
		return nil, nil, fmt.Errorf("at least one of name, description, policies, or fallback_models must be set")
	}

	path := fmt.Sprintf("%s/%s", inferenceRoutersBasePath, uuid)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(inferenceRouterRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Router, resp, nil
}

// DeleteInferenceRouter deletes an inference router by UUID.
func (s *GradientAIServiceOp) DeleteInferenceRouter(ctx context.Context, uuid string) (*InferenceRouterDeleteResponse, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}

	path := fmt.Sprintf("%s/%s", inferenceRoutersBasePath, uuid)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	out := new(InferenceRouterDeleteResponse)
	resp, err := s.client.Do(ctx, req, out)
	if err != nil {
		return nil, resp, err
	}
	if out.UUID == "" {
		out.UUID = uuid
	}

	return out, resp, nil
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

// MCPSearchModels searches available models by name and returns the list of matching UUIDs.
func (g *GradientAIServiceOp) SearchModels(ctx context.Context, query string) ([]string, *Response, error) {
	models, resp, err := g.ListAvailableModels(ctx, nil)
	if err != nil {
		return nil, resp, err
	}

	var uuids []string
	lowerQuery := strings.ToLower(query)
	for _, model := range models {
		if strings.Contains(strings.ToLower(model.Name), lowerQuery) {
			uuids = append(uuids, model.Uuid)
		}
	}

	return uuids, resp, nil
}

// MCPSearchModelByUUID searches available models for a specific UUID and returns the model if it exists.
func (g *GradientAIServiceOp) GetModelByUUID(ctx context.Context, uuid string) (*Model, *Response, error) {
	models, resp, err := g.ListAvailableModels(ctx, nil)
	if err != nil {
		return nil, resp, err
	}

	for _, model := range models {
		if model.Uuid == uuid {
			return model, resp, nil
		}
	}

	return nil, resp, nil
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

// CustomModel represents a user-imported model (from HuggingFace, Spaces, etc.).
type CustomModel struct {
	Uuid                 string                         `json:"uuid,omitempty"`
	Name                 string                         `json:"name,omitempty"`
	Description          string                         `json:"description,omitempty"`
	Status               CustomModelStatus              `json:"status,omitempty"`
	Architecture         string                         `json:"architecture,omitempty"`
	SourceType           CustomModelSourceType          `json:"source_type,omitempty"`
	SourceRef            *CustomModelSourceRef          `json:"source_ref,omitempty"`
	TotalSizeBytes       string                         `json:"total_size_bytes,omitempty"`
	FileCount            int                            `json:"file_count,omitempty"`
	License              string                         `json:"license,omitempty"`
	Tags                 *CustomModelTags               `json:"tags,omitempty"`
	CreatedAt            *Timestamp                     `json:"created_at,omitempty"`
	UpdatedAt            *Timestamp                     `json:"updated_at,omitempty"`
	ActiveDeployments    []*CustomModelActiveDeployment `json:"active_deployments,omitempty"`
	ContextLength        int                            `json:"context_length,omitempty"`
	CostEstimatePerMonth int                            `json:"cost_estimate_per_month,omitempty"`
	InputModalities      []string                       `json:"input_modalities,omitempty"`
	OutputModalities     []string                       `json:"output_modalities,omitempty"`
	Parameters           string                         `json:"parameters,omitempty"`
	TeamId               string                         `json:"team_id,omitempty"`
	ConfigJson           map[string]any                 `json:"config_json,omitempty"`
	StorageRegion        string                         `json:"storage_region,omitempty"`
	ErrorMessage         string                         `json:"error_message,omitempty"`
}

// CustomModelSourceRef references the original source of a custom model.
type CustomModelSourceRef struct {
	RepoId     string                         `json:"repo_id,omitempty"`
	CommitSha  string                         `json:"commit_sha,omitempty"`
	AccessType CustomModelSourceRefAccessType `json:"access_type,omitempty"`
	Bucket     string                         `json:"bucket,omitempty"`
	Region     string                         `json:"region,omitempty"`
	Prefix     string                         `json:"prefix,omitempty"`
	HfToken    string                         `json:"hf_token,omitempty"`
}

// CustomModelTags contains user-defined tags for organizing custom models.
type CustomModelTags struct {
	Tags []string `json:"tags,omitempty"`
}

// CustomModelActiveDeployment represents an active dedicated inference deployment using a custom model.
type CustomModelActiveDeployment struct {
	Id         string                                `json:"id,omitempty"`
	Name       string                                `json:"name,omitempty"`
	RegionSlug string                                `json:"region_slug,omitempty"`
	State      string                                `json:"state,omitempty"`
	Endpoints  *CustomModelActiveDeploymentEndpoints `json:"endpoints,omitempty"`
	CreatedAt  string                                `json:"created_at,omitempty"`
	UpdatedAt  string                                `json:"updated_at,omitempty"`
}

// CustomModelActiveDeploymentEndpoints contains the endpoint URLs for a custom-model deployment.
type CustomModelActiveDeploymentEndpoints struct {
	PublicEndpointFqdn  string `json:"public_endpoint_fqdn,omitempty"`
	PrivateEndpointFqdn string `json:"private_endpoint_fqdn,omitempty"`
}

// CustomModelImportJob tracks the progress of a custom model import.
type CustomModelImportJob struct {
	Uuid         string     `json:"uuid,omitempty"`
	Status       string     `json:"status,omitempty"`
	FilesTotal   int        `json:"files_total,omitempty"`
	FilesDone    int        `json:"files_done,omitempty"`
	BytesTotal   string     `json:"bytes_total,omitempty"`
	BytesDone    string     `json:"bytes_done,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	ErrorStep    string     `json:"error_step,omitempty"`
	StartedAt    *Timestamp `json:"started_at,omitempty"`
	CompletedAt  *Timestamp `json:"completed_at,omitempty"`
	CreatedAt    *Timestamp `json:"created_at,omitempty"`
}

// CustomModelImportValidationStep describes a single validation step performed during a custom model import.
type CustomModelImportValidationStep struct {
	Name   string `json:"name,omitempty"`
	Passed bool   `json:"passed,omitempty"`
	Error  string `json:"error,omitempty"`
}

// CustomModelListOptions specifies optional parameters for listing custom models.
type CustomModelListOptions struct {
	Status CustomModelStatus `url:"status,omitempty"`
	ListOptions
}

// CustomModelImportRequest is the request body for importing a custom model.
type CustomModelImportRequest struct {
	Name                     string                `json:"name"`
	SourceType               CustomModelSourceType `json:"source_type"`
	SourceRef                *CustomModelSourceRef `json:"source_ref,omitempty"`
	Description              string                `json:"description,omitempty"`
	PreferredGpuRegion       string                `json:"preferred_gpu_region,omitempty"`
	AcceptTermsAndConditions bool                  `json:"accept_terms_and_conditions,omitempty"`
	Tags                     *CustomModelTags      `json:"tags,omitempty"`
}

// CustomModelMetadataUpdateRequest is the request body for updating custom model metadata.
type CustomModelMetadataUpdateRequest struct {
	Name             string           `json:"name,omitempty"`
	Description      string           `json:"description,omitempty"`
	Tags             *CustomModelTags `json:"tags,omitempty"`
	InputModalities  []string         `json:"input_modalities,omitempty"`
	OutputModalities []string         `json:"output_modalities,omitempty"`
	Parameters       string           `json:"parameters,omitempty"`
	License          string           `json:"license,omitempty"`
}

// CustomModelListResponse is the response returned by ListCustomModels.
type CustomModelListResponse struct {
	Models       []*CustomModel `json:"models,omitempty"`
	Links        *Links         `json:"links,omitempty"`
	Meta         *Meta          `json:"meta,omitempty"`
	MaxThreshold int            `json:"max_threshold,omitempty"`
}

// CustomModelImportResponse is the response returned by ImportCustomModel.
type CustomModelImportResponse struct {
	Model           *CustomModel                       `json:"model,omitempty"`
	ImportJob       *CustomModelImportJob              `json:"import_job,omitempty"`
	ValidationSteps []*CustomModelImportValidationStep `json:"validation_steps,omitempty"`
	Error           string                             `json:"error,omitempty"`
}

// CustomModelDeleteResponse is the response returned by DeleteCustomModel.
type CustomModelDeleteResponse struct {
	Status DeleteCustomModelStatus `json:"status,omitempty"`
	Error  string                  `json:"error,omitempty"`
}

type customModelRoot struct {
	Model *CustomModel `json:"model"`
}

type customEvaluationMetricRoot struct {
	Metric *EvaluationMetric `json:"metric"`
}

// ListCustomModels returns the list of custom models for the team.
func (s *GradientAIServiceOp) ListCustomModels(ctx context.Context, opt *CustomModelListOptions) (*CustomModelListResponse, *Response, error) {
	path, err := addOptions(customModelsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(CustomModelListResponse)
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
	return root, resp, nil
}

// GetCustomModel retrieves a single custom model by UUID.
func (s *GradientAIServiceOp) GetCustomModel(ctx context.Context, uuid string) (*CustomModel, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}
	path := fmt.Sprintf(customModelByIDPath, uuid)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(customModelRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Model, resp, nil
}

// ImportCustomModel imports a new custom model from a supported source (HuggingFace, Spaces, etc.).
func (s *GradientAIServiceOp) ImportCustomModel(ctx context.Context, importRequest *CustomModelImportRequest) (*CustomModelImportResponse, *Response, error) {
	if importRequest == nil {
		return nil, nil, fmt.Errorf("import request is required")
	}
	if importRequest.Name == "" {
		return nil, nil, fmt.Errorf("Name is required")
	}
	if importRequest.SourceType == "" {
		return nil, nil, fmt.Errorf("SourceType is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, customModelImportPath, importRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(CustomModelImportResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// DeleteCustomModel deletes the custom model with the given UUID.
func (s *GradientAIServiceOp) DeleteCustomModel(ctx context.Context, uuid string) (*CustomModelDeleteResponse, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}
	path := fmt.Sprintf(customModelByIDPath, uuid)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(CustomModelDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ModelEvaluationRunDeleteResponse is the response returned by DeleteModelEvaluationRun.
type ModelEvaluationRunDeleteResponse struct {
	Status DeleteModelEvaluationRunStatus `json:"status,omitempty"`
	Error  string                         `json:"error,omitempty"`
}

// ModelEvaluationRunSummary is a lightweight view of an evaluation run used in
// run history listings and the cancel response.
type ModelEvaluationRunSummary struct {
	CandidateModelName   string                      `json:"candidate_model_name,omitempty"`
	CandidateModelSource CandidateModelSource        `json:"candidate_model_source,omitempty"`
	CandidateModelUuid   string                      `json:"candidate_model_uuid,omitempty"`
	CreatedAt            *Timestamp                  `json:"created_at,omitempty"`
	DatasetName          string                      `json:"dataset_name,omitempty"`
	DatasetUuid          string                      `json:"dataset_uuid,omitempty"`
	EvalRunUuid          string                      `json:"eval_run_uuid,omitempty"`
	JudgeModelName       string                      `json:"judge_model_name,omitempty"`
	JudgeModelUuid       string                      `json:"judge_model_uuid,omitempty"`
	Name                 string                      `json:"name,omitempty"`
	Progress             *ModelEvaluationRunProgress `json:"progress,omitempty"`
	Status               ModelEvaluationRunStatus    `json:"status,omitempty"`
}

// CancelModelEvaluationRunRequest represents the request payload for cancelling
// a model evaluation run.
type CancelModelEvaluationRunRequest struct {
	EvalRunUUID string `json:"eval_run_uuid"`
}

// ModelEvaluationRunCancelResponse is the response returned by CancelModelEvaluationRun.
type ModelEvaluationRunCancelResponse struct {
	Run *ModelEvaluationRunSummary `json:"run,omitempty"`
}

// UpdateModelEvaluationRunRequest represents the request payload for updating a
// model evaluation run. Currently only the run's display name can be updated.
type UpdateModelEvaluationRunRequest struct {
	Name string `json:"name,omitempty"`
}

// ModelEvaluationRunUpdateResponse is the response returned by UpdateModelEvaluationRun.
type ModelEvaluationRunUpdateResponse struct {
	Run *ModelEvaluationRunSummary `json:"run,omitempty"`
}

// ModelEvaluationPresetDeleteResponse is the response returned by
// DeleteModelEvaluationPreset. The underlying API returns an empty object on
// success; this struct exists for forward compatibility and to keep the SDK
// signature consistent with sibling delete operations.
type ModelEvaluationPresetDeleteResponse struct{}

// DeleteModelEvaluationRun deletes the model evaluation run with the given UUID.
// The run must be in a terminal status (successful, partially_successful, failed,
// or cancelled). For runs still in progress, either wait for the run to finish or
// cancel it, then retry the delete.
func (s *GradientAIServiceOp) DeleteModelEvaluationRun(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunDeleteResponse, *Response, error) {
	if evalRunUUID == "" {
		return nil, nil, fmt.Errorf("eval run uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationRunByIDPath, evalRunUUID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// DeleteModelEvaluationPreset deletes the saved model evaluation preset with
// the given UUID.
func (s *GradientAIServiceOp) DeleteModelEvaluationPreset(ctx context.Context, evalPresetUUID string) (*ModelEvaluationPresetDeleteResponse, *Response, error) {
	if evalPresetUUID == "" {
		return nil, nil, fmt.Errorf("eval preset uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationPresetByIDPath, evalPresetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationPresetDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CancelModelEvaluationRun cancels an in-progress model evaluation run. The run
// must be in a non-terminal status (queued, running_dataset, or
// evaluating_results); already-terminal runs return an error. The returned
// summary's status is `cancelling` while the underlying workflow is being torn
// down and transitions to `cancelled` once cluster-side teardown completes.
func (s *GradientAIServiceOp) CancelModelEvaluationRun(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunCancelResponse, *Response, error) {
	if evalRunUUID == "" {
		return nil, nil, fmt.Errorf("eval run uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationRunCancelPath, evalRunUUID)

	cancelRequest := &CancelModelEvaluationRunRequest{
		EvalRunUUID: evalRunUUID,
	}

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, cancelRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunCancelResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UpdateModelEvaluationRun updates mutable fields on an existing model
// evaluation run identified by its UUID. Currently only the run's display name
// can be updated.
func (s *GradientAIServiceOp) UpdateModelEvaluationRun(ctx context.Context, evalRunUUID string, updateRequest *UpdateModelEvaluationRunRequest) (*ModelEvaluationRunUpdateResponse, *Response, error) {
	if evalRunUUID == "" {
		return nil, nil, fmt.Errorf("eval run uuid is required")
	}
	if updateRequest == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}
	path := fmt.Sprintf(UpdateModelEvaluationRunPath, evalRunUUID)

	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunUpdateResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CandidateInferenceConfig is the inference configuration applied to the
// candidate model when running a model evaluation run.
type CandidateInferenceConfig struct {
	MaxTokens    int64   `json:"max_tokens,omitempty"`
	StopToken    string  `json:"stop_token,omitempty"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
	Temperature  float32 `json:"temperature,omitempty"`
}

// PresignedUrlFile describes a single file for which a presigned upload URL is
// requested when uploading a model evaluation dataset.
type PresignedUrlFile struct {
	FileName string `json:"file_name,omitempty"`
	FileSize string `json:"file_size,omitempty"`
}

// FilePresignedUrlResponse describes the presigned URL details returned for a
// single requested file.
type FilePresignedUrlResponse struct {
	ExpiresAt        *Timestamp `json:"expires_at,omitempty"`
	ObjectKey        string     `json:"object_key,omitempty"`
	OriginalFileName string     `json:"original_file_name,omitempty"`
	PresignedURL     string     `json:"presigned_url,omitempty"`
}

// CreateModelEvalDatasetUploadPresignedURLsRequest is the request body for
// creating presigned URLs for uploading model evaluation dataset files.
type CreateModelEvalDatasetUploadPresignedURLsRequest struct {
	Files []*PresignedUrlFile `json:"files,omitempty"`
}

// CreateModelEvalDatasetUploadPresignedURLsResponse is the response returned by
// CreateModelEvalDatasetUploadPresignedURLs.
type CreateModelEvalDatasetUploadPresignedURLsResponse struct {
	RequestID string                      `json:"request_id,omitempty"`
	Uploads   []*FilePresignedUrlResponse `json:"uploads,omitempty"`
}

// CreateModelEvaluationRunRequest is the request body for creating a model
// evaluation run.
type CreateModelEvaluationRunRequest struct {
	CandidateInferenceConfig *CandidateInferenceConfig `json:"candidate_inference_config,omitempty"`
	CandidateModelName       string                    `json:"candidate_model_name,omitempty"`
	CandidateModelSource     CandidateModelSource      `json:"candidate_model_source,omitempty"`
	CandidateModelUUID       string                    `json:"candidate_model_uuid,omitempty"`
	DatasetUUID              string                    `json:"dataset_uuid,omitempty"`
	EvalPresetUUID           string                    `json:"eval_preset_uuid,omitempty"`
	JudgeModelUUID           string                    `json:"judge_model_uuid,omitempty"`
	MetricUUIDs              []string                  `json:"metric_uuids,omitempty"`
	Name                     string                    `json:"name,omitempty"`
	PresetName               string                    `json:"preset_name,omitempty"`
	Source                   string                    `json:"source,omitempty"`
	StarMetric               *StarMetric               `json:"star_metric,omitempty"`
}

// ModelEvaluationRunCreateResponse is the response returned by
// CreateModelEvaluationRun.
type ModelEvaluationRunCreateResponse struct {
	EvalRunUuid string `json:"eval_run_uuid,omitempty"`
}

// ModelEvaluationPreset is a saved, reusable configuration for model
// evaluation runs.
type ModelEvaluationPreset struct {
	CreatedAt      *Timestamp          `json:"created_at,omitempty"`
	DatasetName    string              `json:"dataset_name,omitempty"`
	DatasetUuid    string              `json:"dataset_uuid,omitempty"`
	EvalPresetUuid string              `json:"eval_preset_uuid,omitempty"`
	JudgeModelName string              `json:"judge_model_name,omitempty"`
	JudgeModelUuid string              `json:"judge_model_uuid,omitempty"`
	Metrics        []*EvaluationMetric `json:"metrics,omitempty"`
	Name           string              `json:"name,omitempty"`
	StarMetric     *StarMetric         `json:"star_metric,omitempty"`
}

// MetricResultSummary represents per-metric aggregated pass/fail statistics
// across all prompts in an evaluation run.
type MetricResultSummary struct {
	Description string  `json:"description,omitempty"`
	FailPercent float64 `json:"fail_percent,omitempty"`
	MetricName  string  `json:"metric_name,omitempty"`
	MetricUuid  string  `json:"metric_uuid,omitempty"`
	PassPercent float64 `json:"pass_percent,omitempty"`
}

// LatencyMetrics contains latency metrics for candidate model invocations,
// expressed in milliseconds.
type LatencyMetrics struct {
	AvgE2ELatencyMs float64 `json:"avg_e2e_latency_ms,omitempty"`
	MaxE2ELatencyMs float64 `json:"max_e2e_latency_ms,omitempty"`
	MinE2ELatencyMs float64 `json:"min_e2e_latency_ms,omitempty"`
	P50LatencyMs    float64 `json:"p50_latency_ms,omitempty"`
	P90LatencyMs    float64 `json:"p90_latency_ms,omitempty"`
	P95LatencyMs    float64 `json:"p95_latency_ms,omitempty"`
}

// TokenUsage contains aggregated token usage statistics for an evaluation run.
type TokenUsage struct {
	TotalCandidateInputTokens  string `json:"total_candidate_input_tokens,omitempty"`
	TotalCandidateOutputTokens string `json:"total_candidate_output_tokens,omitempty"`
	TotalCandidateTokens       string `json:"total_candidate_tokens,omitempty"`
	TotalJudgeInputTokens      string `json:"total_judge_input_tokens,omitempty"`
	TotalJudgeOutputTokens     string `json:"total_judge_output_tokens,omitempty"`
	TotalJudgeTokens           string `json:"total_judge_tokens,omitempty"`
}

// PerformanceMetrics contains performance metrics (latency and token usage)
// for an evaluation run. All performance metrics are for the candidate model
// unless noted otherwise.
type PerformanceMetrics struct {
	CandidateLatency *LatencyMetrics `json:"candidate_latency,omitempty"`
	TokenUsage       *TokenUsage     `json:"token_usage,omitempty"`
}

// TokenPricing contains the token pricing breakdown for a single model.
type TokenPricing struct {
	InputCost  float64 `json:"input_cost,omitempty"`
	OutputCost float64 `json:"output_cost,omitempty"`
	TotalCost  float64 `json:"total_cost,omitempty"`
}

// ModelPricingEntry contains the pricing entry for a specific model used in
// an evaluation run.
type ModelPricingEntry struct {
	ModelName   string        `json:"model_name,omitempty"`
	ModelUuid   string        `json:"model_uuid,omitempty"`
	Pricing     *TokenPricing `json:"pricing,omitempty"`
	PromptCount int64         `json:"prompt_count,omitempty"`
}

// EvaluationPricing contains the pricing breakdown for an evaluation run.
type EvaluationPricing struct {
	Currency                 string               `json:"currency,omitempty"`
	JudgeModelPricing        *TokenPricing        `json:"judge_model_pricing,omitempty"`
	PerCandidateModelPricing []*ModelPricingEntry `json:"per_candidate_model_pricing,omitempty"`
	TotalCost                float64              `json:"total_cost,omitempty"`
}

// StarMetricSummary contains the star metric summary with identifying details
// and threshold for an evaluation run.
type StarMetricSummary struct {
	MetricName string  `json:"metric_name,omitempty"`
	MetricUuid string  `json:"metric_uuid,omitempty"`
	Threshold  float32 `json:"threshold,omitempty"`
}

// PerModelResultSummary represents a per-model breakdown of evaluation
// results for router evaluations.
type PerModelResultSummary struct {
	MetricSummaries    []*MetricResultSummary `json:"metric_summaries,omitempty"`
	ModelName          string                 `json:"model_name,omitempty"`
	PerformanceMetrics *PerformanceMetrics    `json:"performance_metrics,omitempty"`
	PromptCount        int64                  `json:"prompt_count,omitempty"`
}

// PerModelResultSummaries wraps the per-model summaries used inside a
// ModelEvaluationRunResultSummary.
type PerModelResultSummaries struct {
	Summaries []*PerModelResultSummary `json:"summaries,omitempty"`
}

// ModelEvaluationRunResultSummary contains the aggregated result summary for
// a completed model evaluation run.
type ModelEvaluationRunResultSummary struct {
	EndTime              *Timestamp               `json:"end_time,omitempty"`
	MetricSummaries      []*MetricResultSummary   `json:"metric_summaries,omitempty"`
	OverallScorePercent  float64                  `json:"overall_score_percent,omitempty"`
	PerModelSummaries    *PerModelResultSummaries `json:"per_model_summaries,omitempty"`
	PerformanceMetrics   *PerformanceMetrics      `json:"performance_metrics,omitempty"`
	Pricing              *EvaluationPricing       `json:"pricing,omitempty"`
	StarMetricSummary    *StarMetricSummary       `json:"star_metric_summary,omitempty"`
	StartTime            *Timestamp               `json:"start_time,omitempty"`
	TotalDurationSeconds int64                    `json:"total_duration_seconds,omitempty"`
}

// ModelEvaluationRunProgress reports per-phase progress for a model evaluation
// run. The candidate phase invokes the candidate model once per dataset row;
// the judge phase scores each candidate-success row with the configured
// metrics. Counts grow as the run advances; compare against TotalRows to render
// a progress bar.
type ModelEvaluationRunProgress struct {
	// CandidateRowsEvaluated is the number of dataset rows whose candidate model
	// call has completed (success or failure).
	CandidateRowsEvaluated *int64 `json:"candidate_rows_evaluated,omitempty"`
	// JudgeRowsEvaluated is the number of candidate-success rows the judge has
	// finished (scored or skipped). It caps at the number of candidate
	// successes, which may be below TotalRows.
	JudgeRowsEvaluated *int64 `json:"judge_rows_evaluated,omitempty"`
	// TotalRows is the total number of dataset rows for the run, sourced from the
	// evaluation dataset.
	TotalRows *int64 `json:"total_rows,omitempty"`
}

// ModelEvaluationRunDetail is the full view of a model evaluation run
// returned when fetching a specific run.
type ModelEvaluationRunDetail struct {
	CandidateInferenceConfig *CandidateInferenceConfig        `json:"candidate_inference_config,omitempty"`
	CandidateModelName       string                           `json:"candidate_model_name,omitempty"`
	CandidateModelSource     CandidateModelSource             `json:"candidate_model_source,omitempty"`
	CandidateModelUuid       string                           `json:"candidate_model_uuid,omitempty"`
	CompletedAt              *Timestamp                       `json:"completed_at,omitempty"`
	CreatedAt                *Timestamp                       `json:"created_at,omitempty"`
	DatasetName              string                           `json:"dataset_name,omitempty"`
	DatasetUuid              string                           `json:"dataset_uuid,omitempty"`
	ErrorDescription         string                           `json:"error_description,omitempty"`
	EvalPresetName           string                           `json:"eval_preset_name,omitempty"`
	EvalPresetUuid           string                           `json:"eval_preset_uuid,omitempty"`
	EvalRunUuid              string                           `json:"eval_run_uuid,omitempty"`
	JudgeModelName           string                           `json:"judge_model_name,omitempty"`
	JudgeModelUuid           string                           `json:"judge_model_uuid,omitempty"`
	Metrics                  []*EvaluationMetric              `json:"metrics,omitempty"`
	Name                     string                           `json:"name,omitempty"`
	Progress                 *ModelEvaluationRunProgress      `json:"progress,omitempty"`
	ResultSummary            *ModelEvaluationRunResultSummary `json:"result_summary,omitempty"`
	StarMetric               *StarMetric                      `json:"star_metric,omitempty"`
	StartedAt                *Timestamp                       `json:"started_at,omitempty"`
	Status                   ModelEvaluationRunStatus         `json:"status,omitempty"`
}

// ModelEvaluationMetricResult represents the per-metric score and judge
// reasoning for a single prompt in an evaluation run.
type ModelEvaluationMetricResult struct {
	ErrorDescription string                    `json:"error_description,omitempty"`
	MetricName       string                    `json:"metric_name,omitempty"`
	MetricValueType  EvaluationMetricValueType `json:"metric_value_type,omitempty"`
	NumberValue      float64                   `json:"number_value,omitempty"`
	Reasoning        string                    `json:"reasoning,omitempty"`
	StringValue      string                    `json:"string_value,omitempty"`
}

// ModelEvaluationResult represents the per-prompt result for a model
// evaluation run.
type ModelEvaluationResult struct {
	CandidateModelName string                         `json:"candidate_model_name,omitempty"`
	CandidateModelUuid string                         `json:"candidate_model_uuid,omitempty"`
	GroundTruth        string                         `json:"ground_truth,omitempty"`
	Input              string                         `json:"input,omitempty"`
	MetricResults      []*ModelEvaluationMetricResult `json:"metric_results,omitempty"`
	Output             string                         `json:"output,omitempty"`
}

// ModelEvaluationRunGetOptions specifies optional pagination parameters for
// the per-prompt results returned by GetModelEvaluationRun.
type ModelEvaluationRunGetOptions struct {
	Page    int `url:"page,omitempty"`
	PerPage int `url:"per_page,omitempty"`
}

// ModelEvaluationRunGetResponse is the response returned by
// GetModelEvaluationRun. It contains the run detail and a paginated list of
// per-prompt evaluation results.
type ModelEvaluationRunGetResponse struct {
	Run     *ModelEvaluationRunDetail `json:"run,omitempty"`
	Results []*ModelEvaluationResult  `json:"results,omitempty"`
	Links   *Links                    `json:"links,omitempty"`
	Meta    *Meta                     `json:"meta,omitempty"`
}

// ModelEvaluationPresetGetResponse is the response returned by
// GetModelEvaluationPreset.
type ModelEvaluationPresetGetResponse struct {
	Preset *ModelEvaluationPreset `json:"preset,omitempty"`
}

// ModelEvaluationRunResultsDownloadURLResponse is the response returned by
// GetModelEvaluationRunResultsDownloadURL. It contains a presigned URL pointing
// to the gzip-compressed JSON results file.
type ModelEvaluationRunResultsDownloadURLResponse struct {
	DownloadURL string     `json:"download_url,omitempty"`
	ExpiresAt   *Timestamp `json:"expires_at,omitempty"`
}

// ModelEvaluationRunListOptions specifies optional parameters for listing
// model evaluation runs.
type ModelEvaluationRunListOptions struct {
	EvalPresetUUID string                          `url:"eval_preset_uuid,omitempty"`
	Status         ModelEvaluationRunStatus        `url:"status,omitempty"`
	Statuses       []ModelEvaluationRunStatus      `url:"statuses,omitempty"`
	CandidateTypes []CandidateModelSource          `url:"candidate_types,omitempty"`
	Search         string                          `url:"search,omitempty"`
	SortBy         ModelEvaluationRunSortField     `url:"sort_by,omitempty"`
	SortDirection  ModelEvaluationRunSortDirection `url:"sort_direction,omitempty"`
	ListOptions
}

// ModelEvaluationRunListResponse is the response returned by
// ListModelEvaluationRuns.
type ModelEvaluationRunListResponse struct {
	Runs  []*ModelEvaluationRunSummary `json:"runs,omitempty"`
	Links *Links                       `json:"links,omitempty"`
	Meta  *Meta                        `json:"meta,omitempty"`
}

// ModelEvaluationPresetListResponse is the response returned by
// ListModelEvaluationPresets.
type ModelEvaluationPresetListResponse struct {
	Presets []*ModelEvaluationPreset `json:"presets,omitempty"`
}

// ModelEvaluationMetricListResponse is the response returned by
// ListModelEvaluationMetrics.
type ModelEvaluationMetricListResponse struct {
	Metrics []*EvaluationMetric `json:"metrics,omitempty"`
}

// EvaluationDatasetListOptions holds the optional filters for
// ListEvaluationDatasets.
type EvaluationDatasetListOptions struct {
	// DatasetType filters the results by evaluation dataset type.
	DatasetType EvaluationDatasetType `url:"dataset_type,omitempty"`
}

// EvaluationDatasetInfo represents an evaluation dataset returned by
// ListEvaluationDatasets.
type EvaluationDatasetInfo struct {
	CreatedAt      *Timestamp            `json:"created_at,omitempty"`
	DatasetName    string                `json:"dataset_name,omitempty"`
	DatasetType    EvaluationDatasetType `json:"dataset_type,omitempty"`
	DatasetUUID    string                `json:"dataset_uuid,omitempty"`
	FileSize       string                `json:"file_size,omitempty"`
	HasGroundTruth bool                  `json:"has_ground_truth,omitempty"`
	RowCount       int64                 `json:"row_count,omitempty"`
}

// EvaluationDatasetListResponse is the response returned by
// ListEvaluationDatasets.
type EvaluationDatasetListResponse struct {
	EvaluationDatasets []*EvaluationDatasetInfo `json:"evaluation_datasets,omitempty"`
}

// EvaluationDatasetDeleteResponse is the response returned by
// DeleteEvaluationDataset.
type EvaluationDatasetDeleteResponse struct{}

// CreateModelEvaluationRun creates a new model evaluation run.
func (s *GradientAIServiceOp) CreateModelEvaluationRun(ctx context.Context, createRequest *CreateModelEvaluationRunRequest) (*ModelEvaluationRunCreateResponse, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, modelEvaluationRunsBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunCreateResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateModelEvalDatasetUploadPresignedURLs creates presigned URLs that can be
// used to upload model evaluation dataset files.
func (s *GradientAIServiceOp) CreateModelEvalDatasetUploadPresignedURLs(ctx context.Context, createRequest *CreateModelEvalDatasetUploadPresignedURLsRequest) (*CreateModelEvalDatasetUploadPresignedURLsResponse, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, modelEvaluationDatasetUploadURLsPath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(CreateModelEvalDatasetUploadPresignedURLsResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetModelEvaluationRun retrieves a model evaluation run by UUID. Optional
// pagination options control the per-prompt results page returned alongside
// the run detail.
func (s *GradientAIServiceOp) GetModelEvaluationRun(ctx context.Context, evalRunUUID string, opt *ModelEvaluationRunGetOptions) (*ModelEvaluationRunGetResponse, *Response, error) {
	if evalRunUUID == "" {
		return nil, nil, fmt.Errorf("eval run uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationRunByIDPath, evalRunUUID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunGetResponse)
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
	return root, resp, nil
}

// GetModelEvaluationPreset retrieves a saved model evaluation preset by UUID.
func (s *GradientAIServiceOp) GetModelEvaluationPreset(ctx context.Context, evalPresetUUID string) (*ModelEvaluationPresetGetResponse, *Response, error) {
	if evalPresetUUID == "" {
		return nil, nil, fmt.Errorf("eval preset uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationPresetByIDPath, evalPresetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationPresetGetResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetModelEvaluationRunResultsDownloadURL returns a presigned download URL
// (gzip-compressed JSON) for a model evaluation run's results.
func (s *GradientAIServiceOp) GetModelEvaluationRunResultsDownloadURL(ctx context.Context, evalRunUUID string) (*ModelEvaluationRunResultsDownloadURLResponse, *Response, error) {
	if evalRunUUID == "" {
		return nil, nil, fmt.Errorf("eval run uuid is required")
	}
	path := fmt.Sprintf(modelEvaluationRunResultsDownloadURLPath, evalRunUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunResultsDownloadURLResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListModelEvaluationRuns lists model evaluation runs. Results can be filtered
// by preset UUID, status (single or multiple), candidate model source types,
// and a free-text search across run, candidate model, and dataset names. The
// result set can also be sorted via SortBy / SortDirection.
func (s *GradientAIServiceOp) ListModelEvaluationRuns(ctx context.Context, opt *ModelEvaluationRunListOptions) (*ModelEvaluationRunListResponse, *Response, error) {
	path, err := addOptions(modelEvaluationRunsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationRunListResponse)
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
	return root, resp, nil
}

// ListModelEvaluationPresets lists all saved model evaluation presets.
func (s *GradientAIServiceOp) ListModelEvaluationPresets(ctx context.Context) (*ModelEvaluationPresetListResponse, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, modelEvaluationPresetsBasePath, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationPresetListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListModelEvaluationMetrics lists all available metrics that can be selected
// when creating a model evaluation run.
func (s *GradientAIServiceOp) ListModelEvaluationMetrics(ctx context.Context) (*ModelEvaluationMetricListResponse, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, modelEvaluationMetricsBasePath, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ModelEvaluationMetricListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListEvaluationDatasets lists evaluation datasets. Results can be filtered by
// dataset type using the provided options.
func (s *GradientAIServiceOp) ListEvaluationDatasets(ctx context.Context, opt *EvaluationDatasetListOptions) (*EvaluationDatasetListResponse, *Response, error) {
	path, err := addOptions(evaluationDatasetsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(EvaluationDatasetListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// DeleteEvaluationDataset deletes the evaluation dataset with the given UUID.
func (s *GradientAIServiceOp) DeleteEvaluationDataset(ctx context.Context, datasetUUID string) (*EvaluationDatasetDeleteResponse, *Response, error) {
	if datasetUUID == "" {
		return nil, nil, fmt.Errorf("dataset uuid is required")
	}
	path := fmt.Sprintf(evaluationDatasetByIDPath, datasetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(EvaluationDatasetDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UpdateCustomModelMetadata updates the metadata of an existing custom model.
func (s *GradientAIServiceOp) UpdateCustomModelMetadata(ctx context.Context, uuid string, updateRequest *CustomModelMetadataUpdateRequest) (*CustomModel, *Response, error) {
	if uuid == "" {
		return nil, nil, fmt.Errorf("uuid is required")
	}
	if updateRequest == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}
	path := fmt.Sprintf(customModelMetadataPath, uuid)

	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(customModelRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Model, resp, nil
}

// CreateCustomEvaluationMetric creates a custom model-evaluation metric.
func (s *GradientAIServiceOp) CreateCustomEvaluationMetric(ctx context.Context, createRequest *CreateCustomEvaluationMetricRequest) (*EvaluationMetric, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, customEvaluationMetricsPath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(customEvaluationMetricRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Metric, resp, nil
}

// UpdateCustomEvaluationMetric updates an existing custom model-evaluation metric.
func (s *GradientAIServiceOp) UpdateCustomEvaluationMetric(ctx context.Context, metricUUID string, updateRequest *UpdateCustomEvaluationMetricRequest) (*EvaluationMetric, *Response, error) {
	if metricUUID == "" {
		return nil, nil, fmt.Errorf("metricUUID is required")
	}
	if updateRequest == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}

	path := fmt.Sprintf(customEvaluationMetricByIDPath, metricUUID)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(customEvaluationMetricRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Metric, resp, nil
}

// DeleteCustomEvaluationMetric soft-deletes a custom model-evaluation metric.
func (s *GradientAIServiceOp) DeleteCustomEvaluationMetric(ctx context.Context, metricUUID string) (*Response, error) {
	if metricUUID == "" {
		return nil, fmt.Errorf("metricUUID is required")
	}

	path := fmt.Sprintf(customEvaluationMetricByIDPath, metricUUID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)
	return resp, err
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

func (r InferenceRouterSummary) String() string {
	return Stringify(r)
}

func (r InferenceRouter) String() string {
	return Stringify(r)
}

func (r InferenceRouterConfig) String() string {
	return Stringify(r)
}

func (r InferenceRouterDeleteResponse) String() string {
	return Stringify(r)
}
