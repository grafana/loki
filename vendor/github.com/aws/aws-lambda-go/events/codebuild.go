package events

import (
	"encoding/json"
	"time"
)

const (
	CodeBuildEventSource           = "aws.codebuild"
	CodeBuildStateChangeDetailType = "CodeBuild Build State Change"
	CodeBuildPhaseChangeDetailType = "CodeBuild Build Phase Change"
)

// CodeBuildPhaseStatus represents the status of code build phase (i.e. failed, in progress)
type CodeBuildPhaseStatus string

const (
	CodeBuildPhaseStatusFailed     CodeBuildPhaseStatus = "FAILED"
	CodeBuildPhaseStatusFault                           = "FAULT"
	CodeBuildPhaseStatusInProgress                      = "IN_PROGRESS"
	CodeBuildPhaseStatusQueued                          = "QUEUED"
	CodeBuildPhaseStatusStopped                         = "STOPPED"
	CodeBuildPhaseStatusSucceeded                       = "SUCCEEDED"
	CodeBuildPhaseStatusTimedOut                        = "TIMED_OUT"
)

// CodeBuildPhaseType represents the type of the code build phase (i.e. submitted, install)
type CodeBuildPhaseType string

const (
	CodeBuildPhaseTypeSubmitted       CodeBuildPhaseType = "SUBMITTED"
	CodeBuildPhaseTypeQueued                             = "QUEUED"
	CodeBuildPhaseTypeProvisioning                       = "PROVISIONING"
	CodeBuildPhaseTypeDownloadSource                     = "DOWNLOAD_SOURCE"
	CodeBuildPhaseTypeInstall                            = "INSTALL"
	CodeBuildPhaseTypePreBuild                           = "PRE_BUILD"
	CodeBuildPhaseTypeBuild                              = "BUILD"
	CodeBuildPhaseTypePostBuild                          = "POST_BUILD"
	CodeBuildPhaseTypeUploadArtifacts                    = "UPLOAD_ARTIFACTS"
	CodeBuildPhaseTypeFinalizing                         = "FINALIZING"
	CodeBuildPhaseTypeCompleted                          = "COMPLETED"
)

// CodeBuildEvent is documented at:
// https://docs.aws.amazon.com/codebuild/latest/userguide/sample-build-notifications.html#sample-build-notifications-ref
type CodeBuildEvent struct {
	// AccountID is the id of the AWS account from which the event originated.
	AccountID string `json:"account"`

	// Region is the AWS region from which the event originated.
	Region string `json:"region"`

	// DetailType informs the schema of the Detail field. For build state-change
	// events, the value will be CodeBuildStateChangeDetailType. For phase-change
	// events, it will be CodeBuildPhaseChangeDetailType.
	DetailType string `json:"detail-type"`

	// Source should be equal to CodeBuildEventSource.
	Source string `json:"source"`

	// Version is the version of the event's schema.
	Version string `json:"version"`

	// Time is the event's timestamp.
	Time time.Time `json:"time"`

	// ID is the GUID of this event.
	ID string `json:"id"`

	// Resources is a list of ARNs of CodeBuild builds that this event pertains to.
	Resources []string `json:"resources"`

	// Detail contains information specific to a build state-change or
	// build phase-change event.
	Detail CodeBuildEventDetail `json:"detail"`
}

// CodeBuildEventDetail represents the all details related to the code build event
type CodeBuildEventDetail struct {
	BuildStatus           CodeBuildPhaseStatus                `json:"build-status"`
	ProjectName           string                              `json:"project-name"`
	BuildID               string                              `json:"build-id"`
	AdditionalInformation CodeBuildEventAdditionalInformation `json:"additional-information"`
	CurrentPhase          CodeBuildPhaseStatus                `json:"current-phase"`
	CurrentPhaseContext   string                              `json:"current-phase-context"`
	Version               string                              `json:"version"`

	CompletedPhaseStatus   CodeBuildPhaseStatus `json:"completed-phase-status"`
	CompletedPhase         CodeBuildPhaseStatus `json:"completed-phase"`
	CompletedPhaseContext  string               `json:"completed-phase-context"`
	CompletedPhaseDuration DurationSeconds      `json:"completed-phase-duration-seconds"`
	CompletedPhaseStart    CodeBuildTime        `json:"completed-phase-start"`
	CompletedPhaseEnd      CodeBuildTime        `json:"completed-phase-end"`
}

//CodeBuildEventAdditionalInformation represents additional informations to the code build event
type CodeBuildEventAdditionalInformation struct {
	Artifact CodeBuildArtifact `json:"artifact"`

	Environment CodeBuildEnvironment `json:"environment"`

	Timeout DurationMinutes `json:"timeout-in-minutes"`

	BuildComplete bool `json:"build-complete"`

	Initiator string `json:"initiator"`

	BuildStartTime CodeBuildTime `json:"build-start-time"`

	Source CodeBuildSource `json:"source"`

	Logs CodeBuildLogs `json:"logs"`

	Phases []CodeBuildPhase `json:"phases"`
}

// CodeBuildArtifact represents the artifact provided to build
type CodeBuildArtifact struct {
	MD5Sum    string `json:"md5sum"`
	SHA256Sum string `json:"sha256sum"`
	Location  string `json:"location"`
}

// CodeBuildEnvironment represents the environment for a build
type CodeBuildEnvironment struct {
	Image                string                         `json:"image"`
	PrivilegedMode       bool                           `json:"privileged-mode"`
	ComputeType          string                         `json:"compute-type"`
	Type                 string                         `json:"type"`
	EnvironmentVariables []CodeBuildEnvironmentVariable `json:"environment-variables"`
}

// CodeBuildEnvironmentVariable encapsulate environment variables for the code build
type CodeBuildEnvironmentVariable struct {
	// Name is the name of the environment variable.
	Name string `json:"name"`

	// Type is PLAINTEXT or PARAMETER_STORE.
	Type string `json:"type"`

	// Value is the value of the environment variable.
	Value string `json:"value"`
}

// CodeBuildSource represent the code source will be build
type CodeBuildSource struct {
	Location string `json:"location"`
	Type     string `json:"type"`
}

// CodeBuildLogs gives the log details of a code build
type CodeBuildLogs struct {
	GroupName  string `json:"group-name"`
	StreamName string `json:"stream-name"`
	DeepLink   string `json:"deep-link"`
}

// CodeBuildPhase represents the phase of a build and its details
type CodeBuildPhase struct {
	PhaseContext []interface{} `json:"phase-context"`

	StartTime CodeBuildTime `json:"start-time"`

	EndTime CodeBuildTime `json:"end-time"`

	Duration DurationSeconds `json:"duration-in-seconds"`

	PhaseType CodeBuildPhaseType `json:"phase-type"`

	PhaseStatus CodeBuildPhaseStatus `json:"phase-status"`
}

// CodeBuildTime represents the time of the build
type CodeBuildTime time.Time

const codeBuildTimeFormat = "Jan 2, 2006 3:04:05 PM"

// MarshalJSON converts a given CodeBuildTime to json
func (t CodeBuildTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).Format(codeBuildTimeFormat))
}

// UnmarshalJSON converts a given json to a CodeBuildTime
func (t *CodeBuildTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	ts, err := time.Parse(codeBuildTimeFormat, s)
	if err != nil {
		return err
	}

	*t = CodeBuildTime(ts)
	return nil
}
