#!/usr/bin/env bash

set -eo pipefail

source /etc/profile.d/gimme.sh

export JAVA_HOME=/usr/lib/jvm/java-11
export PATH=${GOPATH}/bin:/go/bin:/opt/gradle/gradle-6.6/bin:$PATH

eval "$@"
