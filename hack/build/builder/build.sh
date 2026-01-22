#!/usr/bin/env bash

#Copyright 2026 The AAQ Authors.
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
if [ "${1}" != "" ]; then
    VERSION="${1}"
fi

# If qemu-static has already been registered as a runner for foreign
# binaries, for example by installing qemu-user and qemu-user-binfmt
# packages on Fedora or by having already run this script earlier,
# then we shouldn't alter the existing configuration to avoid the
# risk of possibly breaking it
if ! grep -q -E '^enabled$' /proc/sys/fs/binfmt_misc/qemu-aarch64 2>/dev/null; then
    ${AAQ_CRI} >&2 run --rm --privileged docker.io/multiarch/qemu-user-static --reset -p yes
fi

for ARCH in ${ARCHITECTURES}; do
    ${AAQ_CRI} >&2 pull --platform="linux/${ARCH}" quay.io/centos/centos:stream9
    ${AAQ_CRI} >&2 build --platform="linux/${ARCH}" -t "${DOCKER_PREFIX}/${DOCKER_IMAGE}:${VERSION}-${ARCH}" -f "${script_dir}/Dockerfile" "${script_dir}"
done
