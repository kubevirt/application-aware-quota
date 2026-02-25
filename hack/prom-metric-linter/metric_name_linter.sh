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
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.  * See the License for the specific language governing permissions and
# limitations under the License.
#
# Copyright 2023 Red Hat, Inc.
#
#

set -e

linter_image_tag="v0.0.11"

# Detect container runtime if not set
if [[ -z "${KUBEVIRT_CRI:-}" ]]; then
    if command -v podman >/dev/null 2>&1; then
        KUBEVIRT_CRI="podman"
    elif command -v docker >/dev/null 2>&1; then
        KUBEVIRT_CRI="docker"
    else
        echo "No container runtime found (podman or docker). Set KUBEVIRT_CRI to your runtime."
        exit 1
    fi
fi

# Parse command-line arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
    --operator-name=*)
        operator_name="${1#*=}"
        shift
        ;;
    --sub-operator-name=*)
        sub_operator_name="${1#*=}"
        shift
        ;;
    --metrics-file=*)
        metrics_file="${1#*=}"
        shift
        ;;
    *)
        echo "Invalid argument: $1"
        exit 1
        ;;
    esac
done

# Run the linter by using the prom-metrics-linter Docker container
# Linter image and source references:
# - Image: quay.io/kubevirt/prom-metrics-linter:${linter_image_tag}
# - Source and Dockerfile: https://github.com/kubevirt/monitoring/tree/main/test/metrics/prom-metrics-linter
errors=$(${KUBEVIRT_CRI} run -i "quay.io/kubevirt/prom-metrics-linter:$linter_image_tag" \
    --metric-families="$(cat "$metrics_file")" \
    --operator-name="$operator_name" \
    --sub-operator-name="$sub_operator_name" 2>/dev/null)

# Check if the linter found any errors with the metrics names, if yes print and fail
if [[ $errors != "" ]]; then
    echo "$errors"
    exit 1
fi

