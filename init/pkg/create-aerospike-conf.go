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
	aerospikeConf      = "/etc/aerospike/aerospike.template.conf"
	peers              = "/etc/aerospike/peers"
	access             = "access"
	alternateAccess    = "alternate-access"
	tlsAccess          = "tls-access"
	tlsAlternateAccess = "tls-alternate-access"
)

func (initp *InitParams) createAerospikeConf() error {
	data, err := os.ReadFile(aerospikeConf)
	if err != nil {
		return err
	}

	confString := string(data)

	// Update node and rack ids configuration file
	re := regexp.MustCompile("rack-id.*0")
	if rackStr := re.FindString(confString); rackStr != "" {
		confString = strings.ReplaceAll(confString, rackStr, "rack-id    "+initp.rackID)
	}

	confString = strings.ReplaceAll(confString, "ENV_NODE_ID", initp.nodeID)
	confString = initp.substituteEndpoint(
		initp.networkInfo.networkPolicy.AccessType, access, initp.networkInfo.configureAccessIP,
		initp.networkInfo.customAccessNetworkIPs, confString)
	confString = initp.substituteEndpoint(
		initp.networkInfo.networkPolicy.AlternateAccessType, alternateAccess, initp.networkInfo.configuredAlterAccessIP,
		initp.networkInfo.customAlternateAccessNetworkIPs, confString)

	if tlsEnabled, _ := strconv.ParseBool(myPodTLSEnabled); tlsEnabled {
		confString = initp.substituteEndpoint(
			initp.networkInfo.networkPolicy.TLSAccessType, tlsAccess, initp.networkInfo.configureAccessIP,
			initp.networkInfo.customTLSAccessNetworkIPs, confString)
		confString = initp.substituteEndpoint(
			initp.networkInfo.networkPolicy.TLSAlternateAccessType, tlsAlternateAccess,
			initp.networkInfo.configuredAlterAccessIP, initp.networkInfo.customTLSAlternateAccessNetworkIPs, confString)
	}

	if initp.networkInfo.networkPolicy.FabricType == asdbv1beta1.AerospikeNetworkTypeCustomInterface {
		for _, ip := range initp.networkInfo.customFabricNetworkIPs {
			confString = strings.ReplaceAll(confString, "fabric {",
				fmt.Sprintf("fabric {\n        address %s", ip))
		}
	}

	if initp.networkInfo.networkPolicy.TLSFabricType == asdbv1beta1.AerospikeNetworkTypeCustomInterface {
		for _, ip := range initp.networkInfo.customTLSFabricNetworkIPs {
			confString = strings.ReplaceAll(confString, "fabric {",
				fmt.Sprintf("fabric {\n        tls-address %s", ip))
		}
	}

	readFile, err := os.Open(peers)
	if err != nil {
		return err
	}

	defer readFile.Close()

	fileScanner := bufio.NewScanner(readFile)

	//  Update mesh seeds in the configuration file
	for fileScanner.Scan() {
		peer := fileScanner.Text()
		if strings.Contains(peer, initp.podName) {
			continue
		}

		if initp.networkInfo.heartBeatPort != 0 {
			confString = strings.ReplaceAll(confString, "heartbeat {",
				fmt.Sprintf("heartbeat {\n        mesh-seed-address-port %s %d", peer, initp.networkInfo.heartBeatPort))
		}

		if initp.networkInfo.heartBeatTLSPort != 0 {
			confString = strings.ReplaceAll(confString, "heartbeat {",
				fmt.Sprintf("heartbeat {\n        tls-mesh-seed-address-port %s %d", peer, initp.networkInfo.heartBeatTLSPort))
		}
	}

	// If host networking is used force heartbeat and fabric to advertise network
	// interface bound to K8s node's host network.
	if initp.networkInfo.hostNetwork {
		// 8 spaces, fixed in config writer file config manager lib
		// TODO: The search pattern is not robust. Add a better marker in management lib.
		if initp.networkInfo.heartBeatPort != 0 {
			confString = strings.ReplaceAll(confString, "heartbeat {",
				fmt.Sprintf("heartbeat {\n        address %s", initp.networkInfo.podIP))
		}

		if initp.networkInfo.heartBeatTLSPort != 0 {
			confString = strings.ReplaceAll(confString, "heartbeat {",
				fmt.Sprintf("heartbeat {\n        tls-address %s", initp.networkInfo.podIP))
		}

		if initp.networkInfo.fabricPort != 0 {
			confString = strings.ReplaceAll(confString, "fabric {",
				fmt.Sprintf("fabric {\n        address %s", initp.networkInfo.podIP))
		}

		if initp.networkInfo.fabricTLSPort != 0 {
			confString = strings.ReplaceAll(confString, "fabric {",
				fmt.Sprintf("fabric {\n        tls-address %s", initp.networkInfo.podIP))
		}
	}

	if err = os.WriteFile(aerospikeConf, []byte(confString), 0644); err != nil { //nolint:gocritic,gosec // file permission
		return err
	}

	initp.logger.Info("Final aerospike conf file", "aerospike.template.conf", confString)

	return nil
}

