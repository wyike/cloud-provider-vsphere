#!/usr/bin/env bash

# Copyright 2021 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# SCRIPT_ROOT: the directory in which this script is located
SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
echo $SCRIPT_ROOT
ls $SCRIPT_ROOT
#go get github.com/nomad-software/vend
#vend
#chmod 755 vendor/k8s.io/code-generator/generate-groups.sh

# CODEGEN_PKG: the codegen package which we use to generate client code
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
# ROOT_DIR: the root directory in which the apis are defined
MODULE="k8s.io/cloud-provider-vsphere"
BASE_DIR="pkg/cloudprovider/vsphereparavirtual"
# CUSTOM_RESOURCE_PACKAGE: the name of the custom resource package that we are generating client code for
CUSTOM_RESOURCE_PACKAGE="nsxnetworking"
# CUSTOM_RESOURCE_VERSION: the version of the resource
CUSTOM_RESOURCE_VERSION_1="v1alpha1"
CUSTOM_RESOURCE_VERSION_2="v1alpha2"

# emojis to make nice output
printf "\xF0\x9F\x94\x8D\n"

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
# This generates deepcopy, client, informer and lister for the resource package
bash "${CODEGEN_PKG}"/generate-groups.sh applyconfiguration,client,deepcopy,informer,lister \
  "${MODULE}"/"${BASE_DIR}"/client "${MODULE}"/"${BASE_DIR}"/apis \
  "$CUSTOM_RESOURCE_PACKAGE:$CUSTOM_RESOURCE_VERSION_1,$CUSTOM_RESOURCE_VERSION_2" \
  --output-base "${SCRIPT_ROOT}" \
  --go-header-file "${SCRIPT_ROOT}"/hack/boilerplate.go.txt

#bash "${CODEGEN_PKG}"/generate-groups.sh all \
#  "${MODULE}"/"${BASE_DIR}"/client/"${CUSTOM_RESOURCE_VERSION_2}" "${MODULE}"/"${BASE_DIR}"/apis \
#  "$CUSTOM_RESOURCE_PACKAGE:$CUSTOM_RESOURCE_VERSION_2" \
#  --output-base "${SCRIPT_ROOT}" \
#  --go-header-file "${SCRIPT_ROOT}"/hack/boilerplate.go.txt

cp -r "${SCRIPT_ROOT}"/"${MODULE}"/"${BASE_DIR}"/* "${BASE_DIR}"
rm -rf "${SCRIPT_ROOT}"/k8s.io

#rm -rf vendor