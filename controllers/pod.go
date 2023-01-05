package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RestartType is the type of pod restart to use.
type RestartType int

const (
	// NoRestart needed.
	NoRestart RestartType = iota

	// PodRestart indicates that restart requires a restart of the pod.
	PodRestart

	// QuickRestart indicates that only Aerospike service can be restarted.
	QuickRestart
)

// mergeRestartType generates the updated restart type based on precedence.
// PodRestart > QuickRestart > NoRestart
func mergeRestartType(current, incoming RestartType) RestartType {
	if current == PodRestart || incoming == PodRestart {
		return PodRestart
	}

	if current == QuickRestart || incoming == QuickRestart {
		return QuickRestart
	}

	return NoRestart
}

// Fetching restartType of all pods, based on the operation being performed.
func (r *SingleClusterReconciler) getRollingRestartTypeMap(
	rackState RackState, pods []*corev1.Pod,
) (map[string]RestartType, error) {
	var restartTypeMap = make(map[string]RestartType)
	var addedNSDevices []string
	confMap, err := r.getConfigMap(rackState.Rack.ID)
	if err != nil {
		return nil, err
	}
	requiredConfHash := confMap.Data[AerospikeConfHashFileName]
	for i := range pods {
		podStatus := r.aeroCluster.Status.Pods[pods[i].Name]
		if addedNSDevices == nil && podStatus.AerospikeConfigHash != requiredConfHash {
			// Fetching all block devices that has been added in namespaces.
			addedNSDevices, err = r.getNSAddedDevices(rackState)
			if err != nil {
				return nil, err
			}
		}
		restartType, err := r.getRollingRestartTypePod(rackState, pods[i], confMap, addedNSDevices)
		if err != nil {
			return nil, err
		}
		restartTypeMap[pods[i].Name] = restartType
	}
	return restartTypeMap, nil
}

func (r *SingleClusterReconciler) getRollingRestartTypePod(
	rackState RackState, pod *corev1.Pod,
	confMap *corev1.ConfigMap, addedNSDevices []string,
) (RestartType, error) {

	restartType := NoRestart

	// AerospikeConfig nil means status not updated yet
	if r.aeroCluster.Status.AerospikeConfig == nil {
		return restartType, nil
	}

	requiredConfHash := confMap.Data[AerospikeConfHashFileName]
	requiredNetworkPolicyHash := confMap.Data[NetworkPolicyHashFileName]
	requiredPodSpecHash := confMap.Data[PodSpecHashFileName]

	podStatus := r.aeroCluster.Status.Pods[pod.Name]

	// Check if aerospikeConfig is updated
	if podStatus.AerospikeConfigHash != requiredConfHash {

		// checking if volumes added in namespace is part of dirtyVolumes.
		// if yes, then podRestart is needed.
		podRestartType, err := r.handleNSOrDeviceAddition(addedNSDevices, pod.Name)
		if err != nil {
			return restartType, err
		}
		restartType = mergeRestartType(restartType, podRestartType)
		r.Log.Info(
			"AerospikeConfig changed. Need rolling restart",
			"requiredHash", requiredConfHash,
			"currentHash", podStatus.AerospikeConfigHash,
		)
	}

	// Check if networkPolicy is updated
	if podStatus.NetworkPolicyHash != requiredNetworkPolicyHash {
		restartType = mergeRestartType(restartType, QuickRestart)
		r.Log.Info(
			"Aerospike network policy changed. Need rolling restart",
			"requiredHash", requiredNetworkPolicyHash,
			"currentHash", podStatus.NetworkPolicyHash,
		)
	}

	// Check if podSpec is updated
	if podStatus.PodSpecHash != requiredPodSpecHash {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info(
			"Aerospike pod spec changed. Need rolling restart",
			"requiredHash", requiredPodSpecHash,
			"currentHash", podStatus.PodSpecHash,
		)
	}

	// Check if rack storage is updated
	if r.isRackStorageUpdatedInAeroCluster(rackState, pod) {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("Aerospike rack storage changed. Need rolling restart")
	}

	return restartType, nil
}

