#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
${CODEGEN_PKG}/generate-groups.sh "deepcopy,defaulter,client,lister,informer" \
  github.com/openshift/ci-vm-operator/pkg/client github.com/openshift/ci-vm-operator/pkg/apis \
  virtualmachines:v1alpha1
