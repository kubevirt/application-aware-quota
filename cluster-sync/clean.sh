#!/bin/bash -e

source ./hack/build/config.sh
source ./hack/config-kubevirtci.sh
source ./kubevirtci/cluster-up/hack/common.sh
source ./kubevirtci/cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

echo "Cleaning up ..."

OPERATOR_CR_MANIFEST=./_out/manifests/release/aaq-cr.yaml
OPERATOR_MANIFEST=./_out/manifests/release/aaq-operator.yaml
LABELS=("operator.aaq.kubevirt.io" "aaq.kubevirt.io" "prometheus.aaq.kubevirt.io")
NAMESPACES=(default kube-system "${AAQ_NAMESPACE}")



if [ -f "${OPERATOR_CR_MANIFEST}" ]; then
  echo "Cleaning CR object ..."
  _kubectl delete  --ignore-not-found  -f "${OPERATOR_CR_MANIFEST}" || true
  _kubectl wait  aaqs.aaq.kubevirt.io/${CR_NAME} --for=delete --timeout=30s || echo "this is fine"
fi


if [ "${AAQ_CLEAN}" == "all" ] && [ -f "${OPERATOR_MANIFEST}" ]; then
	echo "Deleting operator ..."
  _kubectl delete --ignore-not-found -f "${OPERATOR_MANIFEST}"
fi

# Everything should be deleted by now, but just to be sure
for n in ${NAMESPACES[@]}; do
  for label in ${LABELS[@]}; do
    _kubectl -n ${n} delete deployment -l ${label} >/dev/null
    _kubectl -n ${n} delete services -l ${label} >/dev/null
    _kubectl -n ${n} delete secrets -l ${label} >/dev/null
    _kubectl -n ${n} delete configmaps -l ${label} >/dev/null
    _kubectl -n ${n} delete pods -l ${label} >/dev/null
    _kubectl -n ${n} delete rolebinding -l ${label} >/dev/null
    _kubectl -n ${n} delete roles -l ${label} >/dev/null
    _kubectl -n ${n} delete serviceaccounts -l ${label} >/dev/null
  done
done

for label in ${LABELS[@]}; do
    _kubectl delete pv -l ${label} >/dev/null
    _kubectl delete clusterrolebinding -l ${label} >/dev/null
    _kubectl delete clusterroles -l ${label} >/dev/null
    _kubectl delete customresourcedefinitions -l ${label} >/dev/null
done

if [ "${AAQ_CLEAN}" == "all" ] && [ -n "$(_kubectl get ns | grep "aaq ")" ]; then
    echo "Clean aaq namespace"
    _kubectl delete ns AAQ_NAMESPACE

    start_time=0
    sample=10
    timeout=120
    echo "Waiting for aaq namespace to disappear ..."
    while [ -n "$(_kubectl get ns | grep "AAQ_NAMESPACE ")" ]; do
        sleep $sample
        start_time=$((current_time + sample))
        if [[ $current_time -gt $timeout ]]; then
            exit 1
        fi
    done
fi
sleep 2
echo "Done"
