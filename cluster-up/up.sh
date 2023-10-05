#!/usr/bin/env bash

function validate_single_stack_ipv6() {
    local kube_ns="kube-system"
    local pod_label="calico-kube-controllers"

    echo "validating provider is single stack IPv6"
    until _kubectl wait --for=condition=Ready pod --timeout=10s -n $kube_ns -lk8s-app=${pod_label}; do sleep 1; done > /dev/null 2>&1

    local pod=$(_kubectl get pods -n ${kube_ns} -lk8s-app=${pod_label} -o=custom-columns=NAME:.metadata.name --no-headers)
    local primary_ip=$(_kubectl get pod -n ${kube_ns} ${pod} -ojsonpath="{ @.status.podIP }")

    if [[ ! ${primary_ip} =~ fd00 ]]; then
        echo "error: single stack primary ip ($primary_ip) is not IPv6 as expected"
        exit 1
    fi

    if _kubectl get pod -n ${kube_ns} ${pod} -ojsonpath="{ @.status.podIPs[1] }" > /dev/null 2>&1; then
        echo "error: single stack cluster expected"
        exit 1
    fi
}

if [ -z "$KUBEVIRTCI_PATH" ]; then
    KUBEVIRTCI_PATH="$(
        cd "$(dirname "$BASH_SOURCE[0]")/"
        echo "$(pwd)/"
    )"
fi


source ${KUBEVIRTCI_PATH}hack/common.sh
source ${KUBEVIRTCI_CLUSTER_PATH}/$KUBEVIRT_PROVIDER/provider.sh
up

# check if the environment has a corrupted host
if [[ $(${KUBEVIRTCI_PATH}kubectl.sh get nodes | grep localhost) != "" ]]; then
    echo "The environment has a corrupted host"
    exit 1
fi

if [ ${KUBEVIRT_SINGLE_STACK} == true ]; then
    validate_single_stack_ipv6
fi

if [ "$KUBEVIRT_RELEASE" = "latest_nightly" ]; then
  LATEST=$(curl -L https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/latest)
  kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${LATEST}/kubevirt-operator.yaml
  kubectl apply -f https://storage.googleapis.com/kubevirt-prow/devel/nightly/release/kubevirt/kubevirt/${LATEST}/kubevirt-cr.yaml
elif [ "$KUBEVIRT_RELEASE" = "latest_stable" ]; then
  RELEASE=$(curl https://storage.googleapis.com/kubevirt-prow/release/kubevirt/kubevirt/stable.txt)
  kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-operator.yaml
  kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${RELEASE}/kubevirt-cr.yaml
else
  kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_RELEASE}/kubevirt-operator.yaml
  kubectl apply -f https://github.com/kubevirt/kubevirt/releases/download/${KUBEVIRT_RELEASE}/kubevirt-cr.yaml
fi
# Ensure the KubeVirt CRD is created
count=0
until kubectl get crd kubevirts.kubevirt.io; do
    ((count++)) && ((count == 30)) && echo "KubeVirt CRD not found" && exit 1
    echo "waiting for KubeVirt CRD"
    sleep 1
done

# Ensure the KubeVirt API is available
count=0
until kubectl api-resources --api-group=kubevirt.io | grep kubevirts; do
    ((count++)) && ((count == 30)) && echo "KubeVirt API not found" && exit 1
    echo "waiting for KubeVirt API"
    sleep 1
done


# Ensure the KubeVirt CR is created
count=0
until kubectl -n kubevirt get kv kubevirt; do
    ((count++)) && ((count == 30)) && echo "KubeVirt CR not found" && exit 1
    echo "waiting for KubeVirt CR"
    sleep 1
done

# Wait until KubeVirt is ready
count=0
until kubectl wait -n kubevirt kv kubevirt --for condition=Available --timeout 5m; do
    ((count++)) && ((count == 5)) && echo "KubeVirt not ready in time" && exit 1
    echo "Error waiting for KubeVirt to be Available, sleeping 1m and retrying"
    sleep 1m
done