func (r *SingleClusterReconciler) rollingRestartPods(
	rackState RackState, podsToRestart []*corev1.Pod, ignorablePods []corev1.Pod, restartTypeMap map[string]RestartType,
) reconcileResult {

	failedPods, activePods := getFailedAndActivePods(podsToRestart)

	// If already dead node (failed pod) then no need to check node safety, migration
	if len(failedPods) != 0 {
		r.Log.Info("Restart failed pods", "pods", getPodNames(failedPods))
		if res := r.restartPods(rackState, failedPods, restartTypeMap); !res.isSuccess {
			return res
		}
	}

	if len(activePods) != 0 {
		r.Log.Info("Restart active pods", "pods", getPodNames(activePods))
		if res := r.waitForMultipleNodesSafeStopReady(activePods, ignorablePods); !res.isSuccess {
			return res
		}
		if res := r.restartPods(rackState, activePods, restartTypeMap); !res.isSuccess {
			return res
		}
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) restartASDInPod(
	rackState RackState, pod *corev1.Pod,
) error {
	cmName := getNamespacedNameForSTSConfigMap(r.aeroCluster, rackState.Rack.ID)
	cmd := []string{
		"bash",
		"/etc/aerospike/refresh-cmap-restart-asd.sh",
		cmName.Namespace,
		cmName.Name,
	}

	// Quick restart attempt should not take significant time.
	// Therefore, it's ok to block the operator on the quick restart attempt.
	stdout, stderr, err := utils.Exec(
		pod, asdbv1beta1.AerospikeServerContainerName, cmd, r.KubeClient,
		r.KubeConfig,
	)
	if err != nil {
		r.Log.V(1).Info(
			"Failed warm restart", "err", err, "podName", pod.Name, "stdout",
			stdout, "stderr", stderr,
		)

		return err
	}
	r.Recorder.Eventf(r.aeroCluster, corev1.EventTypeNormal, "PodWarmRestarted",
		"[rack-%d] Restarted Pod %s", rackState.Rack.ID, pod.Name)

	r.Log.V(1).Info("Pod warm restarted", "podName", pod.Name)
	return nil
}

func (r *SingleClusterReconciler) restartPods(rackState RackState, podsToRestart []*corev1.Pod, restartTypeMap map[string]RestartType) reconcileResult {
	var restartedPods []*corev1.Pod
	for i := range podsToRestart {
		pod := podsToRestart[i]
		// Check if this pod needs restart
		restartType := restartTypeMap[pod.Name]

		if restartType == QuickRestart {
			if err := r.restartASDInPod(rackState, pod); err == nil {
				continue
			}
			// If ASD restart fails then go ahead and restart the pod
		}

		if err := r.Client.Delete(context.TODO(), pod); err != nil {
			r.Log.Error(err, "Failed to delete pod")
			return reconcileError(err)
		}
		restartedPods = append(restartedPods, pod)

		r.Log.V(1).Info("Pod deleted", "podName", pod.Name)
	}
	if len(restartedPods) > 0 {
		return r.ensurePodsRunningAndReady(restartedPods)
	}
	return reconcileSuccess()
}

func (r *SingleClusterReconciler) ensurePodsRunningAndReady(podsToCheck []*corev1.Pod) reconcileResult {
	podNames := getPodNames(podsToCheck)
	readyPods := map[string]bool{}

	const maxRetries = 6
	const retryInterval = time.Second * 10

	for i := 0; i < maxRetries; i++ {
		r.Log.V(1).Info("Waiting for pods to be ready after delete", "pods", podNames)

		for _, pod := range podsToCheck {
			if readyPods[pod.Name] {
				continue
			}

			r.Log.V(1).Info(
				"Waiting for pod to be ready", "podName", pod.Name,
				"status", pod.Status.Phase, "DeletionTimestamp",
				pod.DeletionTimestamp,
			)

			updatedPod := &corev1.Pod{}
			podName := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
			if err := r.Client.Get(context.TODO(), podName, updatedPod); err != nil {
				return reconcileError(err)
			}

			if err := utils.CheckPodFailed(updatedPod); err != nil {
				return reconcileError(err)
			}

			if !utils.IsPodRunningAndReady(updatedPod) {
				break
			}

			readyPods[pod.Name] = true

			r.Log.Info("Pod is restarted", "podName", updatedPod.Name)
			r.Recorder.Eventf(r.aeroCluster, corev1.EventTypeNormal, "PodRestarted",
				"[rack-%s] Restarted Pod %s", pod.Labels[asdbv1beta1.AerospikeRackIdLabel], pod.Name)
		}

		if len(readyPods) == len(podsToCheck) {
			r.Log.Info(
				"Pods are running and ready", "pods",
				podNames,
			)
			return reconcileSuccess()
		}
		time.Sleep(retryInterval)
	}

	r.Log.Info(
		"Timed out waiting for pods to come up", "pods",
		podNames,
	)
	return reconcileRequeueAfter(10)
}

func getRearrangedFailedAndActivePods(pods []*corev1.Pod) []*corev1.Pod {
	var rearrangedPods []*corev1.Pod

	failedPods, activePods := getFailedAndActivePods(pods)
	rearrangedPods = append(rearrangedPods, failedPods...)
	rearrangedPods = append(rearrangedPods, activePods...)

	return rearrangedPods
}

func getFailedAndActivePods(pods []*corev1.Pod) (failedPods, activePods []*corev1.Pod) {
	for i := range pods {
		pod := pods[i]

		if err := utils.CheckPodFailed(pod); err != nil {
			failedPods = append(failedPods, pod)
			continue
		}
		activePods = append(activePods, pod)
	}
	return failedPods, activePods
}

func (r *SingleClusterReconciler) safelyDeletePodsAndEnsureImageUpdated(
	rackState RackState, podsToUpdate []*corev1.Pod, ignorablePods []corev1.Pod,
) reconcileResult {

	failedPods, activePods := getFailedAndActivePods(podsToUpdate)

	// If already dead node (failed pod) then no need to check node safety, migration
	if len(failedPods) != 0 {
		r.Log.Info("Restart failed pods with updated container image", "pods", getPodNames(failedPods))
		if res := r.deletePodAndEnsureImageUpdated(rackState, failedPods); !res.isSuccess {
			return res
		}
	}

	if len(activePods) != 0 {
		r.Log.Info("Restart active pods with updated container image", "pods", getPodNames(failedPods))
		if res := r.waitForMultipleNodesSafeStopReady(activePods, ignorablePods); !res.isSuccess {
			return res
		}
		if res := r.deletePodAndEnsureImageUpdated(rackState, activePods); !res.isSuccess {
			return res
		}
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) deletePodAndEnsureImageUpdated(
	rackState RackState, podsToUpdate []*corev1.Pod) reconcileResult {

	// Delete pods
	for _, p := range podsToUpdate {
		if err := r.Client.Delete(context.TODO(), p); err != nil {
			return reconcileError(err)
		}
		r.Log.V(1).Info("Pod deleted", "podName", p.Name)

		r.Recorder.Eventf(r.aeroCluster, corev1.EventTypeNormal, "PodWaitUpdate",
			"[rack-%d] Waiting to update Pod %s", rackState.Rack.ID, p.Name)
	}

	return r.ensurePodsImageUpdated(podsToUpdate)
}

func (r *SingleClusterReconciler) ensurePodsImageUpdated(podsToCheck []*corev1.Pod) reconcileResult {
	podNames := getPodNames(podsToCheck)
	updatedPods := map[string]bool{}

	const maxRetries = 6
	const retryInterval = time.Second * 10

	for i := 0; i < maxRetries; i++ {
		r.Log.V(1).Info(
			"Waiting for pods to be ready after delete", "pods", podNames,
		)

		for _, pod := range podsToCheck {
			if updatedPods[pod.Name] {
				continue
			}

			r.Log.V(1).Info(
				"Waiting for pod to be ready", "podName", pod.Name,
			)

			updatedPod := &corev1.Pod{}
			podName := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}
			if err := r.Client.Get(context.TODO(), podName, updatedPod); err != nil {
				return reconcileError(err)
			}

			if err := utils.CheckPodFailed(updatedPod); err != nil {
				return reconcileError(err)
			}

			if !r.isPodUpgraded(updatedPod) {
				break
			}

			updatedPods[pod.Name] = true
			r.Log.Info("Pod is upgraded/downgraded", "podName", pod.Name)
		}

		if len(updatedPods) == len(podsToCheck) {
			r.Log.Info("Pods are upgraded/downgraded", "pod", podNames)
			return reconcileSuccess()
		}
		time.Sleep(retryInterval)
	}

	r.Log.Info(
		"Timed out waiting for pods to come up with new image", "pods",
		podNames,
	)
	return reconcileRequeueAfter(10)
}

// cleanupPods checks pods and status before scale-up to detect and fix any
// status anomalies.
func (r *SingleClusterReconciler) cleanupPods(
	podNames []string, rackState RackState,
) error {

	r.Log.Info("Removing pvc for removed pods", "pods", podNames)

	// Delete PVCs if cascadeDelete
	pvcItems, err := r.getPodsPVCList(podNames, rackState.Rack.ID)
	if err != nil {
		return fmt.Errorf("could not find pvc for pods %v: %v", podNames, err)
	}
	storage := rackState.Rack.Storage
	if err := r.removePVCs(&storage, pvcItems); err != nil {
		return fmt.Errorf("could not cleanup pod PVCs: %v", err)
	}

	var needStatusCleanup []string

	clusterPodList, err := r.getClusterPodList()
	if err != nil {
		return fmt.Errorf("could not cleanup pod PVCs: %v", err)
	}

	podNameSet := sets.NewString(podNames...)

	for _, podName := range podNames {
		// Clear references to this pod in the running cluster.
		for _, np := range clusterPodList.Items {
			if !utils.IsPodRunningAndReady(&np) {
				r.Log.Info(
					"Pod is not running and ready. Skip clearing from tipHostnames.",
					"pod", np.Name, "host to clear", podNames,
				)
				continue
			}

			if podNameSet.Has(np.Name) {
				// Skip running info commands on pods which are being cleaned
				// up.
				continue
			}

			// TODO: We remove node from the end. Nodes will not have seed of successive nodes
			// So this will be no op.
			// We should tip in all nodes the same seed list,
			// then only this will have any impact. Is it really necessary?

			// TODO: tip after scale-up and create
			// All nodes from other rack
			r.Log.Info(
				"About to remove host from tipHostnames and reset alumni in pod...",
				"pod to remove", podName, "remove and reset on pod", np.Name,
			)

			if err := r.tipClearHostname(&np, podName); err != nil {
				r.Log.V(2).Info("Failed to tipClear", "hostName", podName, "from the pod", np.Name)
			}

			if err := r.alumniReset(&np); err != nil {
				r.Log.V(2).Info(fmt.Sprintf("Failed to reset alumni for the pod %s", np.Name))
			}
		}

		if r.aeroCluster.Spec.PodSpec.MultiPodPerHost {
			// Remove service for pod
			// TODO: make it more robust, what if it fails
			if err := r.deletePodService(
				podName, r.aeroCluster.Namespace,
			); err != nil {
				return err
			}
		}

		if _, ok := r.aeroCluster.Status.Pods[podName]; ok {
			needStatusCleanup = append(needStatusCleanup, podName)
		}
	}

	if len(needStatusCleanup) > 0 {
		r.Log.Info("Removing pod status for dangling pods", "pods", podNames)

		if err := r.removePodStatus(needStatusCleanup); err != nil {
			return fmt.Errorf("could not cleanup pod status: %v", err)
		}
	}

	return nil
}

// removePodStatus removes podNames from the cluster's pod status.
// Assumes the pods are not running so that the no concurrent update to this pod status is possible.
func (r *SingleClusterReconciler) removePodStatus(podNames []string) error {
	if len(podNames) == 0 {
		return nil
	}

	var patches []jsonpatch.JsonPatchOperation

	for _, podName := range podNames {
		patch := jsonpatch.JsonPatchOperation{
			Operation: "remove",
			Path:      "/status/pods/" + podName,
		}
		patches = append(patches, patch)
	}

	jsonPatchJSON, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("error creating json-patch : %v", err)
	}

	constantPatch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

	// Since the pod status is updated from pod init container,
	//set the field owner to "pod" for pod status updates.
	if err = r.Client.Status().Patch(
		context.TODO(), r.aeroCluster, constantPatch, client.FieldOwner("pod"),
	); err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) cleanupDanglingPodsRack(
	sts *appsv1.StatefulSet, rackState RackState,
) error {
	// Clean up any dangling resources associated with the new pods.
	// This implements a safety net to protect scale up against failed cleanup operations when cluster
	// is scaled down.
	var danglingPods []string

	// Find dangling pods in pods
	for podName := range r.aeroCluster.Status.Pods {
		rackID, err := utils.GetRackIDFromPodName(podName)
		if err != nil {
			return fmt.Errorf(
				"failed to get rackID for the pod %s", podName,
			)
		}
		if *rackID != rackState.Rack.ID {
			// This pod is from other rack, so skip it
			continue
		}

		ordinal, err := getSTSPodOrdinal(podName)
		if err != nil {
			return fmt.Errorf("invalid pod name: %s", podName)
		}

		if *ordinal >= *sts.Spec.Replicas {
			danglingPods = append(danglingPods, podName)
		}
	}

	if err := r.cleanupPods(danglingPods, rackState); err != nil {
		return fmt.Errorf("failed dangling pod cleanup: %v", err)
	}

	return nil
}

