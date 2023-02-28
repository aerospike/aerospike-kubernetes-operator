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

# Refreshes Aerospike config map and tries to warm restart Aerospike.
# Usage: ./refresh-cmap-restart-asd.sh namespace configmap-name

set -e
set -x

script_dir="$(dirname $(realpath $0))"
cd $script_dir


if [ -z "$1" ]
then
    echo "Error: Kubernetes namespace required as the first argument"
	exit 1
fi

if [ -z "$2" ]
then
    echo "Error: Aerospike configmap required as the second argument"
	exit 1
fi

# Include local files.
export PATH=$PATH:$script_dir

mkdir -p configmap

# Run the Aerospike warm restart script from the fetched configmap
if [ -f "./akoinit" ]; then
    chmod +x ./akoinit
    ./akoinit export-configmap \
    --namespace "$1" \
    --cmName "$2" \
    --toDir configmap
else
    chmod +x ./kubernetes-configmap-exporter
    ./kubernetes-configmap-exporter "$1" "$2" configmap
fi


bash configmap/restart-asd.sh
