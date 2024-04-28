#!/usr/bin/env bash

set -e

source $(dirname "$0")/build/common.sh
source $(dirname "$0")/build/config.sh

protoc --go_out=plugins=grpc:. staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.proto
cp staging/src/kubevirt.io/application-aware-quota-api/libsidecar/evaluator-server-com/evaluate.pb.go pkg/util/net/generated