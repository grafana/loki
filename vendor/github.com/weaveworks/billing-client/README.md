# billing-client

A client library for sending usage data to the billing system.

Open sourced so it can be imported into our open-source projects.

## Usage

`dep ensure github.com/weaveworks/billing-client`

then

```Go
import billing "github.com/weaveworks/billing-client"

func init() {
  billing.MustRegisterMetrics()
}

func main() {
  var cfg billing.Config
  cfg.RegisterFlags(flag.CommandLine)
  flag.Parse()

  client, err := billing.NewClient(cfg)
  defer client.Close()

  err = client.AddAmounts(
    uniqueKey, // Unique hash of the data, or a uuid here for deduping
    internalInstanceID,
    timestamp,
    billing.Amounts{
      billing.ContainerSeconds: 1234,
    },
    map[string]string{
      "metadata": "goes here"
    },
  )
}

```
