package godo

import (
	"context"
	"fmt"
	"net/http"
)

const (
	scenarioSetsBasePath                 = "/v2/gen-ai/scenario_sets"
	scenarioSetUploadPresignedURLsPath   = scenarioSetsBasePath + "/file_upload_presigned_urls"
	scenarioSetGeneratePath              = scenarioSetsBasePath + "/generate"
	scenarioSetByIDPath                  = scenarioSetsBasePath + "/%s"
	scenarioSetScenariosPath             = scenarioSetByIDPath + "/scenarios"
	scenarioSetDownloadURLPath           = scenarioSetByIDPath + "/download_url"
	scenarioLibraryBasePath              = "/v2/gen-ai/scenario_library"
	scenarioLibraryByIDPath              = scenarioLibraryBasePath + "/%s"
	scenarioLibraryScenariosPath         = scenarioLibraryByIDPath + "/scenarios"
	scenarioLibraryCreateScenarioSetPath = scenarioLibraryByIDPath + "/create_scenario_set"
	simulationRunsBasePath               = "/v2/gen-ai/simulation_runs"
	simulationRunByIDPath                = simulationRunsBasePath + "/%s"
	simulationRunCancelPath              = simulationRunByIDPath + "/cancel"
	simulationRunEpisodesPath            = simulationRunByIDPath + "/episodes"
	simulationEpisodeByIDPath            = simulationRunEpisodesPath + "/%s"
	simulationEpisodeTrajectoryPath      = simulationEpisodeByIDPath + "/trajectory"
	simulationEpisodeTrajectoryURLPath   = simulationEpisodeByIDPath + "/trajectory_url"
)

// GenAISortDirection is the sort direction used by GenAI list endpoints.
type GenAISortDirection string

const (
	GenAISortDirectionUnspecified GenAISortDirection = "SORT_DIRECTION_UNSPECIFIED"
	GenAISortDirectionAsc         GenAISortDirection = "SORT_DIRECTION_ASC"
	GenAISortDirectionDesc        GenAISortDirection = "SORT_DIRECTION_DESC"
)

// ScenarioSetStatus is the lifecycle status of a scenario set.
type ScenarioSetStatus string

const (
	ScenarioSetStatusUnspecified ScenarioSetStatus = "SCENARIO_SET_STATUS_UNSPECIFIED"
	ScenarioSetStatusGenerating  ScenarioSetStatus = "SCENARIO_SET_STATUS_GENERATING"
	ScenarioSetStatusReady       ScenarioSetStatus = "SCENARIO_SET_STATUS_READY"
	ScenarioSetStatusFailed      ScenarioSetStatus = "SCENARIO_SET_STATUS_FAILED"
	ScenarioSetStatusCancelled   ScenarioSetStatus = "SCENARIO_SET_STATUS_CANCELLED"
)

// ScenarioSetSourceKind describes how a scenario set was created.
type ScenarioSetSourceKind string

const (
	ScenarioSetSourceKindUnspecified     ScenarioSetSourceKind = "SCENARIO_SET_SOURCE_KIND_UNSPECIFIED"
	ScenarioSetSourceKindUserUpload      ScenarioSetSourceKind = "SCENARIO_SET_SOURCE_KIND_USER_UPLOAD"
	ScenarioSetSourceKindGoalGenerated   ScenarioSetSourceKind = "SCENARIO_SET_SOURCE_KIND_GOAL_GENERATED"
	ScenarioSetSourceKindLibrary         ScenarioSetSourceKind = "SCENARIO_SET_SOURCE_KIND_LIBRARY"
	ScenarioSetSourceKindSignalGenerated ScenarioSetSourceKind = "SCENARIO_SET_SOURCE_KIND_SIGNAL_GENERATED"
)

// ScenarioSetSortField is a sortable field for ListScenarioSets.
type ScenarioSetSortField string

const (
	ScenarioSetSortFieldUnspecified   ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_UNSPECIFIED"
	ScenarioSetSortFieldCreatedAt     ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_CREATED_AT"
	ScenarioSetSortFieldName          ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_NAME"
	ScenarioSetSortFieldStatus        ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_STATUS"
	ScenarioSetSortFieldScenarioCount ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_SCENARIO_COUNT"
	ScenarioSetSortFieldUpdatedAt     ScenarioSetSortField = "SCENARIO_SET_SORT_FIELD_UPDATED_AT"
)

// ScenarioSortField is a sortable field for ListScenarios.
type ScenarioSortField string

const (
	ScenarioSortFieldUnspecified ScenarioSortField = "SCENARIO_SORT_FIELD_UNSPECIFIED"
	ScenarioSortFieldFileOrder   ScenarioSortField = "SCENARIO_SORT_FIELD_FILE_ORDER"
	ScenarioSortFieldName        ScenarioSortField = "SCENARIO_SORT_FIELD_NAME"
	ScenarioSortFieldDescription ScenarioSortField = "SCENARIO_SORT_FIELD_DESCRIPTION"
)

// ScenarioLibraryEntryStatus is the lifecycle status of a scenario library entry.
type ScenarioLibraryEntryStatus string

const (
	ScenarioLibraryEntryStatusUnspecified ScenarioLibraryEntryStatus = "SCENARIO_LIBRARY_ENTRY_STATUS_UNSPECIFIED"
	ScenarioLibraryEntryStatusActive      ScenarioLibraryEntryStatus = "SCENARIO_LIBRARY_ENTRY_STATUS_ACTIVE"
	ScenarioLibraryEntryStatusArchived    ScenarioLibraryEntryStatus = "SCENARIO_LIBRARY_ENTRY_STATUS_ARCHIVED"
)

// ScenarioLibrarySortField is a sortable field for ListScenarioLibrary.
type ScenarioLibrarySortField string

