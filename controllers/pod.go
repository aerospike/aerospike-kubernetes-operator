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

func (r *SingleClusterReconciler) getRollingRestartTypePod(
	rackState RackState, pod corev1.Pod,
) (RestartType, error) {

	restartType := NoRestart

	// AerospikeConfig nil means status not updated yet
	if r.aeroCluster.Status.AerospikeConfig == nil {
		return restartType, nil
	}

	cmName := getNamespacedNameForSTSConfigMap(r.aeroCluster, rackState.Rack.ID)
	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), cmName, confMap)
	if err != nil {
		return restartType, err
	}

	requiredConfHash := confMap.Data[AerospikeConfHashFileName]
	requiredNetworkPolicyHash := confMap.Data[NetworkPolicyHashFileName]
	requiredPodSpecHash := confMap.Data[PodSpecHashFileName]

	podStatus := r.aeroCluster.Status.Pods[pod.Name]

	// Check if aerospikeConfig is updated
	if podStatus.AerospikeConfigHash != requiredConfHash {
		restartType = mergeRestartType(restartType, QuickRestart)
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

func (r *SingleClusterReconciler) rollingRestartPod(
	rackState RackState, pod corev1.Pod, restartType RestartType,
	ignorablePods []corev1.Pod,
) reconcileResult {

	if restartType == NoRestart {
		r.Log.Info(
			"This Pod doesn't need rolling restart, Skip this", "pod", pod.Name,
		)
		return reconcileSuccess()
	}

	// Also check if statefulSet is in stable condition
	// Check for all containers. Status.ContainerStatuses doesn't include init container
	if pod.Status.ContainerStatuses == nil {
		return reconcileError(
			fmt.Errorf(
				"pod %s containerStatus is nil, pod may be in unscheduled state",
				pod.Name,
			),
		)
	}

	r.Log.Info("Rolling restart pod", "podName", pod.Name)
	var pFound *corev1.Pod

	for i := 0; i < 5; i++ {
		r.Log.V(1).Info("Waiting for pod to be ready", "podName", pod.Name)

		pFound = &corev1.Pod{}
		err := r.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace},
			pFound,
		)
		if err != nil {
			r.Log.Error(err, "Failed to get pod, try retry after 5 sec")
			time.Sleep(time.Second * 5)
			pFound = nil
			continue
		}

		if utils.IsPodRunningAndReady(pFound) {
			break
		}

		if utils.IsPodCrashed(pFound) {
			r.Log.Error(err, "Pod has crashed", "podName", pFound.Name)
			break
		}

		r.Log.Error(err, "Pod containerStatus is not ready, try after 5 sec")
		time.Sleep(time.Second * 5)
	}

	if pFound == nil {
		return reconcileError(fmt.Errorf("pod %s not ready", pod.Name))
	}

	err := utils.CheckPodFailed(pFound)
	if err == nil {
		// Check for migration
		if res := r.waitForNodeSafeStopReady(
			pFound, ignorablePods,
		); !res.isSuccess {
			return res
		}
	} else {
		// TODO: Check a user flag to restart failed pods.
		r.Log.Info("Restarting failed pod", "podName", pod.Name, "error", err)

		// The pod has failed. Quick start is not possible.
		restartType = PodRestart
	}

	if restartType == QuickRestart {
		return r.quickRestart(rackState, pFound)
	}

	return r.podRestart(pFound)
}

func (r *SingleClusterReconciler) quickRestart(
	rackState RackState, pod *corev1.Pod,
) reconcileResult {
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

		// Fallback to pod restart.
		return r.podRestart(pod)
	}

	r.Log.V(1).Info("Pod warm restarted", "podName", pod.Name)
	return reconcileSuccess()
}

