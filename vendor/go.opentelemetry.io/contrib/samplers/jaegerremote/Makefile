PROTOC=docker run --rm -v${PWD}:${PWD} -w${PWD} otel/build-protobuf:latest --proto_path=${PWD}

PROTO_INCLUDES=-I/usr/include/github.com/gogo/protobuf

.PHONY: proto-gen
proto-gen:
	mkdir -p internal/proto-gen

	$(PROTOC) $(PROTO_INCLUDES) \
	    --gogo_out=$(PWD)/internal/proto-gen \
	    ${PWD}/jaeger-idl/proto/api_v2/sampling.proto