const (
	ScenarioLibrarySortFieldUnspecified ScenarioLibrarySortField = "SCENARIO_LIBRARY_SORT_FIELD_UNSPECIFIED"
	ScenarioLibrarySortFieldName        ScenarioLibrarySortField = "SCENARIO_LIBRARY_SORT_FIELD_NAME"
	ScenarioLibrarySortFieldCreatedAt   ScenarioLibrarySortField = "SCENARIO_LIBRARY_SORT_FIELD_CREATED_AT"
)

// SimulationRunStatus is the lifecycle status of a simulation run.
type SimulationRunStatus string

const (
	SimulationRunStatusUnspecified         SimulationRunStatus = "SIMULATION_RUN_STATUS_UNSPECIFIED"
	SimulationRunStatusPending             SimulationRunStatus = "SIMULATION_RUN_STATUS_PENDING"
	SimulationRunStatusRunning             SimulationRunStatus = "SIMULATION_RUN_STATUS_RUNNING"
	SimulationRunStatusSucceeded           SimulationRunStatus = "SIMULATION_RUN_STATUS_SUCCEEDED"
	SimulationRunStatusFailed              SimulationRunStatus = "SIMULATION_RUN_STATUS_FAILED"
	SimulationRunStatusCancelled           SimulationRunStatus = "SIMULATION_RUN_STATUS_CANCELLED"
	SimulationRunStatusEvaluating          SimulationRunStatus = "SIMULATION_RUN_STATUS_EVALUATING"
	SimulationRunStatusPartiallySuccessful SimulationRunStatus = "SIMULATION_RUN_STATUS_PARTIALLY_SUCCESSFUL"
)

// SimulationRunSortField is a sortable field for ListSimulationRuns.
type SimulationRunSortField string

const (
	SimulationRunSortFieldUnspecified SimulationRunSortField = "SIMULATION_RUN_SORT_FIELD_UNSPECIFIED"
	SimulationRunSortFieldCreatedAt   SimulationRunSortField = "SIMULATION_RUN_SORT_FIELD_CREATED_AT"
	SimulationRunSortFieldName        SimulationRunSortField = "SIMULATION_RUN_SORT_FIELD_NAME"
	SimulationRunSortFieldStatus      SimulationRunSortField = "SIMULATION_RUN_SORT_FIELD_STATUS"
	SimulationRunSortFieldUpdatedAt   SimulationRunSortField = "SIMULATION_RUN_SORT_FIELD_UPDATED_AT"
)

// SimulationEpisodeStatus is the lifecycle status of a simulation episode.
type SimulationEpisodeStatus string

const (
	SimulationEpisodeStatusUnspecified SimulationEpisodeStatus = "SIMULATION_EPISODE_STATUS_UNSPECIFIED"
	SimulationEpisodeStatusRunning     SimulationEpisodeStatus = "SIMULATION_EPISODE_STATUS_RUNNING"
	SimulationEpisodeStatusFinished    SimulationEpisodeStatus = "SIMULATION_EPISODE_STATUS_FINISHED"
	SimulationEpisodeStatusFailed      SimulationEpisodeStatus = "SIMULATION_EPISODE_STATUS_FAILED"
	SimulationEpisodeStatusPreparing   SimulationEpisodeStatus = "SIMULATION_EPISODE_STATUS_PREPARING"
)

// SimulationEpisodeVerdict is the judge verdict for an episode.
type SimulationEpisodeVerdict string

const (
	SimulationEpisodeVerdictUnspecified  SimulationEpisodeVerdict = "SIMULATION_EPISODE_VERDICT_UNSPECIFIED"
	SimulationEpisodeVerdictSuccess      SimulationEpisodeVerdict = "SIMULATION_EPISODE_VERDICT_SUCCESS"
	SimulationEpisodeVerdictFailure      SimulationEpisodeVerdict = "SIMULATION_EPISODE_VERDICT_FAILURE"
	SimulationEpisodeVerdictInconclusive SimulationEpisodeVerdict = "SIMULATION_EPISODE_VERDICT_INCONCLUSIVE"
)

// SimulationEpisodeSortField is a sortable field for ListSimulationEpisodes.
type SimulationEpisodeSortField string

const (
	SimulationEpisodeSortFieldUnspecified SimulationEpisodeSortField = "SIMULATION_EPISODE_SORT_FIELD_UNSPECIFIED"
	SimulationEpisodeSortFieldScenario    SimulationEpisodeSortField = "SIMULATION_EPISODE_SORT_FIELD_SCENARIO"
	SimulationEpisodeSortFieldCreatedAt   SimulationEpisodeSortField = "SIMULATION_EPISODE_SORT_FIELD_CREATED_AT"
	SimulationEpisodeSortFieldStatus      SimulationEpisodeSortField = "SIMULATION_EPISODE_SORT_FIELD_STATUS"
	SimulationEpisodeSortFieldVerdict     SimulationEpisodeSortField = "SIMULATION_EPISODE_SORT_FIELD_VERDICT"
)

// SimulationTrajectoryStatus is the lifecycle status of a trajectory JSON object.
type SimulationTrajectoryStatus string

const (
	SimulationTrajectoryStatusUnspecified SimulationTrajectoryStatus = "SIMULATION_TRAJECTORY_STATUS_UNSPECIFIED"
	SimulationTrajectoryStatusRunning     SimulationTrajectoryStatus = "SIMULATION_TRAJECTORY_STATUS_RUNNING"
	SimulationTrajectoryStatusCompleted   SimulationTrajectoryStatus = "SIMULATION_TRAJECTORY_STATUS_COMPLETED"
	SimulationTrajectoryStatusFailed      SimulationTrajectoryStatus = "SIMULATION_TRAJECTORY_STATUS_FAILED"
	SimulationTrajectoryStatusCancelled   SimulationTrajectoryStatus = "SIMULATION_TRAJECTORY_STATUS_CANCELLED"
)

