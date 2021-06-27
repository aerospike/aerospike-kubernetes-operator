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

script_dir="$(dirname $(dirname $(realpath $0)))"
cd $script_dir

# Set up common environment variables.
source ./common-env.sh

# ------------------------------------------------------------------------------
# Update pod status in the k8s aerospike cluster object
# ------------------------------------------------------------------------------

# Get pod image
POD_JSON="$(curl -f --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "$KUBE_API_SERVER/api/v1/namespaces/$NAMESPACE/pods/$MY_POD_NAME")"
export POD_IMAGE="$(echo $POD_JSON | python3 -c "import sys, json
data = json.load(sys.stdin)
print(data['spec']['containers'][0]['image'])")"

# Parse out cluster name, formatted as: stsname-rackid-index
# https://www.linuxjournal.com/article/8919
# Trim index and rackid

AERO_CLUSTER_NAME=${MY_POD_NAME%-*}
AERO_CLUSTER_NAME=${AERO_CLUSTER_NAME%-*}

# Read this pod's Aerospike pod status from the cluster status.
AERO_CLUSTER_JSON="$(curl -f --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "$KUBE_API_SERVER/apis/asdb.aerospike.com/v1alpha1/namespaces/$NAMESPACE/aerospikeclusters/$AERO_CLUSTER_NAME")"

if [ $? -ne 0 ]
then
   echo "ERROR: failed to read status for $AERO_CLUSTER_NAME"
   exit 1
fi

IS_NEW="$(echo $AERO_CLUSTER_JSON | python3 -c "import sys, json
data = json.load(sys.stdin)

if 'status' in data:
   status = data['status']
else:
   status = {}

podname = '${MY_POD_NAME}';
def isNew(status, podname):
    if  not 'pods' in status:
      return True
    return podname not in status['pods']
print(isNew(status, podname))")"

if [ "$IS_NEW" == "True" ]
then
    echo "Pod first run - initializing"

else
    echo "Pod restarted"
fi

echo $AERO_CLUSTER_JSON | python3 create_pod_status_patch.py $MY_POD_NAME

if [ $? -ne 0 ]
then
   echo "ERROR: failed to initialize and update pod status"
   exit 1
fi

# Patch the pod status.
cat /tmp/patch.json | curl -f -X PATCH -d @- --cacert $CA_CERT -H "Authorization: Bearer $TOKEN"\
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json-patch+json' \
     "$KUBE_API_SERVER/apis/asdb.aerospike.com/v1alpha1/namespaces/$NAMESPACE/aerospikeclusters/$AERO_CLUSTER_NAME/status?fieldManager=pod"
