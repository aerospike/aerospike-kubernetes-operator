package configmap

import (
	"bytes"
	"text/template"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
)

const initializeShTemplateStr = `
#! /bin/bash
# ------------------------------------------------------------------------------
# Copyright 2012-2020 Aerospike, Inc.
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


{{- if .WorkDir }}
# Create required directories.
DEFAULT_WORK_DIR="/filesystem-volumes{{.WorkDir}}"
REQUIRED_DIRS=("smd"  "usr/udf/lua" "xdr")

for d in ${REQUIRED_DIRS[*]}; do
    TO_CREATE="$DEFAULT_WORK_DIR/$d"
    echo creating directory "${TO_CREATE}"
    mkdir -p "$TO_CREATE"
done
{{- end }}

# Kubernetes API details.
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
KUBE_API_SERVER=https://kubernetes.default.svc

# Parse out cluster name, formatted as: petset_name-rackid-index
IFS='-' read -ra ADDR <<< "$(hostname)"
AERO_CLUSTER_NAME="${ADDR[0]}"

# Read this pod's Aerospike pod status from the cluster status.
NAMESPACE=$MY_POD_NAMESPACE
AERO_CLUSTER_JSON="$(curl -f --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "$KUBE_API_SERVER/apis/aerospike.com/v1alpha1/namespaces/$NAMESPACE/aerospikeclusters/$AERO_CLUSTER_NAME")"

if [ $? -ne 0 ]
then
   echo "ERROR: failed to read status for $AERO_CLUSTER_NAME"
   exit 1
fi

IS_NEW="$(echo $AERO_CLUSTER_JSON | python -c "import sys, json
data = json.load(sys.stdin)

if 'status' in data:
   status = data['status']
else:
   status = {}

podname = '${MY_POD_NAME}';
def isNew(status, podname):
    if  not 'podStatus' in status:
      return True
    return podname not in status['podStatus']
print isNew(status, podname)")"

if [ "$IS_NEW" == "True" ]
then
    echo "Pod first run - initializing"

else
    echo "Pod restarted"
fi

cat << EOF > initVolumes.py
import sys
import json
import os

# Constants
fileSystemMountPoint = '/filesystem-volumes'
blockMountPoint = '/block-volumes'


def executeCommand(command):
    print 'Executing command\n\t' + command
    exit = os.system(command)
    if exit != 0:
        raise Exception('Error executing command')

def getRack(data, podname):
    print ('Checking for rack in rackConfig')
    # Assuming podname format petset_name-rackid-index
    rackID = podname.split("-")[-2]
    if 'rackConfig' in data and 'racks' in data['rackConfig']:
        racks = data['rackConfig']['racks']
        for rack in racks:
            if rack['id'] == int(rackID):
                return rack

podname = sys.argv[1]
data = json.load(sys.stdin)

if 'status' in data:
    status = data['status']
else:
    status = {}

if 'spec' in data:
    spec = data['spec']
else:
    spec = {}

rack = getRack(spec, podname)
if rack is None:
    raise Exception('Rack not found for pod ' + podname + ' spec ' + spec)

if 'storage' in rack and 'volumes' in rack['storage'] and len(rack['storage']['volumes']) > 0:
    volumes = rack['storage']['volumes']
else:
    if 'storage' in spec and 'volumes' in spec['storage']:
        volumes = spec['storage']['volumes']
    else:
        volumes = []

if 'podStatus' in status and podname in status['podStatus'] and 'initializedVolumePaths' in status['podStatus'][podname]:
    alreadyInitialized = status['podStatus'][podname]['initializedVolumePaths']
else:
    alreadyInitialized = []

# Initialize unintialized volumes.
initialized = []
for volume in volumes:
    # volume path is always absolute.
    if volume['volumeMode'] == 'block':
        localVolumePath = blockMountPoint + volume['path']
    elif volume['volumeMode'] == 'filesystem':
        localVolumePath = fileSystemMountPoint + volume['path']

    if not os.path.exists(localVolumePath):
        raise Exception(
            'Volume ' + volume['path'] + ' not attached to path ' + localVolumePath)

    if volume['path'] not in alreadyInitialized:
        if volume['volumeMode'] == 'block':
            localVolumePath = blockMountPoint + volume['path']
            if volume['effectiveInitMethod'] == 'dd':
                executeCommand('dd if=/dev/zero of=' +
                               localVolumePath + ' bs=1M')
            elif volume['effectiveInitMethod'] == 'blkdiscard':
                executeCommand('blkdiscard ' + localVolumePath + ' bs=1M')
        elif volume['volumeMode'] == 'filesystem':
            # volume path is always absolute.
            localVolumePath = fileSystemMountPoint + volume['path']
            if volume['effectiveInitMethod'] == 'deleteFiles':
                executeCommand(
                    'find ' + localVolumePath + ' -type f -delete')
        print 'device ' + volume['path'] + ' initialized'

    else:
        print 'device ' + volume['path'] + ' already initialized'

    initialized.append(volume['path'])


# Create the patch payload for updating pod status.
pathPayload = [{'op': 'replace', 'path': '/status/podStatus/' +
                podname, 'value': {'initializedVolumePaths': initialized}}]


with open('/tmp/patch.json', 'w') as outfile:
    json.dump(pathPayload, outfile)
EOF

echo $AERO_CLUSTER_JSON | python initVolumes.py $MY_POD_NAME

if [ $? -ne 0 ]
then
   echo "ERROR: failed to initialize volumes"
   exit 1
fi

# Patch the pod status.
cat /tmp/patch.json | curl -f -X PATCH -d @- --cacert $CA_CERT -H "Authorization: Bearer $TOKEN"\
     -H 'Accept: application/json' \
     -H 'Content-Type: application/json-patch+json' \
     "$KUBE_API_SERVER/apis/aerospike.com/v1alpha1/namespaces/$NAMESPACE/aerospikeclusters/$AERO_CLUSTER_NAME/status?fieldManager=pod"
`

