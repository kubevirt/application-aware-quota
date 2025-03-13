#!/bin/bash -e

aaq=$1
aaq="${aaq##*/}"

echo aaq

source ./hack/build/config.sh
source ./hack/build/common.sh
source ./kubevirtci/cluster-up/hack/common.sh
source ./kubevirtci/cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

if [ "${KUBEVIRT_PROVIDER}" = "external" ]; then
   AAQ_SYNC_PROVIDER="external"
else
   AAQ_SYNC_PROVIDER="kubevirtci"
fi
source ./cluster-sync/${AAQ_SYNC_PROVIDER}/provider.sh


AAQ_NAMESPACE=${AAQ_NAMESPACE:-aaq}
AAQ_INSTALL_TIMEOUT=${AAQ_INSTALL_TIMEOUT:-120}
AAQ_AVAILABLE_TIMEOUT=${AAQ_AVAILABLE_TIMEOUT:-600}
AAQ_PODS_UPDATE_TIMEOUT=${AAQ_PODS_UPDATE_TIMEOUT:-480}
AAQ_UPGRADE_RETRY_COUNT=${AAQ_UPGRADE_RETRY_COUNT:-60}

# Set controller verbosity to 3 for functional tests.
export VERBOSITY=3

PULL_POLICY=${PULL_POLICY:-IfNotPresent}
# The default DOCKER_PREFIX is set to kubevirt and used for builds, however we don't use that for cluster-sync
# instead we use a local registry; so here we'll check for anything != "external"
# wel also confuse this by swapping the setting of the DOCKER_PREFIX variable around based on it's context, for
# build and push it's localhost, but for manifests, we sneak in a change to point a registry container on the
# kubernetes cluster.  So, we introduced this MANIFEST_REGISTRY variable specifically to deal with that and not
# have to refactor/rewrite any of the code that works currently.
MANIFEST_REGISTRY=$DOCKER_PREFIX

if [ "${KUBEVIRT_PROVIDER}" != "external" ]; then
  registry=${IMAGE_REGISTRY:-localhost:$(_port registry)}
  DOCKER_PREFIX=${registry}
  MANIFEST_REGISTRY="registry:5000"
fi

if [ "${KUBEVIRT_PROVIDER}" == "external" ]; then
  # No kubevirtci local registry, likely using something external
  if [[ $(${AAQ_CRI} login --help | grep authfile) ]]; then
    registry_provider=$(echo "$DOCKER_PREFIX" | cut -d '/' -f 1)
    echo "Please log in to "${registry_provider}", bazel push expects external registry creds to be in ~/.docker/config.json"
    ${AAQ_CRI} login --authfile "${HOME}/.docker/config.json" $registry_provider
  fi
fi

# Need to set the DOCKER_PREFIX appropriately in the call to `make docker push`, otherwise make will just pass in the default `kubevirt`

DOCKER_PREFIX=$MANIFEST_REGISTRY PULL_POLICY=$PULL_POLICY make manifests
DOCKER_PREFIX=$DOCKER_PREFIX make push


function check_structural_schema {
  for crd in "$@"; do
    status=$(_kubectl get crd $crd -o jsonpath={.status.conditions[?\(@.type==\"NonStructuralSchema\"\)].status})
    if [ "$status" == "True" ]; then
      echo "ERROR CRD $crd is not a structural schema!, please fix"
      _kubectl get crd $crd -o yaml
      exit 1
    fi
    echo "CRD $crd is a StructuralSchema"
  done
}

function wait_aaq_available {
  echo "Waiting $AAQ_AVAILABLE_TIMEOUT seconds for AAQ to become available"
  if [ "$KUBEVIRT_PROVIDER" == "os-3.11.0-crio" ]; then
    echo "Openshift 3.11 provider"
    available=$(_kubectl get aaq aaq -o jsonpath={.status.conditions[0].status})
    wait_time=0
    while [[ $available != "True" ]] && [[ $wait_time -lt ${AAQ_AVAILABLE_TIMEOUT} ]]; do
      wait_time=$((wait_time + 5))
      sleep 5
      sleep 5
      available=$(_kubectl get aaq aaq -o jsonpath={.status.conditions[0].status})
      fix_failed_sdn_pods
    done
  else
    if ! _kubectl wait aaqs.aaq.kubevirt.io/${CR_NAME} --for=condition=Available --timeout=${AAQ_AVAILABLE_TIMEOUT}s; then
      echo "timeout while waiting to AAQ CR to be available"
      _kubectl get aaqs.aaq.kubevirt.io/${CR_NAME} -o yaml
      _kubectl get deployment -n "${AAQ_NAMESPACE}" aaq-operator -o yaml
      _kubectl get pod -n "${AAQ_NAMESPACE}"
      _kubectl describe pod -n "${AAQ_NAMESPACE}" -l name=aaq-operator
      _kubectl logs -n "${AAQ_NAMESPACE}" -l name=aaq-operator
      exit 1
    fi
  fi
}


OLD_AAQ_VER_PODS="./_out/tests/old_aaq_ver_pods"
NEW_AAQ_VER_PODS="./_out/tests/new_aaq_ver_pods"

mkdir -p ./_out/tests
rm -f $OLD_AAQ_VER_PODS $NEW_AAQ_VER_PODS

# Install AAQ
install_aaq

#wait aaq crd is installed with timeout
wait_aaq_crd_installed $AAQ_INSTALL_TIMEOUT


_kubectl apply -f "./_out/manifests/release/aaq-cr.yaml"
wait_aaq_available



# Grab all the AAQ crds so we can check if they are structural schemas
aaq_crds=$(_kubectl get crd -l aaq.kubevirt.io -o jsonpath={.items[*].metadata.name})
crds=($aaq_crds)
operator_crds=$(_kubectl get crd -l operator.aaq.kubevirt.io -o jsonpath={.items[*].metadata.name})
crds+=($operator_crds)
check_structural_schema "${crds[@]}"
