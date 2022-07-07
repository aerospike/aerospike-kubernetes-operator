#!/usr/bin/env python3
import os
import re
import sys
import json
import logging
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

logging.basicConfig(format='%(levelname)s:%(message)s', level=logging.DEBUG)


class Volume(object):

    def __init__(self, pod_name, volume):
        self.pod_name = pod_name
        self.volume_mode = volume["source"]["persistentVolume"]["volumeMode"]
        self.volume_name = volume["name"]
        logging.debug(f"pod-name: {self.pod_name} - Initializing volume object: {self.volume_name}")

        self.effective_wipe_method = volume["effectiveWipeMethod"]
        self.effective_init_method = volume["effectiveInitMethod"]

        if "aerospike" in volume:
            self.attachment_type = "aerospike"
            self.volume_path = volume["aerospike"]["path"]

        elif "sidecars" in volume:
            self.attachment_type = "sidecars"
            self.volume_path = [(sidecar["containerName"], sidecar["path"]) for sidecar in volume["sidecars"]]

        elif "initContainers" in volume:
            self.attachment_type = "initContainers"
            self.volume_path = [(initContainer["containerName"], initContainer["path"])
                                for initContainer in volume["initContainers"]]

        else:
            logging.debug(f"pod-name: {self.pod_name} volume-name: {self.volume_name} - "
                          f"Empty attachment-type and volume-path")
            self.attachment_type = ""
            self.volume_path = ""
        logging.debug(f"pod-name: {self.pod_name} - Initialized volume object: {self.volume_name}")

    def get_mount_point(self):

        if self.volume_mode == "Block":

            point = os.path.join(BLOCK_MOUNT_POINT, self.volume_name)
            logging.debug(f"pod-name: {self.pod_name} volume-name: {self.volume_name} mount-point: {point}")
            return point

        point = os.path.join(FILE_SYSTEM_MOUNT_POINT, self.volume_name)
        logging.debug(f"pod-name: {self.pod_name} volume-name: {self.volume_name} mount-point: {point}")

        return point

    def get_attachment_path(self):
        return self.volume_path

    def __str__(self):
        return f"pod-name: {self.pod_name} volume-name: {self.volume_name} " \
               f"volume-type: {self.attachment_type} volume-path: {self.volume_path} " \
               f"effective-init-method: {self.effective_init_method} effective-wipe-method: " \
               f"{self.effective_wipe_method}"


def get_image_tag(image):
    return re.search(r"\d+\.\d+\.\d+\.\d+$", image).group(0)


def get_image_version(image):
    return tuple(map(int, get_image_tag(image).split(".")))


def execute(cmd):

    logging.debug(f"Executing command: {cmd}")
    return_value = os.system(cmd)

    if return_value != 0:
        raise OSError(f"Execution Failed - command: {cmd}")


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


def get_cluster_json(cluster_name, namespace, api_server, token, ca_cert):

    url = f"{api_server}/apis/asdb.aerospike.com/v1beta1/namespaces/{namespace}/aerospikeclusters/{cluster_name}"
    logging.debug(f"Request config from url: {url}")
    request = urllib.request.Request(url=url, method="GET")
    request.add_header("Authorization", f"Bearer {token}")

    with urllib.request.urlopen(request, cafile=ca_cert) as response:
        body = response.read()

    return json.loads(body)


def get_pod_image(pod_name, namespace, api_server, token, ca_cert):
    url = f"{api_server}/api/v1/namespaces/{namespace}/pods/{pod_name}"
    logging.debug(f"Request pod-image from url: {url}")
    request = urllib.request.Request(url=url, method="GET")
    request.add_header("Authorization", f"Bearer {token}")

    with urllib.request.urlopen(request, cafile=ca_cert) as response:
        body = response.read()

    data = json.loads(body)

    try:
        logging.debug("Looking for Pod-Image")
        pod_server_image = data["spec"]["containers"][0]["image"]
        return pod_server_image
    except KeyError:
        logging.debug("Pod-Image not found")
        return ""


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


