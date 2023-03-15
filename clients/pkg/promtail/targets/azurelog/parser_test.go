package azurelog

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/Shopify/sarama"
)

//go:embed testdata/function_app_logs_message.txt
var functionAppLogsMessageBody string

func Test_parseMessage(t *testing.T) {
	messageParser := eventHubMessageParser(parseMessage)

	message := &sarama.ConsumerMessage{
		Value: []byte(functionAppLogsMessageBody),
	}

	lines, err := messageParser.Parse(message)
	assert.NoError(t, err)
	assert.Len(t, lines, 2)

	expectedLine1 := "{ \"time\": \"2023-03-08T12:06:46Z\",\n\t\"resourceId\": \"AZURE-FUNC-APP\",\n\t\"category\": \"FunctionAppLogs\",\n\t\"operationName\": \"Microsoft.Web/sites/functions/log\",\n\t\"level\": \"Informational\",\n\t\"location\": \"My Location\",\n\t\"properties\": {\n\t\"appName\":\"\",\n\t\"roleInstance\":\"123123123123\",\n\t\"message\":\"Loading functions metadata\",\n\t\"category\":\"Host.Startup\",\n\t\"hostVersion\":\"X.XX.X.X\",\n\t\"hostInstanceId\":\"myInstance\",\n\t\"level\":\"Information\",\n\t\"levelId\":2,\n\t\"processId\":155,\n\t\"eventId\":3143,\n\t\"eventName\":\"FunctionMetadataManagerLoadingFunctionsMetadata\"\n\t}\n\t}"
	expectedLine2 := "{ \"time\": \"2023-03-08T12:06:47Z\",\n\t\"resourceId\": \"AZURE-FUNC-APP-2\",\n\t\"category\": \"FunctionAppLogs\",\n\t\"operationName\": \"Microsoft.Web/sites/functions/log\",\n\t\"level\": \"Informational\",\n\t\"location\": \"My Location\",\n\t\"properties\": {\n\t\"appName\":\"\",\n\t\"roleInstance\":\"123123123123\",\n\t\"message\":\"Loading functions metadata\",\n\t\"category\":\"Host.Startup\",\n\t\"hostVersion\":\"X.XX.X.X\",\n\t\"hostInstanceId\":\"myInstance\",\n\t\"level\":\"Information\",\n\t\"levelId\":2,\n\t\"processId\":155,\n\t\"eventId\":3143,\n\t\"eventName\":\"FunctionMetadataManagerLoadingFunctionsMetadata\"\n\t}\n\t}"
	assert.Equal(t, expectedLine1, lines[0])
	assert.Equal(t, expectedLine2, lines[1])
}
