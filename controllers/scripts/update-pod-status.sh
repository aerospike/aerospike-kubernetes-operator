#! /bin/bash
# ------------------------------------------------------------------------------
# Copyright 2012-2021 Aerospike, Inc.
#
# Portions may be licensed to Aerospike, Inc. under one or more contributor
# license agreements.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not
# use this file except in compliance with the License. You may obtain a copy of
# the License at http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
# WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
# License for the specific language governing permissions and limitations under
# the License.
# ------------------------------------------------------------------------------
set -e
set -x

script_dir="$(dirname $(realpath $0))"
cd $script_dir

# Set up common environment variables.
source ./common-env.sh

# ------------------------------------------------------------------------------
# Update pod status in the k8s aerospike cluster object
# ------------------------------------------------------------------------------

# Parse out cluster name, formatted as: stsname-rackid-index
# https://www.linuxjournal.com/article/8919
# Trim index and rackid

AERO_CLUSTER_NAME=${MY_POD_NAME%-*}
AERO_CLUSTER_NAME=${AERO_CLUSTER_NAME%-*}

python3 create_pod_status_patch.py \
--pod-name $MY_POD_NAME \
--cluster-name $AERO_CLUSTER_NAME \
--namespace $NAMESPACE \
--api-server $KUBE_API_SERVER \
--token $TOKEN \
--ca-cert $CA_CERT

if [ $? -ne 0 ]
then
   echo "ERROR: failed to initialize and update pod status"
   exit 1
fi

# Patch the pod status.
cat /tmp/patch.json | curl -f -X PATCH -d @- --cacert $CA_CERT -H "Authorization: Bearer $TOKEN"\
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json-patch+json' \
     "$KUBE_API_SERVER/apis/asdb.aerospike.com/v1beta1/namespaces/$NAMESPACE/aerospikeclusters/$AERO_CLUSTER_NAME/status?fieldManager=pod"
