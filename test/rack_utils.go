package test

import (
	goctx "context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/info"
)

type RackState struct {
	Rack asdbv1.Rack
	Size int
}

func addRack(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, rack *asdbv1.Rack,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}
	// Remove default rack
	defaultRackID := asdbv1.DefaultRackID
	if len(aeroCluster.Spec.RackConfig.Racks) == 1 && aeroCluster.Spec.RackConfig.Racks[0].ID == defaultRackID {
		aeroCluster.Spec.RackConfig = asdbv1.RackConfig{Racks: []asdbv1.Rack{}}
	}

	aeroCluster.Spec.RackConfig.Racks = append(
		aeroCluster.Spec.RackConfig.Racks, *rack,
	)
	// Size shouldn't make any difference in working. Still put different size to check if it create any issue.
	aeroCluster.Spec.Size++

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func removeLastRack(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	racks := aeroCluster.Spec.RackConfig.Racks
	if len(racks) > 0 {
		racks = racks[:len(racks)-1]
	}

	aeroCluster.Spec.RackConfig.Racks = racks
	aeroCluster.Spec.Size--
	// This will also indirectly check if older rack is removed or not.
	// If older node is not deleted then cluster sz will not be as expected

	return updateCluster(k8sClient, ctx, aeroCluster)
}

func validateAerospikeConfigServiceUpdate(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName, rack *asdbv1.Rack,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}

	var found bool

	for podName := range aeroCluster.Status.Pods {
		pod := aeroCluster.Status.Pods[podName]
		if !isNodePartOfRack(pod.Aerospike.NodeID, strconv.Itoa(rack.ID)) {
			continue
		}

		found = true
		// TODO:
		// We may need to check for all keys in aerospikeConfig in rack
		// but we know that we are changing for service only for now
		host, err := createHost(&pod)
		if err != nil {
			return err
		}

		asinfo := info.NewAsInfo(
			log, host, getClientPolicy(aeroCluster, k8sClient),
		)

		confs, err := getAsConfig(asinfo, "service")
		if err != nil {
			return err
		}

		svcConfs := confs["service"].(lib.Stats)

		for k, v := range rack.InputAerospikeConfig.Value["service"].(map[string]interface{}) {
			if vint, ok := v.(int); ok {
				v = int64(vint)
			}

			cv, ok := svcConfs[k]
			if !ok {
				return fmt.Errorf(
					"config %s missing in aerospikeConfig %v", k, svcConfs,
				)
			}

			if !reflect.DeepEqual(cv, v) {
				return fmt.Errorf(
					"config %s mismatch with config. got %v:%T, want %v:%T, aerospikeConfig %v",
					k, cv, cv, v, v, svcConfs,
				)
			}
		}
	}

	if !found {
		return fmt.Errorf(
			"no pod found in for rack. Pods %v, Rack %v",
			aeroCluster.Status.Pods, *rack,
		)
	}

	return nil
}

func isNamespaceRackEnabled(
	log logr.Logger, k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
	nsName string,
) (bool, error) {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return false, err
	}

	if len(aeroCluster.Status.Pods) == 0 {
		return false, fmt.Errorf("cluster has empty pod list in status")
	}

	var pod asdbv1.AerospikePodStatus
	for podName := range aeroCluster.Status.Pods {
		pod = aeroCluster.Status.Pods[podName]
	}

	host, err := createHost(&pod)
	if err != nil {
		return false, err
	}

	asinfo := info.NewAsInfo(log, host, getClientPolicy(aeroCluster, k8sClient))

	confs, err := getAsConfig(asinfo, "racks")
	if err != nil {
		return false, err
	}

	for _, rackConf := range confs["racks"].([]lib.Stats) {
		// rack_0 is form non-rack namespace. So if rack_0 is present then it's not rack enabled
		_, ok := rackConf["rack_0"]

		ns := rackConf.TryString("ns", "")
		if ns == nsName && !ok {
			return true, nil
		}
	}

	return false, nil
}

func getPodSpecAnnotations(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) ([]map[string]string, error) {
	annotations := make([]map[string]string, 0)

	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	rackStateList := getConfiguredRackStateList(aeroCluster)

	for rackStateIndex := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, rackStateList[rackStateIndex].Rack.ID)

		err := k8sClient.Get(ctx, stsName, found)
		if errors.IsNotFound(err) {
			// statefulset should exist
			return nil, err
		}

		annotations = append(annotations, found.Spec.Template.ObjectMeta.Annotations)
	}

	return annotations, nil
}

func getPodSpecLabels(
	k8sClient client.Client, ctx goctx.Context, clusterNamespacedName types.NamespacedName,
) ([]map[string]string, error) {
	ls := make([]map[string]string, 0)

	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return nil, err
	}

	rackStateList := getConfiguredRackStateList(aeroCluster)
	for rackStateIndex := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, rackStateList[rackStateIndex].Rack.ID)

		err := k8sClient.Get(ctx, stsName, found)
		if errors.IsNotFound(err) {
			// statefulset should exist
			return nil, err
		}

		ls = append(ls, found.Spec.Template.ObjectMeta.Labels)
	}

	return ls, nil
}

