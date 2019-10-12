#!/bin/bash

cd /root
source .bashrc
go get github.com/mattn/go-oci8
cd /go/src/gRPC
#tail -f /dev/null
go run $1
