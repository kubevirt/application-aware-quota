#Copyright 2023 The AAQ Authors.
#
#Licensed under the Apache License, Version 2.0 (the "License");
#you may not use this file except in compliance with the License.
#You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
#Unless required by applicable law or agreed to in writing, software
#distributed under the License is distributed on an "AS IS" BASIS,
#WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#See the License for the specific language governing permissions and
#limitations under the License.

.PHONY: manifests \
		cluster-up cluster-down cluster-sync \
		test test-functional test-unit test-lint \
		publish \
		aaq_controller \
		aaq_server \
		aaq_operator \
		fmt \
		goveralls \
		release-description \
		bazel-build-images push-images \
		fossa \
		bump-kubevirtci
all: build

build:  aaq_controller aaq_server aaq_operator

DOCKER?=1
ifeq (${DOCKER}, 1)
	# use entrypoint.sh (default) as your entrypoint into the container
	DO=./hack/build/in-docker.sh
	# use entrypoint-bazel.sh as your entrypoint into the container.
	DO_BAZ=./hack/build/bazel-docker.sh
else
	DO=eval
	DO_BAZ=eval
endif

all: manifests build-images

manifests:
	${DO_BAZ} "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} VERBOSITY=${VERBOSITY} PULL_POLICY=${PULL_POLICY} CR_NAME=${CR_NAME} AAQ_NAMESPACE=${AAQ_NAMESPACE} ./hack/build/build-manifests.sh"

builder-push:
	./hack/build/bazel-build-builder.sh

generate:
	${DO_BAZ} "./hack/update-codegen.sh"

generate-verify: generate
	./hack/verify-generate.sh
	./hack/check-for-binaries.sh

cluster-up:
	./hack/cluster-up.sh

cluster-down:
	./kubevirtci/cluster-up/down.sh

push-images:
	eval "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG}  ./hack/build/build-docker.sh push"

build-images:
	eval "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG}  ./hack/build/build-docker.sh"

push: build-images push-images

cluster-clean-aaq:
	./cluster-sync/clean.sh

cluster-sync: cluster-clean-aaq
	./cluster-sync/sync.sh AAQ_AVAILABLE_TIMEOUT=${AAQ_AVAILABLE_TIMEOUT} DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} PULL_POLICY=${PULL_POLICY} AAQ_NAMESPACE=${AAQ_NAMESPACE}

test: WHAT = ./pkg/... ./cmd/...
test: bootstrap-ginkgo
	${DO_BAZ} "ACK_GINKGO_DEPRECATIONS=${ACK_GINKGO_DEPRECATIONS} ./hack/build/run-unit-tests.sh ${WHAT}"

build-functest:
	${DO_BAZ} ./hack/build/build-functest.sh

functest:  WHAT = ./tests/...
functest: build-functest
	./hack/build/run-functional-tests.sh ${WHAT} "${TEST_ARGS}"

bootstrap-ginkgo:
	${DO_BAZ} ./hack/build/bootstrap-ginkgo.sh

aaq_controller:
	go build -o aaq_controller -v cmd/aaq-controller/*.go
	chmod 777 aaq_controller

aaq_operator:
	go build -o aaq_operator -v cmd/aaq-operator/*.go
	chmod 777 aaq_operator

aaq_server:
	go build -o aaq_server -v cmd/aaq-server/*.go
	chmod 777 aaq_server

csv-generator:
	go build -o bin/csv-generator -v tools/csv-generator/csv-generator.go
	chmod 777 bin/csv-generator

release-description:
	./hack/build/release-description.sh ${RELREF} ${PREREF}

clean:
	rm ./aaq_controller ./aaq_operator ./aaq_server -f

gen-proto:
	${DO_BAZ} "DOCKER_PREFIX=${DOCKER_PREFIX} DOCKER_TAG=${DOCKER_TAG} IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY} VERBOSITY=${VERBOSITY} ./hack/gen-proto.sh"

fmt:
	go fmt .

run: build
	sudo ./aaq_controller

bump-kubevirtci:
	./hack/bump-kubevirtci.sh

build-metrics-docs:
	${DO_BAZ} "./hack/build-metrics-docs.sh"