// Scenario is a single unit of what to test within a scenario set.
type Scenario struct {
	ScenarioUUID      string   `json:"scenario_uuid,omitempty"`
	Name              string   `json:"name,omitempty"`
	Description       string   `json:"description,omitempty"`
	UserPersona       string   `json:"user_persona,omitempty"`
	StoppingCriteria  []string `json:"stopping_criteria,omitempty"`
	MaxTurns          uint32   `json:"max_turns,omitempty"`
	ExplorationBudget uint32   `json:"exploration_budget,omitempty"`
}

// ScenarioSet is a team-owned pool of scenarios backed by a canonical JSONL file.
type ScenarioSet struct {
	ScenarioSetUUID       string                `json:"scenario_set_uuid,omitempty"`
	Name                  string                `json:"name,omitempty"`
	Description           string                `json:"description,omitempty"`
	Status                ScenarioSetStatus     `json:"status,omitempty"`
	SourceKind            ScenarioSetSourceKind `json:"source_kind,omitempty"`
	ScenarioCount         uint32                `json:"scenario_count,omitempty"`
	BucketName            string                `json:"bucket_name,omitempty"`
	BucketRegion          string                `json:"bucket_region,omitempty"`
	SpacesKey             string                `json:"spaces_key,omitempty"`
	FailureReason         string                `json:"failure_reason,omitempty"`
	SourceGoalDescription string                `json:"source_goal_description,omitempty"`
	GeneratorModelUUID    string                `json:"generator_model_uuid,omitempty"`
	WorkflowUUID          string                `json:"workflow_uuid,omitempty"`
	LibraryScenarioUUID   string                `json:"library_scenario_uuid,omitempty"`
	SourceExportID        string                `json:"source_export_id,omitempty"`
	CreatedAt             *Timestamp            `json:"created_at,omitempty"`
	UpdatedAt             *Timestamp            `json:"updated_at,omitempty"`
	DeletedAt             *Timestamp            `json:"deleted_at,omitempty"`
}

// ScenarioLibraryEntry is a platform-curated scenario set from the common library.
type ScenarioLibraryEntry struct {
	LibraryScenarioUUID string                     `json:"library_scenario_uuid,omitempty"`
	Name                string                     `json:"name,omitempty"`
	Description         string                     `json:"description,omitempty"`
	GoalDescription     string                     `json:"goal_description,omitempty"`
	Category            string                     `json:"category,omitempty"`
	ScenarioCount       uint32                     `json:"scenario_count,omitempty"`
	Status              ScenarioLibraryEntryStatus `json:"status,omitempty"`
	CreatedAt           *Timestamp                 `json:"created_at,omitempty"`
	UpdatedAt           *Timestamp                 `json:"updated_at,omitempty"`
}

// CandidateAgentConfig configures the candidate agent under test for a simulation run.
type CandidateAgentConfig struct {
	AgentUUID           string `json:"agent_uuid,omitempty"`
	AgentDeploymentUUID string `json:"agent_deployment_uuid,omitempty"`
	Name                string `json:"name,omitempty"`
}

// SimulationEvaluationConfig opts a simulation run into an attached evaluation.
type SimulationEvaluationConfig struct {
	MetricUUIDs []string    `json:"metric_uuids,omitempty"`
	StarMetric  *StarMetric `json:"star_metric,omitempty"`
}

// SimulationTokenUsage is per-actor token accounting for a run or episode.
type SimulationTokenUsage struct {
	SimulatorTokens      string `json:"simulator_tokens,omitempty"`
	JudgeTokens          string `json:"judge_tokens,omitempty"`
	CandidateAgentTokens string `json:"candidate_agent_tokens,omitempty"`
	GeneratorTokens      string `json:"generator_tokens,omitempty"`
	TotalTokens          string `json:"total_tokens,omitempty"`
}

// SimulationEpisodeCounts is a breakdown of episode outcomes by verdict.
type SimulationEpisodeCounts struct {
	SuccessCount      uint32 `json:"success_count,omitempty"`
	FailureCount      uint32 `json:"failure_count,omitempty"`
	InconclusiveCount uint32 `json:"inconclusive_count,omitempty"`
}

// SimulationRunResultSummary is the aggregated final result of a simulation run.
type SimulationRunResultSummary struct {
	VerdictCounts    *SimulationEpisodeCounts `json:"verdict_counts,omitempty"`
	TokenUsage       *SimulationTokenUsage    `json:"token_usage,omitempty"`
	TotalDurationSec string                   `json:"total_duration_sec,omitempty"`
}

// SimulationScenarioResult is a per-scenario rollup of episode outcomes within a run.
type SimulationScenarioResult struct {
	ScenarioUUID     string                   `json:"scenario_uuid,omitempty"`
	TotalEpisodes    uint32                   `json:"total_episodes,omitempty"`
	EpisodesFinished uint32                   `json:"episodes_finished,omitempty"`
	VerdictCounts    *SimulationEpisodeCounts `json:"verdict_counts,omitempty"`
}

