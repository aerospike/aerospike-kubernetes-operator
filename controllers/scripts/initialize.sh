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

# This script initializes storage devices on first pod run.

script_dir="$(dirname $(realpath $0))"
cd $script_dir

CONFIG_VOLUME="/etc/aerospike"
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

# Copy required files to config volume for initialization.
mkdir -p "${CONFIG_VOLUME}"

bash ./copy-templates.sh /configs "${CONFIG_VOLUME}"

# Copy scripts and binaries needed for warm restart.
cp /usr/bin/kubernetes-configmap-exporter "${CONFIG_VOLUME}"/
cp ./refresh-cmap-restart-asd.sh "${CONFIG_VOLUME}"/

if [ -f /configs/features.conf ]; then
    cp /configs/features.conf "${CONFIG_VOLUME}"/
fi

# Create Aerospike configuration
bash ./create-aerospike-conf.sh

# Update pod status in the k8s aerospike cluster object
bash ./update-pod-status.sh