// getIgnorablePods returns pods from racksToDelete that are currently not running and can be ignored in stability checks.
func (r *SingleClusterReconciler) getIgnorablePods(racksToDelete []asdbv1beta1.Rack) (
	[]corev1.Pod, error,
) {
	var ignorablePods []corev1.Pod
	for _, rack := range racksToDelete {
		rackPods, err := r.getRackPodList(rack.ID)

		if err != nil {
			return nil, err
		}

		for _, pod := range rackPods.Items {
			if !utils.IsPodRunningAndReady(&pod) {
				ignorablePods = append(ignorablePods, pod)
			}
		}
	}
	return ignorablePods, nil
}

// nolint:unused
// getPodIPs returns the pod IP, host internal IP and the host external IP unless there is an error.
// Note: the IPs returned from here should match the IPs generated in the pod initialization script for the init container.
func (r *SingleClusterReconciler) getPodIPs(pod *corev1.Pod) (
	string, string, string, error,
) {
	podIP := pod.Status.PodIP
	hostInternalIP := pod.Status.HostIP
	hostExternalIP := hostInternalIP

	k8sNode := &corev1.Node{}
	err := r.Client.Get(
		context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode,
	)
	if err != nil {
		return "", "", "", fmt.Errorf(
			"failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName,
			pod.Name, err,
		)
	}
	// If externalIP is present than give external ip
	for _, add := range k8sNode.Status.Addresses {
		if add.Type == corev1.NodeExternalIP && add.Address != "" {
			hostExternalIP = add.Address
		} else if add.Type == corev1.NodeInternalIP && add.Address != "" {
			hostInternalIP = add.Address
		}
	}

	return podIP, hostInternalIP, hostExternalIP, nil
}

