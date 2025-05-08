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

determine_aaq_bin() {
    if [ "${KUBEVIRTCI_RUNTIME-}" = "podman" ]; then
        echo podman
    elif [ "${KUBEVIRTCI_RUNTIME-}" = "docker" ]; then
        echo docker
    else
        if docker ps >/dev/null 2>&1; then
            echo docker
        elif podman ps >/dev/null 2>&1; then
            echo podman
        else
            echo ""
        fi
    fi
}


AAQ_DIR="$(cd $(dirname $0)/../../ && pwd -P)"

AAQ_GO_PACKAGE=kubevirt.io/costume-quota-operator
BIN_DIR=${AAQ_DIR}/bin
OUT_DIR=${AAQ_DIR}/_out
CMD_OUT_DIR=${AAQ_DIR}/cmd
TESTS_OUT_DIR=${OUT_DIR}/tests
BUILD_DIR=${AAQ_DIR}/hack/build
MANIFEST_TEMPLATE_DIR=${AAQ_DIR}/manifests/templates
MANIFEST_GENERATED_DIR=${AAQ_DIR}/manifests/generated
SOURCE_DIRS="pkg tests tools"
APIDOCS_OUT_DIR=${OUT_DIR}/apidocs
CACHE_DIR=${OUT_DIR}/gocache
VENDOR_DIR=${AAQ_DIR}/vendor
ARCHITECTURE="${BUILD_ARCH:-$(uname -m)}"
HOST_ARCHITECTURE="$(uname -m)"
AAQ_CRI="$(determine_aaq_bin)"

if [ "${AAQ_CRI}" = "docker" ]; then
   AAQ_CONTAINER_BUILDCMD=${AAQ_CONTAINER_BUILDCMD:-docker}
else
   AAQ_CONTAINER_BUILDCMD=${AAQ_CONTAINER_BUILDCMD:-buildah}
fi
echo "AAQ_CRI: ${AAQ_CRI}, AAQ_CONTAINER_BUILDCMD: ${AAQ_CONTAINER_BUILDCMD}"


