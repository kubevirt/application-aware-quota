#!/usr/bin/env bash
set -e
source cluster-sync/install.sh
source hack/common-funcs.sh

num_nodes=${KUBEVIRT_NUM_NODES:-1}

re='^-?[0-9]+$'
if ! [[ $num_nodes =~ $re ]] || [[ $num_nodes -lt 1 ]] ; then
    num_nodes=1
fi


