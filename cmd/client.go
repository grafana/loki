package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.Dial("localhost:9007", opts...)
	user1 := "user1"
	if err != nil {
		fmt.Print("BADD")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Hour)

	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {

		}
	}(conn)
	ctx, err = user.InjectIntoGRPCRequest(user.InjectOrgID(ctx, user1))
	if err != nil {
		fmt.Println("Error injecting org id in grpc request")
		return
	}
	client := frontendv2pb.NewStreamServiceClient(conn)
	stream, err := client.FetchResponse(ctx, &frontendv2pb.LokiStreamingRequest{
		Query:   "sum by (level) (count_over_time({compose_project=\"loki-tsdb-storage-s3\", compose_service=\"query-frontend\"} | drop __error__[1m]))",
		Limit:   20,
		Step:    1,
		StartTs: time.Unix(1709278200, 0),
		EndTs:   time.Unix(1709280000, 0),
	})

	if err != nil {
		fmt.Print("BADD2", err)
	}
	i := 0
	go func() {
		for {
			v, err := stream.Recv()
			if err == io.EOF {
				fmt.Println("EOF", err)
				os.Exit(0)
			}
			if err != nil {
				fmt.Println("Error in receiving stream", err)
				os.Exit(0)
			}
			i++
			fmt.Println(strconv.Itoa(i) + "--" + strconv.Itoa(int(v.Limit)))
			if i == 10 {
				cancel()
			}
		}
	}()

	for {
		select {
		case <-stream.Context().Done():
			//fmt.Println("All done, possible error", stream.Context().Err())
			break
		}
	}
}
