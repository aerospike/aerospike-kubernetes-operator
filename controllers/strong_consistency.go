package controllers

import (
	"fmt"
	"strings"

	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v5"
)

func (r *SingleClusterReconciler) getAndSetRoster(aerospikePolicy *as.ClientPolicy) error {
	scNsList := r.getSCNamespaces()
	r.Log.Info("Strong-consistency namespaces list", "namespaces", scNsList)

	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	for _, scNs := range scNsList {
		if err := r.validateClusterNsState(hostConns, aerospikePolicy, scNs); err != nil {
			return fmt.Errorf("cluster namespace state not good, can not set roster: %v", err)
		}

		// Setting roster is skipped if roster already set
		rosterNodes, err := r.getRosterNodesForNs(hostConns, aerospikePolicy, scNs)
		if err != nil {
			return err
		}

		if !isObservedNodesValid(rosterNodes) {
			return fmt.Errorf("roster observed_nodes not same for all the nodes: %v", rosterNodes)
		}

		err = r.setRosterForNs(hostConns, aerospikePolicy, scNs, rosterNodes)
		if err != nil {
			return err
		}

		err = r.runRecluster(hostConns, aerospikePolicy)
		if err != nil {
			return err
		}

		// TODO: validate cluster size with ns_cluster_sz
	}
	return nil
}

func (r *SingleClusterReconciler) getSCNamespaces() []string {
	var scNsList []string

	nsConfInterfaceList := r.aeroCluster.Spec.AerospikeConfig.Value["namespaces"].([]interface{})
	for _, nsConfInterface := range nsConfInterfaceList {
		nsConf := nsConfInterface.(map[string]interface{})
		sc, ok := nsConf["strong-consistency"]
		if ok && sc.(bool) {
			scNsList = append(scNsList, nsConf["name"].(string))
		}
	}
	return scNsList
}

func (r *SingleClusterReconciler) getRosterNodesForNs(hostConns []*deployment.HostConn, aerospikePolicy *as.ClientPolicy, ns string) (map[string]map[string]string, error) {
	r.Log.Info("Getting roster", "namespace", ns)

	rosterNodes := map[string]map[string]string{}

	for _, hostConn := range hostConns {
		kvmap, err := r.getRoster(hostConn, aerospikePolicy, ns)
		if err != nil {
			return nil, err
		}

		rosterNodes[hostConn.String()] = kvmap
	}

	r.Log.V(1).Info("roster nodes in cluster", "roster_nodes", rosterNodes)

	// // Check if all nodes have same observed nodes list
	// var tempObNode string
	// for _, obNode := range observedNodes {
	// 	if tempObNode == "" {
	// 		tempObNode = obNode
	// 	}
	// 	if tempObNode != obNode {
	// 		return "", fmt.Errorf("observed_nodes not same for all the nodes: %v", observedNodes)
	// 	}
	// }

	// // TODO: What if some nodes don't have a namespace. observed_nodes will be less than size in that case.
	// // // Check if observed nodes list has all the cluster nodes
	// // obNodesList := strings.Split(tempObNode, ",")
	// // if len(obNodesList) != int(r.aeroCluster.Spec.Size) {
	// // 	return "", fmt.Errorf("observed_nodes list does not have all the cluster nodes, observed_nodes: %v, cluster_size: %v", observedNodes, r.aeroCluster.Spec.Size)
	// // }

	// return tempObNode, nil
	return rosterNodes, nil
}

func isObservedNodesValid(rosterNodes map[string]map[string]string) bool {
	var tempObNodes string

	for _, rosterNodesMap := range rosterNodes {
		observedNodes := rosterNodesMap["observed_nodes"]
		// Check if all nodes have same observed nodes list
		if tempObNodes == "" {
			tempObNodes = observedNodes
			continue
		}
		if tempObNodes != observedNodes {
			return false
			// return "", fmt.Errorf("observed_nodes not same for all the nodes: %v", observedNodes)
		}
	}
	return true
}

