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

# Kubernetes API details.
CA_CERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
TOKEN=$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)
KUBE_API_SERVER=https://kubernetes.default.svc
NAMESPACE=$MY_POD_NAMESPACE

# Get IPs
export PODIP="$MY_POD_IP"

# Sets up port related variables.
export POD_PORT="{{.PodPort}}"
export POD_TLSPORT="{{.PodTLSPort}}"

# Get tls, info port
if [ $is_python3 -ne 0 ]
then
  eval $(/etc/aerospike/akoinit set-ip-env \
  --pod-name $MY_POD_NAME \
  --namespace $MY_POD_NAMESPACE \
  --host-ip ${MY_HOST_IP})
else
  SVC="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "$KUBE_API_SERVER/api/v1/namespaces/$NAMESPACE/services")"
  PORTSTRING="$(echo $SVC | python3 -c "import sys, json
data = json.load(sys.stdin);
podname = '${MY_POD_NAME}';
def getport(data, podname):
    for item in data['items']:
        if item['metadata']['name'] == podname:
            infoport = '0'
            tlsport = '0'
            for port in item['spec']['ports']:
                if port['name'] == 'service':
                    infoport = port['nodePort']
                if port['name'] == 'tls-service':
                    tlsport = port['nodePort']
            return infoport, tlsport
print(getport(data, podname))")"

  export infoport="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $2}')"
  export tlsport="$(echo $PORTSTRING | awk -F'[, |(|)]' '{print $4}')"

  # Get External IP
  DATA="$(curl --cacert $CA_CERT -H "Authorization: Bearer $TOKEN" "$KUBE_API_SERVER/api/v1/nodes")"

  # Note: the IPs returned from here should match the IPs used in the node summary.
  HOSTIPS="$(echo $DATA | python3 -c "import sys, json
data = json.load(sys.stdin);
host = '${MY_HOST_IP}';
def gethost(data, host):
    internalIP = host
    externalIP = host

    # Iterate over all nodes and find this pod's node IPs.
    for item in data['items']:
        nodeInternalIP = ''
        nodeExternalIP = ''
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

print(gethost(data, host))")"

  export INTERNALIP=$(echo $HOSTIPS | awk '{print $1}')
  export EXTERNALIP=$(echo $HOSTIPS | awk '{print $2}')
fi

# Compute the mapped access ports based on config.
{{- if .MultiPodPerHost}}
# Use mapped service ports.
export MAPPED_PORT="${infoport}"
export MAPPED_TLSPORT="${tlsport}"
{{- else}}
# Use the actual ports.
export MAPPED_PORT="$POD_PORT"
export MAPPED_TLSPORT="$POD_TLSPORT"
{{- end}}

# Parse out cluster name, formatted as: stsname-rackid-index
IFS='-' read -ra ADDR <<< "${MY_POD_NAME}"

POD_ORDINAL="${ADDR[-1]}"

# Find rack-id
export RACK_ID="${ADDR[-2]}"
export NODE_ID="${RACK_ID}a${POD_ORDINAL}"

GENERATED_ENV="/tmp/generate-env.sh"
if [ -f $GENERATED_ENV ]; then
	source $GENERATED_ENV
fi
