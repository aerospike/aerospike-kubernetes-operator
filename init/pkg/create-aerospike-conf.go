package pkg

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

const (
	cfg                = "/etc/aerospike/aerospike.template.conf"
	peers              = "/etc/aerospike/peers"
	access             = "access"
	alternateAccess    = "alternate-access"
	tlsAccess          = "tls-access"
	tlsAlternateAccess = "tls-alternate-access"
)

func (initp *InitParams) createAerospikeConf() error {
	data, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}

	confString := string(data)

	re := regexp.MustCompile("rack-id.*0")
	if re.FindString(confString) != "" {
		confString = strings.ReplaceAll(confString, re.FindString(confString), "rack-id    "+initp.rackID)
	}

	confString = strings.ReplaceAll(confString, "ENV_NODE_ID", initp.nodeID)
	confString = initp.substituteEndpoint(initp.networkInfo.NetworkPolicy.AccessType, access, confString)
	confString = initp.substituteEndpoint(initp.networkInfo.NetworkPolicy.AlternateAccessType, alternateAccess, confString)

	if os.Getenv("MY_POD_TLS_ENABLED") == "true" {
		confString = initp.substituteEndpoint(initp.networkInfo.NetworkPolicy.TLSAccessType,
			tlsAccess, confString)
		confString = initp.substituteEndpoint(initp.networkInfo.NetworkPolicy.TLSAlternateAccessType,
			tlsAlternateAccess, confString)
	}

	readFile, err := os.Open(peers)
	if err != nil {
		fmt.Println(err)
	}

	defer readFile.Close()

	fileScanner := bufio.NewScanner(readFile)
	fileScanner.Split(bufio.ScanLines)

	for fileScanner.Scan() {
		peer := fileScanner.Text()
		if strings.Contains(peer, initp.podName) {
			continue
		}

		if initp.networkInfo.HeartBeatPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        mesh-seed-address-port %s %d%s",
				strdataSplit[0], peer, initp.networkInfo.HeartBeatPort, strdataSplit[1])
		}

		if initp.networkInfo.HeartBeatTLSPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        tls-mesh-seed-address-port %s %d%s",
				strdataSplit[0], peer, initp.networkInfo.HeartBeatTLSPort, strdataSplit[1])
		}
	}

	if initp.networkInfo.HostNetwork {
		if initp.networkInfo.HeartBeatPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        address %s%s",
				strdataSplit[0], initp.networkInfo.podIP, strdataSplit[1])
		}

		if initp.networkInfo.HeartBeatTLSPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        tls-address %s%s",
				strdataSplit[0], initp.networkInfo.podIP, strdataSplit[1])
		}

		if initp.networkInfo.FabricPort != 0 {
			strdataSplit := strings.Split(confString, "fabric {")
			confString = fmt.Sprintf("%sfabric {\n        address %s%s",
				strdataSplit[0], initp.networkInfo.podIP, strdataSplit[1])
		}

		if initp.networkInfo.FabricTLSPort != 0 {
			strdataSplit := strings.Split(confString, "fabric {")
			confString = fmt.Sprintf("%sfabric {\n        tls-address %s%s",
				strdataSplit[0], initp.networkInfo.podIP, strdataSplit[1])
		}
	}

	if err = os.WriteFile(cfg, []byte(confString), 0644); err != nil { //nolint:gocritic,gosec // file permission
		return err
	}

	logrus.Info("aerospike.template.conf = ", confString)

	return nil
}

func (initp *InitParams) substituteEndpoint(networkType asdbv1beta1.AerospikeNetworkType,
	addressType, confString string) string {
	var (
		accessAddress string
		accessPort    int32
		podPort       int32
		mappedPort    int32
	)

	if addressType == access || addressType == alternateAccess {
		podPort = initp.networkInfo.PodPort
		mappedPort = initp.networkInfo.mappedPort
	}

	if addressType == tlsAccess || addressType == tlsAlternateAccess {
		podPort = initp.networkInfo.PodTLSPort
		mappedPort = initp.networkInfo.mappedTLSPort
	}

	switch networkType { //nolint:exhaustive // fallback to default
	case asdbv1beta1.AerospikeNetworkTypePod:
		accessAddress = initp.networkInfo.podIP
		accessPort = podPort

	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		accessAddress = initp.networkInfo.internalIP
		accessPort = mappedPort

	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		accessAddress = initp.networkInfo.externalIP
		accessPort = mappedPort

	default:
		accessAddress = initp.networkInfo.podIP
		accessPort = podPort
	}

	switch addressType {
	case access:
		initp.networkInfo.globalAddressesAndPorts.globalAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalAccessPort = accessPort

	case alternateAccess:
		initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessPort = accessPort

	case tlsAccess:
		initp.networkInfo.globalAddressesAndPorts.globalTLSAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalTLSAccessPort = accessPort

	case tlsAlternateAccess:
		initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessAddress = accessAddress
		initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessPort = accessPort
	}

	confString = strings.ReplaceAll(confString, fmt.Sprintf("<%s-address>", addressType), accessAddress)
	re := regexp.MustCompile(fmt.Sprintf("\\s*%s-port\\s*%d", addressType, podPort))

	portString := re.FindString(confString)
	if portString != "" {
		confString = strings.ReplaceAll(confString, portString,
			strings.ReplaceAll(portString, strconv.Itoa(int(podPort)), strconv.Itoa(int(accessPort))))
	}

	return confString
}