const onStartShTemplateStr = `
#! /bin/bash
# ------------------------------------------------------------------------------
# Copyright 2012-2020 Aerospike, Inc.
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

# Parse out cluster name, formatted as: petset_name-rackid-index
IFS='-' read -ra ADDR <<< "$(hostname)"
CLUSTER_NAME="${ADDR[0]}"

NODE_ID="a${ADDR[-1]}"

# Find rack-id, if given
len=${#ADDR[@]}
if [ ${#ADDR[@]} == 3 ]; then
    RACK_ID="${ADDR[1]}"
    sed -i "s/rack-id.*0/rack-id    ${RACK_ID}/" ${CFG}
    NODE_ID="$RACK_ID$NODE_ID"
fi

# TODO: get the ordinal, this will be used as nodeid.
# This looks hacky way but no other way found yet
sed -i "s/ENV_NODE_ID/${NODE_ID}/" ${CFG}

# Parse lines to insert peer-list
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

# Get IPs
PODIP="$MY_POD_IP"
INTERNALIP="$MY_HOST_IP"

# Get External IP
DATA="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "https://kubernetes.default.svc/api/v1/nodes")"

# Note: the IPs returned from here should match the IPs used in the node summary.
HOSTIPS="$(echo $DATA | python -c "import sys, json
data = json.load(sys.stdin);
host = '${MY_HOST_IP}';
def gethost(data, host):
    internalIP = host
    externalIP = host

    # Iterate over all nodes and find this pod's node IPs.
    for item in data['items']:
        nodeInternalIP = ''
        nodeExternalip = ''
        matchFound = False
        for add in item['status']['addresses']:
            if add['address'] == host:
               matchFound = True
            if add['type'] == 'InternalIP':
                nodeInternalIP = add['address']
                continue
            if add['type'] == 'ExternalIP':
                nodeExternalIP = add['address']
                continue

        if matchFound:
           # Matching node for this pod found.
           if nodeInternalIP != '':
               internalIP = nodeInternalIP

           if nodeExternalIP != '':
               externalIP = nodeExternalIP
           break

    return internalIP + ' ' + externalIP

print gethost(data, host)")"

INTERNALIP=$(echo $HOSTIPS | awk '{print $1}')
EXTERNALIP=$(echo $HOSTIPS | awk '{print $2}')

POD_PORT="{{.PodPort}}"
POD_TLSPORT="{{.PodTLSPort}}"

# Compute the mapped access ports based on config.
{{- if .MultiPodPerHost}}
# Use mapped service ports.
MAPPED_PORT="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $2}')"
MAPPED_TLSPORT="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $4}')"
{{- else}}
# Use the actual ports.
MAPPED_PORT="$POD_PORT"
MAPPED_TLSPORT="$POD_TLSPORT"
{{- end}}

# Compute the access endpoints based on network policy.
substituteEndpoint() {
    local addressType=$1
    local networkType=$2
    local podIP=$3
    local internalIP=$4
    local externalIP=$5
    local podPort=$6
    local mappedPort=$7

    case $networkType in
      pod)
        accessAddress=$podIP
        accessPort=$podPort
        ;;

      hostInternal)
        accessAddress=$internalIP
        accessPort=$mappedPort
        ;;

      hostExternal)
        accessAddress=$externalIP
        accessPort=$mappedPort
        ;;

      *)
        accessAddress=$podIP
        accessPort=$podPort
        ;;
    esac

    # Substitute in the configuration file.
    sed -i "s/^\(\s*\)${addressType}-address.*<${addressType}-address>/\1${addressType}-address    ${accessAddress}/" ${CFG}
    sed -i "s/^\(\s*\)${addressType}-port.*${podPort}/\1${addressType}-port    ${accessPort}/" ${CFG}
}

substituteEndpoint "access" {{.NetworkPolicy.AccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_PORT $MAPPED_PORT
substituteEndpoint "alternate-access" {{.NetworkPolicy.AlternateAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_PORT $MAPPED_PORT
substituteEndpoint "tls-access" {{.NetworkPolicy.TLSAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_TLSPORT $MAPPED_TLSPORT
substituteEndpoint "tls-alternate-access" {{.NetworkPolicy.TLSAlternateAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_TLSPORT $MAPPED_TLSPORT

echo "Generated Aerospike Configuration "
echo "---------------------------------"
cat ${CFG}
echo "---------------------------------"
`