// Update access addresses in the configuration file
// Compute the access endpoints based on network policy.
// As a kludge the computed values are stored late to update node summary.
func (initp *InitParams) substituteEndpoint(networkType asdbv1beta1.AerospikeNetworkType,
	addressType, configuredIP string, interfaceIPs []string, confString string) string {
	var (
		accessAddress []string
		accessPort    int32
	)

	podPort := initp.networkInfo.podPort
	mappedPort := initp.networkInfo.mappedPort

	if addressType == tlsAccess || addressType == tlsAlternateAccess {
		podPort = initp.networkInfo.podTLSPort
		mappedPort = initp.networkInfo.mappedTLSPort
	}

	switch networkType { //nolint:exhaustive // fallback to default
	case asdbv1beta1.AerospikeNetworkTypePod:
		accessAddress = append(accessAddress, initp.networkInfo.podIP)
		accessPort = podPort

	case asdbv1beta1.AerospikeNetworkTypeHostInternal:
		accessAddress = append(accessAddress, initp.networkInfo.internalIP)
		accessPort = mappedPort

	case asdbv1beta1.AerospikeNetworkTypeHostExternal:
		accessAddress = append(accessAddress, initp.networkInfo.externalIP)
		accessPort = mappedPort

	case asdbv1beta1.AerospikeNetworkTypeConfigured:
		if configuredIP == "" {
			initp.logger.Error(fmt.Errorf("configureIP missing"),
				fmt.Sprintf("Please set %s and %s node label to use NetworkPolicy configuredIP for "+
					"access and alternateAccess addresses", configuredAccessIPLabel, configuredAlternateAccessIPLabel))
			os.Exit(1)
		}

		accessAddress = append(accessAddress, configuredIP)
		accessPort = mappedPort

	case asdbv1beta1.AerospikeNetworkTypeCustomInterface:
		accessAddress = interfaceIPs
		accessPort = podPort

	default:
		accessAddress = append(accessAddress, initp.networkInfo.podIP)
		accessPort = podPort
	}

	// Store computed address to update the status later.
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

	// Substitute in the configuration file.
	// If multiple IPs are found, then add all of them in the specific addressType
	var newStr string
	for _, addr := range accessAddress {
		newStr += fmt.Sprintf("%s-address    %s\n        ", addressType, addr)
	}

	confString = strings.ReplaceAll(confString, fmt.Sprintf("%s-address    <%s-address>", addressType, addressType),
		strings.TrimSuffix(newStr, "\n        "))

	re := regexp.MustCompile(fmt.Sprintf("\\s*%s-port\\s*%d", addressType, podPort))

	if portString := re.FindString(confString); portString != "" {
		// # This port is set in api/v1beta1/aerospikecluster_mutating_webhook.go and is used as placeholder.
		confString = strings.ReplaceAll(confString, portString,
			strings.ReplaceAll(portString, strconv.Itoa(int(podPort)), strconv.Itoa(int(accessPort))))
	}

	return confString
}