func validateRackEnabledCluster(
	k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName,
) error {
	aeroCluster, err := getCluster(k8sClient, ctx, clusterNamespacedName)
	if err != nil {
		return err
	}
	// Validate cluster
	rackStateList := getConfiguredRackStateList(aeroCluster)
	for rackStateIndex := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(
			aeroCluster, rackStateList[rackStateIndex].Rack.ID,
		)

		err := k8sClient.Get(ctx, stsName, found)

		if errors.IsNotFound(err) {
			// statefulset should exist
			return err
		}

		// Match size
		if int(*found.Spec.Replicas) != rackStateList[rackStateIndex].Size {
			return fmt.Errorf(
				"statefulset replica size %d, want %d",
				int(*found.Spec.Replicas), rackStateList[rackStateIndex].Size,
			)
		}
		// If Label key are changed for zone, region.. then those should be changed here also

		// Match NodeAffinity, if something else is used in place of affinity then it will fail
		err = validateSTSForRack(found, &rackStateList[rackStateIndex])
		if err != nil {
			return err
		}

		// Match Pod's Node
		err = validateSTSPodsForRack(k8sClient, ctx, found, &rackStateList[rackStateIndex])
		if err != nil {
			return err
		}
	}

	return nil
}

func validateSTSForRack(found *appsv1.StatefulSet, rackState *RackState) error {
	rackLabelKey := "RackLabel"
	hostKey := "kubernetes.io/hostname"
	rackSelectorMap := map[string]string{}

	if rackState.Rack.Zone != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      zoneKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Zone},
		}
		rackSelectorMap[zoneKey] = val.String()
	}

	if rackState.Rack.Region != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      regionKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Region},
		}
		rackSelectorMap[regionKey] = val.String()
	}

	if rackState.Rack.RackLabel != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      rackLabelKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.RackLabel},
		}
		rackSelectorMap[rackLabelKey] = val.String()
	}

	if rackState.Rack.NodeName != "" {
		val := corev1.NodeSelectorRequirement{
			Key:      hostKey,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.NodeName},
		}
		rackSelectorMap[hostKey] = val.String()
	}

	if len(rackSelectorMap) == 0 {
		return nil
	}

	terms := found.Spec.Template.Spec.Affinity.NodeAffinity.
		RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms

	var matched bool

	for _, term := range terms {
		expMap := map[string]string{}
		for _, exp := range term.MatchExpressions {
			expMap[exp.Key] = exp.String()
		}

		matched = reflect.DeepEqual(rackSelectorMap, expMap)
		if matched {
			break
		}
	}

	if !matched {
		return fmt.Errorf(
			"statefulset doesn't have required match strings. terms %v", terms,
		)
	}

	return nil
}

func validateSTSPodsForRack(
	k8sClient client.Client, ctx goctx.Context, found *appsv1.StatefulSet,
	rackState *RackState,
) error {
	rackLabelKey := "RackLabel"
	hostKey := "kubernetes.io/hostname"

	rackSelectorMap := map[string]string{}
	if rackState.Rack.Zone != "" {
		rackSelectorMap[zoneKey] = rackState.Rack.Zone
	}

	if rackState.Rack.Region != "" {
		rackSelectorMap[regionKey] = rackState.Rack.Region
	}

	if rackState.Rack.RackLabel != "" {
		rackSelectorMap[rackLabelKey] = rackState.Rack.RackLabel
	}

	if rackState.Rack.NodeName != "" {
		rackSelectorMap[hostKey] = rackState.Rack.NodeName
	}

	rackPodList, err := getRackPodList(k8sClient, ctx, found)
	if err != nil {
		return err
	}

	for podIndex := range rackPodList.Items {
		node := &corev1.Node{}
		err := k8sClient.Get(
			ctx, types.NamespacedName{Name: rackPodList.Items[podIndex].Spec.NodeName}, node,
		)

		if err != nil {
			return err
		}

		for k, v1 := range rackSelectorMap {
			if v2, ok := node.Labels[k]; !ok {
				// error
				return fmt.Errorf(
					"rack key %s, not present in node labels %v", k,
					node.Labels,
				)
			} else if v1 != v2 {
				// error
				return fmt.Errorf(
					"rack key:val %s:%s doesn't match in node labels %v", k, v1,
					node.Labels,
				)
			}
		}
	}

	return nil
}

func getConfiguredRackStateList(aeroCluster *asdbv1.AerospikeCluster) []RackState {
	topology := splitRacks(
		int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks),
	)

	rackStateList := make([]RackState, 0, len(topology))

	for idx := range aeroCluster.Spec.RackConfig.Racks {
		if topology[idx] == 0 {
			// Skip the rack, if it's size is 0
			continue
		}

		rackStateList = append(
			rackStateList, RackState{
				Rack: aeroCluster.Spec.RackConfig.Racks[idx],
				Size: topology[idx],
			},
		)
	}

	return rackStateList
}

// TODO: Update this
func splitRacks(nodeCount, rackCount int) []int {
	nodesPerRack, extraNodes := nodeCount/rackCount, nodeCount%rackCount

	var topology []int

	for rackIdx := 0; rackIdx < rackCount; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}

		topology = append(topology, nodesForThisRack)
	}

	return topology
}

func getNamespacedNameForStatefulSet(
	aeroCluster *asdbv1.AerospikeCluster, rackID int,
) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func getRackPodList(
	k8sClient client.Client, ctx goctx.Context, found *appsv1.StatefulSet,
) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(found.Spec.Template.Labels)
	listOps := &client.ListOptions{
		Namespace: found.Namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(ctx, podList, listOps); err != nil {
		return nil, err
	}

	return podList, nil
}

func isNodePartOfRack(nodeID, rackID string) bool {
	// NODE_ID="$RACK_ID$NODE_ID",  NODE_ID -> aINT
	lNodeID := strings.ToLower(nodeID)
	tokens := strings.Split(lNodeID, "a")
	// len(tokens) can not be less than 2 if rack is there
	return rackID == tokens[0]
}

func getDummyRackConf(rackIDs ...int) []asdbv1.Rack {
	racks := make([]asdbv1.Rack, 0, len(rackIDs))
	for _, rID := range rackIDs {
		racks = append(racks, asdbv1.Rack{ID: rID})
	}

	return racks
}
