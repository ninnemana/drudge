PKG="${GOPATH}/src/github.com/ninnemana/drudge/example/basic"
SHELL := /bin/bash -o pipefail

UNAME_OS := $(shell uname -s)
UNAME_ARCH := $(shell uname -m)


# proto is a target that uses prototool.
# By depending on $(PROTOTOOL), prototool will be installed on the Makefile's path.
# Since the path above has the temporary GOBIN at the front, this will use the
# locally installed prototool.
.PHONY: generate

godeps:
	@go get github.com/grpc-ecosystem/grpc-gateway@v1.11.3
	@go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway@v1.11.3
	@go get github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger@v1.11.3
	@go get github.com/golang/protobuf/protoc-gen-go@v1.3.2
	@go get github.com/gogo/protobuf/gogoproto@v1.3.1

prototool: godeps
	@curl -sSL \
		https://github.com/uber/prototool/releases/download/v1.9.0/prototool-${UNAME_OS}-${UNAME_ARCH} \
		-o ./prototool
	@chmod u+x ./prototool

generate: prototool
	@./prototool generate prototool.yaml