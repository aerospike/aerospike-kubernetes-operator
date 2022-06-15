#!/usr/bin/env python3
import os
import sys
import json
import argparse
import ipaddress
from pprint import pprint as pp

# Constants
FILE_SYSTEM_MOUNT_POINT = '/workdir/filesystem-volumes'
BLOCK_MOUNT_POINT = '/workdir/block-volumes'
BASE_WIPE_VERSION = 6
ADDRESS_TYPE_NAME = {
    'access': 'accessEndpoints',
    'alternate-access': 'alternateAccessEndpoints',
    'tls-access': 'tlsAccessEndpoints',
    'tls-alternate-access': 'tlsAlternateAccessEndpoints'
}


def process_volumes(pod_name, find, dd, blkdiskard, volumes, effective_method_key):

    result = []

    for volume in filter((lambda x: True if "persistentVolume" in x["source"] else False), volumes):
        volume_mode = volume["source"]["persistentVolume"]["volumeMode"]
        volume_name = volume["name"]
        effective_method = volume[effective_method_key]

        if volume_mode == "Block":
            volume_path = os.path.join(BLOCK_MOUNT_POINT, volume_name)
            if not os.path.exists(volume_path):
                raise FileNotFoundError(f"pod-name: {pod_name} volume: {volume_path} not found")
            if effective_method == "dd":
                print(f"start - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(dd.format(volume_path=volume_path))
                print(f"end - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
            elif effective_method == "blkdiscard":
                print(f"start - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(blkdiskard.format(volume_path=volume_path))
                print(f"end - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
        elif volume_mode == "Filesystem":
            volume_path = os.path.join(FILE_SYSTEM_MOUNT_POINT, volume_name)
            if effective_method == "deleteFiles":
                print(f"start - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
                execute(find.format(volume_path=volume_path))
                print(f"end - pod-name: {pod_name} {effective_method_key}: {effective_method} "
                      f"volume-name: {volume_name}")
        else:
            raise ValueError(f"pod-name: {pod_name} invalid volume-mode: {volume_mode}")

        print(f"pod-name: {pod_name} added volume: {volume_name}")
        result.append(volume_name)
    return result


def get_rack(config, pod_name):
    print('Checking for rack in rackConfig')
    # Assuming podName format stsName-rackID-index
    rack_id = int(pod_name.split("-")[-2])
    try:
        racks = config["spec"]["rackConfig"]["racks"]
        for rack in racks:
            if rack["id"] == rack_id:
                return rack
        raise ValueError(f"pod-name: {pod_name} rack-id: {rack_id} not found")
    except KeyError:
        print(f"pod-name: f{pod_name} unable to get rack-id {rack_id}")
        raise


def get_volumes(pod_name, config):
    rack = get_rack(pod_name=pod_name, config=config)
    try:
        print(f"pod-name: {pod_name} Looking for volumes in rack.storage.volumes")
        volumes = rack['storage']['volumes']
        if not volumes:
            print(f"pod-name: {pod_name} found an empty list in rack.storage.volumes")
            raise KeyError(f"pod-name: {pod_name} - volumes not found")
        return volumes
    except KeyError:
        print(f"pod-name: {pod_name} volumes not found in rack.storage.volumes")
        try:
            print(f"pod-name: {pod_name} Looking for volumes in spec.storage.volumes")
            volumes = config['spec']['storage']['volumes']
            return volumes
        except KeyError:
            print(f"pod-name: {pod_name} volumes not found in spec.storage.volumes")
            return []


def get_initialized_volumes(pod_name, config):
    try:
        print(f"pod-name: {pod_name} looking for initialized volumes in status.pod.{pod_name}.initializedVolumes")
        return set(config['status']['pods'][pod_name]['initializedVolumes'])
    except KeyError:
        print(f"pod-name: {pod_name} initialized volumes not found in status.pod.{pod_name}.initializedVolumes")
        try:
            print(f"pod-name: {pod_name} looking for initialized volumes in status.pod."
                  f"{pod_name}.initializedVolumePaths")
            return set(config['status']['pods'][pod_name]['initializedVolumePaths'])
        except KeyError:
            print(f"pod-name: {pod_name} looking for initialized volumes not found in status.pod."
                  f"{pod_name}.initializedVolumePaths")
            return set()


def get_image_version(image):
    return tuple(map(int, image.partition(":")[2].split(".")))


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
        raise OSError(f"command: {cmd} - execution failed")


def update_status(pod_name, metadata, volumes):
    with open("aerospikeConfHash", mode="r") as f:
        conf_hash = f.read()
    with open("networkPolicyHash", mode="r") as f:
        network_policy_hash = f.read()
    with open("podSpecHash", mode="r") as f:
        pod_spec_hash = f.read()

    metadata.update({
        'initializedVolumes': volumes,
        'aerospikeConfigHash': conf_hash,
        'networkPolicyHash': network_policy_hash,
        'podSpecHash': pod_spec_hash,
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


def main():
    try:
        parser = argparse.ArgumentParser()
        parser.add_argument("--pod-name", type=str, required=True, dest="pod_name")
        parser.add_argument("--config", type=str, required=True, dest="config")
        parser.parse_args()
        args = parser.parse_args()
        config = json.loads(args.config)
        metadata = get_node_metadata()
        dd = 'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || grep -q "No space left on device" ' \
             '/tmp/init-stderr'
        find = "find {volume_path} -type f -delete"
        blkdiskard_init = "blkdiscard {volume_path}"
        blkdiskard_wipe = "blkdiscard -z {volume_path}"
        next_major_ver = get_image_version(image=config["spec"]["image"])[0]
        try:
            image = config["status"]["image"]
        except KeyError:
            image = ""
        print("Checking if volumes should be wiped")
        if image:
            prev_major_ver = get_image_version(image=image)[0]
            print(f"next-major-version: {next_major_ver} prev-major-version: {prev_major_ver}")
            if (next_major_ver >= BASE_WIPE_VERSION > prev_major_ver) or \
                (next_major_ver < BASE_WIPE_VERSION <= prev_major_ver):
                print("volumes should be wiped")
                volumes = process_volumes(pod_name=args.pod_name,
                                          dd=dd, find=find,
                                          blkdiskard=blkdiskard_wipe,
                                          effective_method_key="effectiveWipeMethod",
                                          volumes=get_volumes(pod_name=args.pod_name, config=config))
                update_status(pod_name=args.pod_name, metadata=metadata, volumes=volumes)
                return
        print("volumes should not be wiped")
        init_volumes = get_initialized_volumes(pod_name=args.pod_name, config=config)
        volumes = process_volumes(
            pod_name=args.pod_name,
            dd=dd,
            blkdiskard=blkdiskard_init,
            find=find,
            effective_method_key="effectiveInitMethod",
            volumes=filter(lambda x: True if x["name"] not in init_volumes else False, get_volumes(
                pod_name=args.pod_name, config=config)))
        volumes.extend(init_volumes)
        update_status(pod_name=args.pod_name, metadata=metadata, volumes=volumes)
    except Exception as e:
        print(e)
        sys.exit(1)
    else:
        sys.exit(0)


if __name__ == '__main__':
    main()
