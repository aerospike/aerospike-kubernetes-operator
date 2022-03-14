package controllers

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v5"
)

func (r *SingleClusterReconciler) getAndSetRoster(policy *as.ClientPolicy) error {

	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	scNsList, err := r.getSCNamespaces(hostConns, policy)
	if err != nil {
		return err
	}

	r.Log.Info("Strong-consistency namespaces list", "namespaces", scNsList)

	for _, scNs := range scNsList {
		if err := r.validateClusterNsState(hostConns, policy, scNs); err != nil {
			return fmt.Errorf("cluster namespace state not good, can not set roster: %v", err)
		}

		// Setting roster is skipped if roster already set
		rosterNodes, err := r.getRosterNodesForNs(hostConns, policy, scNs)
		if err != nil {
			return err
		}

		// TODO: can sc namespaces be different in every rack..? in that case, observed_node will not be same for all
		if !isObservedNodesValid(rosterNodes) {
			return fmt.Errorf("roster observed_nodes not same for all the nodes: %v", rosterNodes)
		}

		err = r.setRosterForNs(hostConns, policy, scNs, rosterNodes)
		if err != nil {
			return err
		}

		err = r.runRecluster(hostConns, policy)
		if err != nil {
			return err
		}

		// TODO: validate cluster size with ns_cluster_sz
	}
	return nil
}

func (r *SingleClusterReconciler) getSCNamespaces(hostConns []*deployment.HostConn, policy *as.ClientPolicy) ([]string, error) {
	var scNsList []string

	for _, hostConn := range hostConns {
		namespaces, err := r.getNamespaces(policy, hostConn)
		if err != nil {
			return nil, err
		}

		var nsList []string
		for _, ns := range namespaces {
			isSC, err := r.isNamespaceSCEnabled(policy, hostConn, ns)
			if err != nil {
				return nil, err
			}
			if isSC {
				nsList = append(nsList, ns)
			}
		}

		if len(scNsList) == 0 {
			scNsList = nsList
		}
		if !reflect.DeepEqual(scNsList, nsList) {
			return nil, fmt.Errorf("SC namespaces list can not be different for nodes. node1 %v, node2 %v", scNsList, nsList)
		}
	}
	return scNsList, nil
}

func (r *SingleClusterReconciler) getRosterNodesForNs(hostConns []*deployment.HostConn, policy *as.ClientPolicy, ns string) (map[string]map[string]string, error) {
	r.Log.Info("Getting roster", "namespace", ns)

	rosterNodes := map[string]map[string]string{}

	for _, hostConn := range hostConns {
		kvmap, err := r.getRoster(hostConn, policy, ns)
		if err != nil {
			return nil, err
		}

		rosterNodes[hostConn.String()] = kvmap
	}

	r.Log.V(1).Info("roster nodes in cluster", "roster_nodes", rosterNodes)

	// // TODO: What if some nodes don't have a namespace. observed_nodes will be less than size in that case.
	// // There can be blacklisted nodes also
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
		}
	}
	return true
}

