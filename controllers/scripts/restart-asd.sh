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
set -o pipefail

script_dir="$(dirname $(realpath $0))"
cd $script_dir

# Check if we are running under tini to be able to warm restart.
if ! cat /proc/1/cmdline | tr '\000' ' ' | grep tini | grep -- "-r" > /dev/null; then
	echo "warm restart not supported - aborting"
	exit 1
fi

# Create new Aerospike configuration
bash ./copy-templates.sh . /etc/aerospike
bash ./create-aerospike-conf.sh

# Get current asd pid
asd_pid=$(ps -A -o pid,cmd|grep "asd" | grep -v grep | grep -v tini |head -n 1 | awk '{print $1}')
retVal=$?
if [ $retVal -ne 0 ]; then
    echo "error getting Aerospike server PID"
	exit 1
fi

# Restart ASD by signalling the init process
kill -SIGUSR1 1

# Wait for asd process to restart.
for i in $(seq 1 60); do
	! ps -p $asd_pid >/dev/null && break
    echo "waiting for Aerospike server to terminate ..."
    sleep 5
done

if ps -p $asd_pid >/dev/null; then
	# ASD did not terminate within stipulated time.
	echo "aborting warm start - Aerospike server did not terminate"
	exit 1
fi

# Update pod status in the k8s aerospike cluster object
bash ./update-pod-status.sh "quickRestart"
