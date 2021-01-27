// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package events

// ConfigEvent contains data from an event sent from AWS Config
type ConfigEvent struct {
	AccountID        string `json:"accountId"`     // The ID of the AWS account that owns the rule
	ConfigRuleArn    string `json:"configRuleArn"` // The ARN that AWS Config assigned to the rule
	ConfigRuleID     string `json:"configRuleId"`
	ConfigRuleName   string `json:"configRuleName"` // The name that you assigned to the rule that caused AWS Config to publish the event
	EventLeftScope   bool   `json:"eventLeftScope"` // A boolean value that indicates whether the AWS resource to be evaluated has been removed from the rule's scope
	ExecutionRoleArn string `json:"executionRoleArn"`
	InvokingEvent    string `json:"invokingEvent"`  // If the event is published in response to a resource configuration change, this value contains a JSON configuration item
	ResultToken      string `json:"resultToken"`    // A token that the function must pass to AWS Config with the PutEvaluations call
	RuleParameters   string `json:"ruleParameters"` // Key/value pairs that the function processes as part of its evaluation logic
	Version          string `json:"version"`
}