type initializeTemplateInput struct {
	WorkDir string
}

type onStartTemplateInput struct {
	MultiPodPerHost bool
	NetworkPolicy   aerospikev1alpha1.AerospikeNetworkPolicy
	PodPort         int32
	PodTLSPort      int32
}

var initializeShTemplate, _ = template.New("initializeSh").Parse(initializeShTemplateStr)
var onStartShTemplate, _ = template.New("onStartSh").Parse(onStartShTemplateStr)

// getBaseConfData returns the basic data to be used in the config map for input aeroCluster spec.
func getBaseConfData(aeroCluster *aerospikev1alpha1.AerospikeCluster, rack aerospikev1alpha1.Rack) (map[string]string, error) {
	config := rack.AerospikeConfig
	workDir := utils.GetWorkDirectory(config)

	initializeTemplateInput := initializeTemplateInput{WorkDir: workDir}
	var initializeSh bytes.Buffer
	err := initializeShTemplate.Execute(&initializeSh, initializeTemplateInput)
	if err != nil {
		return nil, err
	}

	onStartTemplateInput := onStartTemplateInput{MultiPodPerHost: aeroCluster.Spec.MultiPodPerHost, NetworkPolicy: aeroCluster.Spec.AerospikeNetworkPolicy, PodPort: utils.ServicePort, PodTLSPort: utils.ServiceTLSPort}
	var onStartSh bytes.Buffer
	err = onStartShTemplate.Execute(&onStartSh, onStartTemplateInput)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"initialize.sh": initializeSh.String(),
		"on-start.sh":   onStartSh.String(),
	}, nil
}
