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

set -e

script_dir="$(readlink -f $(dirname $0))"
source "${script_dir}"/common.sh
source "${script_dir}"/config.sh

opt="${1:-build}"

if ! command -v docker &> /dev/null && ! command -v podman &> /dev/null; then
    echo "Error: Neither Docker nor Podman found. Please install one of them."
    exit 1
fi

cri_cmd=""
insecure=""
if command -v podman &> /dev/null; then
    cri_cmd="podman"
    insecure="--tls-verify=false"
else
    cri_cmd="docker"
fi

PUSH_TARGETS=(${PUSH_TARGETS:-$CONTROLLER_IMAGE_NAME $AAQ_SERVER_IMAGE_NAME $OPERATOR_IMAGE_NAME})
echo "Using ${cri_cmd}, docker_prefix: $DOCKER_PREFIX, docker_tag: $DOCKER_TAG"
for target in ${PUSH_TARGETS[@]}; do
    BIN_NAME="${target}"
    IMAGE="${DOCKER_PREFIX}/${BIN_NAME}:${DOCKER_TAG}"

    if [ "${opt}" == "build" ]; then
        (
            pwd
            ${cri_cmd} "${opt}" -t ${IMAGE} . -f Dockerfile.${BIN_NAME}
        )
    elif [ "${opt}" == "push" ]; then
        ${cri_cmd} "${opt}" ${insecure} "${IMAGE}"
    elif [ "${opt}" == "publish" ]; then
        ${cri_cmd} push ${insecure} ${IMAGE}
    fi
done

cd example_sidecars
PUSH_EXAMPLE_SIDECARS=("$LABEL_SIDECAR_IMAGE_NAME" "$ANNOTATION_SIDECAR_IMAGE_NAME")
for target in ${PUSH_EXAMPLE_SIDECARS[@]}; do
    cd ${target}
    BIN_NAME="${target}"
    IMAGE="${DOCKER_PREFIX}/${BIN_NAME}:${DOCKER_TAG}"

    if [ "${opt}" == "build" ]; then
        (
            pwd
            ${cri_cmd} "${opt}" -t ${IMAGE} . -f Dockerfile.${BIN_NAME}
        )
    elif [ "${opt}" == "push" ]; then
        ${cri_cmd} "${opt}" ${insecure} "${IMAGE}"
    elif [ "${opt}" == "publish" ]; then
        ${cri_cmd} push ${insecure} ${IMAGE}
    fi
    cd ..
done