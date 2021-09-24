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

CFG=/etc/aerospike/aerospike.template.conf
PEERS=/etc/aerospike/peers

# ------------------------------------------------------------------------------
# Update node and rack ids configuration file
# ------------------------------------------------------------------------------
sed -i "s/ENV_NODE_ID/${NODE_ID}/" ${CFG}
sed -i "s/rack-id.*0/rack-id    ${RACK_ID}/" ${CFG}

echo "" > $GENERATED_ENV

# ------------------------------------------------------------------------------
# Update access addresses in the configuration file
# ------------------------------------------------------------------------------
# Compute the access endpoints based on network policy.
# As a kludge the computed values are stored late to update node summary.
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

    # Pass on computed address to python script to update the status.
    varName=$(echo $addressType | sed -e 's/-/_/g')
	echo export global_${varName}_address="$accessAddress" >> $GENERATED_ENV
	echo export global_${varName}_port="$accessPort" >> $GENERATED_ENV

    # Substitute in the configuration file.
    sed -i "s/^\(\s*\)${addressType}-address\s*<${addressType}-address>/\1${addressType}-address    ${accessAddress}/" ${CFG}
    # This port is set in api/v1beta1/aerospikecluster_mutating_webhook.go and is used as placeholder.
    sed -i "s/^\(\s*\)${addressType}-port\s*${podPort}/\1${addressType}-port    ${accessPort}/" ${CFG}
}

substituteEndpoint "access" {{.NetworkPolicy.AccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_PORT $MAPPED_PORT
substituteEndpoint "alternate-access" {{.NetworkPolicy.AlternateAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_PORT $MAPPED_PORT

if [ "true" == "$MY_POD_TLS_ENABLED" ]; then
  substituteEndpoint "tls-access" {{.NetworkPolicy.TLSAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_TLSPORT $MAPPED_TLSPORT
  substituteEndpoint "tls-alternate-access" {{.NetworkPolicy.TLSAlternateAccessType}} $PODIP $INTERNALIP $EXTERNALIP $POD_TLSPORT $MAPPED_TLSPORT
fi

# ------------------------------------------------------------------------------
# Update mesh seeds in the configuration file
# ------------------------------------------------------------------------------
cat $PEERS | while read PEER || [ -n "$PEER" ]; do
    if [[ "$PEER" == "$MY_POD_NAME."* ]] ;
	then
		# Skip adding self to mesh addresses
		continue
	fi

	# 8 spaces, fixed in config writer file config manager lib
	# TODO: The search pattern is not robust. Add a better marker in management lib.
	{{- if ne .HeartBeatPort  0}}
	sed -i -e "/heartbeat {/a \\        mesh-seed-address-port ${PEER} {{.HeartBeatPort}}" ${CFG}
	{{- end}}

	{{- if ne .HeartBeatTlsPort 0}}
  sed -i -e "/heartbeat {/a \\        tls-mesh-seed-address-port ${PEER} {{.HeartBeatTlsPort}}" ${CFG}
  {{- end}}
done


# ------------------------------------------------------------------------------
# If host networking is used force heartbeat and fabric to advertise network
# interface bound to K8s node's host network.
# ------------------------------------------------------------------------------
{{- if .HostNetwork}}
# 8 spaces, fixed in config writer file config manager lib
# TODO: The search pattern is not robust. Add a better marker in management lib.
sed -i -e "/heartbeat {/a \\        address ${MY_POD_IP}" ${CFG}
sed -i -e "/heartbeat {/a \\        tls-address ${MY_POD_IP}" ${CFG}
sed -i -e "/fabric {/a \\        address ${MY_POD_IP}" ${CFG}
sed -i -e "/fabric {/a \\        tls-address ${MY_POD_IP}" ${CFG}
{{- end}}

echo "---------------------------------"
cat ${CFG}
echo "---------------------------------"
