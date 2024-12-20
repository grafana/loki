package azureeventhubs

import (
	"os"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
)

func Test_parseMessage_function_app(t *testing.T) {
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value: readFile(t, "testdata/function_app_logs_message.txt"),
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
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value: readFile(t, "testdata/logic_app_logs_message.json"),
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
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_payload_text.txt"),
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
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value: readFile(t, "testdata/custom_payload_text.txt"),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_custom_payload_json(t *testing.T) {
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_payload_json.json"),
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
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_payload_json_with_records_string.json"),
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
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value: readFile(t, "testdata/custom_payload_json_with_records_string.json"),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_custom_payload_json_with_records_array(t *testing.T) {
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_payload_json_with_records_array.json"),
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
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_payload_json_with_records_array.json"),
		Timestamp: time.Date(2021, time.March, 17, 8, 44, 03, 0, time.UTC),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func Test_parseMessage_message_with_invalid_time(t *testing.T) {
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/message_with_invalid_time.json"),
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
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value: readFile(t, "testdata/function_app_logs_message.txt"),
	}

	relableConfigs := []*relabel.Config{
		{
			SourceLabels: model.LabelNames{"__azure_event_hubs_category"},
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

func Test_parseMessage_custom_message_and_logic_app_logs(t *testing.T) {
	messageParser := &messageParser{}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_message_and_logic_app_logs.json"),
		Timestamp: time.Date(2023, time.March, 17, 8, 44, 02, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)

	expectedLine1 := "{\n      \"time\": \"2023-03-17T08:44:02.8921579Z\",\n      \"workflowId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP\",\n      \"resourceId\": \"/WORKFLOWS/AUZRE-PROMTAIL-TESTING-APP/RUNS/11111/TRIGGERS/MANUAL\",\n      \"category\": \"WorkflowRuntime\",\n      \"level\": \"Information\",\n      \"operationName\": \"Microsoft.Logic/workflows/workflowTriggerStarted\",\n      \"properties\": {\n        \"$schema\": \"2016-06-01\",\n        \"startTime\": \"2023-03-17T08:44:02.8358364Z\",\n        \"status\": \"Succeeded\",\n        \"fired\": true,\n        \"resource\": {\n          \"subscriptionId\": \"someSubscriptionId\",\n          \"resourceGroupName\": \"AzureLogsTesting\",\n          \"workflowId\": \"someWorkflowId\",\n          \"workflowName\": \"auzre-promtail-testing-app\",\n          \"runId\": \"someRunId\",\n          \"location\": \"eastus\",\n          \"triggerName\": \"manual\"\n        },\n        \"correlation\": {\n          \"clientTrackingId\": \"someClientTrackingId\"\n        },\n        \"api\": {}\n      },\n      \"location\": \"eastus\"\n    }"
	expectedLine2 := "{\n      \"time\": \"2023-03-17T08:44:03.2036497Z\",\n      \"category\": \"MyCustomCategory\"\n    }"
	assert.Equal(t, expectedLine1, entries[0].Line)
	assert.Equal(t, expectedLine2, entries[1].Line)

	assert.Equal(t, time.Date(2023, time.March, 17, 8, 44, 2, 892157900, time.UTC), entries[0].Timestamp)
	assert.Equal(t, time.Date(2023, time.March, 17, 8, 44, 2, 0, time.UTC), entries[1].Timestamp)
}

func Test_parseMessage_custom_message_and_logic_app_logs_disallowCustomMessages(t *testing.T) {
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/custom_message_and_logic_app_logs.json"),
		Timestamp: time.Date(2023, time.March, 17, 8, 44, 02, 0, time.UTC),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.Error(t, err)
}

func readFile(t *testing.T, filename string) []byte {
	data, err := os.ReadFile(filename)
	assert.NoError(t, err)
	return data
}

func Test_parseMessage_message_without_time_with_time_stamp(t *testing.T) {
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/message_without_time_with_time_stamp.json"),
		Timestamp: time.Date(2023, time.March, 17, 8, 44, 02, 0, time.UTC),
	}

	entries, err := messageParser.Parse(message, nil, nil, true)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)

	expectedLine1 := "{\n      \"timeStamp\": \"2024-09-18T00:45:09+00:00\",\n      \"resourceId\": \"/RESOURCE_ID\",\n      \"operationName\": \"ApplicationGatewayAccess\",\n      \"category\": \"ApplicationGatewayAccessLog\"\n    }"
	assert.Equal(t, expectedLine1, entries[0].Line)

	assert.Equal(t, time.Date(2024, time.September, 18, 00, 45, 9, 0, time.UTC), entries[0].Timestamp)
}

func Test_parseMessage_message_without_time_and_time_stamp(t *testing.T) {
	messageParser := &messageParser{
		disallowCustomMessages: true,
	}

	message := &sarama.ConsumerMessage{
		Value:     readFile(t, "testdata/message_without_time_and_time_stamp.json"),
		Timestamp: time.Date(2023, time.March, 17, 8, 44, 02, 0, time.UTC),
	}

	_, err := messageParser.Parse(message, nil, nil, true)
	assert.EqualError(t, err, "required field or fields is empty")
}
