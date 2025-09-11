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
    local bin_name=$1
    local build_dir=$2
    local images=""
    local manifest_name="${DOCKER_PREFIX}/${bin_name}:${DOCKER_TAG}"

    for arch in ${BUILD_ARCHES}; do
        image_name="${manifest_name}-${arch}"
        echo "building image ${image_name} in ${build_dir}"
        set -x
        ${AAQ_CRI} build \
            --platform="linux/${arch}" \
            -t "${image_name}" \
            --build-arg git_sha=${GIT_HASH} \
            "${build_dir}" -f "${build_dir}/Dockerfile.${bin_name}"
        set +x
        echo "pushing image ${image_name}"
        ${AAQ_CRI} push ${insecure_param} "${image_name}"
        images="${images} ${image_name}"
    done

    # podman does not support overwrite of an existing manifest. Remove it if it exists
    if ${AAQ_CRI} manifest exists "${manifest_name}"; then
        echo "removing existing manifest ${manifest_name}"
        ${AAQ_CRI} manifest rm "${manifest_name}"
    fi

    echo "creating manifest ${manifest_name}"
    ${AAQ_CRI} manifest create ${manifest_name} ${images}
    echo "pushing manifest ${manifest_name}"
    ${AAQ_CRI} manifest push ${insecure_param} "${manifest_name}"
}

# allow running binary within image with one architecture, running on machine with another architecture
${AAQ_CRI} run --rm --privileged docker.io/multiarch/qemu-user-static --reset -p yes

PUSH_TARGETS=(${PUSH_TARGETS:-$CONTROLLER_IMAGE_NAME $AAQ_SERVER_IMAGE_NAME $OPERATOR_IMAGE_NAME})

echo "Using ${AAQ_CRI}, docker_prefix: $DOCKER_PREFIX, docker_tag: $DOCKER_TAG"
for target in ${PUSH_TARGETS[@]}; do
    create_push_multi_arch_manifest ${target} .
done

EXAMPLE_SIDECAR_DIR=example_sidecars

PUSH_EXAMPLE_SIDECARS=$(ls -l ${EXAMPLE_SIDECAR_DIR} | grep '^d' | awk '{print $9}')
for target in ${PUSH_EXAMPLE_SIDECARS[@]}; do
    create_push_multi_arch_manifest ${target} ./${EXAMPLE_SIDECAR_DIR}/${target}
done
