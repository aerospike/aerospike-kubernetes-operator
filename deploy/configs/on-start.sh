#! /bin/bash
# ------------------------------------------------------------------------------
# Copyright 2012-2017 Aerospike, Inc.
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

# This script writes out an aerospike config using a list of newline seperated
# peer DNS names it accepts through stdin.

# /etc/aerospike is assumed to be a shared volume so we can modify aerospike.conf as required


set -e
set -x
CFG=/etc/aerospike/aerospike.template.conf

function join {
    local IFS="$1"; shift; echo "$*";
}

HOSTNAME=$(hostname)

# Parse out cluster name, formatted as: petset_name-index
IFS='-' read -ra ADDR <<< "$(hostname)"
CLUSTER_NAME="${ADDR[0]}"

# TODO: get the ordinal, this will be used as nodeid.
# This looks hacky way but no other way found yet
NODE_ID="${ADDR[-1]}"
sed -i "s/ENV_NODE_ID/${NODE_ID}/" ${CFG}

while read -ra LINE; do
    if [[ "${LINE}" == *"${HOSTNAME}"* ]]; then
        MY_NAME=$LINE
    fi
    PEERS=("${PEERS[@]}" $LINE)
done

for PEER in "${PEERS[@]}"; do
	sed -i -e "/mesh-seed-placeholder/a \\\t\tmesh-seed-address-port ${PEER} 3002" ${CFG}
done

# Get External nodeIP
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
DATA="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "https://kubernetes/api/v1/nodes")"
EXTERNALIP="$(echo $DATA | python -c "import sys, json
data = json.load(sys.stdin);
host = '${MY_HOST_IP}';
def gethost(data, host):
        for item in data['items']:
                for add in item['status']['addresses']:
                        if add['type'] == 'InternalIP':
                                internalIP = add['address']
                                continue
                        if add['type'] == 'ExternalIP':
                                externalIP = add['address']
                                continue
                        if internalIP != '' and externalIP != '':
                                break
                if internalIP == host:
                        return externalIP
print gethost(data, host)")"

# If multiPodPerHost then get service clusterIP in access-address else get hostIP
if [ "true" == "${MULTI_POD_PER_HOST}" ]
then
    PORT=$((30000 + $NODE_ID))
    sed -i -e "/# access-port <PORT>/a \\\t\taccess-port ${PORT}" ${CFG}
    sed -i -e "/# alternate-access-port <PORT>/a \\\t\talternate-access-port ${PORT}" ${CFG}
fi
sed -i -e "/# access-address <IPADDR>/a \\\t\taccess-address ${EXTERNALIP}" ${CFG}
sed -i -e "/# alternate-access-address <IPADDR>/a \\\t\talternate-access-address ${EXTERNALIP}" ${CFG}

# don't need a restart, we're just writing the conf in case there's an
# unexpected restart on the node.

# # Get External nodeIP
# CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
# TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
# DATA="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "https://kubernetes/api/v1/nodes")"
# EXTERNALIP="$(echo $DATA | python -c "import sys, json
# data = json.load(sys.stdin);
# host = '${MY_HOST_IP}';
# def gethost(data, host):
#         for item in data['items']:
#                 for add in item['status']['addresses']:
#                         if add['type'] == 'InternalIP':
#                                 internalIP = add['address']
#                                 continue
#                         if add['type'] == 'ExternalIP':
#                                 externalIP = add['address']
#                                 continue
#                         if internalIP != '' and externalIP != '':
#                                 break
#                 if internalIP == host:
#                         return externalIP
# print gethost(data, host)")"
#
# echo $EXTERNALIP