func (r *SingleClusterReconciler) setRosterForNs(hostConns []*deployment.HostConn, aerospikePolicy *as.ClientPolicy, ns string, rosterNodes map[string]map[string]string) error {
	r.Log.Info("Setting roster", "namespace", ns, "roster", rosterNodes)

	for _, hostConn := range hostConns {

		observedNodes := rosterNodes[hostConn.String()]["observed_nodes"]
		currentRoster := rosterNodes[hostConn.String()]["roster"]

		if observedNodes == currentRoster {
			r.Log.Info("Roster already set for the node. Skipping", "node", hostConn.String())
			continue
		}

		if err := r.setRoster(hostConn, aerospikePolicy, ns, observedNodes); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) runRecluster(hostConns []*deployment.HostConn, aerospikePolicy *as.ClientPolicy) error {
	r.Log.Info("Run recluster")

	for _, hostConn := range hostConns {
		if err := r.recluster(hostConn, aerospikePolicy); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) validateClusterState(aerospikePolicy *as.ClientPolicy) error {
	scNsList := r.getSCNamespaces()

	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	for _, ns := range scNsList {
		if r.validateClusterNsState(hostConns, aerospikePolicy, ns); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) validateClusterNsState(hostConns []*deployment.HostConn, aerospikePolicy *as.ClientPolicy, ns string) error {
	r.Log.Info("Validate Cluster namespace State. Looking for unavailable or dead partitions", "namespaces", ns)

	for _, hostConn := range hostConns {

		kvmap, err := r.namespaceStats(hostConn, aerospikePolicy, ns)
		if err != nil {
			return err
		}

		// https://docs.aerospike.com/reference/metrics#unavailable_partitions
		// This is the number of partitions that are unavailable when roster nodes are missing.
		// Will turn into dead_partitions if still unavailable when all roster nodes are present.
		// Some partitions would typically be unavailable under some cluster split situations or
		// when removing more than replication-factor number of nodes from a strong-consistency enabled namespace
		if kvmap["unavailable_partitions"] != "0" {
			return fmt.Errorf("cluster namespace %s has non-zero unavailable_partitions %v", ns, kvmap["unavailable_partitions"])
		}

		// https://docs.aerospike.com/reference/metrics#dead_partitions
		if kvmap["dead_partitions"] != "0" {
			return fmt.Errorf("cluster namespace %s has non-zero dead_partitions %v", ns, kvmap["dead_partitions"])
		}
	}
	return nil
}

// Info calls

func (r *SingleClusterReconciler) recluster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy) error {
	cmd := "recluster:"
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd, "output", cmdOutput)

	if strings.ToLower(cmdOutput) != "ok" && strings.ToLower(cmdOutput) != "ignored-by-non-principal" {
		return fmt.Errorf("failed to run `%s` for cluster, %v", cmd, cmdOutput)
	}
	return nil
}

func (r *SingleClusterReconciler) namespaceStats(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("namespace/%s", namespace)
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd)

	return ParseInfoIntoMap(cmdOutput, ";", "=")
}

func (r *SingleClusterReconciler) setRoster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy, namespace, observedNodes string) error {
	cmd := fmt.Sprintf("roster-set:namespace=%s;nodes=%s", namespace, observedNodes)
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd, "output", cmdOutput)

	if strings.ToLower(cmdOutput) != "ok" {
		return fmt.Errorf("failed to set roster for namespace %s, %v", namespace, cmdOutput)
	}

	return nil
}

func (r *SingleClusterReconciler) getRoster(hostConn *deployment.HostConn, aerospikePolicy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("roster:namespace=%s", namespace)
	res, err := hostConn.ASConn.RunInfo(aerospikePolicy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd, "output", cmdOutput)

	return ParseInfoIntoMap(cmdOutput, ":", "=")
}
