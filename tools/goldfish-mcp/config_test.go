package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConfigValidate_ValidConfig(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3100",
		Timeout: 30 * time.Second,
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfigValidate_InvalidURL(t *testing.T) {
	cfg := Config{
		APIURL:  "not a url",
		Timeout: 30 * time.Second,
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API URL must include scheme")
}

func TestConfigValidate_TimeoutTooShort(t *testing.T) {
	cfg := Config{
		APIURL:  "http://localhost:3100",
		Timeout: 500 * time.Millisecond,
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout must be at least 1s")
}

func TestConfigValidate_EmptyURL(t *testing.T) {
	cfg := Config{
		APIURL:  "",
		Timeout: 30 * time.Second,
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API URL cannot be empty")
}

func TestConfigValidate_MalformedURL(t *testing.T) {
	cfg := Config{
		APIURL:  "ht!tp://bad url",
		Timeout: 30 * time.Second,
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid API URL")
}

func TestConfigValidate_ValidHTTPS(t *testing.T) {
	cfg := Config{
		APIURL:  "https://goldfish.example.com:8080/api",
		Timeout: 60 * time.Second,
	}
	err := cfg.Validate()
	assert.NoError(t, err)
}