// nolint:unused
func (r *SingleClusterReconciler) getServiceForPod(pod *corev1.Pod) (
	*corev1.Service, error,
) {
	service := &corev1.Service{}
	err := r.Client.Get(
		context.TODO(),
		types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get service for pod %s: %v", pod.Name, err,
		)
	}
	return service, nil
}

func (r *SingleClusterReconciler) getPodsPVCList(
	podNames []string, rackID int,
) ([]corev1.PersistentVolumeClaim, error) {
	pvcListItems, err := r.getRackPVCList(rackID)
	if err != nil {
		return nil, err
	}
	// https://github.com/kubernetes/kubernetes/issues/72196
	// No regex support in field-selector
	// Can not get pvc having matching podName. Need to check more.
	var newPVCItems []corev1.PersistentVolumeClaim
	for _, pvc := range pvcListItems {
		for _, podName := range podNames {
			// Get PVC belonging to pod only
			if strings.HasSuffix(pvc.Name, podName) {
				newPVCItems = append(newPVCItems, pvc)
			}
		}
	}
	return newPVCItems, nil
}

func (r *SingleClusterReconciler) getClusterPodList() (
	*corev1.PodList, error,
) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(r.aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := r.Client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *SingleClusterReconciler) isAnyPodInImageFailedState(podList []corev1.Pod) bool {

	for _, p := range podList {
		// TODO: Should we use checkPodFailed or CheckPodImageFailed?
		// scaleDown, rollingRestart should work even if node is crashed
		// If node was crashed due to wrong config then only rollingRestart can bring it back.
		if err := utils.CheckPodImageFailed(&p); err != nil {
			r.Log.Info(
				"AerospikeCluster Pod is in failed state", "podName", p.Name, "err", err,
			)
			return true
		}
	}
	return false
}

