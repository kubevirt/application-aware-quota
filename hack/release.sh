#!/bin/bash

source $(dirname "$0")/build/common.sh
source $(dirname "$0")/build/config.sh

set -e

if [ -z "${AAQ_CRI}" ]; then
    echo >&2 "no working container runtime found. Neither docker nor podman seems to work."
    exit 1
fi

if [ -z ${GPG_PRIVATE_KEY_FILE} ]; then
    echo "GPG_PRIVATE_KEY_FILE env var must be set"
    exit 1
elif [ -z ${GPG_PASSPHRASE_FILE} ]; then
    echo "GPG_PASSPHRASE_FILE env var must be set"
    exit 1
elif [ -z ${GITHUB_API_TOKEN_FILE} ]; then
    echo "GITHUB_API_TOKEN_FILE env var must be set"
    exit 1
fi

GIT_USER=${GIT_USER:-$(git config user.name)}
GIT_EMAIL=${GIT_EMAIL:-$(git config user.email)}

echo "git user:  $GIT_USER"
echo "git email: $GIT_EMAIL"

${AAQ_CRI} pull quay.io/kubevirtci/release-tool:latest

echo "${AAQ_CRI} run -it --rm \
-v ${GPG_PRIVATE_KEY_FILE}:/home/releaser/gpg-private:Z \
-v ${GPG_PASSPHRASE_FILE}:/home/releaser/gpg-passphrase:Z \
-v ${GITHUB_API_TOKEN_FILE}:/home/releaser/github-api-token:Z \
-v ~/.gitconfig:/home/.gitconfig \
quay.io/kubevirtci/release-tool:latest \
--org=kubevirt \
--repo=application-aware-quota \
--git-email \"${GIT_EMAIL}\" \
--git-user \"${GIT_USER}\"
\"$@\""

${AAQ_CRI} run -it --rm \
    -v ${GPG_PRIVATE_KEY_FILE}:/home/releaser/gpg-private:Z \
    -v ${GPG_PASSPHRASE_FILE}:/home/releaser/gpg-passphrase:Z \
    -v ${GITHUB_API_TOKEN_FILE}:/home/releaser/github-api-token:Z \
    -v ~/.gitconfig:/home/.gitconfig \
    quay.io/kubevirtci/release-tool:latest \
    --org=kubevirt \
    --repo=application-aware-quota \
    --git-email "${GIT_EMAIL}" \
    --git-user "${GIT_USER}" \
    "$@"