#!/usr/bin/env bash

source cluster-sync/install.sh
source hack/common-funcs.sh

function _kubectl(){
  kubectl "$@"
}


