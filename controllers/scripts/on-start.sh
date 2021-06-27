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

function join {
    local IFS="$1"; shift; echo "$*";
}

CFG=/etc/aerospike/aerospike.template.conf

HOSTNAME=$(hostname)
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

echo "Generated Aerospike Configuration "
echo "---------------------------------"
cat ${CFG}
echo "---------------------------------"
