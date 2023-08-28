#!/usr/bin/env bash

set -e

source hack/build/common.sh

# Find every folder containing tests
for dir in $(find ${AAQ_DIR}/pkg/ -type f -name '*_test.go' -printf '%h\n' | sort -u); do
    # If there is no file ending with _suite_test.go, bootstrap ginkgo
    SUITE_FILE=$(find $dir -maxdepth 4 -type f -name '*_test.go')
    echo ${SUITE_FILE}
    echo $dir
    if [ ! -z "$SUITE_FILE" ]; then
        (cd $dir && ginkgo bootstrap || :)
    fi
done
