TEST?=./...

test:
	go test $(TEST) $(TESTARGS) -timeout=3s -parallel=4
	go vet $(TEST)
	go test $(TEST) -race
	go test $(TEST) -v -coverprofile=coverage.out

.PHONY: test
