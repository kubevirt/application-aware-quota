#!/usr/bin/env bash

set -e
AAQ_INSTALL_TIMEOUT=${AAQ_INSTALL_TIMEOUT:-120}     #timeout for installation sequence

function install_aaq {
  _kubectl apply -f "./_out/manifests/release/aaq-operator.yaml"
}

function wait_aaq_crd_installed {
  timeout=$1
  crd_defined=0
  while [ $crd_defined -eq 0 ] && [ $timeout > 0 ]; do
      crd_defined=$(_kubectl get customresourcedefinition| grep aaqs.aaq.kubevirt.io | wc -l)
      sleep 1
      timeout=$(($timeout-1))
  done

  #In case AAQ crd is not defined after 120s - throw error
  if [ $crd_defined -eq 0 ]; then
     echo "ERROR - AAQ CRD is not defined after timeout"
     exit 1
  fi  
}


