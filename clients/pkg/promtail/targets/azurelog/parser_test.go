package azurelog

import (
	_ "embed"
	"testing"
	"time"

	"github.com/Shopify/sarama"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
)

//go:embed testdata/function_app_logs_message.txt
var functionAppLogsMessageBody []byte

//go:embed testdata/logic_app_logs_message.json
var logicAppLogsMessageBody []byte

//go:embed testdata/custom_payload_text.txt
var customPayloadText []byte

//go:embed testdata/custom_payload_json.json
var customPayloadJSON []byte

//go:embed testdata/custom_payload_json_with_records_string.json
var customPayloadJSONWithRecordsString []byte

//go:embed testdata/custom_payload_json_with_records_array.json
var customPayloadJSONWithRecordsArray []byte

//go:embed testdata/message_with_invalid_time.json
var messageWithInvalidTime []byte

func Test_parseMessage_function_app(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value: functionAppLogsMessageBody,
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	expectedLine1 := "{ \"time\": \"2023-03-08T12:06:46Z\",\n\t\"resourceId\": \"AZURE-FUNC-APP\",\n\t\"category\": \"FunctionAppLogs\",\n\t\"operationName\": \"Microsoft.Web/sites/functions/log\",\n\t\"level\": \"Informational\",\n\t\"location\": \"My Location\",\n\t\"properties\": {\n\t\"appName\":\"\",\n\t\"roleInstance\":\"123123123123\",\n\t\"message\":\"Loading functions metadata\",\n\t\"category\":\"Host.Startup\",\n\t\"hostVersion\":\"X.XX.X.X\",\n\t\"hostInstanceId\":\"myInstance\",\n\t\"level\":\"Information\",\n\t\"levelId\":2,\n\t\"processId\":155,\n\t\"eventId\":3143,\n\t\"eventName\":\"FunctionMetadataManagerLoadingFunctionsMetadata\"\n\t}\n\t}"
	expectedLine2 := "{ \"time\": \"2023-03-08T12:06:47Z\",\n\t\"resourceId\": \"AZURE-FUNC-APP-2\",\n\t\"category\": \"FunctionAppLogs\",\n\t\"operationName\": \"Microsoft.Web/sites/functions/log\",\n\t\"level\": \"Informational\",\n\t\"location\": \"My Location\",\n\t\"properties\": {\n\t\"appName\":\"\",\n\t\"roleInstance\":\"123123123123\",\n\t\"message\":\"Loading functions metadata\",\n\t\"category\":\"Host.Startup\",\n\t\"hostVersion\":\"X.XX.X.X\",\n\t\"hostInstanceId\":\"myInstance\",\n\t\"level\":\"Information\",\n\t\"levelId\":2,\n\t\"processId\":155,\n\t\"eventId\":3143,\n\t\"eventName\":\"FunctionMetadataManagerLoadingFunctionsMetadata\"\n\t}\n\t}"
	assert.Equal(t, expectedLine1, entries[0].Line)
	assert.Equal(t, expectedLine2, entries[1].Line)

	assert.Equal(t, time.Date(2023, time.March, 8, 12, 6, 46, 0, time.UTC), entries[0].Timestamp)
	assert.Equal(t, time.Date(2023, time.March, 8, 12, 6, 47, 0, time.UTC), entries[1].Timestamp)
}

