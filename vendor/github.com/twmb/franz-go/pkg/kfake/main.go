//go:build none

package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/twmb/franz-go/pkg/kfake"
)

func main() {
	c, err := kfake.NewCluster(
		kfake.Ports(9092, 9093, 9094),
		kfake.SeedTopics(-1, "foo"),
	)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	addrs := c.ListenAddrs()
	for _, addr := range addrs {
		fmt.Println(addr)
	}

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt)
	<-sigs
}
