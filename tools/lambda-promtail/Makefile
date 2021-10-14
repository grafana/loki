all: build docker

build:
	GOOS=linux CGO_ENABLED=0 go build lambda-promtail/main.go

clean:
	rm main
