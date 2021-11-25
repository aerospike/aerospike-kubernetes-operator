import sys
import json
import os
from ipaddress import ip_address, IPv4Address

# Constants
fileSystemMountPoint = '/workdir/filesystem-volumes'
blockMountPoint = '/workdir/block-volumes'


def executeCommand(command):
    print('Executing command\n\t' + command)
    exit = os.system(command)
    if exit != 0:
        raise Exception('Error executing command')


def getRack(data, podName):
    print('Checking for rack in rackConfig')
    # Assuming podName format stsName-rackID-index
    rackID = podName.split("-")[-2]
    if 'rackConfig' in data and 'racks' in data['rackConfig']:
        racks = data['rackConfig']['racks']
        for rack in racks:
            if rack['id'] == int(rackID):
                return rack


podName = sys.argv[1]
data = json.load(sys.stdin)

if 'status' in data:
    status = data['status']
else:
    status = {}

if 'spec' in data:
    spec = data['spec']
else:
    spec = {}

rack = getRack(spec, podName)
if rack is None:
    print("spec: ", spec)
    raise Exception('Rack not found for pod ' + podName + ' in above spec')

if 'storage' in rack and 'volumes' in rack['storage'] and len(rack['storage']['volumes']) > 0:
    volumes = rack['storage']['volumes']
else:
    if 'storage' in spec and 'volumes' in spec['storage']:
        volumes = spec['storage']['volumes']
    else:
        volumes = []

if 'pods' in status and podName in status['pods'] and 'initializedVolumes' in status['pods'][podName]:
    alreadyInitialized = status['pods'][podName]['initializedVolumes']
elif 'pods' in status and podName in status['pods'] and 'initializedVolumePaths' in status['pods'][podName]:
    # Read legacy property.
    alreadyInitialized = status['pods'][podName]['initializedVolumePaths']
else:
    alreadyInitialized = []

# Initialize uninitialized volumes. This script also run on Aerospike server containers while warm restarting. It
# should automatically noop in that case.
initialized = []
for volume in volumes:
    if 'persistentVolume' not in volume['source']:
        continue

    # volume path is always absolute.
    volumePath = '/' + volume['name']

    volumeMode = volume['source']['persistentVolume']['volumeMode']

    if volumeMode == 'Block':
        localVolumePath = blockMountPoint + volumePath
    elif volumeMode == 'Filesystem':
        localVolumePath = fileSystemMountPoint + volumePath
    else:
        continue

    if volume['name'] not in alreadyInitialized:
        # This check should happen only on init container and not on Aerospike server container.
        # Hence it should in the uninitialized code path which will only be executed on the init container.
        if not os.path.exists(localVolumePath):
            raise Exception('Volume ' + volume['name'] + ' not attached to path ' + localVolumePath)

        if volumeMode == 'Block':
            localVolumePath = blockMountPoint + volumePath
            if volume['effectiveInitMethod'] == 'dd':
                # If device size and block size are not exact multiples or there os overhead on the device we will get "no space left on device". Ignore that error.
                executeCommand('dd if=/dev/zero of=' +
                               localVolumePath + ' bs=1M 2> /tmp/init-stderr || grep -q "No space left on device" /tmp/init-stderr')
            elif volume['effectiveInitMethod'] == 'blkdiscard':
                executeCommand('blkdiscard ' + localVolumePath)
        elif volumeMode == 'Filesystem':
            # volume path is always absolute.
            localVolumePath = fileSystemMountPoint + volumePath
            if volume['effectiveInitMethod'] == 'deleteFiles':
                executeCommand(
                    'find ' + localVolumePath + ' -type f -delete')
        print('device ' + volume['name'] + ' initialized')

    else:
        print('device ' + volume['name'] + ' already initialized')

    initialized.append(volume['name'])


def isIpv6Address(host):
    try:
        return False if type(ip_address(host)) is IPv4Address else True
    except ValueError:
        return False


def joinHostPort(host, port):
    if isIpv6Address(host):
        return '[' + host + ']:' + port
    else:
        return host + ':' + port


def getEndpoints(addressType):
    addressType = addressType.replace('-', '_')
    environAddressKey = 'global_' + addressType + '_address'
    environPortKey = 'global_' + addressType + '_port'

    if environAddressKey in os.environ and environPortKey in os.environ:
        if os.environ[environAddressKey] and os.environ[environPortKey] and os.environ[environPortKey] != "''":
            return [joinHostPort(os.environ[environAddressKey], os.environ[environPortKey])]

    # Address type not defined.
    return []


def readFile(filePath):
    file = open(filePath, mode='r')
    data = file.read()
    file.close()
    return data


podPort = os.environ['POD_PORT']
servicePort = os.environ['MAPPED_PORT']

if 'MY_POD_TLS_ENABLED' in os.environ and "true" == os.environ['MY_POD_TLS_ENABLED']:
    podPort = os.environ['POD_TLSPORT']
    servicePort = os.environ['MAPPED_TLSPORT']

# Get AerospikeConfigHash and NetworkPolicyHash, all assumed to be in current working directory
confHashFile = 'aerospikeConfHash'
networkPolicyHashFile = 'networkPolicyHash'
podSpecHashFile = 'podSpecHash'

confHash = readFile(confHashFile)
networkPolicyHash = readFile(networkPolicyHashFile)
podSpecHash = readFile(podSpecHashFile)

value = {
    'image': os.environ.get('POD_IMAGE', ''),
    'podIP': os.environ.get('PODIP', ''),
    'hostInternalIP': os.environ.get('INTERNALIP', ''),
    'hostExternalIP': os.environ.get('EXTERNALIP', ''),
    'podPort': int(podPort),
    'servicePort': int(servicePort),
    'aerospike': {
        'clusterName': os.environ.get('MY_POD_CLUSTER_NAME', ''),
        'nodeID': os.environ.get('NODE_ID', ''),
        'tlsName': os.environ.get('MY_POD_TLS_NAME', '')
    },
    'initializedVolumes': initialized,
    'initializedVolumePaths': None,
    'aerospikeConfigHash': confHash,
    'networkPolicyHash': networkPolicyHash,
    'podSpecHash': podSpecHash,
}

# Add access type to pod status variable name.
addressTypeNameMap = {
    'access': 'accessEndpoints',
    'alternate-access': 'alternateAccessEndpoints',
    'tls-access': 'tlsAccessEndpoints',
    'tls-alternate-access': 'tlsAlternateAccessEndpoints'
}
for k, v in addressTypeNameMap.items():
    value['aerospike'][v] = getEndpoints(k)

value['aerospike']['rackID'] = rack['id']

# Create the patch payload for updating pod status.
pathPayload = [{'op': 'replace', 'path': '/status/pods/' +
                podName, 'value': value}]

with open('/tmp/patch.json', 'w') as outfile:
    json.dump(pathPayload, outfile)