func Test_parseMessage_logic_app(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value: logicAppLogsMessageBody,
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	expectedLine1 := "{\n      \"time\": \"2023-03-17T08:44:02.8921579Z\",\n      \"workflowId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP\",\n      \"resourceId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP/RUNS/11111/TRIGGERS/MANUAL\",\n      \"category\": \"WorkflowRuntime\",\n      \"level\": \"Information\",\n      \"operationName\": \"Microsoft.Logic/workflows/workflowTriggerStarted\",\n      \"properties\": {\n        \"$schema\": \"2016-06-01\",\n        \"startTime\": \"2023-03-17T08:44:02.8358364Z\",\n        \"status\": \"Succeeded\",\n        \"fired\": true,\n        \"resource\": {\n          \"subscriptionId\": \"someSubscriptionId\",\n          \"resourceGroupName\": \"AzureLogsTesting\",\n          \"workflowId\": \"someWorkflowId\",\n          \"workflowName\": \"auzre-promtail-testing-app\",\n          \"runId\": \"someRunId\",\n          \"location\": \"eastus\",\n          \"triggerName\": \"manual\"\n        },\n        \"correlation\": {\n          \"clientTrackingId\": \"someClientTrackingId\"\n        },\n        \"api\": {}\n      },\n      \"location\": \"eastus\"\n    }"
	expectedLine2 := "{\n      \"time\": \"2023-03-17T08:44:03.2036497Z\",\n      \"workflowId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP\",\n      \"resourceId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP/RUNS/11111/TRIGGERS/MANUAL\",\n      \"category\": \"WorkflowRuntime\",\n      \"level\": \"Information\",\n      \"operationName\": \"Microsoft.Logic/workflows/workflowTriggerCompleted\",\n      \"properties\": {\n        \"$schema\": \"2016-06-01\",\n        \"startTime\": \"2023-03-17T08:44:02.8358364Z\",\n        \"endTime\": \"2023-03-17T08:44:02.8983217Z\",\n        \"status\": \"Succeeded\",\n        \"fired\": true,\n        \"resource\": {\n          \"subscriptionId\": \"someSubscriptionId\",\n          \"resourceGroupName\": \"AzureLogsTesting\",\n          \"workflowId\": \"someWorkflowId\",\n          \"workflowName\": \"auzre-promtail-testing-app\",\n          \"runId\": \"someRunId\",\n          \"location\": \"eastus\",\n          \"triggerName\": \"manual\"\n        },\n        \"correlation\": {\n          \"clientTrackingId\": \"someClientTrackingId\"\n        },\n        \"api\": {}\n      },\n      \"location\": \"eastus\"\n    }"
	assert.Equal(t, expectedLine1, entries[0].Line)
	assert.Equal(t, expectedLine2, entries[1].Line)

	assert.Equal(t, time.Date(2023, time.March, 17, 8, 44, 02, 892157900, time.UTC), entries[0].Timestamp)
	assert.Equal(t, time.Date(2023, time.March, 17, 8, 44, 03, 203649700, time.UTC), entries[1].Timestamp)

}

func Test_parseMessage_custom_payload_text(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     customPayloadText,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	expectedLine := "Message with quotes \" `"
	assert.Equal(t, expectedLine, entries[0].Line)

	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[0].Timestamp)
}

func Test_parseMessage_custom_payload_text_error(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value: customPayloadText,
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_custom_payload_json(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     customPayloadJSON,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	expectedLine := `{"json":"I am valid json"}`
	assert.Equal(t, expectedLine, entries[0].Line)

	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[0].Timestamp)
}

func Test_parseMessage_custom_payload_json_with_records_string(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     customPayloadJSONWithRecordsString,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	expectedLine := `{"records":"I am valid json"}`
	assert.Equal(t, expectedLine, entries[0].Line)

	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[0].Timestamp)
}

func Test_parseMessage_custom_payload_json_with_records_string_custom_payload_not_allowed(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value: customPayloadJSONWithRecordsString,
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_custom_payload_json_with_records_array(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     customPayloadJSONWithRecordsArray,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 3)

	expectedLine1 := `"I am valid json"`
	expectedLine2 := `"Me too"`
	expectedLine3 := "{\n    \"MyKey\": \"MyValue\"\n  }"
	assert.Equal(t, expectedLine1, entries[0].Line)
	assert.Equal(t, expectedLine2, entries[1].Line)
	assert.Equal(t, expectedLine3, entries[2].Line)

	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[0].Timestamp)
	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[1].Timestamp)
	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[2].Timestamp)
}

func Test_parseMessage_custom_payload_json_with_records_array_custom_payload_not_allowed(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value:     customPayloadJSONWithRecordsArray,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_message_with_invalid_time(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value:     messageWithInvalidTime,
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	expectedLine := "{\n      \"time\": \"SOME-ERROR-HERE-T08:44:02.8921579Z\",\n      \"category\": \"WorkflowRuntime\"\n    }"
	assert.Equal(t, expectedLine, entries[0].Line)

	assert.Equal(t, time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC), entries[0].Timestamp)
}

func Test_parseMessage_relable_config(t *testing.T) {
	messageParser := &eventHubMessageParser{
		allowCustomPayload: false,
	}

	message := &sarama.ConsumerMessage{
		Value: functionAppLogsMessageBody,
	}

	relableConfigs := []*relabel.Config{
		{
			SourceLabels: model.LabelNames{"__azurelog_category"},
			Regex:        relabel.MustNewRegexp("(.*)"),
			TargetLabel:  "category",
			Replacement:  "$1",
			Action:       "replace",
		},
	}

	entries, err := messageParser.Parse(message, nil, relableConfigs, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	assert.Equal(t, model.LabelSet{"category": "FunctionAppLogs"}, entries[0].Labels)
	assert.Equal(t, model.LabelSet{"category": "FunctionAppLogs"}, entries[1].Labels)
}
