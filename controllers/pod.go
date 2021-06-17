package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
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

// mergeRestartType generates the updated restart type based on precendence.
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

func (r *AerospikeClusterReconciler) getRollingRestartTypePod(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) (RestartType, error) {

	restartType := NoRestart

	// AerospikeConfig nil means status not updated yet
	if aeroCluster.Status.AerospikeConfig == nil {
		return restartType, nil
	}

	cmName := getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID)
	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), cmName, confMap)
	if err != nil {
		return restartType, err
	}

	requiredConfHash := confMap.Data[AerospikeConfHashFileName]
	requiredNetworkPolicyHash := confMap.Data[NetworkPolicyHashFileName]
	requiredPodSpecHash := confMap.Data[PodSpecHashFileName]

	podStatus := aeroCluster.Status.Pods[pod.Name]

	// Check if aerospikeConfig is updated
	if podStatus.AerospikeConfigHash != requiredConfHash {
		restartType = mergeRestartType(restartType, QuickRestart)
		r.Log.Info("AerospikeConfig changed. Need rolling restart",
			"requiredHash", requiredConfHash,
			"currentHash", podStatus.AerospikeConfigHash)
	}

	// Check if networkPolicy is updated
	if podStatus.NetworkPolicyHash != requiredNetworkPolicyHash {
		restartType = mergeRestartType(restartType, QuickRestart)
		r.Log.Info("Aerospike network policy changed. Need rolling restart",
			"requiredHash", requiredNetworkPolicyHash,
			"currentHash", podStatus.NetworkPolicyHash)
	}

	// Check if podSpec is updated
	if podStatus.PodSpecHash != requiredPodSpecHash {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("Aerospike pod spec changed. Need rolling restart",
			"requiredHash", requiredPodSpecHash,
			"currentHash", podStatus.PodSpecHash)
	}

	// Check if secret is updated
	if r.isAerospikeConfigSecretUpdatedInAeroCluster(aeroCluster, pod) {
		// TODO: If we can force the secret to mount or refresh on the pod,
		// we can get away with QuickRestart in this case too.
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("AerospikeConfigSecret changed. Need rolling restart")
	}

	// Check if resources are updated
	if r.isResourceUpdatedInAeroCluster(aeroCluster, pod) {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("Aerospike resources changed. Need rolling restart")
	}

	// Check if podSpec is updated
	if isPodSpecUpdatedInAeroCluster(aeroCluster, pod) {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("Aerospike podSpec changed. Need rolling restart")
	}

	// Check if RACKSTORAGE/CONFIGMAP is updated
	if r.isRackConfigMapsUpdatedInAeroCluster(aeroCluster, rackState, pod) {
		restartType = mergeRestartType(restartType, PodRestart)
		r.Log.Info("Aerospike rack storage configMaps changed. Need rolling restart")
	}

	return restartType, nil
}

func isPodSpecUpdatedInAeroCluster(aeroCluster *asdbv1alpha1.AerospikeCluster, pod corev1.Pod) bool {
	return areContainersChanged(pod.Spec.Containers, aeroCluster.Spec.PodSpec.Sidecars) ||
		areContainersChanged(pod.Spec.InitContainers, aeroCluster.Spec.PodSpec.InitSidecars)
}

func areContainersChanged(podContainers []corev1.Container, specSidecars []corev1.Container) bool {
	var extraPodContainers []corev1.Container
	for _, podContainer := range podContainers {
		if podContainer.Name == asdbv1alpha1.AerospikeServerContainerName ||
			podContainer.Name == asdbv1alpha1.AerospikeServerInitContainerName {
			// Check any other container also if added in default container list of statefulset
			continue
		}
		extraPodContainers = append(extraPodContainers, podContainer)
	}

	if len(extraPodContainers) != len(specSidecars) {
		return true
	}
	for _, sideCar := range specSidecars {
		found := false
		for _, podContainer := range extraPodContainers {
			if sideCar.Name == podContainer.Name {
				found = true
				break
			}
		}
		if !found {
			// SideCar not present in podSpec
			return true
		}
	}
	return false
}

