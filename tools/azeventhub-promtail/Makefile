# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
UNAME_S := $(shell uname -s)
LOCAL_PORT ?= 3500
ifeq ($(UNAME_S),Linux)
	LOCAL_ENDPOINT=http://localhost:$(LOCAL_PORT)/loki/api/v1/push
else
	LOCAL_ENDPOINT=http://host.docker.internal:$(LOCAL_PORT)/loki/api/v1/push
endif

all: clean build
build:
		$(GOBUILD) -o ./bin/azeventhub-promtail ./azeventhub-promtail/main.go
clean:
		$(GOCLEAN)
		rm -rf ./bin