// SimulationRun is one execution of a scenario set against a candidate agent.
type SimulationRun struct {
	RunUUID                string                      `json:"run_uuid,omitempty"`
	Name                   string                      `json:"name,omitempty"`
	ScenarioSetUUID        string                      `json:"scenario_set_uuid,omitempty"`
	AgentConfig            *CandidateAgentConfig       `json:"agent_config,omitempty"`
	Status                 SimulationRunStatus         `json:"status,omitempty"`
	UserSimulatorModelUUID string                      `json:"user_simulator_model_uuid,omitempty"`
	JudgeModelUUID         string                      `json:"judge_model_uuid,omitempty"`
	UserSimulatorConfig    map[string]interface{}      `json:"user_simulator_config,omitempty"`
	ScenarioCount          uint32                      `json:"scenario_count,omitempty"`
	TotalEpisodes          uint32                      `json:"total_episodes,omitempty"`
	EpisodesFinished       uint32                      `json:"episodes_finished,omitempty"`
	ResultSummary          *SimulationRunResultSummary `json:"result_summary,omitempty"`
	WorkflowUUID           string                      `json:"workflow_uuid,omitempty"`
	ExplorationBudget      uint32                      `json:"exploration_budget,omitempty"`
	MaxTurns               uint32                      `json:"max_turns,omitempty"`
	FailureReason          string                      `json:"failure_reason,omitempty"`
	CreatedAt              *Timestamp                  `json:"created_at,omitempty"`
	UpdatedAt              *Timestamp                  `json:"updated_at,omitempty"`
	DeletedAt              *Timestamp                  `json:"deleted_at,omitempty"`
	UserSimulatorModelName string                      `json:"user_simulator_model_name,omitempty"`
	JudgeModelName         string                      `json:"judge_model_name,omitempty"`
	CreatedByUserID        string                      `json:"created_by_user_id,omitempty"`
	CreatedByUserEmail     string                      `json:"created_by_user_email,omitempty"`
	EvaluationRunUUID      string                      `json:"evaluation_run_uuid,omitempty"`
}

// SimulationEpisode is one independent execution within a simulation run.
type SimulationEpisode struct {
	EpisodeUUID            string                   `json:"episode_uuid,omitempty"`
	RunUUID                string                   `json:"run_uuid,omitempty"`
	ScenarioUUID           string                   `json:"scenario_uuid,omitempty"`
	EpisodeIndex           uint32                   `json:"episode_index,omitempty"`
	SessionID              string                   `json:"session_id,omitempty"`
	Status                 SimulationEpisodeStatus  `json:"status,omitempty"`
	Verdict                SimulationEpisodeVerdict `json:"verdict,omitempty"`
	TrajectoryBucketName   string                   `json:"trajectory_bucket_name,omitempty"`
	TrajectoryBucketRegion string                   `json:"trajectory_bucket_region,omitempty"`
	TrajectorySpacesKey    string                   `json:"trajectory_spaces_key,omitempty"`
	TokenUsage             *SimulationTokenUsage    `json:"token_usage,omitempty"`
	DurationSec            string                   `json:"duration_sec,omitempty"`
	JudgeReasoning         string                   `json:"judge_reasoning,omitempty"`
	FailureReason          string                   `json:"failure_reason,omitempty"`
	CreatedAt              *Timestamp               `json:"created_at,omitempty"`
	UpdatedAt              *Timestamp               `json:"updated_at,omitempty"`
}

// SimulationTrajectoryToolCall is a tool call recorded on an assistant turn.
type SimulationTrajectoryToolCall struct {
	Name            string                 `json:"name,omitempty"`
	Description     string                 `json:"description,omitempty"`
	InputParameters map[string]interface{} `json:"input_parameters,omitempty"`
	Output          interface{}            `json:"output,omitempty"`
	ToolCallID      string                 `json:"tool_call_id,omitempty"`
	Ok              bool                   `json:"ok,omitempty"`
}

// SimulationTrajectoryMessageTokens is token usage for a single assistant message.
type SimulationTrajectoryMessageTokens struct {
	Input  uint32 `json:"input,omitempty"`
	Output uint32 `json:"output,omitempty"`
}

// SimulationTrajectoryMessage is one user or assistant message in a trajectory.
type SimulationTrajectoryMessage struct {
	TurnIndex   uint32                             `json:"turn_index,omitempty"`
	Role        string                             `json:"role,omitempty"`
	Content     string                             `json:"content,omitempty"`
	StartedAt   string                             `json:"started_at,omitempty"`
	CompletedAt string                             `json:"completed_at,omitempty"`
	ToolCalls   []*SimulationTrajectoryToolCall    `json:"tool_calls,omitempty"`
	Tokens      *SimulationTrajectoryMessageTokens `json:"tokens,omitempty"`
}