func (r *AerospikeClusterReconciler) rollingRestartPod(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod, restartType RestartType, ignorablePods []corev1.Pod) reconcileResult {

	if restartType == NoRestart {
		r.Log.Info("This Pod doesn't need rolling restart, Skip this", "pod", pod.Name)
		return reconcileSuccess()
	}

	// Also check if statefulSet is in stable condition
	// Check for all containers. Status.ContainerStatuses doesn't include init container
	if pod.Status.ContainerStatuses == nil {
		return reconcileError(fmt.Errorf("pod %s containerStatus is nil, pod may be in unscheduled state", pod.Name))
	}

	r.Log.Info("Rolling restart pod", "podName", pod.Name)
	var pFound *corev1.Pod

	for i := 0; i < 5; i++ {
		r.Log.V(1).Info("Waiting for pod to be ready", "podName", pod.Name)

		pFound = &corev1.Pod{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pFound)
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
		if res := r.waitForNodeSafeStopReady(aeroCluster, pFound, ignorablePods); !res.isSuccess {
			return res
		}
	} else {
		// TODO: Check a user flag to restart failed pods.
		r.Log.Info("Restarting failed pod", "podName", pod.Name, "error", err)

		// The pod has failed. Quick start is not possible.
		restartType = PodRestart
	}

	if restartType == QuickRestart {
		return r.quickRestart(aeroCluster, rackState, pFound)
	}

	return r.podRestart(aeroCluster, rackState, pFound)
}

func (r *AerospikeClusterReconciler) quickRestart(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod *corev1.Pod) reconcileResult {
	cmName := getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID)
	cmd := []string{
		"bash",
		"/etc/aerospike/refresh-cmap-restart-asd.sh",
		cmName.Namespace,
		cmName.Name,
	}

	// Quick restart attempt should not take significant time.
	// Therefore its ok to block the operator on the quick restart attempt.
	stdout, stderr, err := utils.Exec(pod, asdbv1alpha1.AerospikeServerContainerName, cmd, r.KubeClient, r.KubeConfig)
	if err != nil {
		r.Log.V(1).Info("Failed warm restart", "err", err, "podName", pod.Name, "stdout", stdout, "stderr", stderr)

		// Fallback to pod restart.
		return r.podRestart(aeroCluster, rackState, pod)
	}

	r.Log.V(1).Info("Pod warm restarted", "podName", pod.Name)
	return reconcileSuccess()
}

func (r *AerospikeClusterReconciler) podRestart(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod *corev1.Pod) reconcileResult {
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
		r.Log.V(1).Info("Waiting for pod to be ready after delete", "podName", pod.Name, "status", pod.Status.Phase, "DeletionTimestamp", pod.DeletionTimestamp)

		updatedPod := &corev1.Pod{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, updatedPod)
		if err != nil {
			r.Log.Error(err, "Failed to get pod")
			time.Sleep(time.Second * 5)
			continue
		}

		if err := utils.CheckPodFailed(updatedPod); err != nil {
			return reconcileError(err)
		}

		if !utils.IsPodRunningAndReady(updatedPod) {
			r.Log.V(1).Info("Waiting for pod to be ready", "podName", updatedPod.Name, "status", updatedPod.Status.Phase, "DeletionTimestamp", updatedPod.DeletionTimestamp)
			time.Sleep(time.Second * 5)
			continue
		}

		r.Log.Info("Pod is restarted", "podName", updatedPod.Name)
		started = true
		break
	}

	// TODO: In what situation this can happen?
	if !started {
		r.Log.Error(err, "Pod is not running or ready. Pod might also be terminating", "podName", pod.Name, "status", pod.Status.Phase, "DeletionTimestamp", pod.DeletionTimestamp)
	}

	return reconcileSuccess()
}

