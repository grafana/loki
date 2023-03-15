package azurelog

import (
	_ "embed"
	"github.com/stretchr/testify/assert"
	"testing"

	"github.com/Shopify/sarama"
)

//go:embed testdata/function_app_logs_message.txt
var functionAppLogsMessageBody string

func Test_messageParser(t *testing.T) {
	message := &sarama.ConsumerMessage{
		Value: []byte(functionAppLogsMessageBody),
	}

	lines, err := messageParser(message)
	assert.NoError(t, err)
	assert.Len(t, lines, 2)

	expectedLine1 := `{"category":"FunctionAppLogs","level":"Informational","location":"My Location","operationName":"Microsoft.Web/sites/functions/log","properties":{"appName":"","category":"Host.Startup","eventId":3143,"eventName":"FunctionMetadataManagerLoadingFunctionsMetadata","hostInstanceId":"myInstance","hostVersion":"X.XX.X.X","level":"Information","levelId":2,"message":"Loading functions metadata","processId":155,"roleInstance":"123123123123"},"resourceId":"AZURE-FUNC-APP","time":"2023-03-08T12:06:46Z"}`
	expectedLine2 := `{"category":"FunctionAppLogs","level":"Informational","location":"My Location","operationName":"Microsoft.Web/sites/functions/log","properties":{"appName":"","category":"Host.Startup","eventId":3143,"eventName":"FunctionMetadataManagerLoadingFunctionsMetadata","hostInstanceId":"myInstance","hostVersion":"X.XX.X.X","level":"Information","levelId":2,"message":"Loading functions metadata","processId":155,"roleInstance":"123123123123"},"resourceId":"AZURE-FUNC-APP-2","time":"2023-03-08T12:06:47Z"}`
	assert.Equal(t, expectedLine1, lines[0])
	assert.Equal(t, expectedLine2, lines[1])
}