func getFQDNForPod(
	aeroCluster *asdbv1beta1.AerospikeCluster, host string,
) string {
	return fmt.Sprintf(
		"%s.%s.%s.svc.cluster.local", host, aeroCluster.Name,
		aeroCluster.Namespace,
	)
}

// GetEndpointsFromInfo returns the aerospike service endpoints as a slice of host:port elements named addressName from the info endpointsMap. It returns an empty slice if the access address with addressName is not found in endpointsMap.
//
// E.g. addressName are access, alternate-access
func GetEndpointsFromInfo(
	addressName string, endpointsMap map[string]string,
) []string {
	var endpoints []string

	portStr, ok := endpointsMap["service."+addressName+"-port"]
	if !ok {
		return endpoints
	}

	port, err := strconv.ParseInt(fmt.Sprintf("%v", portStr), 10, 32)

	if err != nil || port == 0 {
		return endpoints
	}

	hostsStr, ok := endpointsMap["service."+addressName+"-addresses"]
	if !ok {
		return endpoints
	}

	hosts := strings.Split(fmt.Sprintf("%v", hostsStr), ",")

	for _, host := range hosts {
		endpoints = append(
			endpoints, net.JoinHostPort(host, strconv.Itoa(int(port))),
		)
	}
	return endpoints
}