func (r *AerospikeClusterReconciler) deletePodAndEnsureImageUpdated(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, p corev1.Pod, ignorablePods []corev1.Pod) reconcileResult {
	// If already dead node, so no need to check node safety, migration
	if err := utils.CheckPodFailed(&p); err == nil {
		if res := r.waitForNodeSafeStopReady(aeroCluster, &p, ignorablePods); !res.isSuccess {
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
		r.Log.V(1).Info("Waiting for pod to be ready after delete", "podName", p.Name)

		pFound := &corev1.Pod{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, pFound)
		if err != nil {
			r.Log.Error(err, "Failed to get pod", "podName", p.Name, "err", err)

			if _, err = r.getSTS(aeroCluster, rackState); err != nil {
				// Stateful set has been deleted.
				// TODO Ashish to rememeber which scenario this can happen.
				r.Log.Error(err, "Statefulset has been deleted for pod", "podName", p.Name, "err", err)
				return reconcileError(err)
			}

			time.Sleep(retryInterval)
			continue
		}
		if err := utils.CheckPodFailed(pFound); err != nil {
			return reconcileError(err)
		}

		if utils.IsPodUpgraded(pFound, aeroCluster) {
			r.Log.Info("Pod is upgraded/downgraded", "podName", p.Name)
			return reconcileSuccess()
		}

		r.Log.V(1).Info("Waiting for pod to come up with new image", "podName", p.Name)
		time.Sleep(retryInterval)
	}

	r.Log.Info("Timed out waiting for pod to come up with new image", "podName", p.Name)
	return reconcileRequeueAfter(10)
}

// cleanupPods checks pods and status before scaleup to detect and fix any status anomalies.
func (r *AerospikeClusterReconciler) cleanupPods(aeroCluster *asdbv1alpha1.AerospikeCluster, podNames []string, rackState RackState) error {

	r.Log.Info("Removing pvc for removed pods", "pods", podNames)

	// Delete PVCs if cascadeDelete
	pvcItems, err := r.getPodsPVCList(aeroCluster, podNames, rackState.Rack.ID)
	if err != nil {
		return fmt.Errorf("could not find pvc for pods %v: %v", podNames, err)
	}
	storage := rackState.Rack.Storage
	if err := r.removePVCs(aeroCluster, &storage, pvcItems); err != nil {
		return fmt.Errorf("could not cleanup pod PVCs: %v", err)
	}

	needStatusCleanup := []string{}

	clusterPodList, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return fmt.Errorf("could not cleanup pod PVCs: %v", err)
	}

	for _, podName := range podNames {
		// Clear references to this pod in the running cluster.
		for _, np := range clusterPodList.Items {
			if !utils.IsPodRunningAndReady(&np) {
				continue
			}

			// TODO: We remove node from the end. Nodes will not have seed of successive nodes
			// So this will be no op.
			// We should tip in all nodes the same seed list,
			// then only this will have any impact. Is it really necessary?

			// TODO: tip after scaleup and create
			// All nodes from other rack
			r.tipClearHostname(aeroCluster, &np, podName)

			r.alumniReset(aeroCluster, &np)
		}

		if aeroCluster.Spec.MultiPodPerHost {
			// Remove service for pod
			// TODO: make it more roboust, what if it fails
			if err := r.deletePodService(podName, aeroCluster.Namespace); err != nil {
				return err
			}
		}

		_, ok := aeroCluster.Status.Pods[podName]
		if ok {
			needStatusCleanup = append(needStatusCleanup, podName)
		}
	}

	if len(needStatusCleanup) > 0 {
		r.Log.Info("Removing pod status for dangling pods", "pods", podNames)

		if err := r.removePodStatus(aeroCluster, needStatusCleanup); err != nil {
			return fmt.Errorf("could not cleanup pod status: %v", err)
		}
	}

	return nil
}

// removePodStatus removes podNames from the cluster's pod status.
// Assumes the pods are not running so that the no concurrent update to this pod status is possbile.
func (r *AerospikeClusterReconciler) removePodStatus(aeroCluster *asdbv1alpha1.AerospikeCluster, podNames []string) error {
	if len(podNames) == 0 {
		return nil
	}

	patches := []jsonpatch.JsonPatchOperation{}

	for _, podName := range podNames {
		patch := jsonpatch.JsonPatchOperation{
			Operation: "remove",
			Path:      "/status/pods/" + podName,
		}
		patches = append(patches, patch)
	}

	jsonpatchJSON, err := json.Marshal(patches)
	constantPatch := client.RawPatch(types.JSONPatchType, jsonpatchJSON)

	// Since the pod status is updated from pod init container, set the fieldowner to "pod" for pod status updates.
	if err = r.Client.Status().Patch(context.TODO(), aeroCluster, constantPatch, client.FieldOwner("pod")); err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}

	return nil
}