func (r *SingleClusterReconciler) setRosterForNs(hostConns []*deployment.HostConn, policy *as.ClientPolicy, ns string, rosterNodes map[string]map[string]string) error {
	r.Log.Info("Setting roster", "namespace", ns, "roster", rosterNodes)

	for _, hostConn := range hostConns {

		observedNodes := rosterNodes[hostConn.String()]["observed_nodes"]

		// Remove blacklisted node from observed_nodes
		observedNodesList := strings.Split(observedNodes, ",")
		var newObservedNodesList []string

		for _, obn := range observedNodesList {
			// nodeRoster := nodeID + "@" + fmt.Sprint(rackID)
			obnNodeID := strings.Split(obn, "@")[0]
			if !v1beta1.ContainsString(r.aeroCluster.Spec.RosterBlacklist, obnNodeID) {
				newObservedNodesList = append(newObservedNodesList, obn)
			}
		}

		newObservedNodes := strings.Join(newObservedNodesList, ",")

		currentRoster := rosterNodes[hostConn.String()]["roster"]

		if newObservedNodes == currentRoster {
			r.Log.Info("Roster already set for the node. Skipping", "node", hostConn.String())
			continue
		}

		if err := r.setRoster(hostConn, policy, ns, newObservedNodes); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) runRecluster(hostConns []*deployment.HostConn, policy *as.ClientPolicy) error {
	r.Log.Info("Run recluster")

	for _, hostConn := range hostConns {
		if err := r.recluster(hostConn, policy); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) validateClusterState(policy *as.ClientPolicy) error {

	hostConns, err := r.newAllHostConn()
	if err != nil {
		return err
	}

	scNsList, err := r.getSCNamespaces(hostConns, policy)
	if err != nil {
		return err
	}

	for _, ns := range scNsList {
		if err := r.validateClusterNsState(hostConns, policy, ns); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) validateClusterNsState(hostConns []*deployment.HostConn, policy *as.ClientPolicy, ns string) error {
	r.Log.Info("Validate Cluster namespace State. Looking for unavailable or dead partitions", "namespaces", ns)

	for _, hostConn := range hostConns {

		kvmap, err := r.namespaceStats(hostConn, policy, ns)
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

func (r *SingleClusterReconciler) recluster(hostConn *deployment.HostConn, policy *as.ClientPolicy) error {
	cmd := "recluster:"
	res, err := hostConn.ASConn.RunInfo(policy, cmd)
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

func (r *SingleClusterReconciler) namespaceStats(hostConn *deployment.HostConn, policy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("namespace/%s", namespace)
	res, err := hostConn.ASConn.RunInfo(policy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd)

	return ParseInfoIntoMap(cmdOutput, ";", "=")
}

func (r *SingleClusterReconciler) setRoster(hostConn *deployment.HostConn, policy *as.ClientPolicy, namespace, observedNodes string) error {
	cmd := fmt.Sprintf("roster-set:namespace=%s;nodes=%s", namespace, observedNodes)
	res, err := hostConn.ASConn.RunInfo(policy, cmd)
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

func (r *SingleClusterReconciler) getRoster(hostConn *deployment.HostConn, policy *as.ClientPolicy, namespace string) (map[string]string, error) {
	cmd := fmt.Sprintf("roster:namespace=%s", namespace)
	res, err := hostConn.ASConn.RunInfo(policy, cmd)
	if err != nil {
		return nil, err
	}

	cmdOutput := res[cmd]

	r.Log.V(1).Info("Run info command", "host", hostConn.String(), "cmd", cmd, "output", cmdOutput)

	return ParseInfoIntoMap(cmdOutput, ":", "=")
}

func (r *SingleClusterReconciler) isNodeInRoster(policy *as.ClientPolicy, hostConn *deployment.HostConn, ns string) (bool, error) {
	nodeID, err := r.getNodeID(policy, hostConn)
	if err != nil {
		return false, err
	}

	rosterNodesMap, err := r.getRoster(hostConn, policy, ns)
	if err != nil {
		return false, err
	}
	r.Log.Info("Check if node is in roster or not", "node", hostConn.String(), "roster", rosterNodesMap)

	rosterStr := rosterNodesMap["roster"]
	rosterList := strings.Split(rosterStr, ",")

	for _, roster := range rosterList {
		rosterNodeID := strings.Split(roster, "@")[0]
		if nodeID == rosterNodeID {
			return true, nil
		}
	}
	return false, nil
}

func (r *SingleClusterReconciler) isNamespaceSCEnabled(policy *as.ClientPolicy, hostConn *deployment.HostConn, ns string) (bool, error) {

	cmd := fmt.Sprintf("get-config:context=namespace;id=%s", ns)

	res, err := hostConn.ASConn.RunInfo(policy, cmd)
	if err != nil {
		return false, err
	}

	r.Log.Info("Check if namespace is SC namespace", "ns", ns, "nsstat", res)

	configs, err := ParseInfoIntoMap(res[cmd], ";", "=")
	if err != nil {
		return false, err
	}
	scstr, ok := configs["strong-consistency"]
	if !ok {
		return false, fmt.Errorf("strong-consistency config not found, config %v", res)
	}
	scbool, err := strconv.ParseBool(scstr)
	if err != nil {
		return false, err
	}

	return scbool, nil
}

func (r *SingleClusterReconciler) getNodeID(policy *as.ClientPolicy, hostConn *deployment.HostConn) (string, error) {
	cmd := "node"

	res, err := hostConn.ASConn.RunInfo(policy, cmd)
	if err != nil {
		return "", err
	}

	r.Log.Info("Get nodeID for host", "host", hostConn.String(), "cmd", cmd, "res", res)

	return res[cmd], nil
}

func (r *SingleClusterReconciler) getNamespaces(policy *as.ClientPolicy, hostConn *deployment.HostConn) ([]string, error) {
	cmd := "namespaces"
	res, err := hostConn.ASConn.RunInfo(policy, cmd)
	if err != nil {
		return nil, err
	}

	if len(res[cmd]) > 0 {
		return strings.Split(res[cmd], ";"), nil
	}
	return nil, nil
}
