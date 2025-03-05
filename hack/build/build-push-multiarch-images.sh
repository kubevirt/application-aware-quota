#!/usr/bin/env bash

#Copyright 2025 The AAQ Authors.
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

################################################################################
# This script builds and push multi-arch manifest images
# The reason we can't split the build and the push, as desired, is that both
# docker and earlier versions of podman, cannot build manifests from local
# images, and so we must push the per-architecture images first, before creating
# the manifest from them. This is fixed in podman 5, but many build machine are
# still using older versions.
################################################################################

set -e

script_dir="$(readlink -f $(dirname $0))"
source "${script_dir}/common.sh"
source "${script_dir}/config.sh"

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

# allow running binary within image with one architecture, running on machine with another architecture
${cri_cmd} run --rm --privileged docker.io/multiarch/qemu-user-static --reset -p yes

PUSH_TARGETS=(${PUSH_TARGETS:-$CONTROLLER_IMAGE_NAME $AAQ_SERVER_IMAGE_NAME $OPERATOR_IMAGE_NAME})
echo "Using ${cri_cmd}, docker_prefix: $DOCKER_PREFIX, docker_tag: $DOCKER_TAG"
for target in ${PUSH_TARGETS[@]}; do
    BIN_NAME="${target}"

    IMAGE="${DOCKER_PREFIX}/${BIN_NAME}:${DOCKER_TAG}"
    IMAGES=""
    for arch in ${BUILD_ARCHES}; do
      IMAGE_NAME="${IMAGE}-${arch}"
      echo "building image ${IMAGE_NAME}"
      ${cri_cmd} build --platform="linux/${arch}" -t ${IMAGE_NAME} . -f Dockerfile.${BIN_NAME}
      echo "pushing image ${IMAGE_NAME}"
      ${cri_cmd} push ${insecure} "${IMAGE_NAME}"
      IMAGES="${IMAGES} ${IMAGE_NAME}"
    done

    echo "creating manifest ${IMAGE}"
    ${cri_cmd} manifest create ${IMAGE} ${IMAGES}
    echo "pushing manifest ${IMAGE}"
    ${cri_cmd} manifest push "${IMAGE}"
done

cd example_sidecars
PUSH_EXAMPLE_SIDECARS=("$LABEL_SIDECAR_IMAGE_NAME")
for target in ${PUSH_EXAMPLE_SIDECARS[@]}; do
    cd ${target}
    BIN_NAME="${target}"
    IMAGE="${DOCKER_PREFIX}/${BIN_NAME}:${DOCKER_TAG}"

    IMAGES=""
    for arch in ${BUILD_ARCHES}; do
      IMAGE_NAME="${IMAGE}-${arch}"
      ${cri_cmd} build --platform="linux/${arch}" -t ${IMAGE_NAME} . -f Dockerfile.${BIN_NAME}
      ${cri_cmd} push ${insecure} "${IMAGE_NAME}"
      IMAGES="${IMAGES} ${IMAGE_NAME}"
    done

    ${cri_cmd} manifest create ${IMAGE} ${IMAGES}
    ${cri_cmd} manifest push "${IMAGE}"
    cd ..
done
