#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail
set -x
export GO111MODULE=on

export SCRIPT_ROOT="$(cd "$(dirname $0)/../" && pwd -P)"
CODEGEN_PKG=${CODEGEN_PKG:-$(
    cd ${SCRIPT_ROOT}
    ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator
)}
OPENAPI_PKG=${OPENAPI_PKG:-$(
    cd ${SCRIPT_ROOT}
    ls -d -1 ./vendor/k8s.io/kube-openapi 2>/dev/null || echo ../kube-openapi
)}

(GOPROXY=off go install ${CODEGEN_PKG}/cmd/deepcopy-gen)
(GOPROXY=off go install ${CODEGEN_PKG}/cmd/client-gen)
(GOPROXY=off go install ${CODEGEN_PKG}/cmd/informer-gen)
(GOPROXY=off go install ${CODEGEN_PKG}/cmd/lister-gen)

find "${SCRIPT_ROOT}/pkg/" -name "*generated*.go" -exec rm {} -f \;
find "${SCRIPT_ROOT}/staging/src/kubevirt.io/application-aware-quota-api/" -name "*generated*.go" -exec rm {} -f \;
rm -rf "${SCRIPT_ROOT}/pkg/generated"

mkdir "${SCRIPT_ROOT}/pkg/generated"

mkdir "${SCRIPT_ROOT}/pkg/generated/kubevirt"
mkdir "${SCRIPT_ROOT}/pkg/generated/kubevirt/clientset"

mkdir "${SCRIPT_ROOT}/pkg/generated/cluster-resource-quota"
mkdir "${SCRIPT_ROOT}/pkg/generated/cluster-resource-quota/clientset"

mkdir "${SCRIPT_ROOT}/pkg/generated/aaq"
mkdir "${SCRIPT_ROOT}/pkg/generated/aaq/clientset"
mkdir "${SCRIPT_ROOT}/pkg/generated/aaq/informers"
mkdir "${SCRIPT_ROOT}/pkg/generated/aaq/listers"

deepcopy-gen \
	--output-file zz_generated.deepcopy.go \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1


client-gen \
	--clientset-name versioned \
	--input-base kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis \
    --output-dir "${SCRIPT_ROOT}/pkg/generated/aaq/clientset" \
	--output-pkg kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset \
	--apply-configuration-package '' \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    --input core/v1alpha1

client-gen \
	--clientset-name versioned \
	--input-base kubevirt.io/api \
    --output-dir "${SCRIPT_ROOT}/pkg/generated/kubevirt/clientset" \
	--output-pkg kubevirt.io/application-aware-quota/pkg/generated/kubevirt/clientset \
	--apply-configuration-package '' \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    --input core/v1

client-gen \
	--clientset-name versioned \
	--input-base github.com/openshift/api \
    --output-dir "${SCRIPT_ROOT}/pkg/generated/cluster-resource-quota/clientset" \
	--output-pkg kubevirt.io/application-aware-quota/pkg/generated/cluster-resource-quota/clientset \
	--apply-configuration-package '' \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    --input /quota/v1


lister-gen \
	--output-dir "${SCRIPT_ROOT}/pkg/generated/aaq/listers" \
    --output-pkg kubevirt.io/application-aware-quota/pkg/generated/aaq/listers \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1

informer-gen \
	--versioned-clientset-package kubevirt.io/application-aware-quota/pkg/generated/aaq/clientset/versioned \
	--listers-package kubevirt.io/application-aware-quota/pkg/generated/aaq/listers \
	--output-dir "${SCRIPT_ROOT}/pkg/generated/aaq/informers" \
    --output-pkg kubevirt.io/application-aware-quota/pkg/generated/aaq/informers \
	--go-header-file "${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt" \
    kubevirt.io/application-aware-quota/staging/src/kubevirt.io/application-aware-quota-api/pkg/apis/core/v1alpha1




echo "************* running controller-gen to generate schema yaml ********************"
(
    mkdir -p "${SCRIPT_ROOT}/_out/manifests/schema"
    find "${SCRIPT_ROOT}/_out/manifests/schema/" -type f -exec rm {} -f \;
    cd ./staging/src/kubevirt.io/application-aware-quota-api
    controller-gen crd:crdVersions=v1 output:dir=${SCRIPT_ROOT}/_out/manifests/schema paths=./pkg/apis/core/...
)

(cd "${SCRIPT_ROOT}/tools/crd-generator/" && go build -o "${SCRIPT_ROOT}/bin/crd-generator" ./...)
${SCRIPT_ROOT}/bin/crd-generator --crdDir=${SCRIPT_ROOT}/_out/manifests/schema/ --outputDir=${SCRIPT_ROOT}/pkg/aaq-operator/resources/
