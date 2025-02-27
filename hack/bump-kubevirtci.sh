#!/bin/bash

set -ex

source $(dirname "$0")/config.sh

val=$(curl -L https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirtci/latest)
sed -i "/^[[:blank:]]*kubevirtci_git_hash[[:blank:]]*=/s/=.*/=\"${val}\"/" hack/config.sh

hack/sync-kubevirtci.sh