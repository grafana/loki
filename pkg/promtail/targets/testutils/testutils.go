package testutils

import (
	"math/rand"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
)

type TestClient struct {
	Log      log.Logger
	Messages []string
	sync.Mutex
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	level.Debug(c.Log).Log("msg", "received log", "log", s)

	c.Lock()
	defer c.Unlock()
	c.Messages = append(c.Messages, s)
	return nil
}

func InitRandom() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

