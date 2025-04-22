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

script_dir="$(cd "$(dirname "$0")" && pwd -P)"

source "${script_dir}"/common.sh

source "${script_dir}"/../common.sh

VERSION=$(date +"%y%m%d%H%M")-$(git rev-parse --short HEAD)
"${script_dir}/build.sh" "${VERSION}"

for ARCH in ${ARCHITECTURES}; do
    ${AAQ_CRI} push "${DOCKER_PREFIX}/${DOCKER_IMAGE}:${VERSION}-${ARCH}"
    TMP_IMAGES="${TMP_IMAGES} ${DOCKER_PREFIX}/${DOCKER_IMAGE}:${VERSION}-${ARCH}"
done

export DOCKER_CLI_EXPERIMENTAL=enabled
${AAQ_CRI} manifest create --amend ${DOCKER_PREFIX}/${DOCKER_IMAGE}:${VERSION} ${TMP_IMAGES}
${AAQ_CRI} manifest push ${DOCKER_PREFIX}/${DOCKER_IMAGE}:${VERSION}
