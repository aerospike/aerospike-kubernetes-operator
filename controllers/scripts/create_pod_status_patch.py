#!/usr/bin/env python3
import os
import sys
import json
import argparse
import ipaddress
import subprocess
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


def wipe_volumes(pod_name, config):

    volumes = []

    for volume in get_volumes(pod_name=pod_name, config=config):
        volume_name = volume["name"]
        effective_wipe_method = volume['effectiveWipeMethod']
        volume_mode = volume["source"]["persistentVolume"]["volumeMode"]
        if volume_mode == "Block":
            volume_path = os.path.join(BLOCK_MOUNT_POINT, volume_name)
        elif volume_mode == "Filesystem":
            volume_path = os.path.join(FILE_SYSTEM_MOUNT_POINT, volume_name)
        else:
            raise ValueError(f"pod-name: {pod_name} invalid volume-mode: {volume_mode}")

        if not os.path.exists(volume_path):
            raise FileNotFoundError(f"pod-name: {pod_name} volume: {volume_path} not found")
        if effective_wipe_method == "dd":
            print(f"TEST: start - pod-name: {pod_name} volume-wipe-method: {effective_wipe_method} volume-name: {volume_name}")
            p = subprocess.Popen(
                f'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || '
                f'grep -q "No space left on device" /tmp/init-stderr', shell=True).wait()
            print(f"TEST: end - pod-name: {pod_name} volume-wipe-method: {effective_wipe_method} volume-name: {volume_name}")
        elif effective_wipe_method == "blkdiscard":
            print(f"TEST: start - pod-name: {pod_name} volume-wipe-method: {effective_wipe_method} volume-name: {volume_name}")
            p = subprocess.Popen(f"blkdiscard -z {volume_path}", shell=True).wait()
            print(f"TEST: end - pod-name: {pod_name} volume-wipe-method: {effective_wipe_method} volume-name: {volume_name}")
        else:
            print(f"TEST: Error - pod-name: {pod_name} volume-wipe-method: {effective_wipe_method} volume-name: {volume_name}")
            raise ValueError(f"pod-name: {pod_name} invalid effective-wipe-method: {effective_wipe_method}")
        print(f"pod-name: {pod_name} wiped volume: {volume_name}")
        volumes.append(volume)

    return volumes


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


def init_volumes(pod_name, config):
    initialized_volumes = get_initialized_volumes(pod_name=pod_name, config=config)
    volumes = []
    for volume in get_volumes(pod_name=pod_name, config=config):
        try:
            volume_mode = volume["source"]["persistentVolume"]["volumeMode"]
        except KeyError:
            print(f"pod-name: {pod_name} volume not found skipping..")
            continue

        volume_name = volume["name"]
        effective_init_method = volume['effectiveInitMethod']
        if volume_name not in initialized_volumes:
            if volume_mode == 'Block':
                volume_path = os.path.join(BLOCK_MOUNT_POINT, volume_name)
                if not os.path.exists(volume_path):
                    raise FileNotFoundError(f"pod-name: {pod_name} volume: {volume_path} not found")
                if effective_init_method == "dd":
                    print(f"TEST: start - pod-name: {pod_name} volume-init-method: {effective_init_method} volume-name: {volume_name}")
                    p = subprocess.Popen(
                        f'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || '
                        f'grep -q "No space left on device" /tmp/init-stderr', shell=True).wait()
                    print(f"TEST: end - pod-name: {pod_name} volume-init-method: {effective_init_method} volume-name: {volume_name}")
                elif effective_init_method == "blkdiscard":
                    print(f"TEST: start - pod-name: {pod_name} volume-init-method: {effective_init_method} volume-name: {volume_name}")
                    p = subprocess.Popen(f"blkdiscard {volume_path}", shell=True).wait()
                    print(f"TEST: end - pod-name: {pod_name} volume-wipe-method: {effective_init_method} volume-name: {volume_name}")
            elif volume_mode == 'Filesystem':
                volume_path = os.path.join(FILE_SYSTEM_MOUNT_POINT, volume_name)
                if effective_init_method == "deleteFiles":
                    print(f"TEST: start - pod-name: {pod_name} volume-init-method: {effective_init_method} volume-name: {volume_name}")
                    try:
                        p = subprocess.Popen(f"'find {volume_path} -type f -delete").wait()
                    except FileNotFoundError:
                        pass
                    print(f"TEST: end - pod-name: {pod_name} volume-init-method: {effective_init_method} volume-name: {volume_name}")
            print(f"pod-name: {pod_name} volume: {volume_name} "
                  f"volume-mode: {volume_mode}  init-method: {effective_init_method} initialized")
        else:
            print(f"pod-name: {pod_name} volume: {volume_name} already initialized")
        volumes.append(volume_name)
    return volumes


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


def main():
    print(f"The version is: {sys.version}")
    try:
        parser = argparse.ArgumentParser()
        parser.add_argument("--pod-name", type=str, required=True, dest="pod_name")
        parser.add_argument("--config", type=str, required=True, dest="config")
        parser.parse_args()
        args = parser.parse_args()
        config = json.loads(args.config)
        metadata = get_node_metadata()
        try:
            next_major_ver = get_image_version(image=config["spec"]["image"])[0]
        except ValueError:
            raise ValueError(f"pod-name: {args.pod_name} unable to get spec.image version")
        try:
            prev_major_ver = get_image_version(image=metadata["image"])[0]
            if (next_major_ver < BASE_WIPE_VERSION and prev_major_ver < BASE_WIPE_VERSION) or \
                    (next_major_ver >= BASE_WIPE_VERSION and prev_major_ver >= BASE_WIPE_VERSION):
                raise ValueError(f"pod-name: {args.pod_name} - volumes should not be wiped")
            volumes = wipe_volumes(pod_name=args.pod_name, config=config)
        except ValueError as e:
            if str(e) != f"pod-name: {args.pod_name} - volumes should not be wiped":
                raise e
            volumes = init_volumes(pod_name=args.pod_name, config=config)

    except Exception as e:
        print(e)
        print(sys.exc_info())
        sys.exit(1)
    else:
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
        metadata["aerospike"]["rackID"] = get_rack(config=config, pod_name=args.pod_name)["id"]

        payload = [{"op": "replace", "path": f"/status/pods/{args.pod_name}", "value": metadata}]
        print("######################################################################################################")
        pp(payload)
        print("######################################################################################################")
        with open("/tmp/patch.json", mode="w") as f:
            json.dump(payload, f)
            f.flush()
        sys.exit(0)


if __name__ == '__main__':
    main()
