package main

import (
	"testing"
	"time"
	"github.com/stretchr/testify/assert"
)

func TestGetLokiConfig(t* testing.T) {
	c, err := getLokiConfig("", "", "", "")
	if err != nil {
		t.Fatalf("failed test %#v", err)
	}

	assert.Equal(t, "http://localhost:3100/api/prom/push", c.url.String(), "Use default value of URL")
	assert.Equal(t, 10 * time.Millisecond, c.batchWait, "Use default value of batchWait")
	assert.Equal(t, 10 * 1024, c.batchSize, "Use default value of batchSize")

	// Invalid URL
	c, err = getLokiConfig("invalid---URL+*#Q(%#Q", "", "", "")
	if err == nil {
		t.Fatalf("failed test %#v", err)
	}

	// batchWait, batchSize

	c, err = getLokiConfig("", "15", "30", "")
	if err != nil {
		t.Fatalf("failed test %#v", err)
	}
	assert.Equal(t, 15 * time.Millisecond, c.batchWait, "Use user-defined value of batchWait")
	assert.Equal(t, 30 * 1024, c.batchSize, "Use user-defined value of batchSize")

	// LabelSets
	labelJSON := `
{"labels": [
  {"key": "test", "label": "fluent-bit-go"},
  {"key": "lang", "label": "Golang"}
]}
`
	c, err = getLokiConfig("", "15", "30", labelJSON)
	if err != nil {
		t.Fatalf("failed test %#v", err)
	}
	assert.Equal(t, "{lang=\"Golang\", test=\"fluent-bit-go\"}",
		c.labelSet.String(), "Use user-defined value of labels")

}