func (r *SingleClusterReconciler) podRestart(pod *corev1.Pod) reconcileResult {
	var err error = nil

	// Delete pod
	if err := r.Client.Delete(context.TODO(), pod); err != nil {
		r.Log.Error(err, "Failed to delete pod")
		return reconcileError(err)
	}
	r.Log.V(1).Info("Pod deleted", "podName", pod.Name)

	// Wait for pod to come up
	var started bool
	for i := 0; i < 20; i++ {
		r.Log.V(1).Info(
			"Waiting for pod to be ready after delete", "podName", pod.Name,
			"status", pod.Status.Phase, "DeletionTimestamp",
			pod.DeletionTimestamp,
		)

		updatedPod := &corev1.Pod{}
		err := r.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace},
			updatedPod,
		)
		if err != nil {
			r.Log.Error(err, "Failed to get pod")
			time.Sleep(time.Second * 5)
			continue
		}

		if err := utils.CheckPodFailed(updatedPod); err != nil {
			return reconcileError(err)
		}

		if !utils.IsPodRunningAndReady(updatedPod) {
			r.Log.V(1).Info(
				"Waiting for pod to be ready", "podName", updatedPod.Name,
				"status", updatedPod.Status.Phase, "DeletionTimestamp",
				updatedPod.DeletionTimestamp,
			)
			time.Sleep(time.Second * 5)
			continue
		}

		r.Log.Info("Pod is restarted", "podName", updatedPod.Name)
		started = true
		break
	}

	// TODO: In what situation this can happen?
	if !started {
		r.Log.Error(
			err, "Pod is not running or ready. Pod might also be terminating",
			"podName", pod.Name, "status", pod.Status.Phase,
			"DeletionTimestamp", pod.DeletionTimestamp,
		)
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) deletePodAndEnsureImageUpdated(
	rackState RackState, p corev1.Pod, ignorablePods []corev1.Pod,
) reconcileResult {
	// If already dead node, so no need to check node safety, migration
	if err := utils.CheckPodFailed(&p); err == nil {
		if res := r.waitForNodeSafeStopReady(
			&p, ignorablePods,
		); !res.isSuccess {
			return res
		}
	}

	r.Log.V(1).Info("Delete the Pod", "podName", p.Name)
	// Delete pod
	if err := r.Client.Delete(context.TODO(), &p); err != nil {
		return reconcileError(err)
	}
	r.Log.V(1).Info("Pod deleted", "podName", p.Name)

	// Wait for pod to come up
	const maxRetries = 6
	const retryInterval = time.Second * 10
	for i := 0; i < maxRetries; i++ {
		r.Log.V(1).Info(
			"Waiting for pod to be ready after delete", "podName", p.Name,
		)

		pFound := &corev1.Pod{}
		err := r.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, pFound,
		)
		if err != nil {
			r.Log.Error(err, "Failed to get pod", "podName", p.Name, "err", err)

			if _, err = r.getSTS(rackState); err != nil {
				// Stateful set has been deleted.
				// TODO Ashish to remember which scenario this can happen.
				r.Log.Error(
					err, "Statefulset has been deleted for pod", "podName",
					p.Name, "err", err,
				)
				return reconcileError(err)
			}

			time.Sleep(retryInterval)
			continue
		}
		if err := utils.CheckPodFailed(pFound); err != nil {
			return reconcileError(err)
		}

		if utils.IsPodUpgraded(pFound, r.aeroCluster) {
			r.Log.Info("Pod is upgraded/downgraded", "podName", p.Name)
			return reconcileSuccess()
		}

		r.Log.V(1).Info(
			"Waiting for pod to come up with new image", "podName", p.Name,
		)
		time.Sleep(retryInterval)
	}

	r.Log.Info(
		"Timed out waiting for pod to come up with new image", "podName",
		p.Name,
	)
	return reconcileRequeueAfter(10)
}

// cleanupPods checks pods and status before scale-up to detect and fix any
//status anomalies.
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

			if utils.ContainsString(podNames, np.Name) {
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
			_ = r.tipClearHostname(&np, podName)

			_ = r.alumniReset(&np)
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

		_, ok := r.aeroCluster.Status.Pods[podName]
		if ok {
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
	if r.aeroCluster.Status.Pods != nil {
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
	}

	err := r.cleanupPods(danglingPods, rackState)
	if err != nil {
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

func (r *SingleClusterReconciler) isAnyPodInFailedState(podList []corev1.Pod) bool {

	for _, p := range podList {
		for _, ps := range p.Status.ContainerStatuses {
			// TODO: Should we use checkPodFailed or CheckPodImageFailed?
			// scaleDown, rollingRestart should work even if node is crashed
			// If node was crashed due to wrong config then only rollingRestart can bring it back.
			if err := utils.CheckPodImageFailed(&p); err != nil {
				r.Log.Info(
					"AerospikeCluster Pod is in failed state", "currentImage",
					ps.Image, "podName", p.Name, "err", err,
				)
				return true
			}
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
	addressName string, endpointsMap map[string]interface{},
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
