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

# Copies template and supporting files to Aerospike config folder from input folder.
# Refreshes Aerospike config map and tries to warm restart Aerospike.
# Usage: ./.#copy-templates.sh source destination

set -e
set -x

if [ -z "$1" ]
then
    echo "Error: Template source volume not specified"
	exit 1
fi

if [ -z "$2" ]
then
    echo "Error: Template destination volume not specified"
	exit 1
fi

source=$1
destination=$2
echo installing aerospike.conf into "${destination}"
\cp -L "${source}/aerospike.template.conf" "${destination}"/
\cp -L "${source}/peers" "${destination}"/