func getPodNames(pods []*corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

func (r *SingleClusterReconciler) handleNSOrDeviceRemoval(rackState RackState, podsToRestart []*corev1.Pod) error {
	var rackStatus asdbv1beta1.Rack
	var removedDevices []string
	var removedFiles []string

	rackFound := false
	for _, rackStatus = range r.aeroCluster.Status.RackConfig.Racks {
		if rackStatus.ID == rackState.Rack.ID {
			rackFound = true
			break
		}
	}

	if !rackFound {
		r.Log.Info("Could not find rack status, skipping namespace device handling", "ID", rackState.Rack.ID)
		return nil
	}

	for _, statusNamespace := range rackStatus.AerospikeConfig.Value["namespaces"].([]interface{}) {
		namespaceFound := false
		for _, specNamespace := range rackState.Rack.AerospikeConfig.Value["namespaces"].([]interface{}) {
			if specNamespace.(map[string]interface{})["name"] == statusNamespace.(map[string]interface{})["name"] {
				namespaceFound = true
				specStorage := specNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})
				statusStorage := statusNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})

				statusDevices := sets.String{}
				specDevices := sets.String{}
				if statusStorage["devices"] != nil {
					for _, statusDeviceInterface := range statusStorage["devices"].([]interface{}) {
						statusDevices.Insert(strings.Fields(statusDeviceInterface.(string))...)
					}
				}
				if specStorage["devices"] != nil {
					for _, specDeviceInterface := range specStorage["devices"].([]interface{}) {
						specDevices.Insert(strings.Fields(specDeviceInterface.(string))...)
					}
				}

				removedDevicesPerNS := statusDevices.Difference(specDevices).List()
				for _, removedDevice := range removedDevicesPerNS {
					deviceName := getVolumeNameFromDevicePath(rackStatus.Storage.Volumes, removedDevice)
					r.Log.Info(
						"Device is removed from namespace", "device", deviceName, "namespace", specNamespace.(map[string]interface{})["name"],
					)
					removedDevices = append(removedDevices, deviceName)
				}

				statusFiles := sets.String{}
				specFiles := sets.String{}
				if statusStorage["files"] != nil {
					for _, statusFileInterface := range statusStorage["files"].([]interface{}) {
						statusFiles.Insert(strings.Fields(statusFileInterface.(string))...)
					}
				}
				if specStorage["files"] != nil {
					for _, specFileInterface := range specStorage["files"].([]interface{}) {
						specFiles.Insert(strings.Fields(specFileInterface.(string))...)
					}
				}

				removedFilesPerNS := statusFiles.Difference(specFiles).List()
				if len(removedFilesPerNS) > 0 {
					removedFiles = append(removedFiles, removedFilesPerNS...)
				}
				break
			}
		}
		if !namespaceFound {
			r.Log.Info(
				"Namespace is deleted", "namespace", statusNamespace.(map[string]interface{})["name"],
			)
			statusStorage := statusNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})

			if statusStorage["devices"] != nil {
				var statusDevices []string
				for _, statusDeviceInterface := range statusStorage["devices"].([]interface{}) {
					statusDevices = append(statusDevices, strings.Fields(statusDeviceInterface.(string))...)
				}
				for _, statusDevice := range statusDevices {
					deviceName := getVolumeNameFromDevicePath(rackStatus.Storage.Volumes, statusDevice)
					removedDevices = append(removedDevices, deviceName)
				}
			}
			if statusStorage["files"] != nil {
				var statusFiles []string
				for _, statusFileInterface := range statusStorage["files"].([]interface{}) {
					statusFiles = append(statusFiles, strings.Fields(statusFileInterface.(string))...)
				}
				removedFiles = append(removedFiles, statusFiles...)
			}
		}
	}

	for _, pod := range podsToRestart {
		r.Log.Info("handleNSOrDeviceRemovalPerPod call", "pod", pod.Name)
		err := r.handleNSOrDeviceRemovalPerPod(removedDevices, removedFiles, pod)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SingleClusterReconciler) handleNSOrDeviceRemovalPerPod(removedDevices []string, removedFiles []string, pod *corev1.Pod) error {

	podStatus := r.aeroCluster.Status.Pods[pod.Name]

	for _, file := range removedFiles {
		err := r.deleteFileStorage(pod, file)
		if err != nil {
			return err
		}
	}
	if len(removedDevices) > 0 {
		dirtyVolumes := sets.String{}
		dirtyVolumes.Insert(removedDevices...)
		dirtyVolumes.Insert(podStatus.DirtyVolumes...)
		var patches []jsonpatch.JsonPatchOperation

		patch1 := jsonpatch.JsonPatchOperation{
			Operation: "replace",
			Path:      "/status/pods/" + pod.Name + "/dirtyVolumes",
			Value:     dirtyVolumes.List(),
		}

		patches = append(patches, patch1)

		jsonPatchJSON, err := json.Marshal(patches)
		if err != nil {
			return err
		}
		constantPatch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

		// Since the pod status is updated from pod init container,
		//set the field owner to "pod" for pod status updates.
		if err = r.Client.Status().Patch(
			context.TODO(), r.aeroCluster, constantPatch, client.FieldOwner("pod"),
		); err != nil {
			return fmt.Errorf("error updating status: %v", err)
		}
	}
	return nil
}

