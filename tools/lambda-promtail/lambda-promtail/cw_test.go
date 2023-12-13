package main

import (
	"testing"

	"github.com/prometheus/common/model"
)

func TestECSLogGroupParsing(t *testing.T) {
	for taskName, expected := range logGroups {
		t.Run(taskName, func(t *testing.T) {
			actual := parseECSTask(taskName)
			if !expected.Equal(actual) {
				t.Errorf("actual != expected. Actual: %s. Expected %s", actual.String(), expected.String())
				t.Fail()
			}
		})
	}
}

// This is a subset of the log groups I found in production by running the following loki query:
// sum by(cloudwatch_log_group, cloudwatch_owner)(rate({cloudwatch_log_group=~".+"} [15m]))
var logGroups = map[string]model.LabelSet{
	// These should not parse and should return an empty label set
	"/aws/amazonmq/broker/b-7144cd02-e9b3-4209-b89d-8c19074b6290/connection": {},
	"/networking/twingate/us-west-1-segshare-prod":                           {},
	"RDSOSMetrics": {},

	// These should parse with the app config label
	"/ecs/us-west-1/prod/acs-bridge-live": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("prod"),
		model.LabelName(LabelServiceName):           model.LabelValue("acs-bridge"),
		model.LabelName(LabelAppConfig):             model.LabelValue("live"),
	},
	"/ecs/us-west-1/prod/asa-proxy-sandbox": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("prod"),
		model.LabelName(LabelServiceName):           model.LabelValue("asa-proxy"),
		model.LabelName(LabelAppConfig):             model.LabelValue("sandbox"),
	},
	"/ecs/us-west-1/staging/acs-bridge-live": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("staging"),
		model.LabelName(LabelServiceName):           model.LabelValue("acs-bridge"),
		model.LabelName(LabelAppConfig):             model.LabelValue("live"),
	},
	"/ecs/us-west-1/staging/asa-proxy-sandbox": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("staging"),
		model.LabelName(LabelServiceName):           model.LabelValue("asa-proxy"),
		model.LabelName(LabelAppConfig):             model.LabelValue("sandbox"),
	},
	"/ecs/us-west-2/dev/iso-bridge-dev": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-2"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("dev"),
		model.LabelName(LabelServiceName):           model.LabelValue("iso-bridge"),
		model.LabelName(LabelAppConfig):             model.LabelValue("dev"),
	},

	// These should parse without the app config label
	"/ecs/us-west-1/prod/visa-bridge-1": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("prod"),
		model.LabelName(LabelServiceName):           model.LabelValue("visa-bridge-1"),
	},
	"/ecs/us-west-1/staging/grafana-agent-collector": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("staging"),
		model.LabelName(LabelServiceName):           model.LabelValue("grafana-agent-collector"),
	},
	"/ecs/us-west-1/staging/mastercard-bridge": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("staging"),
		model.LabelName(LabelServiceName):           model.LabelValue("mastercard-bridge"),
	},
	"/ecs/us-west-1/staging/pwp-proxy": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-1"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("staging"),
		model.LabelName(LabelServiceName):           model.LabelValue("pwp-proxy"),
	},
	"/ecs/us-west-2/prod/fml": {
		model.LabelName(LabelCloudRegion):           model.LabelValue("us-west-2"),
		model.LabelName(LabelDeploymentEnvironment): model.LabelValue("prod"),
		model.LabelName(LabelServiceName):           model.LabelValue("fml"),
	},
}