func (r *AerospikeClusterReconciler) cleanupDanglingPodsRack(aeroCluster *asdbv1alpha1.AerospikeCluster, sts *appsv1.StatefulSet, rackState RackState) error {
	// Clean up any dangling resources associated with the new pods.
	// This implements a safety net to protect scale up against failed cleanup operations when cluster
	// is scaled down.
	danglingPods := []string{}

	// Find dangling pods in pods
	if aeroCluster.Status.Pods != nil {
		for podName := range aeroCluster.Status.Pods {
			rackID, err := utils.GetRackIDFromPodName(podName)
			if err != nil {
				return fmt.Errorf("failed to get rackID for the pod %s", podName)
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

	err := r.cleanupPods(aeroCluster, danglingPods, rackState)
	if err != nil {
		return fmt.Errorf("failed dangling pod cleanup: %v", err)
	}

	return nil
}

// getIgnorablePods returns pods from racksToDelete that are currently not running and can be ignored in stability checks.
func (r *AerospikeClusterReconciler) getIgnorablePods(aeroCluster *asdbv1alpha1.AerospikeCluster, racksToDelete []asdbv1alpha1.Rack) ([]corev1.Pod, error) {
	ignorablePods := []corev1.Pod{}
	for _, rack := range racksToDelete {
		rackPods, err := r.getRackPodList(aeroCluster, rack.ID)

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
// Note: the IPs returned from here should match the IPs generated in the pod intialization script for the init container.
func (r *AerospikeClusterReconciler) getPodIPs(pod *corev1.Pod) (string, string, string, error) {
	podIP := pod.Status.PodIP
	hostInternalIP := pod.Status.HostIP
	hostExternalIP := hostInternalIP

	k8sNode := &corev1.Node{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Spec.NodeName}, k8sNode)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get k8s node %s for pod %v: %v", pod.Spec.NodeName, pod.Name, err)
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

func (r *AerospikeClusterReconciler) getServiceForPod(pod *corev1.Pod) (*corev1.Service, error) {
	service := &corev1.Service{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, service)
	if err != nil {
		return nil, fmt.Errorf("failed to get service for pod %s: %v", pod.Name, err)
	}
	return service, nil
}

func (r *AerospikeClusterReconciler) getServicePortForPod(aeroCluster *asdbv1alpha1.AerospikeCluster, pod *corev1.Pod) (int32, error) {
	var port int32
	tlsName := getServiceTLSName(aeroCluster)

	if aeroCluster.Spec.MultiPodPerHost {
		svc, err := r.getServiceForPod(pod)
		if err != nil {
			return 0, fmt.Errorf("error getting service port: %v", err)
		}
		if tlsName == "" {
			port = svc.Spec.Ports[0].NodePort
		} else {
			for _, portInfo := range svc.Spec.Ports {
				if portInfo.Name == "tls" {
					port = portInfo.NodePort
					break
				}
			}
		}
	} else {
		if tlsName == "" {
			port = asdbv1alpha1.ServicePort
		} else {
			port = asdbv1alpha1.ServiceTLSPort
		}
	}

	return port, nil
}

func (r *AerospikeClusterReconciler) getPodsPVCList(aeroCluster *asdbv1alpha1.AerospikeCluster, podNames []string, rackID int) ([]corev1.PersistentVolumeClaim, error) {
	pvcListItems, err := r.getRackPVCList(aeroCluster, rackID)
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

func (r *AerospikeClusterReconciler) getClusterPodList(aeroCluster *asdbv1alpha1.AerospikeCluster) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := r.Client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *AerospikeClusterReconciler) isAnyPodInFailedState(aeroCluster *asdbv1alpha1.AerospikeCluster, podList []corev1.Pod) bool {

	for _, p := range podList {
		for _, ps := range p.Status.ContainerStatuses {
			// TODO: Should we use checkPodFailed or CheckPodImageFailed?
			// scaleDown, rollingRestart should work even if node is crashed
			// If node was crashed due to wrong config then only rollingRestart can bring it back.
			if err := utils.CheckPodImageFailed(&p); err != nil {
				r.Log.Info("AerospikeCluster Pod is in failed state", "currentImage", ps.Image, "podName", p.Name, "err", err)
				return true
			}
		}
	}
	return false
}

func getServiceTLSName(aeroCluster *asdbv1alpha1.AerospikeCluster) string {
	// TODO: Should we return err, should have failed in validation
	aeroConf := aeroCluster.Spec.AerospikeConfig.Value

	if networkConfTmp, ok := aeroConf["network"]; ok {
		networkConf := networkConfTmp.(map[string]interface{})
		if tlsName, ok := networkConf["service"].(map[string]interface{})["tls-name"]; ok {
			return tlsName.(string)
		}
	}
	return ""
}

func getFQDNForPod(aeroCluster *asdbv1alpha1.AerospikeCluster, host string) string {
	return fmt.Sprintf("%s.%s.%s.svc.cluster.local", host, aeroCluster.Name, aeroCluster.Namespace)
}

// GetEndpointsFromInfo returns the aerospike service endpoints as a slice of host:port elements named addressName from the info endpointsMap. It returns an empty slice if the access address with addressName is not found in endpointsMap.
//
// E.g. addressName are access, alternate-access
func GetEndpointsFromInfo(addressName string, endpointsMap map[string]interface{}) []string {
	endpoints := []string{}

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
		endpoints = append(endpoints, net.JoinHostPort(host, strconv.Itoa(int(port))))
	}
	return endpoints
}