func (r *SingleClusterReconciler) getNSAddedDevices(rackState RackState) ([]string, error) {
	var rackStatus asdbv1beta1.Rack
	var err error
	var volumes []string

	newAeroCluster := &asdbv1beta1.AerospikeCluster{}
	err = r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	)
	if err != nil {
		return nil, err
	}

	rackFound := false
	for _, rackStatus = range newAeroCluster.Status.RackConfig.Racks {
		if rackStatus.ID == rackState.Rack.ID {
			rackFound = true
			break
		}
	}

	if !rackFound {
		r.Log.Info("Could not find rack status, skipping namespace device handling", "ID", rackState.Rack.ID)
		return nil, nil
	}

	for _, specNamespace := range rackState.Rack.AerospikeConfig.Value["namespaces"].([]interface{}) {
		namespaceFound := false
		for _, statusNamespace := range rackStatus.AerospikeConfig.Value["namespaces"].([]interface{}) {
			if specNamespace.(map[string]interface{})["name"] == statusNamespace.(map[string]interface{})["name"] {
				namespaceFound = true
				specStorage := specNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})
				statusStorage := statusNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})

				var specDevices []string
				statusDevices := sets.String{}
				if specStorage["devices"] != nil {
					for _, specDeviceInterface := range specStorage["devices"].([]interface{}) {
						specDevices = append(specDevices, strings.Fields(specDeviceInterface.(string))...)
					}
				}
				if statusStorage["devices"] != nil {
					for _, statusDeviceInterface := range statusStorage["devices"].([]interface{}) {
						statusDevices.Insert(strings.Fields(statusDeviceInterface.(string))...)
					}
				}
				for _, specDevice := range specDevices {
					if !statusDevices.Has(specDevice) {
						r.Log.Info(
							"Device is added in namespace",
						)
						deviceName := getVolumeNameFromDevicePath(rackState.Rack.Storage.Volumes, specDevice)
						volumes = append(volumes, deviceName)
					}
				}
				break
			}
		}
		if !namespaceFound {
			r.Log.Info(
				"Namespace added",
			)
			specStorage := specNamespace.(map[string]interface{})["storage-engine"].(map[string]interface{})
			if specStorage["type"] == "device" && specStorage["devices"] != nil {
				var specDevices []string
				for _, specDeviceInterface := range specStorage["devices"].([]interface{}) {
					specDevices = append(specDevices, strings.Fields(specDeviceInterface.(string))...)
				}
				for _, specDevice := range specDevices {
					deviceName := getVolumeNameFromDevicePath(rackState.Rack.Storage.Volumes, specDevice)
					volumes = append(volumes, deviceName)
				}
			}
		}
	}
	return volumes, nil
}

