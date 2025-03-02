#!/usr/bin/env bash

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

set -exo pipefail

readonly MAX_AAQ_WAIT_RETRY=30
readonly AAQ_WAIT_TIME=10

script_dir="$(cd "$(dirname "$0")" && pwd -P)"
source hack/build/config.sh
source hack/build/common.sh
source kubevirtci/cluster-up/hack/common.sh

KUBEVIRTCI_CONFIG_PATH="$(
    cd "$(dirname "$BASH_SOURCE[0]")/../../"
    echo "$(pwd)/kubevirtci/_ci-configs"
)"

# functional testing
BASE_PATH=${KUBEVIRTCI_CONFIG_PATH:-$PWD}
KUBECONFIG=${KUBECONFIG:-$BASE_PATH/$KUBEVIRT_PROVIDER/.kubeconfig}
GOCLI=${GOCLI:-${AAQ_DIR}/kubevirtci/cluster-up/cli.sh}
KUBE_URL=${KUBE_URL:-""}
AAQ_NAMESPACE=${AAQ_NAMESPACE:-aaq}


OPERATOR_CONTAINER_IMAGE=$(./kubevirtci/cluster-up/kubectl.sh get deployment -n $AAQ_NAMESPACE aaq-operator -o'custom-columns=spec:spec.template.spec.containers[0].image' --no-headers)
DOCKER_PREFIX=${OPERATOR_CONTAINER_IMAGE%/*}
DOCKER_TAG=${OPERATOR_CONTAINER_IMAGE##*:}

if [ -z "${KUBECTL+x}" ]; then
    kubevirtci_kubectl="${BASE_PATH}/${KUBEVIRT_PROVIDER}/.kubectl"
    if [ -e ${kubevirtci_kubectl} ]; then
        KUBECTL=${kubevirtci_kubectl}
    else
        KUBECTL=$(which kubectl)
    fi
fi

# parsetTestOpts sets 'pkgs' and test_args
parseTestOpts "${@}"

arg_kubeurl="${KUBE_URL:+-kubeurl=$KUBE_URL}"
arg_namespace="${AAQ_NAMESPACE:+-aaq-namespace=$AAQ_NAMESPACE}"
arg_kubeconfig_aaq="${KUBECONFIG:+-kubeconfig-aaq=$KUBECONFIG}"
arg_kubeconfig="${KUBECONFIG:+-kubeconfig=$KUBECONFIG}"
arg_kubectl="${KUBECTL:+-kubectl-path-aaq=$KUBECTL}"
arg_oc="${KUBECTL:+-oc-path-aaq=$KUBECTL}"
arg_gocli="${GOCLI:+-gocli-path-aaq=$GOCLI}"
arg_docker_prefix="${DOCKER_PREFIX:+-docker-prefix=$DOCKER_PREFIX}"
arg_docker_tag="${DOCKER_TAG:+-docker-tag=$DOCKER_TAG}"

test_args="${test_args}  -ginkgo.v  ${arg_kubeurl} ${arg_namespace} ${arg_kubeconfig} ${arg_kubeconfig_aaq} ${arg_kubectl} ${arg_oc} ${arg_gocli} ${arg_docker_prefix} ${arg_docker_tag}"

echo 'Wait until all AAQ Pods are ready'
retry_counter=0
while [ $retry_counter -lt $MAX_AAQ_WAIT_RETRY ] && [ -n "$(./kubevirtci/cluster-up/kubectl.sh get pods -n $AAQ_NAMESPACE -o'custom-columns=status:status.containerStatuses[*].ready' --no-headers | grep false)" ]; do
    retry_counter=$((retry_counter + 1))
    sleep $AAQ_WAIT_TIME
    echo "Checking AAQ pods again, count $retry_counter"
    if [ $retry_counter -gt 1 ] && [ "$((retry_counter % 6))" -eq 0 ]; then
        ./kubevirtci/cluster-up/kubectl.sh get pods -n $AAQ_NAMESPACE
    fi
done

if [ $retry_counter -eq $MAX_AAQ_WAIT_RETRY ]; then
    echo "Not all AAQ pods became ready"
    ./kubevirtci/cluster-up/kubectl.sh get pods -n $AAQ_NAMESPACE
    ./kubevirtci/cluster-up/kubectl.sh get pods -n $AAQ_NAMESPACE -o yaml
    ./kubevirtci/cluster-up/kubectl.sh describe pods -n $AAQ_NAMESPACE
    exit 1
fi

(
    export TESTS_WORKDIR=${AAQ_DIR}/tests
    ginkgo_args="--trace --timeout=8h --v"
    ${TESTS_OUT_DIR}/ginkgo ${ginkgo_args} ${TESTS_OUT_DIR}/tests.test -- ${test_args}
)