all: build

build:
	GOOS=linux CGO_ENABLED=0 go build -o ./main lambda-promtail/*.go

clean:
	rm main
