gen-proto:
	rm -rf ./src/proto/gen
	# Use gogo protobuf because the Google library fails very basic marshal/unmarshal fuzz tests
	# which makes fuzz testing this library impossible.
	docker run --rm -v `pwd`:`pwd` -w `pwd` znly/protoc --proto_path=./src/proto --gofast_out=./src/proto ./src/proto/simple.proto

test:
	go test ./...