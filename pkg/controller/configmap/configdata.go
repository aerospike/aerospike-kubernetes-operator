package configmap

const installSh = `
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


CONFIG_VOLUME="/etc/aerospike"
NAMESPACE=${MY_POD_NAMESPACE:-default}
K8_SERVICE=${SERVICE:-aerospike}
for i in "$@"
do
case $i in
    -c=*|--config=*)
    CONFIG_VOLUME="${i#*=}"
    shift
    ;;
    *)
    # unknown option
    ;;
esac
done

echo installing aerospike.conf into "${CONFIG_VOLUME}"
mkdir -p "${CONFIG_VOLUME}"
#chown -R aerospike:aerospike "${CONFIG_VOLUME}"

cp /configs/on-start.sh /on-start.sh
cp /configs/aerospike.template.conf "${CONFIG_VOLUME}"/
if [ -f /configs/features.conf ]; then
        cp /configs/features.conf "${CONFIG_VOLUME}"/
fi
chmod +x /on-start.sh
chmod +x /peer-finder
/peer-finder -on-start=/on-start.sh -service=$K8_SERVICE -ns=${NAMESPACE} -domain=cluster.local
`

const onStartSh = `
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
        # 8 spaces, fixed in configwriter file config manager lib
	sed -i -e "/heartbeat {/a \\        mesh-seed-address-port ${PEER} 3002" ${CFG}
	#sed -i "0,/mesh-seed-address-port.*<mesh_seed_address_port>/s/mesh-seed-address-port.*<mesh_seed_address_port>/mesh-seed-address-port    ${PEER} 3002/" ${CFG}
done

# Get External nodeIP

CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)

# Get tls, info port
NAMESPACE=$MY_POD_NAMESPACE
SVC="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "https://kubernetes.default.svc/api/v1/namespaces/$NAMESPACE/services")"
PORTSTRING="$(echo $SVC | python -c "import sys, json
data = json.load(sys.stdin);
podname = '${MY_POD_NAME}';
def getport(data, podname):
    for item in data['items']:
        if item['metadata']['name'] == podname:
            infoport = ''
            tlsport = ''
            for port in item['spec']['ports']:
                if port['name'] == 'info':
                    infoport = port['nodePort']
                if port['name'] == 'tls':
                    tlsport = port['nodePort']
            return infoport, tlsport
print getport(data, podname)")"
PORT="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $2}')"
TLSPORT="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $4}')"

# Get nodeIP
DATA="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "https://kubernetes.default.svc/api/v1/nodes")"
EXTERNALIP="$(echo $DATA | python -c "import sys, json
data = json.load(sys.stdin);
host = '${MY_HOST_IP}';
def gethost(data, host):
        for item in data['items']:
                externalIP = ''
                for add in item['status']['addresses']:
                        if add['type'] == 'InternalIP':
                                internalIP = add['address']
                                continue
                        if add['type'] == 'ExternalIP':
                                externalIP = add['address']
                                continue
                        if internalIP != '' and externalIP != '':
                                break
                if internalIP == host and externalIP != '':
                        return externalIP
        return host
print gethost(data, host)")"

if [ "true" == "${MULTI_POD_PER_HOST}" ]
then
    sed -i "s/access-port.*3000/access-port    ${PORT}/" ${CFG}
    # No need for alternate-access-port replace, access-port will replace both
    # sed -i "s/alternate-access-port.*3000/alternate-access-port    ${PORT}/" ${CFG}
    sed -i "s/tls-access-port.*4333/tls-access-port    ${TLSPORT}/" ${CFG}
    sed -i "s/tls-alternate-access-port.*4333/tls-alternate-access-port    ${TLSPORT}/" ${CFG}
fi
sed -i "s/access-address.*<access_address>/access-address    ${EXTERNALIP}/" ${CFG}
sed -i "s/alternate-access-address.*<alternate_access_address>/alternate-access-address    ${EXTERNALIP}/" ${CFG}
sed -i "s/tls-access-address.*<tls-access-address>/tls-access-address    ${EXTERNALIP}/" ${CFG}
sed -i "s/tls-alternate-access-address.*<tls-alternate-access-address>/tls-alternate-access-address    ${EXTERNALIP}/" ${CFG}

cat ${CFG}
`

var confData = map[string]string{
	"on-start.sh": onStartSh,
}
