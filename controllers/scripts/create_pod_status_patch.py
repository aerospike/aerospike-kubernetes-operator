#!/usr/bin/env python3
import os
import re
import sys
import json
import argparse
import ipaddress
import urllib.error
import urllib.request
from pprint import pprint as pp

# Constants
FILE_SYSTEM_MOUNT_POINT = "/workdir/filesystem-volumes"
BLOCK_MOUNT_POINT = "/workdir/block-volumes"
BASE_WIPE_VERSION = 6
ADDRESS_TYPE_NAME = {
    "access": "accessEndpoints",
    "alternate-access": "alternateAccessEndpoints",
    "tls-access": "tlsAccessEndpoints",
    "tls-alternate-access": "tlsAlternateAccessEndpoints"
}


def format_volumes(pod_name, find, dd, blkdiskard, volumes, effective_method_key):

    result = []

    for volume in filter((lambda x: True if "persistentVolume" in x["source"] else False), volumes):
        volume_mode = volume["source"]["persistentVolume"]["volumeMode"]
        volume_name = volume["name"]
        effective_method = volume[effective_method_key]

        if volume_mode == "Block":
            volume_path = os.path.join(BLOCK_MOUNT_POINT, volume_name)
            if not os.path.exists(volume_path):
                raise FileNotFoundError(f"pod-name: {pod_name} volume: {volume_path} Not Found")

            if effective_method == "dd":
                print(f"pod-name: {pod_name} - Executing: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(dd.format(volume_path=volume_path))
                print(f"pod-name: {pod_name} - Execution Succeeded:  {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            elif effective_method == "blkdiscard":
                print(f"pod-name: {pod_name} - Executing: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(blkdiskard.format(volume_path=volume_path))
                print(f"pod-name: {pod_name} - Execution Succeeded: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            elif effective_method == "none" and effective_method_key == "effectiveInitMethod":
                print(f"pod-name: {pod_name} - Passthrough: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            else:
                raise ValueError(f"Invalid effective method: {effective_method}")
        elif volume_mode == "Filesystem":
            volume_path = os.path.join(FILE_SYSTEM_MOUNT_POINT, volume_name)
            if not os.path.exists(volume_path):
                raise FileNotFoundError(f"pod-name: {pod_name} volume: {volume_path} Not Found")

            if effective_method == "deleteFiles":
                print(f"pod-name: {pod_name} - Executing: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(find.format(volume_path=volume_path))
                print(f"pod-name: {pod_name} - Execution Succeeded: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            elif effective_method == "none" and effective_method_key == "effectiveInitMethod":
                print(f"pod-name: {pod_name} - Passthrough: {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            else:
                raise ValueError(f"Invalid effective method: {effective_method}")
        else:
            raise ValueError(f"pod-name: {pod_name} Invalid volume-mode: {volume_mode}")

        print(f"pod-name: {pod_name} - Added volume: {volume_name}")
        result.append(volume_name)
    return result


def get_rack(pod_name, config):
    print(f"pod-name: {pod_name} - Checking for rack in rackConfig")
    # Assuming podName format stsName-rackID-index
    rack_id = int(pod_name.split("-")[-2])
    try:
        racks = config["spec"]["rackConfig"]["racks"]
        for rack in racks:
            if rack["id"] == rack_id:
                return rack
        raise ValueError(f"pod-name: {pod_name} rack-id: {rack_id} not found")
    except KeyError:
        print(f"pod-name: f{pod_name} - Unable to get rack-id {rack_id}")
        raise


def get_aerospike_volume_paths(pod_name, config):
    rack = get_rack(pod_name=pod_name, config=config)
    try:
        namespaces = rack["effectiveAerospikeConfig"]["namespaces"]
    except KeyError as e:
        print(f"pod-name: {pod_name} Unable to find namespaces")
        raise e
    for namespace in namespaces:
        storage_engine = namespace["storage-engine"]
        if storage_engine["type"] == "device":
            if "devices" in storage_engine:
                devices = storage_engine["devices"]
                for device in devices:
                    print(f"pod-name: {pod_name} Get namespace device: {device}")
                    yield device
            if "files" in storage_engine:
                files = storage_engine["files"]
                for f in files:
                    dirname = os.path.dirname(f)
                    print(f"pod-name: {pod_name} Get file path: {dirname}")
                    yield dirname


def get_volumes_for_wiping(pod_name, config):
    aerospike_volume_paths = set(get_aerospike_volume_paths(pod_name=pod_name, config=config))
    for volume in filter(lambda x: True if "aerospike" in x else False, get_volumes(pod_name=pod_name, config=config)):
        if volume["aerospike"]["path"] in aerospike_volume_paths:
            yield volume


def get_volumes(pod_name, config):
    rack = get_rack(pod_name=pod_name, config=config)
    try:
        print(f"pod-name: {pod_name} - Looking for volumes in rack.storage.volumes")
        volumes = rack["storage"]["volumes"]
        if not volumes:
            print(f"pod-name: {pod_name} - Found an empty list in rack.storage.volumes")
            raise KeyError(f"pod-name: {pod_name} - volumes not found")
        return volumes
    except KeyError:
        print(f"pod-name: {pod_name} - Volumes not found in rack.storage.volumes")
        try:
            print(f"pod-name: {pod_name} - Looking for volumes in spec.storage.volumes")
            volumes = config["spec"]["storage"]["volumes"]
            if not volumes:
                print(f"pod-name: {pod_name} - Found an empty list in spec.storage.volumes")
                raise KeyError(f"pod-name: {pod_name} - volumes not found")
            return volumes
        except KeyError:
            print(f"pod-name: {pod_name} - Volumes not found in spec.storage.volumes")
            return []


def get_initialized_volumes(pod_name, config):
    try:
        print(f"pod-name: {pod_name} - Looking for initialized volumes in status.pod.{pod_name}.initializedVolumes")
        return set(config["status"]["pods"][pod_name]["initializedVolumes"])
    except KeyError:
        print(f"pod-name: {pod_name} - Initialized volumes not found in status.pod.{pod_name}.initializedVolumes")
        try:
            print(f"pod-name: {pod_name} - Looking for initialized volumes in status.pod."
                  f"{pod_name}.initializedVolumePaths")
            return set(config["status"]["pods"][pod_name]["initializedVolumePaths"])
        except KeyError:
            print(f"pod-name: {pod_name} - Looking for initialized volumes not found in status.pod."
                  f"{pod_name}.initializedVolumePaths")
            return set()


def get_image_tag(image):
    return re.search(r"\d+\.\d+\.\d+\.\d+$", image).group(0)


def get_image_version(image):
    return tuple(map(int, get_image_tag(image).split(".")))


def get_node_metadata():
    pod_port = os.environ["POD_PORT"]
    service_port = os.environ["MAPPED_PORT"]
    if strtobool(os.environ.get("MY_POD_TLS_ENABLED", default="")):
        pod_port = os.environ["POD_TLSPORT"]
        service_port = os.environ["MAPPED_TLSPORT"]
    return {
        "image": os.environ.get("POD_IMAGE", default=""),
        "podIP": os.environ.get("PODIP", default=""),
        "hostInternalIP": os.environ.get("INTERNALIP", default=""),
        "hostExternalIP": os.environ.get("EXTERNALIP", default=""),
        "podPort": int(pod_port),
        "servicePort": int(service_port),
        "aerospike": {
            "clusterName": os.environ.get("MY_POD_CLUSTER_NAME", default=""),
            "nodeID": os.environ.get("NODE_ID", default=""),
            "tlsName": os.environ.get("MY_POD_TLS_NAME", default="")
        }
    }


def get_endpoints(address_type):
    try:
        addr_type = address_type.replace("-", "_")
        host = ipaddress.ip_address(os.environ[f"global_{addr_type}_address"])
        port = os.environ[f"global_{addr_type}_port"]
        if type(host) == ipaddress.IPv4Address:
            return [f"{host}:{port}"]
        elif type(host) == ipaddress.IPv6Address:
            return [f"[{host}]:{port}"]
        else:
            raise ValueError("Invalid ipaddress")
    except (ValueError, KeyError):
        return []


def strtobool(param):
    if len(param) == 0:
        return False
    param = param.lower()
    if param == "false":
        return False
    elif param == "true":
        return True
    else:
        raise ValueError("Invalid value")


def execute(cmd):
    return_value = os.system(cmd)
    if return_value != 0:
        raise OSError(f"Execution Failed - command: {cmd}")


def update_status(pod_name, metadata, volumes):
    with open("aerospikeConfHash", mode="r") as f:
        conf_hash = f.read()
    with open("networkPolicyHash", mode="r") as f:
        network_policy_hash = f.read()
    with open("podSpecHash", mode="r") as f:
        pod_spec_hash = f.read()

    metadata.update({
        "initializedVolumes": volumes,
        "aerospikeConfigHash": conf_hash,
        "networkPolicyHash": network_policy_hash,
        "podSpecHash": pod_spec_hash,
    })
    for pod_addr_name, conf_addr_name in ADDRESS_TYPE_NAME.items():
        metadata["aerospike"][conf_addr_name] = get_endpoints(address_type=pod_addr_name)

    payload = [{"op": "replace", "path": f"/status/pods/{pod_name}", "value": metadata}]
    print(40 * "#" + " payload " + 40 * "#")
    pp(payload)
    print(89 * "#")
    with open("/tmp/patch.json", mode="w") as f:
        json.dump(payload, f)
        f.flush()


def get_cluster_json(cluster_name, namespace, api_server, token, ca_cert):
    url = f"{api_server}/apis/asdb.aerospike.com/v1beta1/namespaces/{namespace}/aerospikeclusters/{cluster_name}"
    request = urllib.request.Request(url=url, method="GET")
    request.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(request, cafile=ca_cert) as response:
        body = response.read()
    return json.loads(body)


def main():
    try:
        parser = argparse.ArgumentParser()
        parser.add_argument("--pod-name", type=str, required=True, dest="pod_name")
        parser.add_argument("--ca-cer", type=str, required=True, dest="ca_cert")
        parser.add_argument("--token", type=str, required=True, dest="token")
        parser.add_argument("--api-server", type=str, required=True, dest="api_server")
        parser.add_argument("--namespace", type=str, required=True, dest="namespace")
        parser.add_argument("--cluster-name", type=str, required=True, dest="cluster_name")
        args = parser.parse_args()
        try:
            config = get_cluster_json(args.cluster_name, args.namespace, args.api_server, args.token, args.ca_cert)
        except urllib.error.URLError as e:
            print(f"pod-name: {args.pod_name} - Failed to read status for {args.cluster_name} error: {e}")
            raise e
        print(f"pod-name: {args.pod_name} - Config successfully loaded")

        try:
            image = config["status"]["image"]
            print(f"pod-name: {args.pod_name} Restarted")
        except KeyError:
            print(f"pod-name: {args.pod_name} - First run - initializing")
            image = ""

        metadata = get_node_metadata()
        dd = 'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || grep -q "No space left on device" ' \
             '/tmp/init-stderr'
        find = "find {volume_path} -type f -delete"
        blkdiskard_init = "blkdiscard {volume_path}"
        blkdiskard_wipe = "blkdiscard -z {volume_path}"
        next_major_ver = get_image_version(image=config["spec"]["image"])[0]

        print(f"pod-name: {args.pod_name} - Checking if volumes should be wiped")
        if image:
            prev_major_ver = get_image_version(image=image)[0]
            print(f"pod-name: {args.pod_name} - Checking if volumes should be wiped: "
                  f"next-major-version: {next_major_ver} prev-major-version: {prev_major_ver}")
            if (next_major_ver >= BASE_WIPE_VERSION > prev_major_ver) or \
                (next_major_ver < BASE_WIPE_VERSION <= prev_major_ver):
                print(f"pod-name: {args.pod_name} - Volumes should be wiped")
                volumes = format_volumes(
                    pod_name=args.pod_name,
                    dd=dd,
                    find=find,
                    blkdiskard=blkdiskard_wipe,
                    effective_method_key="effectiveWipeMethod",
                    volumes=get_volumes_for_wiping(
                        pod_name=args.pod_name,
                        config=config))

        print(f"pod-name: {args.pod_name} - Volumes should not be wiped")
        all_volumes = get_volumes(pod_name=args.pod_name, config=config)
        initialized_volumes = get_initialized_volumes(pod_name=args.pod_name, config=config)
        print(f"pod-name: {args.pod_name} initialized-volumes: {initialized_volumes}")
        volumes = format_volumes(
                pod_name=args.pod_name,
                dd=dd,
                blkdiskard=blkdiskard_init,
                find=find,
                effective_method_key="effectiveInitMethod",
                volumes=filter(lambda x: True if x["name"] not in initialized_volumes else False, all_volumes))
        volumes.extend(initialized_volumes)
        update_status(pod_name=args.pod_name, metadata=metadata, volumes=volumes)
    except Exception as e:
        print(e)
        sys.exit(1)
    else:
        sys.exit(0)


if __name__ == "__main__":
    main()
