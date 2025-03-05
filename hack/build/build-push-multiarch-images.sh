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

GIT_HASH=$(git rev-parse HEAD)

insecure_param=""
if [[ "${INSECURE_REGISTRY}" == "true" ]]; then
    echo "Using insecure registry"
     insecure_param="--tls-verify=false"
fi

function create_push_multi_arch_manifest() {
    IMAGES=""
    BIN_NAME=$1
    MANIFEST_NAME="${DOCKER_PREFIX}/${BIN_NAME}:${DOCKER_TAG}"

    for arch in ${BUILD_ARCHES}; do
        IMAGE_NAME="${MANIFEST_NAME}-${arch}"
        echo "building image ${IMAGE_NAME}"
        set -x
        ${AAQ_CRI} build --platform="linux/${arch}" -t ${IMAGE_NAME} --build-arg git_sha=${GIT_HASH} . -f Dockerfile.${BIN_NAME}
        set +x
        echo "pushing image ${IMAGE_NAME}"
        ${AAQ_CRI} push ${insecure_param} "${IMAGE_NAME}"
        IMAGES="${IMAGES} ${IMAGE_NAME}"
    done

    # podman does not support overwrite of an existing manifest. Remove it if it exists
    if ${AAQ_CRI} manifest exists "${MANIFEST_NAME}"; then
        echo "removing existing manifest ${MANIFEST_NAME}"
        ${AAQ_CRI} manifest rm "${MANIFEST_NAME}"
    fi

    echo "creating manifest ${MANIFEST_NAME}"
    ${AAQ_CRI} manifest create ${MANIFEST_NAME} ${IMAGES}
    echo "pushing manifest ${MANIFEST_NAME}"
    ${AAQ_CRI} manifest push ${insecure_param} "${MANIFEST_NAME}"
}

# allow running binary within image with one architecture, running on machine with another architecture
${AAQ_CRI} run --rm --privileged docker.io/multiarch/qemu-user-static --reset -p yes

PUSH_TARGETS=(${PUSH_TARGETS:-$CONTROLLER_IMAGE_NAME $AAQ_SERVER_IMAGE_NAME $OPERATOR_IMAGE_NAME})

echo "Using ${AAQ_CRI}, docker_prefix: $DOCKER_PREFIX, docker_tag: $DOCKER_TAG"
for target in ${PUSH_TARGETS[@]}; do
    create_push_multi_arch_manifest ${target}
done

cd example_sidecars
PUSH_EXAMPLE_SIDECARS=$(ls -l | grep '^d' | awk '{print $9}')
for target in ${PUSH_EXAMPLE_SIDECARS[@]}; do
    cd ${target}
    create_push_multi_arch_manifest ${target}
    cd ..
done
