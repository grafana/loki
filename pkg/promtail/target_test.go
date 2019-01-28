package promtail

import (
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLongSyncDelayStillSavesCorrectPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFile := dirName + "/positions.yml"
	logFile := dirName + "/test.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	//Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	//everything saved was done through channel notifications when target.stop() was called
	positions, err := NewPositions(logger, PositionsConfig{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFile,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	//Create the target which should start tailing
	target, err := NewTarget(logger, client, positions, logFile, nil)
	if err != nil {
		t.Error(err)
		return
	}

	//Create the test log file
	f, err := os.Create(logFile)
	if err != nil {
		t.Error(err)
		return
	}

	//Write some entries to it
	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	//Stop the target
	target.Stop()

	//Load the positions file and unmarshal it
	buf, err := ioutil.ReadFile(filepath.Clean(positionsFile))
	if err != nil {
		t.Error("Expected to find a positions file but did not", err)
		t.FailNow()
	}
	var p PositionsFile
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Error("Failed to parse positions file:", err)
		t.FailNow()
	}

	//Assert the position value is in the correct spot
	if val, ok := p.Positions[logFile]; ok {
		if val != 50 {
			t.Error("Incorrect position found, expected 50, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}

	//Assert the number of messages the handler received is correct
	if len(client.messages) != 10 {
		t.Error("Handler did not receive the correct number of messages, expected 10 received", len(client.messages))
	}

	//Spot check one of the messages
	if client.messages[0] != "test" {
		t.Error("Expected first log message to be 'test' but was", client.messages[0])
	}

}

func TestFileRolls(t *testing.T) {

}

func TestResumesWhereLeftOff(t *testing.T) {

}

func TestGlobWithMultipleFiles(t *testing.T) {

}

type TestClient struct {
	log      log.Logger
	messages []string //FIXME should this be an array of pointers to strings?
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	c.messages = append(c.messages, s)
	//level.Info(c.log).Log("msg", "received log", "log", s)
	return nil
}

func initRandom() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
