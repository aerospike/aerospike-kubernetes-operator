#!/bin/bash
#set -x
#set -e

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

cp /configs/on-start.sh /usr/bin/on-start.sh
cp /configs/aerospike.template.conf "${CONFIG_VOLUME}"/
if [ -f /configs/features.conf ]; then
        cp /configs/features.conf "${CONFIG_VOLUME}"/
fi
chmod +x /usr/bin/on-start.sh
/usr/bin/peer-finder -on-start=/usr/bin/on-start.sh -service=$K8_SERVICE -ns=${NAMESPACE} -domain=cluster.local
