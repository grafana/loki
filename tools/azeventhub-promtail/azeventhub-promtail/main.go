package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Azure/azure-amqp-common-go/v3/conn"
	eventhub "github.com/Azure/azure-event-hubs-go/v3"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

// const (
// 	connStr = "Endpoint=sb://prom-trek-prd.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=U7/+JjDTGfGmi7q/h+wihEAoB279+5PX4EcOyGibB+0=;EntityPath=prd-aag"
// )

const (
	// We use snappy-encoded protobufs over http by default.
	contentType  = "application/x-protobuf"
	maxErrMsgLen = 1024
)

var hubAddr *eventhub.Hub
var promtailAddress *url.URL
var hubParse *conn.ParsedConn

func init() {
	var err error
	ptaddr := os.Getenv("PROMTAIL_ADDRESS")
	if ptaddr == "" {
		panic(errors.New("required environmental variable PROMTAIL_ADDRESS not present"))
	}

	promtailAddress, err = url.Parse(ptaddr)
	if err != nil {
		panic(err)
	}

	azaddr := os.Getenv("AZ_CONNECTION_STRING")
	if azaddr == "" {
		panic(errors.New("required environmental variable AZ_CONNECTION_STRING not present"))
	}

	hubAddr, err = eventhub.NewHubFromConnectionString(azaddr)
	if err != nil {
		fmt.Println("Error: ", err)
	}

	hubParse, err = conn.ParsedConnectionFromStr(azaddr)
	if err != nil {
		fmt.Println("Error: ", err)
	}
}

func main() {
	handler := func(ctx context.Context, event *eventhub.Event) error {
		text := event.Data
		var result map[string][]interface{}
		json.Unmarshal(text, &result)
		for _, m := range result {
			stream := logproto.Stream{
				Labels: model.LabelSet{
					model.LabelName("__az_eventhub_namespace"): model.LabelValue(hubParse.Namespace),
					model.LabelName("__az_eventhub_hub"):       model.LabelValue(hubParse.HubName),
				}.String(),
				Entries: make([]logproto.Entry, 0, len(m)),
			}

			for _, data := range m {
				remarshalledJson, _ := json.Marshal(&data)
				stream.Entries = append(stream.Entries, logproto.Entry{
					Line: string(remarshalledJson),
				})
			}

			buf, err := proto.Marshal(&logproto.PushRequest{
				Streams: []logproto.Stream{stream},
			})
			if err != nil {
				return err
			}

			// Push to promtail
			buf = snappy.Encode(nil, buf)
			req, err := http.NewRequest("POST", promtailAddress.String(), bytes.NewReader(buf))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", contentType)

			resp, err := http.DefaultClient.Do(req.WithContext(ctx))
			if err != nil {
				return err
			}

			if resp.StatusCode/100 != 2 {
				scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
				line := ""
				if scanner.Scan() {
					line = scanner.Text()
				}
				err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
			}
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	runtimeInfo, err := hubAddr.GetRuntimeInformation(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, partitionID := range runtimeInfo.PartitionIDs {
		_, err := hubAddr.Receive(ctx, partitionID, handler, eventhub.ReceiveWithLatestOffset())
		if err != nil {
			fmt.Println("Error: ", err)
			return
		}
	}
	cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	<-signalChan
}