func (r *SingleClusterReconciler) handleNSOrDeviceAddition(volumes []string, podName string) (RestartType, error) {
	podStatus := r.aeroCluster.Status.Pods[podName]
	for _, volume := range volumes {
		r.Log.Info(
			"Checking dirty volumes list", "device", volume, "podname", podName,
		)
		if utils.ContainsString(podStatus.DirtyVolumes, volume) {
			return PodRestart, nil
		}
	}

	return QuickRestart, nil
}

func getVolumeNameFromDevicePath(volumes []asdbv1beta1.VolumeSpec, s string) string {
	for _, volume := range volumes {
		if volume.Aerospike.Path == s {
			return volume.Name
		}
	}
	return ""
}

func (r *SingleClusterReconciler) deleteFileStorage(pod *corev1.Pod, fileName string) error {

	cmd := []string{
		"bash", "-c", fmt.Sprintf(
			"rm -rf %s",
			fileName,
		),
	}
	r.Log.Info(
		"Deleting file", "file", fileName, "cmd", cmd, "podname", pod.Name,
	)
	stdout, stderr, err := utils.Exec(pod, asdbv1beta1.AerospikeServerContainerName, cmd, r.KubeClient, r.KubeConfig)

	if err != nil {
		r.Log.V(1).Info(
			"File deletion failed", "err", err, "podName", pod.Name, "stdout",
			stdout, "stderr", stderr,
		)
		return fmt.Errorf("error deleting file %v", err)
	}
	return nil
}

func (r *SingleClusterReconciler) getConfigMap(rackID int) (*corev1.ConfigMap, error) {
	cmName := getNamespacedNameForSTSConfigMap(r.aeroCluster, rackID)
	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), cmName, confMap)
	if err != nil {
		return nil, err
	}
	return confMap, nil
}