def get_node_metadata():

    pod_port = os.environ["POD_PORT"]
    service_port = os.environ["MAPPED_PORT"]

    if strtobool(os.environ.get("MY_POD_TLS_ENABLED", default="")):
        pod_port = os.environ["POD_TLSPORT"]
        service_port = os.environ["MAPPED_TLSPORT"]

    return {
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


def update_status(pod_name, pod_image, metadata, volumes):

    with open("aerospikeConfHash", mode="r") as f:
        conf_hash = f.read()

    with open("networkPolicyHash", mode="r") as f:
        network_policy_hash = f.read()

    with open("podSpecHash", mode="r") as f:
        pod_spec_hash = f.read()

    metadata.update({
        "image": pod_image,
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


def get_initialized_volumes(pod_name, config):

    try:
        logging.debug(
            f"pod-name: {pod_name} - Looking for initialized volumes in status.pod.{pod_name}.initializedVolumes")

        return set(config["status"]["pods"][pod_name]["initializedVolumes"])
    except KeyError:
        logging.warning(
            f"pod-name: {pod_name} - Initialized volumes not found in status.pod.{pod_name}.initializedVolumes")

        try:
            logging.debug(f"pod-name: {pod_name} - Looking for initialized volumes in status.pod."
                          f"{pod_name}.initializedVolumePaths")

            return set(config["status"]["pods"][pod_name]["initializedVolumePaths"])
        except KeyError:
            logging.warning(f"pod-name: {pod_name} - Initialized volumes not found")
            return set()


def get_rack(pod_name, config):

    # Assuming podName format stsName-rackID-index
    rack_id = int(pod_name.split("-")[-2])

    logging.debug(f"pod-name: {pod_name} - Checking for rack in rackConfig rack-id: {rack_id}")
    try:
        racks = config["spec"]["rackConfig"]["racks"]

        for rack in racks:
            if rack["id"] == rack_id:
                return rack

        logging.error(f"pod-name: {pod_name} rack-id: {rack_id} - Not found")
        raise ValueError(f"pod-name: {pod_name} rack-id: {rack_id} - Not found")

    except KeyError:

        logging.error(f"pod-name: {pod_name} - Unable to get rack-id {rack_id}")
        raise


def get_attached_volumes(pod_name, config):

    rack = get_rack(pod_name=pod_name, config=config)

    try:
        logging.debug(f"pod-name: {pod_name} - Looking for volumes in rack.storage.volumes")
        volumes = rack["storage"]["volumes"]

        if not volumes:
            logging.warning(f"pod-name: {pod_name} - Found an empty list in rack.storage.volumes")
            raise KeyError(f"pod-name: {pod_name} - volumes not found")

        return volumes

    except KeyError:
        logging.debug(f"pod-name: {pod_name} - Volumes not found in rack.storage.volumes")

        try:
            logging.debug(f"pod-name: {pod_name} - Looking for volumes in spec.storage.volumes")
            volumes = config["spec"]["storage"]["volumes"]

            if not volumes:

                logging.warning(f"pod-name: {pod_name} - Found an empty list in spec.storage.volumes")
                raise KeyError(f"pod-name: {pod_name} - Found an empty list in spec.storage.volumes")

            return volumes

        except KeyError:
            logging.error(f"pod-name: {pod_name} - Volumes not found")
            return []


def get_persistent_volumes(volumes):
    for volume in filter(lambda x: True if "persistentVolume" in x["source"] else False, volumes):
        yield volume


def get_namespace_volume_paths(pod_name, config):

    filepaths = []
    devicepaths = set()
    rack = get_rack(pod_name=pod_name, config=config)

    try:
        namespaces = rack["effectiveAerospikeConfig"]["namespaces"]
    except KeyError as e:
        logging.error(f"pod-name: {pod_name} - Unable to find namespaces")
        raise e

    for namespace in namespaces:

        storage_engine = namespace["storage-engine"]
        device_type = storage_engine["type"]
        if device_type == "device":

            if "devices" in storage_engine:
                devices = storage_engine["devices"]

                for device in devices:
                    for d in device.split():
                        logging.debug(f"pod-name: {pod_name} - Get device-type: {device_type}  device: {d}")
                        devicepaths.add(d)

            if "files" in storage_engine:
                files = storage_engine["files"]

                for fi in files:
                    for f in fi.split():
                        logging.debug(f"pod-name: {pod_name} Get device-type: {device_type} file: {f}")
                        filepaths.append(f)

    return devicepaths, filepaths


def init_volumes(pod_name, config):

    volumes = []
    initialized_volumes = get_initialized_volumes(pod_name=pod_name, config=config)

    for vol in (v for v in filter(lambda x: True if x["name"] not in initialized_volumes else False,
                                  get_persistent_volumes(volumes=get_attached_volumes(
                                      pod_name=pod_name, config=config)))):

        volume = Volume(pod_name=pod_name, volume=vol)

        logging.debug(f"Starting initialization: {volume}")
        if volume.volume_mode == "Block":

            if not os.path.exists(volume.get_mount_point()):
                logging.error(f"pod-name: {pod_name} volume-name: {volume.volume_name} - Mounting point "
                              f"does not exists")
                raise FileNotFoundError(f"{volume} Volume path not found")

            if volume.effective_init_method == "dd":

                dd = 'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || grep -q "No space left on device" '\
                     '/tmp/init-stderr'.format(volume_path=volume.get_mount_point())
                execute(dd)
                logging.info(f"{volume} - Initialized")

            elif volume.effective_init_method == "blkdiscard":

                blkdiskard = "blkdiscard {volume_path}".format(volume_path=volume.get_mount_point())
                execute(blkdiskard)
                logging.info(f"{volume} - Initialized")

            elif volume.effective_init_method == "none":
                logging.info(f"{volume} - Passthrough")
            else:
                logging.error(f"{volume} - Has invalid effective method")
                raise ValueError(f"{volume} - Has invalid effective method")

        elif volume.volume_mode == "Filesystem":
            logging.debug(f"In Filesystem initialization: {volume}")
            if not os.path.exists(volume.get_mount_point()):
                logging.error(f"pod-name: {pod_name} volume-name: {volume.volume_name} - Mounting point "
                              f"does not exists")
                raise FileNotFoundError(f"{volume} Volume path not found")

            if volume.effective_init_method == "deleteFiles":

                find = "find {volume_path} -type f -delete".format(volume_path=volume.get_mount_point())
                execute(find)
                logging.info(f"{volume} - Initialized")

            elif volume.effective_init_method == "none":
                logging.info(f"{volume} - Passthrough")
            else:
                logging.error(f"{volume} - Has invalid effective method")
                raise ValueError(f"{volume} - Has invalid effective method")
        else:
            logging.error(f"{volume} - Invalid volume-mode: {volume.volume_mode}")
            raise ValueError(f"pod-name: {pod_name} - Invalid volume-mode: {volume.volume_mode}")

        logging.debug(f"{volume} - Added to initialized-volume list")
        volumes.append(volume.volume_name)

    logging.debug(f"{volumes} - Extending initialized-volume list")
    volumes.extend(initialized_volumes)

    return volumes


def wipe_volumes(pod_name, config):

    ns_device_paths, ns_file_paths = get_namespace_volume_paths(pod_name=pod_name, config=config)

    for vol in (v for v in filter(lambda x: True if "aerospike" in x else False, get_persistent_volumes(
            get_attached_volumes(pod_name=pod_name, config=config)))):

        volume = Volume(pod_name=pod_name, volume=vol)

        if volume.volume_mode == "Block":

            if volume.volume_path in ns_device_paths:

                if not os.path.exists(volume.get_mount_point()):
                    logging.error(f"pod-name: {pod_name} volume-name: {volume.volume_name}"
                                  f" - Mounting point does not exists")
                    raise FileNotFoundError(f"{volume} - Volume path not found")

                if volume.effective_wipe_method == "dd":

                    dd = 'dd if=/dev/zero of={volume_path} bs=1M 2> /tmp/init-stderr || grep -q "No space left on device" '\
                        '/tmp/init-stderr'.format(volume_path=volume.get_mount_point())

                    execute(dd)
                    logging.info(f"{volume} - Wiped")

                elif volume.effective_wipe_method == "blkdiscard":

                    blkdiskard = "blkdiscard -z {volume_path}".format(volume_path=volume.get_mount_point())
                    execute(blkdiskard)
                    logging.info(f"{volume} - Wiped")

                else:
                    raise ValueError(f"{volume} - Has invalid effective method")
        elif volume.volume_mode == "Filesystem":

            if volume.effective_wipe_method == "deleteFiles":

                if not os.path.exists(volume.get_mount_point()):
                    logging.error(f"pod-name: {pod_name} volume-name: {volume.volume_name} "
                                  f"- Mounting point does not exists")
                    raise FileNotFoundError(f"{volume} Volume path not found")

                for ns_file_path in filter(lambda x: x.startswith(volume.get_attachment_path()), ns_file_paths):
                    _, filename = os.path.split(ns_file_path)
                    file_path = os.path.join(volume.get_mount_point(), filename)
                    if os.path.exists(file_path):
                        logging.info(f"{file_path} - Removing file")
                        os.remove(file_path)
                        logging.info(f"{file_path} - Wiped")
                    else:
                        logging.warning(f"{volume} namespace-file-path: {file_path} - Does not exists")

            else:
                logging.error(f"{volume} - Has invalid effective method")
                raise ValueError(f"{volume} - Has invalid effective method")
        else:
            logging.error(f"pod-name: {pod_name} Invalid volume-mode: {volume.volume_mode}")
            raise ValueError(f"pod-name: {pod_name} Invalid volume-mode: {volume.volume_mode}")
    return


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
            logging.info(f"pod-name: {args.pod_name} - Get configuration request")
            config = get_cluster_json(
                cluster_name=args.cluster_name,
                namespace=args.namespace,
                api_server=args.api_server,
                token=args.token,
                ca_cert=args.ca_cert)
            pod_image = get_pod_image(
                pod_name=args.pod_name,
                namespace=args.namespace,
                api_server=args.api_server,
                token=args.token,
                ca_cert=args.ca_cert)
        except urllib.error.URLError as e:
            logging.error(f"pod-name: {args.pod_name} - Unable to prerform http request - Error: {e}")
            raise e

        try:
            image = config["status"]["image"]
            logging.info(f"pod-name: {args.pod_name} - Restarted")
        except KeyError:
            logging.info(f"pod-name: {args.pod_name} - Initializing")
            image = ""

        metadata = get_node_metadata()
        next_major_ver = get_image_version(image=config["spec"]["image"])[0]

        logging.info(f"pod-name: {args.pod_name} - Checking if volumes should be wiped")

        if image:

            prev_major_ver = get_image_version(image=image)[0]
            logging.info(
                f"pod-name: {args.pod_name} "
                f"- Checking if volumes should be wiped: "
                f"next-major-version: {next_major_ver} prev-major-version: {prev_major_ver}")

            if (next_major_ver >= BASE_WIPE_VERSION > prev_major_ver) or \
                    (next_major_ver < BASE_WIPE_VERSION <= prev_major_ver):
                logging.info(f"pod-name: {args.pod_name} - Volumes should be wiped")
                wipe_volumes(pod_name=args.pod_name, config=config)
            else:
                logging.info(f"pod-name: {args.pod_name} - Volumes should not be wiped")
        else:
            logging.info(f"pod-name: {args.pod_name} - Volumes should not be wiped")

        logging.info(f"pod-name: {args.pod_name} - Checking if volume initialization needed")
        volumes = init_volumes(pod_name=args.pod_name, config=config)

        logging.info(f"pod-name: {args.pod_name} - Updating pod status")
        update_status(pod_name=args.pod_name, pod_image=pod_image, metadata=metadata, volumes=volumes)

    except Exception as e:
        print(e)
        sys.exit(1)
    else:
        sys.exit(0)


if __name__ == "__main__":
    main()
