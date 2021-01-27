
# Sample Function

The following is a Lambda function that receives Amazon CloudWatch event record data as input and writes event detail to Lambda's CloudWatch Logs. Note that by default anything written to Console will be logged as CloudWatch Logs events.

```go
import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
)

func handler(ctx context.Context, event events.CloudWatchEvent) {
	fmt.Printf("Detail = %s\n", event.Detail)
}
```
