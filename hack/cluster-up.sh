#!/usr/bin/env bash
#
# This file is part of the KubeVirt project
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
#
# Copyright 2021 Red Hat, Inc.
#

set -e
set -x

function install_kubevirt() {
  if [ "$KUBEVIRT_RELEASE" = "latest_nightly" ]; then
    LATEST=$(curl -L https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/latest)
    kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${LATEST}/kubevirt-operator.yaml
    kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${LATEST}/kubevirt-cr.yaml
  elif [ "$KUBEVIRT_RELEASE" = "latest_stable" ]; then
    RELEASE=$(curl https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
    kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-operator.yaml
    kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-cr.yaml
  else
    kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_RELEASE}/kubevirt-operator.yaml
    kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_RELEASE}/kubevirt-cr.yaml
  fi
  # Ensure the KubeVirt CRD is created
  count=0
  until kubectl get crd kubevirts.kubevirt.io; do
      ((count++)) && ((count == 30)) && echo "KubeVirt CRD not found" && exit 1
      echo "waiting for KubeVirt CRD"
      sleep 1
  done

  # Ensure the KubeVirt API is available
  count=0
  until kubectl api-resources --api-group=kubevirt.io | grep kubevirts; do
      ((count++)) && ((count == 30)) && echo "KubeVirt API not found" && exit 1
      echo "waiting for KubeVirt API"
      sleep 1
  done


  # Ensure the KubeVirt CR is created
  count=0
  until kubectl -n kubevirt get kv kubevirt; do
      ((count++)) && ((count == 30)) && echo "KubeVirt CR not found" && exit 1
      echo "waiting for KubeVirt CR"
      sleep 1
  done

  # Wait until KubeVirt is ready
  count=0
  until kubectl wait -n kubevirt kv kubevirt --for condition=Available --timeout 5m; do
      ((count++)) && ((count == 5)) && echo "KubeVirt not ready in time" && exit 1
      echo "Error waiting for KubeVirt to be Available, sleeping 1m and retrying"
      sleep 1m
  done
}

source hack/config-kubevirtci.sh
KUBEVIRT_DEPLOY_CDI=${KUBEVIRT_DEPLOY_CDI:-true}
source "${KUBEVIRTCI_PATH}up.sh"
install_kubevirt