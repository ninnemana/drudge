#!/bin/bash

protoc \
-I/usr/local/include \
-I$GOPATH/src/github.com/ninnemana/drudge \
-I$GOPATH/src \
-I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway \
-I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
-I$GOPATH/src/github.com/gogo/protobuf/protobuf \
--gogoslick_out=plugins=grpc,\
Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,\
Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types\
:$GOPATH/src/github.com/ninnemana/drudge \
$GOPATH/src/github.com/ninnemana/drudge/server.proto