// SimulationTrajectoryJudgeCriterion is a per-criterion judge evaluation.
type SimulationTrajectoryJudgeCriterion struct {
	Criterion string `json:"criterion,omitempty"`
	Passed    bool   `json:"passed,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// SimulationTrajectoryJudgeResult is judge output embedded in a trajectory.
type SimulationTrajectoryJudgeResult struct {
	Verdict          SimulationEpisodeVerdict              `json:"verdict,omitempty"`
	Reasoning        string                                `json:"reasoning,omitempty"`
	CriteriaPassFail []*SimulationTrajectoryJudgeCriterion `json:"criteria_pass_fail,omitempty"`
}

// EvaluationMetricResultStatus is the outcome of scoring a single metric.
// Matches cthulhu EvaluationMetricResultStatus.
type EvaluationMetricResultStatus string

const (
	EvaluationMetricResultStatusUnspecified EvaluationMetricResultStatus = "EVALUATION_METRIC_RESULT_STATUS_UNSPECIFIED"
	EvaluationMetricResultStatusCompleted   EvaluationMetricResultStatus = "EVALUATION_METRIC_RESULT_STATUS_COMPLETED"
	EvaluationMetricResultStatusFailed      EvaluationMetricResultStatus = "EVALUATION_METRIC_RESULT_STATUS_FAILED"
	EvaluationMetricResultStatusSkipped     EvaluationMetricResultStatus = "EVALUATION_METRIC_RESULT_STATUS_SKIPPED"
)

// EvaluationMetricResult is a per-metric score and judge reasoning.
// Matches cthulhu EvaluationMetricResult (used on simulation trajectories).
type EvaluationMetricResult struct {
	ErrorDescription string                       `json:"error_description,omitempty"`
	MetricName       string                       `json:"metric_name,omitempty"`
	MetricUUID       string                       `json:"metric_uuid,omitempty"`
	MetricValueType  EvaluationMetricValueType    `json:"metric_value_type,omitempty"`
	NumberValue      float64                      `json:"number_value,omitempty"`
	Reasoning        string                       `json:"reasoning,omitempty"`
	Status           EvaluationMetricResultStatus `json:"status,omitempty"`
	StringValue      string                       `json:"string_value,omitempty"`
}

// SimulationTrajectory is the canonical trajectory JSON for one episode.
type SimulationTrajectory struct {
	AgentID           string                           `json:"agent_id,omitempty"`
	CompletedAt       string                           `json:"completed_at,omitempty"`
	DurationSec       string                           `json:"duration_sec,omitempty"`
	EvaluationMetrics []*EvaluationMetricResult        `json:"evaluation_metrics,omitempty"`
	FailureReason     string                           `json:"failure_reason,omitempty"`
	EpisodeIndex      uint32                           `json:"episode_index,omitempty"`
	EpisodeUUID       string                           `json:"episode_uuid,omitempty"`
	Judge             *SimulationTrajectoryJudgeResult `json:"judge,omitempty"`
	MaxTurns          uint32                           `json:"max_turns,omitempty"`
	Messages          []*SimulationTrajectoryMessage   `json:"messages,omitempty"`
	RunUUID           string                           `json:"run_uuid,omitempty"`
	ScenarioUUID      string                           `json:"scenario_uuid,omitempty"`
	SessionID         string                           `json:"session_id,omitempty"`
	StartedAt         string                           `json:"started_at,omitempty"`
	Status            SimulationTrajectoryStatus       `json:"status,omitempty"`
	TokenUsage        *SimulationTokenUsage            `json:"token_usage,omitempty"`
	TurnCount         uint32                           `json:"turn_count,omitempty"`
	Verdict           SimulationEpisodeVerdict         `json:"verdict,omitempty"`
}

// CreateScenarioSetUploadPresignedURLsRequest requests presigned upload URLs for scenario set files.
type CreateScenarioSetUploadPresignedURLsRequest struct {
	Files []*PresignedUrlFile `json:"files,omitempty"`
}

// CreateScenarioSetUploadPresignedURLsResponse is returned by CreateScenarioSetUploadPresignedURLs.
type CreateScenarioSetUploadPresignedURLsResponse struct {
	RequestID string                      `json:"request_id,omitempty"`
	Uploads   []*FilePresignedUrlResponse `json:"uploads,omitempty"`
}

// CreateScenarioSetRequest creates a scenario set from inline scenarios or an uploaded file.
type CreateScenarioSetRequest struct {
	Name                  string                `json:"name,omitempty"`
	Scenarios             []*Scenario           `json:"scenarios,omitempty"`
	FileUploadScenarioSet *FileUploadDataSource `json:"file_upload_scenario_set,omitempty"`
}

// GenerateScenarioSetRequest dispatches goal-driven scenario generation.
type GenerateScenarioSetRequest struct {
	Name               string `json:"name,omitempty"`
	GoalDescription    string `json:"goal_description,omitempty"`
	NumScenarios       uint32 `json:"num_scenarios,omitempty"`
	GeneratorModelUUID string `json:"generator_model_uuid,omitempty"`
}

// UpdateScenarioSetRequest updates a scenario set name and/or replaces its scenarios.
type UpdateScenarioSetRequest struct {
	ScenarioSetUUID string      `json:"scenario_set_uuid,omitempty"`
	Name            string      `json:"name,omitempty"`
	Scenarios       []*Scenario `json:"scenarios,omitempty"`
}

// CreateScenarioSetFromLibraryRequest materializes a library entry into a team-owned scenario set.
type CreateScenarioSetFromLibraryRequest struct {
	LibraryScenarioUUID string `json:"library_scenario_uuid,omitempty"`
	Name                string `json:"name,omitempty"`
}

// CreateSimulationRunRequest creates a simulation run.
type CreateSimulationRunRequest struct {
	ScenarioSetUUID        string                      `json:"scenario_set_uuid,omitempty"`
	Name                   string                      `json:"name,omitempty"`
	AgentConfig            *CandidateAgentConfig       `json:"agent_config,omitempty"`
	UserSimulatorModelUUID string                      `json:"user_simulator_model_uuid,omitempty"`
	JudgeModelUUID         string                      `json:"judge_model_uuid,omitempty"`
	UserSimulatorConfig    map[string]interface{}      `json:"user_simulator_config,omitempty"`
	ExplorationBudget      uint32                      `json:"exploration_budget,omitempty"`
	MaxTurns               uint32                      `json:"max_turns,omitempty"`
	EvaluationConfig       *SimulationEvaluationConfig `json:"evaluation_config,omitempty"`
}

// UpdateSimulationRunRequest renames a simulation run.
type UpdateSimulationRunRequest struct {
	RunUUID string `json:"run_uuid,omitempty"`
	Name    string `json:"name,omitempty"`
}

// ScenarioSetListOptions specifies optional parameters for listing scenario sets.
type ScenarioSetListOptions struct {
	Statuses      []ScenarioSetStatus     `url:"statuses,omitempty"`
	SourceKinds   []ScenarioSetSourceKind `url:"source_kinds,omitempty"`
	Search        string                  `url:"search,omitempty"`
	SortBy        ScenarioSetSortField    `url:"sort_by,omitempty"`
	SortDirection GenAISortDirection      `url:"sort_direction,omitempty"`
	ListOptions
}

// ScenarioListOptions specifies optional parameters for listing scenarios.
type ScenarioListOptions struct {
	Search        string             `url:"search,omitempty"`
	SortBy        ScenarioSortField  `url:"sort_by,omitempty"`
	SortDirection GenAISortDirection `url:"sort_direction,omitempty"`
	ListOptions
}

// ScenarioLibraryListOptions specifies optional parameters for listing the scenario library.
type ScenarioLibraryListOptions struct {
	Category      string                   `url:"category,omitempty"`
	Search        string                   `url:"search,omitempty"`
	SortBy        ScenarioLibrarySortField `url:"sort_by,omitempty"`
	SortDirection GenAISortDirection       `url:"sort_direction,omitempty"`
	ListOptions
}

// SimulationRunListOptions specifies optional parameters for listing simulation runs.
type SimulationRunListOptions struct {
	ScenarioSetUUID string                 `url:"scenario_set_uuid,omitempty"`
	Statuses        []SimulationRunStatus  `url:"statuses,omitempty"`
	Search          string                 `url:"search,omitempty"`
	SortBy          SimulationRunSortField `url:"sort_by,omitempty"`
	SortDirection   GenAISortDirection     `url:"sort_direction,omitempty"`
	ListOptions
}

// SimulationEpisodeListOptions specifies optional parameters for listing simulation episodes.
type SimulationEpisodeListOptions struct {
	ScenarioUUID  string                     `url:"scenario_uuid,omitempty"`
	Statuses      []SimulationEpisodeStatus  `url:"statuses,omitempty"`
	Verdicts      []SimulationEpisodeVerdict `url:"verdicts,omitempty"`
	Search        string                     `url:"search,omitempty"`
	SortBy        SimulationEpisodeSortField `url:"sort_by,omitempty"`
	SortDirection GenAISortDirection         `url:"sort_direction,omitempty"`
	ListOptions
}

// ScenarioSetListResponse is returned by ListScenarioSets.
type ScenarioSetListResponse struct {
	ScenarioSets            []*ScenarioSet          `json:"scenario_sets,omitempty"`
	Links                   *Links                  `json:"links,omitempty"`
	Meta                    *Meta                   `json:"meta,omitempty"`
	AvailableStatuses       []ScenarioSetStatus     `json:"available_statuses,omitempty"`
	AvailableSourceKinds    []ScenarioSetSourceKind `json:"available_source_kinds,omitempty"`
	AvailableSortBy         []ScenarioSetSortField  `json:"available_sort_by,omitempty"`
	AvailableSortDirections []GenAISortDirection    `json:"available_sort_directions,omitempty"`
}

// ScenarioListResponse is returned by ListScenarios and ListScenarioLibraryScenarios.
type ScenarioListResponse struct {
	Scenarios               []*Scenario          `json:"scenarios,omitempty"`
	Links                   *Links               `json:"links,omitempty"`
	Meta                    *Meta                `json:"meta,omitempty"`
	AvailableSortBy         []ScenarioSortField  `json:"available_sort_by,omitempty"`
	AvailableSortDirections []GenAISortDirection `json:"available_sort_directions,omitempty"`
}

// ScenarioLibraryListResponse is returned by ListScenarioLibrary.
type ScenarioLibraryListResponse struct {
	Scenarios               []*ScenarioLibraryEntry    `json:"scenarios,omitempty"`
	Links                   *Links                     `json:"links,omitempty"`
	Meta                    *Meta                      `json:"meta,omitempty"`
	AvailableCategories     []string                   `json:"available_categories,omitempty"`
	AvailableSortBy         []ScenarioLibrarySortField `json:"available_sort_by,omitempty"`
	AvailableSortDirections []GenAISortDirection       `json:"available_sort_directions,omitempty"`
}

// ScenarioSetDownloadURLResponse is returned by GetScenarioSetDownloadURL.
type ScenarioSetDownloadURLResponse struct {
	DownloadURL string     `json:"download_url,omitempty"`
	ExpiresAt   *Timestamp `json:"expires_at,omitempty"`
}

// ScenarioSetDeleteResponse is returned by DeleteScenarioSet.
type ScenarioSetDeleteResponse struct {
	ScenarioSetUUID string `json:"scenario_set_uuid,omitempty"`
}

// SimulationRunListResponse is returned by ListSimulationRuns.
type SimulationRunListResponse struct {
	SimulationRuns          []*SimulationRun         `json:"simulation_runs,omitempty"`
	Links                   *Links                   `json:"links,omitempty"`
	Meta                    *Meta                    `json:"meta,omitempty"`
	AvailableStatuses       []SimulationRunStatus    `json:"available_statuses,omitempty"`
	AvailableSortBy         []SimulationRunSortField `json:"available_sort_by,omitempty"`
	AvailableSortDirections []GenAISortDirection     `json:"available_sort_directions,omitempty"`
}

// SimulationRunGetResponse is returned by GetSimulationRun.
type SimulationRunGetResponse struct {
	SimulationRun   *SimulationRun              `json:"simulation_run,omitempty"`
	ScenarioResults []*SimulationScenarioResult `json:"scenario_results,omitempty"`
}

// SimulationRunDeleteResponse is returned by DeleteSimulationRun.
type SimulationRunDeleteResponse struct {
	RunUUID string `json:"run_uuid,omitempty"`
}

// SimulationEpisodeListResponse is returned by ListSimulationEpisodes.
type SimulationEpisodeListResponse struct {
	Episodes                []*SimulationEpisode         `json:"episodes,omitempty"`
	Links                   *Links                       `json:"links,omitempty"`
	Meta                    *Meta                        `json:"meta,omitempty"`
	AvailableStatuses       []SimulationEpisodeStatus    `json:"available_statuses,omitempty"`
	AvailableVerdicts       []SimulationEpisodeVerdict   `json:"available_verdicts,omitempty"`
	AvailableSortBy         []SimulationEpisodeSortField `json:"available_sort_by,omitempty"`
	AvailableSortDirections []GenAISortDirection         `json:"available_sort_directions,omitempty"`
}

// SimulationEpisodeTrajectoryURLResponse is returned by GetSimulationEpisodeTrajectoryURL.
type SimulationEpisodeTrajectoryURLResponse struct {
	DownloadURL string     `json:"download_url,omitempty"`
	ExpiresAt   *Timestamp `json:"expires_at,omitempty"`
}

type scenarioSetRoot struct {
	ScenarioSet *ScenarioSet `json:"scenario_set"`
}

type simulationRunRoot struct {
	SimulationRun *SimulationRun `json:"simulation_run"`
}

type simulationEpisodeRoot struct {
	Episode *SimulationEpisode `json:"episode"`
}

type simulationTrajectoryRoot struct {
	Trajectory *SimulationTrajectory `json:"trajectory"`
}

// CreateScenarioSetUploadPresignedURLs creates presigned URLs for uploading
// scenario set files.
func (s *AgentPlatformServiceOp) CreateScenarioSetUploadPresignedURLs(ctx context.Context, createRequest *CreateScenarioSetUploadPresignedURLsRequest) (*CreateScenarioSetUploadPresignedURLsResponse, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}
	if len(createRequest.Files) == 0 {
		return nil, nil, fmt.Errorf("files is required")
	}
	for _, file := range createRequest.Files {
		if file == nil || file.FileName == "" {
			return nil, nil, fmt.Errorf("file_name is required for every requested file")
		}
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, scenarioSetUploadPresignedURLsPath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(CreateScenarioSetUploadPresignedURLsResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateScenarioSet creates a scenario set from inline scenarios or an uploaded
// file. Exactly one of the two sources must be provided.
func (s *AgentPlatformServiceOp) CreateScenarioSet(ctx context.Context, createRequest *CreateScenarioSetRequest) (*ScenarioSet, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}
	if createRequest.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	hasScenarios := len(createRequest.Scenarios) > 0
	hasFile := createRequest.FileUploadScenarioSet != nil && createRequest.FileUploadScenarioSet.StoredObjectKey != ""
	if hasScenarios == hasFile {
		return nil, nil, fmt.Errorf("provide exactly one of scenarios or file_upload_scenario_set")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, scenarioSetsBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(scenarioSetRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.ScenarioSet, resp, nil
}

// GenerateScenarioSet dispatches goal-driven scenario generation.
func (s *AgentPlatformServiceOp) GenerateScenarioSet(ctx context.Context, generateRequest *GenerateScenarioSetRequest) (*ScenarioSet, *Response, error) {
	if generateRequest == nil {
		return nil, nil, fmt.Errorf("generate request is required")
	}
	if generateRequest.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	if generateRequest.GoalDescription == "" {
		return nil, nil, fmt.Errorf("goal_description is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, scenarioSetGeneratePath, generateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(scenarioSetRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.ScenarioSet, resp, nil
}

// ListScenarioSets lists scenario sets for the team.
func (s *AgentPlatformServiceOp) ListScenarioSets(ctx context.Context, opt *ScenarioSetListOptions) (*ScenarioSetListResponse, *Response, error) {
	path, err := addOptions(scenarioSetsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioSetListResponse)
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

// GetScenarioSet retrieves a scenario set by UUID.
func (s *AgentPlatformServiceOp) GetScenarioSet(ctx context.Context, scenarioSetUUID string) (*ScenarioSet, *Response, error) {
	if scenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario set uuid is required")
	}
	path := fmt.Sprintf(scenarioSetByIDPath, scenarioSetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(scenarioSetRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.ScenarioSet, resp, nil
}

// ListScenarios lists scenarios within a scenario set.
func (s *AgentPlatformServiceOp) ListScenarios(ctx context.Context, scenarioSetUUID string, opt *ScenarioListOptions) (*ScenarioListResponse, *Response, error) {
	if scenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario set uuid is required")
	}
	path := fmt.Sprintf(scenarioSetScenariosPath, scenarioSetUUID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioListResponse)
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

// GetScenarioSetDownloadURL returns a presigned download URL for a scenario set's
// canonical JSONL file.
func (s *AgentPlatformServiceOp) GetScenarioSetDownloadURL(ctx context.Context, scenarioSetUUID string) (*ScenarioSetDownloadURLResponse, *Response, error) {
	if scenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario set uuid is required")
	}
	path := fmt.Sprintf(scenarioSetDownloadURLPath, scenarioSetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioSetDownloadURLResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UpdateScenarioSet updates a scenario set name and/or replaces its scenarios.
func (s *AgentPlatformServiceOp) UpdateScenarioSet(ctx context.Context, scenarioSetUUID string, updateRequest *UpdateScenarioSetRequest) (*ScenarioSet, *Response, error) {
	if scenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario set uuid is required")
	}
	if updateRequest == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}
	if updateRequest.Name == "" && len(updateRequest.Scenarios) == 0 {
		return nil, nil, fmt.Errorf("at least one of name or scenarios must be set")
	}
	path := fmt.Sprintf(scenarioSetByIDPath, scenarioSetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(scenarioSetRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.ScenarioSet, resp, nil
}

// DeleteScenarioSet deletes a scenario set by UUID.
func (s *AgentPlatformServiceOp) DeleteScenarioSet(ctx context.Context, scenarioSetUUID string) (*ScenarioSetDeleteResponse, *Response, error) {
	if scenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario set uuid is required")
	}
	path := fmt.Sprintf(scenarioSetByIDPath, scenarioSetUUID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioSetDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListScenarioLibrary lists platform-curated scenario library entries.
func (s *AgentPlatformServiceOp) ListScenarioLibrary(ctx context.Context, opt *ScenarioLibraryListOptions) (*ScenarioLibraryListResponse, *Response, error) {
	path, err := addOptions(scenarioLibraryBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioLibraryListResponse)
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

// ListScenarioLibraryScenarios lists scenarios within a scenario library entry.
func (s *AgentPlatformServiceOp) ListScenarioLibraryScenarios(ctx context.Context, libraryScenarioUUID string, opt *ScenarioListOptions) (*ScenarioListResponse, *Response, error) {
	if libraryScenarioUUID == "" {
		return nil, nil, fmt.Errorf("library scenario uuid is required")
	}
	path := fmt.Sprintf(scenarioLibraryScenariosPath, libraryScenarioUUID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ScenarioListResponse)
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

// CreateScenarioSetFromLibrary materializes a library entry into a team-owned
// scenario set.
func (s *AgentPlatformServiceOp) CreateScenarioSetFromLibrary(ctx context.Context, libraryScenarioUUID string, createRequest *CreateScenarioSetFromLibraryRequest) (*ScenarioSet, *Response, error) {
	if libraryScenarioUUID == "" {
		return nil, nil, fmt.Errorf("library scenario uuid is required")
	}
	if createRequest == nil {
		createRequest = &CreateScenarioSetFromLibraryRequest{}
	}
	if createRequest.LibraryScenarioUUID == "" {
		createRequest.LibraryScenarioUUID = libraryScenarioUUID
	}
	path := fmt.Sprintf(scenarioLibraryCreateScenarioSetPath, libraryScenarioUUID)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(scenarioSetRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.ScenarioSet, resp, nil
}

// CreateSimulationRun creates a simulation run.
func (s *AgentPlatformServiceOp) CreateSimulationRun(ctx context.Context, createRequest *CreateSimulationRunRequest) (*SimulationRun, *Response, error) {
	if createRequest == nil {
		return nil, nil, fmt.Errorf("create request is required")
	}
	if createRequest.ScenarioSetUUID == "" {
		return nil, nil, fmt.Errorf("scenario_set_uuid is required")
	}
	if createRequest.AgentConfig == nil || createRequest.AgentConfig.AgentUUID == "" {
		return nil, nil, fmt.Errorf("agent_config.agent_uuid is required")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, simulationRunsBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(simulationRunRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.SimulationRun, resp, nil
}

// ListSimulationRuns lists simulation runs for the team.
func (s *AgentPlatformServiceOp) ListSimulationRuns(ctx context.Context, opt *SimulationRunListOptions) (*SimulationRunListResponse, *Response, error) {
	path, err := addOptions(simulationRunsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(SimulationRunListResponse)
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

// GetSimulationRun retrieves a simulation run by UUID, including per-scenario
// result rollups when available.
func (s *AgentPlatformServiceOp) GetSimulationRun(ctx context.Context, runUUID string) (*SimulationRunGetResponse, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	path := fmt.Sprintf(simulationRunByIDPath, runUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(SimulationRunGetResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UpdateSimulationRun updates mutable fields on a simulation run.
func (s *AgentPlatformServiceOp) UpdateSimulationRun(ctx context.Context, runUUID string, updateRequest *UpdateSimulationRunRequest) (*SimulationRun, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	if updateRequest == nil {
		return nil, nil, fmt.Errorf("update request is required")
	}
	if updateRequest.Name == "" {
		return nil, nil, fmt.Errorf("name is required")
	}
	path := fmt.Sprintf(simulationRunByIDPath, runUUID)

	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(simulationRunRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.SimulationRun, resp, nil
}

// CancelSimulationRun cancels an in-progress simulation run.
func (s *AgentPlatformServiceOp) CancelSimulationRun(ctx context.Context, runUUID string) (*SimulationRun, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	path := fmt.Sprintf(simulationRunCancelPath, runUUID)

	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(simulationRunRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.SimulationRun, resp, nil
}

// DeleteSimulationRun deletes a simulation run by UUID.
func (s *AgentPlatformServiceOp) DeleteSimulationRun(ctx context.Context, runUUID string) (*SimulationRunDeleteResponse, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	path := fmt.Sprintf(simulationRunByIDPath, runUUID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(SimulationRunDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListSimulationEpisodes lists episodes for a simulation run.
func (s *AgentPlatformServiceOp) ListSimulationEpisodes(ctx context.Context, runUUID string, opt *SimulationEpisodeListOptions) (*SimulationEpisodeListResponse, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	path := fmt.Sprintf(simulationRunEpisodesPath, runUUID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(SimulationEpisodeListResponse)
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

// GetSimulationEpisode retrieves a single episode within a simulation run.
func (s *AgentPlatformServiceOp) GetSimulationEpisode(ctx context.Context, runUUID, episodeUUID string) (*SimulationEpisode, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	if episodeUUID == "" {
		return nil, nil, fmt.Errorf("episode uuid is required")
	}
	path := fmt.Sprintf(simulationEpisodeByIDPath, runUUID, episodeUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(simulationEpisodeRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Episode, resp, nil
}

// GetSimulationEpisodeTrajectoryURL returns a presigned download URL for an
// episode's trajectory JSON.
func (s *AgentPlatformServiceOp) GetSimulationEpisodeTrajectoryURL(ctx context.Context, runUUID, episodeUUID string) (*SimulationEpisodeTrajectoryURLResponse, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	if episodeUUID == "" {
		return nil, nil, fmt.Errorf("episode uuid is required")
	}
	path := fmt.Sprintf(simulationEpisodeTrajectoryURLPath, runUUID, episodeUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(SimulationEpisodeTrajectoryURLResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetSimulationEpisodeTrajectory retrieves the parsed trajectory JSON for an
// episode.
func (s *AgentPlatformServiceOp) GetSimulationEpisodeTrajectory(ctx context.Context, runUUID, episodeUUID string) (*SimulationTrajectory, *Response, error) {
	if runUUID == "" {
		return nil, nil, fmt.Errorf("run uuid is required")
	}
	if episodeUUID == "" {
		return nil, nil, fmt.Errorf("episode uuid is required")
	}
	path := fmt.Sprintf(simulationEpisodeTrajectoryPath, runUUID, episodeUUID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(simulationTrajectoryRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Trajectory, resp, nil
}
