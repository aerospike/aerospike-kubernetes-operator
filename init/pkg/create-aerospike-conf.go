package pkg

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

const (
	CFG                = "/etc/aerospike/aerospike.template.conf"
	PEERS              = "/etc/aerospike/peers"
	Access             = "access"
	AlternateAccess    = "alternate-access"
	TLSAccess          = "tls-access"
	TLSAlternateAccess = "tls-alternate-access"
)

func (initp *InitParams) createAerospikeConf() error {
	data, err := os.ReadFile(CFG)
	if err != nil {
		return err
	}

	confString := string(data)

	re := regexp.MustCompile("rack-id.*0")
	if re.FindString(confString) != "" {
		confString = strings.ReplaceAll(confString, re.FindString(confString), "rack-id    "+initp.rackID)
	}

	confString = strings.ReplaceAll(confString, "ENV_NODE_ID", initp.nodeID)
	confString = initp.substituteEndpoint(Access, confString)
	confString = initp.substituteEndpoint(AlternateAccess, confString)

	if os.Getenv("MY_POD_TLS_ENABLED") == "true" {
		confString = initp.substituteEndpoint(TLSAccess, confString)
		confString = initp.substituteEndpoint(TLSAlternateAccess, confString)
	}

	readFile, err := os.Open(PEERS)
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

		if initp.initTemplateInput.HeartBeatPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        mesh-seed-address-port %s %d%s",
				strdataSplit[0], peer, initp.initTemplateInput.HeartBeatPort, strdataSplit[1])
		}

		if initp.initTemplateInput.HeartBeatTLSPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        tls-mesh-seed-address-port %s %d%s",
				strdataSplit[0], peer, initp.initTemplateInput.HeartBeatTLSPort, strdataSplit[1])
		}
	}

	if initp.initTemplateInput.HostNetwork {
		if initp.initTemplateInput.HeartBeatPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        address %s%s",
				strdataSplit[0], os.Getenv("MY_POD_IP"), strdataSplit[1])
		}

		if initp.initTemplateInput.HeartBeatTLSPort != 0 {
			strdataSplit := strings.Split(confString, "heartbeat {")
			confString = fmt.Sprintf("%sheartbeat {\n        tls-address %s%s",
				strdataSplit[0], os.Getenv("MY_POD_IP"), strdataSplit[1])
		}

		if initp.initTemplateInput.FabricPort != 0 {
			strdataSplit := strings.Split(confString, "fabric {")
			confString = fmt.Sprintf("%sfabric {\n        address %s%s",
				strdataSplit[0], os.Getenv("MY_POD_IP"), strdataSplit[1])
		}

		if initp.initTemplateInput.FabricTLSPort != 0 {
			strdataSplit := strings.Split(confString, "fabric {")
			confString = fmt.Sprintf("%sfabric {\n        tls-address %s%s",
				strdataSplit[0], os.Getenv("MY_POD_IP"), strdataSplit[1])
		}
	}

	if err = os.WriteFile(CFG, []byte(confString), 0644); err != nil { //nolint:gocritic,gosec // file permission
		return err
	}

	return nil
}

func (initp *InitParams) substituteEndpoint(addressType, confString string) string {
	var (
		accessAddress string
		accessPort    int32
		networkType   asdbv1beta1.AerospikeNetworkType
		podPort       int32
		mappedPort    int32
	)

	if addressType == Access || addressType == AlternateAccess {
		if addressType == Access {
			networkType = initp.initTemplateInput.NetworkPolicy.AccessType
		} else {
			networkType = initp.initTemplateInput.NetworkPolicy.AlternateAccessType
		}

		podPort = initp.initTemplateInput.PodPort
		mappedPort = initp.mappedPort
	}

	if addressType == TLSAccess || addressType == TLSAlternateAccess {
		if addressType == TLSAccess {
			networkType = initp.initTemplateInput.NetworkPolicy.TLSAccessType
		} else {
			networkType = initp.initTemplateInput.NetworkPolicy.TLSAlternateAccessType
		}

		podPort = initp.initTemplateInput.PodTLSPort
		mappedPort = initp.mappedTLSPort
	}

	switch networkType { //nolint:exhaustive // fallback to default
	case asdbv1beta1.AerospikeNetworkTypePod:
		accessAddress = os.Getenv("MY_POD_IP")
		accessPort = podPort

	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		accessAddress = initp.internalIP
		accessPort = mappedPort

	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		accessAddress = initp.externalIP
		accessPort = mappedPort

	default:
		accessAddress = os.Getenv("MY_POD_IP")
		accessPort = podPort
	}

	switch addressType {
	case Access:
		initp.globalAddressesAndPorts.globalAccessAddress = accessAddress
		initp.globalAddressesAndPorts.globalAccessPort = accessPort

	case AlternateAccess:
		initp.globalAddressesAndPorts.globalAlternateAccessAddress = accessAddress
		initp.globalAddressesAndPorts.globalAlternateAccessPort = accessPort

	case TLSAccess:
		initp.globalAddressesAndPorts.globalTLSAccessAddress = accessAddress
		initp.globalAddressesAndPorts.globalTLSAccessPort = accessPort

	case TLSAlternateAccess:
		initp.globalAddressesAndPorts.globalTLSAlternateAccessAddress = accessAddress
		initp.globalAddressesAndPorts.globalTLSAlternateAccessPort = accessPort
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
