package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	as "github.com/aerospike/aerospike-client-go/v8"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

// RestartType is the type of pod restart to use.
type RestartType int

const (
	// noRestart needed.
	noRestart RestartType = iota

	// noRestartUpdateConf indicates that restart is not needed but conf file has to be updated.
	noRestartUpdateConf

	// podRestart indicates that restart requires a restart of the pod.
	podRestart

	// quickRestart indicates that only Aerospike service can be restarted.
	quickRestart
)

// mergeRestartType generates the updated restart type based on precedence.
// podRestart > quickRestart > noRestartUpdateConf > noRestart
func mergeRestartType(current, incoming RestartType) RestartType {
	if current == podRestart || incoming == podRestart {
		return podRestart
	}

	if current == quickRestart || incoming == quickRestart {
		return quickRestart
	}

	if current == noRestartUpdateConf || incoming == noRestartUpdateConf {
		return noRestartUpdateConf
	}

	return noRestart
}

// Fetching RestartType of all pods, based on the operation being performed.
func (r *SingleClusterReconciler) getRollingRestartTypeMap(rackState *RackState, ignorablePodNames sets.Set[string]) (
	restartTypeMap map[string]RestartType, dynamicConfDiffPerPod map[string]asconfig.DynamicConfigMap, err error) {
	var addedNSDevices []string

	restartTypeMap = make(map[string]RestartType)
	dynamicConfDiffPerPod = make(map[string]asconfig.DynamicConfigMap)

	pods, err := r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods: %v", err)
	}

	confMap, err := r.getConfigMap(rackState.Rack.ID)
	if err != nil {
		return nil, nil, err
	}

	blockedK8sNodes := sets.NewString(r.aeroCluster.Spec.K8sNodeBlockList...)
	requiredConfHash := confMap.Data[aerospikeConfHashFileName]

	// Fetching all pods requested for on-demand operations.
	onDemandQuickRestarts, onDemandPodRestarts := r.podsToRestart()

	for idx := range pods {
		if ignorablePodNames.Has(pods[idx].Name) {
			continue
		}

		if blockedK8sNodes.Has(pods[idx].Spec.NodeName) {
			r.Log.Info("Pod found in blocked nodes list, will be migrated to a different node",
				"podName", pods[idx].Name)

			restartTypeMap[pods[idx].Name] = podRestart

			continue
		}

		podStatus := r.aeroCluster.Status.Pods[pods[idx].Name]
		if podStatus.AerospikeConfigHash != requiredConfHash {
			serverContainer := getContainer(pods[idx].Spec.Containers, asdbv1.AerospikeServerContainerName)

			version, err := asdbv1.GetImageVersion(serverContainer.Image)
			if err != nil {
				return nil, nil, err
			}

			specToStatusDiffs, err := getConfDiff(r.Log, rackState.Rack.AerospikeConfig.Value, pods[idx].Annotations, version)
			if err != nil {
				return nil, nil, err
			}

			if len(specToStatusDiffs) != 0 {
				for key := range specToStatusDiffs {
					// To update in-memory namespace data-size, we need to restart the pod.
					// Just a warm restart is not enough.
					// https://support.aerospike.com/s/article/How-to-change-data-size-config-in-a-running-cluster
					if strings.HasSuffix(key, ".storage-engine.data-size") {
						restartTypeMap[pods[idx].Name] = mergeRestartType(restartTypeMap[pods[idx].Name], podRestart)

						break
					}
				}

				if restartTypeMap[pods[idx].Name] == podRestart {
					continue
				}

				// If EnableDynamicConfigUpdate is set and dynamic config command exec partially failed in previous try
				// then skip dynamic config update and fall back to rolling restart.
				// Continue with dynamic config update in case of Failed DynamicConfigUpdateStatus
				if asdbv1.GetBool(r.aeroCluster.Spec.EnableDynamicConfigUpdate) &&
					podStatus.DynamicConfigUpdateStatus != asdbv1.PartiallyFailed &&
					isAllDynamicConfig(r.Log, specToStatusDiffs, version) {
					dynamicConfDiffPerPod[pods[idx].Name] = specToStatusDiffs
				}
			}

			if addedNSDevices == nil {
				// Fetching all block devices that have been added in namespaces.
				addedNSDevices, err = r.getNSAddedDevices(rackState)
				if err != nil {
					return nil, nil, err
				}
			}
		}

		restartType, err := r.getRollingRestartTypePod(rackState, pods[idx], confMap, addedNSDevices,
			len(dynamicConfDiffPerPod[pods[idx].Name]) > 0, onDemandQuickRestarts, onDemandPodRestarts)
		if err != nil {
			return nil, nil, err
		}

		restartTypeMap[pods[idx].Name] = restartType

		// Fallback to rolling restart in case of partial failure to recover with the desired Aerospike config
		if podStatus.DynamicConfigUpdateStatus == asdbv1.PartiallyFailed {
			restartTypeMap[pods[idx].Name] = mergeRestartType(restartTypeMap[pods[idx].Name], quickRestart)
		}
	}

	return restartTypeMap, dynamicConfDiffPerPod, nil
}

func (r *SingleClusterReconciler) getRollingRestartTypePod(
	rackState *RackState, pod *corev1.Pod, confMap *corev1.ConfigMap,
	addedNSDevices []string, onlyDynamicConfigChange bool,
	onDemandQuickRestarts, onDemandPodRestarts sets.Set[string],
) (RestartType, error) {
	restartType := noRestart

	// AerospikeConfig nil means status not updated yet
	if r.IsStatusEmpty() {
		return restartType, nil
	}

	requiredConfHash := confMap.Data[aerospikeConfHashFileName]
	requiredNetworkPolicyHash := confMap.Data[networkPolicyHashFileName]
	requiredPodSpecHash := confMap.Data[podSpecHashFileName]

	podStatus := r.aeroCluster.Status.Pods[pod.Name]

	// Check if aerospikeConfig is updated
	if podStatus.AerospikeConfigHash != requiredConfHash {
		podRestartType := quickRestart
		// checking if volumes added in namespace is part of dirtyVolumes.
		// if yes, then podRestart is needed.
		if len(addedNSDevices) > 0 {
			podRestartType = r.handleNSOrDeviceAddition(addedNSDevices, pod.Name)
		} else if onlyDynamicConfigChange {
			// If only dynamic config change is there, then we can update config dynamically.
			podRestartType = noRestartUpdateConf
		}

		restartType = mergeRestartType(restartType, podRestartType)

		r.Log.Info(
			"AerospikeConfig changed. Need rolling restart or update config dynamically",
			"requiredHash", requiredConfHash,
			"currentHash", podStatus.AerospikeConfigHash,
		)
	}

	// Check if networkPolicy is updated
	if podStatus.NetworkPolicyHash != requiredNetworkPolicyHash {
		restartType = mergeRestartType(restartType, quickRestart)

		r.Log.Info(
			"Aerospike network policy changed. Need rolling restart",
			"requiredHash", requiredNetworkPolicyHash,
			"currentHash", podStatus.NetworkPolicyHash,
		)
	}

	// Check if podSpec is updated
	if podStatus.PodSpecHash != requiredPodSpecHash {
		restartType = mergeRestartType(restartType, podRestart)

		r.Log.Info(
			"Aerospike pod spec changed. Need rolling restart",
			"requiredHash", requiredPodSpecHash,
			"currentHash", podStatus.PodSpecHash,
		)
	}

	podSpecUpdated, err := r.isAnyPodSpecUpdated(rackState, pod)
	if err != nil {
		return restartType, fmt.Errorf("failed to check if pod spec is updated: %v", err)
	}

	if podSpecUpdated {
		restartType = mergeRestartType(restartType, podRestart)

		r.Log.Info("Aerospike pod ports changed. Need rolling restart")
	}

	// Check if rack storage is updated
	if r.isRackStorageUpdatedInAeroCluster(rackState, pod) {
		restartType = mergeRestartType(restartType, podRestart)

		r.Log.Info("Aerospike rack storage changed. Need rolling restart")
	}

	if opType := r.onDemandOperationType(pod.Name, onDemandQuickRestarts, onDemandPodRestarts); opType != noRestart {
		restartType = mergeRestartType(restartType, opType)

		r.Log.Info("Pod warm/cold restart requested. Need rolling restart",
			"pod name", pod.Name, "operation", opType, "restartType", restartType)
	}

	return restartType, nil
}

func (r *SingleClusterReconciler) rollingRestartPods(
	rackState *RackState, podsToRestart []*corev1.Pod, ignorablePodNames sets.Set[string],
	restartTypeMap map[string]RestartType,
) common.ReconcileResult {
	failedPods, activePods := getFailedAndActivePods(podsToRestart)

	// If already dead node (failed pod) then no need to check node safety, migration
	if len(failedPods) != 0 {
		r.Log.Info("Restart failed pods", "pods", getPodNames(failedPods))

		if res := r.restartPods(rackState, failedPods, restartTypeMap); !res.IsSuccess {
			return res
		}
	}

	if len(activePods) != 0 {
		r.Log.Info("Restart active pods", "pods", getPodNames(activePods))

		if res := r.waitForMultipleNodesSafeStopReady(activePods, ignorablePodNames); !res.IsSuccess {
			return res
		}

		var clientPolicy *as.ClientPolicy

		setMigrateFillDelay := r.shouldSetMigrateFillDelay(rackState, podsToRestart, restartTypeMap)

		r.Log.Info(
			fmt.Sprintf("Adjust migrate-fill-delay prior to pod restart: %t", setMigrateFillDelay),
		)

		// Revert migrate-fill-delay to the original value before restarting active pods.
		// This will be a no-op in the first reconcile
		if setMigrateFillDelay {
			clientPolicy = r.getClientPolicy()

			if res := r.setMigrateFillDelay(clientPolicy, &rackState.Rack.AerospikeConfig, false,
				ignorablePodNames,
			); !res.IsSuccess {
				r.Log.Error(res.Err,
					"Failed to set migrate-fill-delay to original value before restarting the running pods")
				return res
			}
		}

		if res := r.restartPods(rackState, activePods, restartTypeMap); !res.IsSuccess {
			return res
		}

		// Set migrate-fill-delay O to immediately start the migration. Will be reverted back to the original value
		// in the next reconcile.
		if setMigrateFillDelay {
			if res := r.setMigrateFillDelay(clientPolicy, &rackState.Rack.AerospikeConfig, true,
				ignorablePodNames,
			); !res.IsSuccess {
				r.Log.Error(res.Err, "Failed to set migrate-fill-delay to `0` after restarting the running pods")
				return res
			}
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) restartASDOrUpdateAerospikeConf(podName string,
	operation RestartType) error {
	rackID, err := utils.GetRackIDFromPodName(podName)
	if err != nil {
		return fmt.Errorf(
			"failed to get rackID for the pod %s", podName,
		)
	}

	podNamespacedName := types.NamespacedName{
		Name:      podName,
		Namespace: r.aeroCluster.Namespace,
	}

	cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, *rackID)
	initBinary := "/etc/aerospike/akoinit"

	var subCommand string

	switch operation {
	case noRestart, podRestart:
		return fmt.Errorf("invalid operation for akoinit")
	case quickRestart:
		subCommand = "quick-restart"
	case noRestartUpdateConf:
		subCommand = "update-conf"
	}

	cmd := []string{
		initBinary,
		subCommand,
		"--cm-name",
		cmName.Name,
		"--cm-namespace",
		cmName.Namespace,
	}

	// Quick restart attempt should not take significant time.
	// Therefore, it's ok to block the operator on the quick restart attempt.
	stdout, stderr, err := utils.Exec(
		podNamespacedName, asdbv1.AerospikeServerContainerName, cmd, r.KubeClient,
		r.KubeConfig,
	)
	if err != nil {
		if strings.Contains(err.Error(), initBinary+": no such file or directory") {
			cmd := []string{
				"bash",
				"/etc/aerospike/refresh-cmap-restart-asd.sh",
				cmName.Namespace,
				cmName.Name,
			}

			// Quick restart attempt should not take significant time.
			// Therefore, it's ok to block the operator on the quick restart attempt.
			stdout, stderr, err = utils.Exec(
				podNamespacedName, asdbv1.AerospikeServerContainerName, cmd, r.KubeClient,
				r.KubeConfig,
			)

			if err != nil {
				r.Log.V(1).Info(
					"Failed warm restart", "err", err, "podName", podNamespacedName.Name, "stdout",
					stdout, "stderr", stderr,
				)

				return err
			}
		} else {
			r.Log.V(1).Info(
				"Failed to perform", "operation", subCommand, "err", err, "podName", podNamespacedName.Name, "stdout",
				stdout, "stderr", stderr,
			)

			return err
		}
	}

	if subCommand == "quick-restart" {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "PodWarmRestarted",
			"[rack-%d] Restarted Pod %s", *rackID, podNamespacedName.Name,
		)
		r.Log.V(1).Info("Pod warm restarted", "podName", podNamespacedName.Name)
	} else {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "PodConfUpdated",
			"[rack-%d] Updated Pod %s", *rackID, podNamespacedName.Name,
		)
		r.Log.V(1).Info("Pod conf updated", "podName", podNamespacedName.Name)
	}

	return nil
}

func (r *SingleClusterReconciler) restartPods(
	rackState *RackState, podsToRestart []*corev1.Pod, restartTypeMap map[string]RestartType,
) common.ReconcileResult {
	// For each block volume removed from a namespace, pod status dirtyVolumes is appended with that volume name.
	// For each file removed from a namespace, it is deleted right away.
	if err := r.handleNSOrDeviceRemoval(rackState, podsToRestart); err != nil {
		return common.ReconcileError(err)
	}

	restartedPods := make([]*corev1.Pod, 0, len(podsToRestart))
	restartedPodNames := make([]string, 0, len(podsToRestart))
	restartedASDPodNames := make([]string, 0, len(podsToRestart))

	for idx := range podsToRestart {
		pod := podsToRestart[idx]
		// Check if this pod needs restart
		restartType := restartTypeMap[pod.Name]

		if restartType == quickRestart {
			// We assume that the pod server image supports pod warm restart.
			if err := r.restartASDOrUpdateAerospikeConf(pod.Name, quickRestart); err != nil {
				r.Log.Error(err, "Failed to warm restart pod", "podName", pod.Name)
				return common.ReconcileError(err)
			}

			restartedASDPodNames = append(restartedASDPodNames, pod.Name)
		} else if restartType == podRestart {
			if r.isLocalPVCDeletionRequired(rackState, pod) {
				if err := r.deleteLocalPVCs(rackState, pod); err != nil {
					return common.ReconcileError(err)
				}
			}

			if err := r.Client.Delete(context.TODO(), pod); err != nil {
				r.Log.Error(err, "Failed to delete pod")
				return common.ReconcileError(err)
			}

			restartedPods = append(restartedPods, pod)
			restartedPodNames = append(restartedPodNames, pod.Name)

			r.Log.V(1).Info("Pod deleted", "podName", pod.Name)
		}
	}

	if err := r.updateOperationStatus(restartedASDPodNames, restartedPodNames); err != nil {
		return common.ReconcileError(err)
	}

	if len(restartedPods) > 0 {
		if result := r.ensurePodsRunningAndReady(restartedPods); !result.IsSuccess {
			return result
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) updateAerospikeConfInPod(podName string) error {
	r.Log.Info("Updating aerospike config file in pod", "pod", podName)

	if err := r.restartASDOrUpdateAerospikeConf(podName, noRestartUpdateConf); err != nil {
		return err
	}

	r.Log.V(1).Info("Updated aerospike config file in pod", "podName", podName)

	return nil
}

func (r *SingleClusterReconciler) ensurePodsRunningAndReady(podsToCheck []*corev1.Pod) common.ReconcileResult {
	podNames := getPodNames(podsToCheck)
	readyPods := map[string]bool{}

	const (
		maxRetries    = 6
		retryInterval = time.Second * 10
	)

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
				return common.ReconcileError(err)
			}

			if err := utils.CheckPodFailed(updatedPod); err != nil {
				return common.ReconcileError(err)
			}

			if !utils.IsPodRunningAndReady(updatedPod) {
				break
			}

			readyPods[pod.Name] = true

			r.Log.Info("Pod is restarted", "podName", updatedPod.Name)
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeNormal, "PodRestarted",
				"[rack-%s] Restarted Pod %s", pod.Labels[asdbv1.AerospikeRackIDLabel], pod.Name,
			)
		}

		if len(readyPods) == len(podsToCheck) {
			r.Log.Info(
				"Pods are running and ready", "pods",
				podNames,
			)

			return common.ReconcileSuccess()
		}

		time.Sleep(retryInterval)
	}

	r.Log.Info(
		"Timed out waiting for pods to come up", "pods",
		podNames,
	)

	return common.ReconcileRequeueAfter(10)
}

func getFailedAndActivePods(pods []*corev1.Pod) (failedPods, activePods []*corev1.Pod) {
	for idx := range pods {
		pod := pods[idx]

		if err := utils.CheckPodFailed(pod); err != nil {
			failedPods = append(failedPods, pod)
			continue
		}

		activePods = append(activePods, pod)
	}

	return failedPods, activePods
}

func getNonIgnorablePods(pods []*corev1.Pod, ignorablePodNames sets.Set[string],
) []*corev1.Pod {
	nonIgnorablePods := make([]*corev1.Pod, 0, len(pods))

	for idx := range pods {
		pod := pods[idx]
		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		nonIgnorablePods = append(nonIgnorablePods, pod)
	}

	return nonIgnorablePods
}

func (r *SingleClusterReconciler) safelyDeletePodsAndEnsureImageUpdated(
	rackState *RackState, podsToUpdate []*corev1.Pod, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	failedPods, activePods := getFailedAndActivePods(podsToUpdate)

	// If already dead node (failed pod) then no need to check node safety, migration
	if len(failedPods) != 0 {
		r.Log.Info("Restart failed pods with updated container image", "pods", getPodNames(failedPods))

		if res := r.deletePodAndEnsureImageUpdated(rackState, failedPods); !res.IsSuccess {
			return res
		}
	}

	if len(activePods) != 0 {
		r.Log.Info("Restart active pods with updated container image", "pods", getPodNames(activePods))

		if res := r.waitForMultipleNodesSafeStopReady(activePods, ignorablePodNames); !res.IsSuccess {
			return res
		}

		var clientPolicy *as.ClientPolicy

		setMigrateFillDelay := r.shouldSetMigrateFillDelay(rackState, podsToUpdate, nil)

		r.Log.Info(
			fmt.Sprintf("Adjust migrate-fill-delay prior to pod restart: %t", setMigrateFillDelay))

		// Revert migrate-fill-delay to the original value before restarting active pods.
		// This will be a no-op in the first reconcile
		if setMigrateFillDelay {
			clientPolicy = r.getClientPolicy()

			if res := r.setMigrateFillDelay(clientPolicy, &rackState.Rack.AerospikeConfig, false,
				ignorablePodNames,
			); !res.IsSuccess {
				r.Log.Error(res.Err,
					"Failed to set migrate-fill-delay to original value before upgrading the running pods")
				return res
			}
		}

		if res := r.deletePodAndEnsureImageUpdated(rackState, activePods); !res.IsSuccess {
			return res
		}

		// Set migrate-fill-delay O to immediately start the migration. Will be reverted back to the original value
		// in the next reconcile.
		if setMigrateFillDelay {
			if res := r.setMigrateFillDelay(clientPolicy, &rackState.Rack.AerospikeConfig, true,
				ignorablePodNames,
			); !res.IsSuccess {
				r.Log.Error(res.Err, "Failed to set migrate-fill-delay to `0` after upgrading the running pods")
				return res
			}
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) deletePodAndEnsureImageUpdated(
	rackState *RackState, podsToUpdate []*corev1.Pod,
) common.ReconcileResult {
	// For each block volume removed from a namespace, pod status dirtyVolumes is appended with that volume name.
	// For each file removed from a namespace, it is deleted right away.
	if err := r.handleNSOrDeviceRemoval(rackState, podsToUpdate); err != nil {
		return common.ReconcileError(err)
	}

	// Delete pods
	for _, pod := range podsToUpdate {
		if r.isLocalPVCDeletionRequired(rackState, pod) {
			if err := r.deleteLocalPVCs(rackState, pod); err != nil {
				return common.ReconcileError(err)
			}
		}

		if err := r.Client.Delete(context.TODO(), pod); err != nil {
			return common.ReconcileError(err)
		}

		r.Log.V(1).Info("Pod deleted", "podName", pod.Name)
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "PodWaitUpdate",
			"[rack-%d] Waiting to update Pod %s", rackState.Rack.ID, pod.Name,
		)
	}

	return r.ensurePodsImageUpdated(podsToUpdate)
}

func (r *SingleClusterReconciler) isLocalPVCDeletionRequired(rackState *RackState, pod *corev1.Pod) bool {
	if utils.ContainsString(r.aeroCluster.Spec.K8sNodeBlockList, pod.Spec.NodeName) {
		r.Log.Info("Pod found in blocked nodes list, deleting corresponding local PVCs if any",
			"podName", pod.Name)
		return true
	}

	if asdbv1.GetBool(rackState.Rack.Storage.DeleteLocalStorageOnRestart) {
		r.Log.Info("deleteLocalStorageOnRestart flag is enabled, deleting corresponding local PVCs if any",
			"podName", pod.Name)
		return true
	}

	return false
}

func (r *SingleClusterReconciler) ensurePodsImageUpdated(podsToCheck []*corev1.Pod) common.ReconcileResult {
	podNames := getPodNames(podsToCheck)
	updatedPods := sets.Set[string]{}

	const (
		maxRetries    = 6
		retryInterval = time.Second * 10
	)

	for i := 0; i < maxRetries; i++ {
		r.Log.V(1).Info(
			"Waiting for pods to be ready after delete", "pods", podNames,
		)

		for _, pod := range podsToCheck {
			if updatedPods.Has(pod.Name) {
				continue
			}

			r.Log.V(1).Info(
				"Waiting for pod to be ready", "podName", pod.Name,
			)

			updatedPod := &corev1.Pod{}
			podName := types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}

			if err := r.Client.Get(context.TODO(), podName, updatedPod); err != nil {
				return common.ReconcileError(err)
			}

			if err := utils.CheckPodFailed(updatedPod); err != nil {
				return common.ReconcileError(err)
			}

			if !r.isPodUpgraded(updatedPod) {
				break
			}

			updatedPods.Insert(pod.Name)

			r.Log.Info("Pod is upgraded/downgraded", "podName", pod.Name)
		}

		if len(updatedPods) == len(podsToCheck) {
			r.Log.Info("Pods are upgraded/downgraded", "pod", podNames)
			return common.ReconcileSuccess()
		}

		time.Sleep(retryInterval)
	}

	r.Log.Info(
		"Timed out waiting for pods to come up with new image", "pods",
		podNames,
	)

	return common.ReconcileRequeueAfter(10)
}

// cleanupPods checks pods and status before scale-up to detect and fix any
// status anomalies.
func (r *SingleClusterReconciler) cleanupPods(
	podNames []string, rackState *RackState,
) error {
	r.Log.Info("Removing pvc for removed pods", "pods", podNames)

	// Delete PVCs if cascadeDelete
	pvcItems, err := r.getPodsPVCList(podNames, rackState.Rack.ID)
	if err != nil {
		return fmt.Errorf("could not find pvc for pods %v: %v", podNames, err)
	}

	storage := rackState.Rack.Storage
	if err = r.removePVCs(&storage, pvcItems); err != nil {
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
		for idx := range clusterPodList.Items {
			np := &clusterPodList.Items[idx]
			if !utils.IsPodRunningAndReady(np) {
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

			if err := r.tipClearHostname(np, podName); err != nil {
				r.Log.V(2).Info("Failed to tipClear", "hostName", podName, "from the pod", np.Name)
			}

			if err := r.alumniReset(np); err != nil {
				r.Log.V(2).Info(fmt.Sprintf("Failed to reset alumni for the pod %s", np.Name))
			}
		}

		// Try to delete the corresponding pod service if it was created
		if asdbv1.GetBool(r.aeroCluster.Spec.PodSpec.MultiPodPerHost) {
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

	patches := make([]jsonpatch.PatchOperation, 0, len(podNames))

	for _, podName := range podNames {
		patch := jsonpatch.PatchOperation{
			Operation: "remove",
			Path:      "/status/pods/" + podName,
		}
		patches = append(patches, patch)
	}

	return r.patchPodStatus(context.TODO(), patches)
}

func (r *SingleClusterReconciler) cleanupDanglingPodsRack(sts *appsv1.StatefulSet, rackState *RackState) error {
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

	if len(danglingPods) > 0 {
		if err := r.cleanupPods(danglingPods, rackState); err != nil {
			return fmt.Errorf("failed dangling pod cleanup: %v", err)
		}
	}

	return nil
}

// getIgnorablePods returns pods:
// 1. From racksToDelete that are currently not running and can be ignored in stability checks.
// 2. Failed/pending pods from the configuredRacks identified using maxIgnorablePods field and
// can be ignored from stability checks.
func (r *SingleClusterReconciler) getIgnorablePods(racksToDelete []asdbv1.Rack, configuredRacks []RackState) (
	sets.Set[string], error,
) {
	ignorablePodNames := sets.Set[string]{}

	for rackIdx := range racksToDelete {
		rackPods, err := r.getRackPodList(racksToDelete[rackIdx].ID)
		if err != nil {
			return nil, err
		}

		for podIdx := range rackPods.Items {
			pod := rackPods.Items[podIdx]
			if !utils.IsPodRunningAndReady(&pod) {
				ignorablePodNames.Insert(pod.Name)
			}
		}
	}

	for idx := range configuredRacks {
		rack := &configuredRacks[idx]

		failedAllowed, _ := intstr.GetScaledValueFromIntOrPercent(
			r.aeroCluster.Spec.RackConfig.MaxIgnorablePods, int(rack.Size), false,
		)

		podList, err := r.getRackPodList(rack.Rack.ID)
		if err != nil {
			return nil, err
		}

		var (
			failedPod  []string
			pendingPod []string
		)

		for podIdx := range podList.Items {
			pod := &podList.Items[podIdx]

			if !utils.IsPodRunningAndReady(pod) {
				if isPodUnschedulable, _ := utils.IsPodReasonUnschedulable(pod); isPodUnschedulable {
					pendingPod = append(pendingPod, pod.Name)
					continue
				}

				failedPod = append(failedPod, pod.Name)
			}
		}

		// prepend pendingPod to failedPod
		failedPod = append(pendingPod, failedPod...)

		for podIdx := range failedPod {
			if failedAllowed <= 0 {
				break
			}

			ignorablePodNames.Insert(failedPod[podIdx])

			failedAllowed--
		}
	}

	return ignorablePodNames, nil
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

	for idx := range pvcListItems {
		pvc := pvcListItems[idx]
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

func (r *SingleClusterReconciler) isAnyPodInImageFailedState(podList []*corev1.Pod, ignorablePodNames sets.Set[string],
) bool {
	for idx := range podList {
		pod := podList[idx]
		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		// TODO: Should we use checkPodFailed or CheckPodImageFailed?
		// scaleDown, rollingRestart should work even if node is crashed
		// If node was crashed due to wrong config then only rollingRestart can bring it back.
		if err := utils.CheckPodImageFailed(pod); err != nil {
			r.Log.Info(
				"AerospikeCluster Pod is in failed state", "podName", pod.Name, "err", err,
			)

			return true
		}
	}

	return false
}

func getFQDNForPod(aeroCluster *asdbv1.AerospikeCluster, host string) string {
	return fmt.Sprintf("%s.%s.%s", host, aeroCluster.Name, aeroCluster.Namespace)
}

// GetEndpointsFromInfo returns the aerospike endpoints as a slice of host:port based on context and addressName passed
// from the info endpointsMap. It returns an empty slice if the access address with addressName is not found in
// endpointsMap.
// E.g. addressName are access, alternate-access
func GetEndpointsFromInfo(
	aeroCtx, addressName string, endpointsMap map[string]string,
) []string {
	addressKeyPrefix := aeroCtx + "."

	if addressName != "" {
		addressKeyPrefix += addressName + "-"
	}

	portStr, ok := endpointsMap[addressKeyPrefix+"port"]
	if !ok {
		return nil
	}

	port, err := strconv.ParseInt(portStr, 10, 32)
	if err != nil || port == 0 {
		return nil
	}

	hostsStr, ok := endpointsMap[addressKeyPrefix+"addresses"]
	if !ok {
		return nil
	}

	hosts := strings.Split(hostsStr, ",")
	endpoints := make([]string, 0, len(hosts))

	for _, host := range hosts {
		endpoints = append(
			endpoints, net.JoinHostPort(host, strconv.Itoa(int(port))),
		)
	}

	return endpoints
}

func getPodNames(pods []*corev1.Pod) []string {
	podNames := make([]string, 0, len(pods))

	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}

	return podNames
}

//nolint:gocyclo // for readability
func (r *SingleClusterReconciler) handleNSOrDeviceRemoval(rackState *RackState, podsToRestart []*corev1.Pod) error {
	var (
		rackStatus     asdbv1.Rack
		removedDevices []string
		removedFiles   []string
	)

	rackFound := false

	for idx := range r.aeroCluster.Status.RackConfig.Racks {
		rackStatus = r.aeroCluster.Status.RackConfig.Racks[idx]
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
			if specNamespace.(map[string]interface{})["name"] != statusNamespace.(map[string]interface{})["name"] {
				continue
			}

			namespaceFound = true
			specStorage := specNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})
			statusStorage := statusNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})

			statusDevices := sets.Set[string]{}
			specDevices := sets.Set[string]{}

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

			removedDevicesPerNS := sets.List(statusDevices.Difference(specDevices))
			for _, removedDevice := range removedDevicesPerNS {
				deviceName := getVolumeNameFromDevicePath(rackStatus.Storage.Volumes, removedDevice)
				r.Log.Info(
					"Device is removed from namespace", "device", deviceName, "namespace",
					specNamespace.(map[string]interface{})["name"],
				)

				removedDevices = append(removedDevices, deviceName)
			}

			statusFiles := sets.Set[string]{}
			specFiles := sets.Set[string]{}

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

			removedFilesPerNS := sets.List(statusFiles.Difference(specFiles))
			if len(removedFilesPerNS) > 0 {
				removedFiles = append(removedFiles, removedFilesPerNS...)
			}

			var statusMounts []string

			specMounts := sets.Set[string]{}

			if statusNamespace.(map[string]interface{})["index-type"] != nil {
				statusIndex := statusNamespace.(map[string]interface{})["index-type"].(map[string]interface{})
				if statusIndex["mounts"] != nil {
					for _, statusMountInterface := range statusIndex["mounts"].([]interface{}) {
						statusMounts = append(statusMounts, strings.Fields(statusMountInterface.(string))...)
					}
				}
			}

			if specNamespace.(map[string]interface{})["index-type"] != nil {
				specIndex := specNamespace.(map[string]interface{})["index-type"].(map[string]interface{})
				if specIndex["mounts"] != nil {
					for _, specMountInterface := range specIndex["mounts"].([]interface{}) {
						specMounts.Insert(strings.Fields(specMountInterface.(string))...)
					}
				}
			}

			indexMountRemoved := false

			for index, statusMount := range statusMounts {
				statusMounts[index] += "/*"

				if !specMounts.Has(statusMount) {
					indexMountRemoved = true
				}
			}

			if indexMountRemoved {
				removedFiles = append(removedFiles, statusMounts...)
			}

			break
		}

		if !namespaceFound {
			r.Log.Info(
				"Namespace is deleted", "namespace", statusNamespace.(map[string]interface{})["name"],
			)

			statusStorage := statusNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})

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

			if statusNamespace.(map[string]interface{})["index-type"] != nil {
				statusIndex := statusNamespace.(map[string]interface{})["index-type"].(map[string]interface{})
				if statusIndex["mounts"] != nil {
					var statusMounts []string
					for _, statusMountInterface := range statusStorage["mounts"].([]interface{}) {
						statusMounts = append(statusMounts, strings.Fields(statusMountInterface.(string))...)
					}

					for index := range statusMounts {
						removedFiles = append(removedFiles, statusMounts[index]+"/*")
					}
				}
			}
		}
	}

	for _, pod := range podsToRestart {
		err := r.handleNSOrDeviceRemovalPerPod(removedDevices, removedFiles, pod.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) handleNSOrDeviceRemovalPerPod(
	removedDevices, removedFiles []string, podName string,
) error {
	podStatus := r.aeroCluster.Status.Pods[podName]

	for _, file := range removedFiles {
		err := r.deleteFileStorage(podName, file)
		if err != nil {
			return err
		}
	}

	if len(removedDevices) > 0 {
		dirtyVolumes := sets.Set[string]{}
		dirtyVolumes.Insert(removedDevices...)
		dirtyVolumes.Insert(podStatus.DirtyVolumes...)

		var patches []jsonpatch.PatchOperation

		patch1 := jsonpatch.PatchOperation{
			Operation: "replace",
			Path:      "/status/pods/" + podName + "/dirtyVolumes",
			Value:     sets.List(dirtyVolumes),
		}
		patches = append(patches, patch1)

		if err := r.patchPodStatus(context.TODO(), patches); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) getNSAddedDevices(rackState *RackState) ([]string, error) {
	var (
		rackStatus asdbv1.Rack
		volumes    []string
	)

	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return nil, err
	}

	rackFound := false

	for idx := range newAeroCluster.Status.RackConfig.Racks {
		rackStatus = newAeroCluster.Status.RackConfig.Racks[idx]
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
			if specNamespace.(map[string]interface{})["name"] != statusNamespace.(map[string]interface{})["name"] {
				continue
			}

			namespaceFound = true
			specStorage := specNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})
			statusStorage := statusNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})

			var specDevices []string

			statusDevices := sets.Set[string]{}

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

		if !namespaceFound {
			r.Log.Info(
				"Namespace added",
			)

			specStorage := specNamespace.(map[string]interface{})[asdbv1.ConfKeyStorageEngine].(map[string]interface{})
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

func (r *SingleClusterReconciler) handleNSOrDeviceAddition(volumes []string, podName string) RestartType {
	podStatus := r.aeroCluster.Status.Pods[podName]

	for _, volume := range volumes {
		r.Log.Info(
			"Checking dirty volumes list", "device", volume, "podname", podName,
		)

		if utils.ContainsString(podStatus.DirtyVolumes, volume) {
			return podRestart
		}
	}

	return quickRestart
}

func getVolumeNameFromDevicePath(volumes []asdbv1.VolumeSpec, s string) string {
	for idx := range volumes {
		if volumes[idx].Aerospike.Path == s {
			return volumes[idx].Name
		}
	}

	return ""
}

func (r *SingleClusterReconciler) deleteFileStorage(podName, fileName string) error {
	cmd := []string{
		"bash", "-c", fmt.Sprintf(
			"rm -rf %s",
			fileName,
		),
	}
	r.Log.Info(
		"Deleting file", "file", fileName, "cmd", cmd, "podname", podName,
	)

	podNamespacedName := types.NamespacedName{Name: podName, Namespace: r.aeroCluster.Namespace}

	stdout, stderr, err := utils.Exec(podNamespacedName, asdbv1.AerospikeServerContainerName,
		cmd, r.KubeClient, r.KubeConfig)

	if err != nil {
		r.Log.V(1).Info(
			"File deletion failed", "err", err, "podName", podName, "stdout",
			stdout, "stderr", stderr,
		)

		return fmt.Errorf("error deleting file %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) getConfigMap(rackID int) (*corev1.ConfigMap, error) {
	cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rackID)
	confMap := &corev1.ConfigMap{}

	if err := r.Client.Get(context.TODO(), cmName, confMap); err != nil {
		return nil, err
	}

	return confMap, nil
}

// isAllDynamicConfig checks if all the configuration changes can be applied dynamically
func isAllDynamicConfig(log logger, specToStatusDiffs asconfig.DynamicConfigMap, version string) bool {
	isDynamic, err := asconfig.IsAllDynamicConfig(log, specToStatusDiffs, version)
	if err != nil {
		log.Info("Failed to check if all config is dynamic, fallback to rolling restart", "error", err.Error())
		return false
	}

	if !isDynamic {
		log.Info("Config contains static field changes; dynamic update not possible.")
		return false
	}

	return true
}

func getFlatConfig(log logger, confStr string) (*asconfig.Conf, error) {
	asConf, err := asconfig.NewASConfigFromBytes(log, []byte(confStr), asconfig.AeroConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load config map by lib: %v", err)
	}

	return asConf.GetFlatMap(), nil
}

// getConfDiff retrieves the configuration differences between the spec and status Aerospike configurations.
func getConfDiff(log logger, specConfig map[string]interface{}, podAnnotations map[string]string,
	version string) (asconfig.DynamicConfigMap, error) {
	statusFromAnnotation, ok := podAnnotations["aerospikeConf"]
	if !ok {
		log.Info("Pod annotation 'aerospikeConf' missing")
		return nil, nil
	}

	asConfStatus, err := getFlatConfig(log, statusFromAnnotation)
	if err != nil {
		return nil, fmt.Errorf("failed to load config map by lib: %v", err)
	}

	asConf, err := asconfig.NewMapAsConfig(log, specConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load config map by lib: %v", err)
	}

	// special handling for DNE in ldap configurations
	specConfFile := asConf.ToConfFile()
	specConfFile = strings.ReplaceAll(specConfFile, "$${_DNE}{un}", "${un}")
	specConfFile = strings.ReplaceAll(specConfFile, "$${_DNE}{dn}", "${dn}")

	asConfSpec, err := getFlatConfig(log, specConfFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config map by lib: %v", err)
	}

	specToStatusDiffs, err := asconfig.ConfDiff(log, *asConfSpec, *asConfStatus,
		true, version)
	if err != nil {
		log.Info("Failed to get config diff to change config dynamically, fallback to rolling restart",
			"error", err.Error())
		return nil, nil
	}

	return specToStatusDiffs, nil
}

func (r *SingleClusterReconciler) patchPodStatus(ctx context.Context, patches []jsonpatch.PatchOperation) error {
	if len(patches) == 0 {
		return nil
	}

	jsonPatchJSON, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	constantPatch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

	return retry.OnError(retry.DefaultBackoff, func(_ error) bool {
		// Customize the error check for retrying, return true to retry, false to stop retrying
		return true
	}, func() error {
		// Patch the resource
		// Since the pod status is updated from pod init container,
		// set the field owner to "pod" for pod status updates.
		if err := r.Client.Status().Patch(
			ctx, r.aeroCluster, constantPatch, client.FieldOwner("pod"),
		); err != nil {
			return fmt.Errorf("error updating status: %v", err)
		}

		r.Log.Info("Pod status patched successfully")

		return nil
	})
}

func (r *SingleClusterReconciler) onDemandOperationType(podName string, onDemandQuickRestarts,
	onDemandPodRestarts sets.Set[string]) RestartType {
	switch {
	case onDemandQuickRestarts.Has(podName):
		return quickRestart
	case onDemandPodRestarts.Has(podName):
		return podRestart
	}

	return noRestart
}

func (r *SingleClusterReconciler) updateOperationStatus(restartedASDPodNames, restartedPodNames []string) error {
	if len(restartedASDPodNames)+len(restartedPodNames) == 0 || len(r.aeroCluster.Spec.Operations) == 0 {
		return nil
	}

	statusOps := lib.DeepCopy(r.aeroCluster.Status.Operations).([]asdbv1.OperationSpec)

	allPodNames := asdbv1.GetAllPodNames(r.aeroCluster.Status.Pods)

	quickRestartsSet := sets.New(restartedASDPodNames...)
	podRestartsSet := sets.New(restartedPodNames...)

	specOp := r.aeroCluster.Spec.Operations[0]

	var specPods sets.Set[string]

	// If no pod list is provided, it indicates that all pods need to be restarted.
	if len(specOp.PodList) == 0 {
		specPods = allPodNames
	} else {
		specPods = sets.New(specOp.PodList...)
	}

	opFound := false

	for idx := range statusOps {
		statusOp := &statusOps[idx]
		if statusOp.ID == specOp.ID {
			opFound = true

			if len(statusOp.PodList) != 0 {
				statusPods := sets.New(statusOp.PodList...)

				if statusOp.Kind == asdbv1.OperationWarmRestart {
					if quickRestartsSet.Len() > 0 {
						statusOp.PodList = statusPods.Union(quickRestartsSet.Intersection(specPods)).UnsortedList()
					}

					// If the operation is a warm restart and the pod undergoes a cold restart for any reason,
					// we will still consider the warm restart operation as completed for that pod.
					if podRestartsSet.Len() > 0 {
						statusOp.PodList = statusPods.Union(podRestartsSet.Intersection(specPods)).UnsortedList()
					}
				}

				if statusOp.Kind == asdbv1.OperationPodRestart && podRestartsSet != nil {
					statusOp.PodList = statusPods.Union(podRestartsSet.Intersection(specPods)).UnsortedList()
				}
			}

			break
		}
	}

	if !opFound {
		var podList []string

		if specOp.Kind == asdbv1.OperationWarmRestart {
			if quickRestartsSet.Len() > 0 {
				podList = quickRestartsSet.Intersection(specPods).UnsortedList()
			}

			// If the operation is a warm restart and the pod undergoes a cold restart for any reason,
			// we will still consider the warm restart operation as completed for that pod.
			if podRestartsSet.Len() > 0 {
				podList = append(podList, podRestartsSet.Intersection(specPods).UnsortedList()...)
			}
		}

		if specOp.Kind == asdbv1.OperationPodRestart && podRestartsSet != nil {
			podList = podRestartsSet.Intersection(specPods).UnsortedList()
		}

		statusOps = append(statusOps, asdbv1.OperationSpec{
			ID:      specOp.ID,
			Kind:    specOp.Kind,
			PodList: podList,
		})
	}

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return err
	}

	newAeroCluster.Status.Operations = statusOps

	if err := r.patchStatus(newAeroCluster); err != nil {
		return fmt.Errorf("error updating status: %w", err)
	}

	return nil
}

// podsToRestart returns the pods that need to be restarted(quick/pod restart) based on the on-demand operations.
func (r *SingleClusterReconciler) podsToRestart() (quickRestarts, podRestarts sets.Set[string]) {
	quickRestarts = make(sets.Set[string])
	podRestarts = make(sets.Set[string])

	specOps := r.aeroCluster.Spec.Operations
	statusOps := r.aeroCluster.Status.Operations
	allPodNames := asdbv1.GetAllPodNames(r.aeroCluster.Status.Pods)

	// If no spec operations, no pods to restart
	// If the Spec.Operations and Status.Operations are equal, no pods to restart.
	if len(specOps) == 0 || reflect.DeepEqual(specOps, statusOps) {
		return quickRestarts, podRestarts
	}

	// Assuming only one operation is present in the spec.
	specOp := specOps[0]

	var (
		podsToRestart, specPods sets.Set[string]
	)
	// If no pod list is provided, it indicates that all pods need to be restarted.
	if len(specOp.PodList) == 0 {
		specPods = allPodNames
	} else {
		specPods = sets.New(specOp.PodList...)
	}

	opFound := false

	// If the operation is not present in the status, all pods need to be restarted.
	// If the operation is present in the status, only the pods that are not present in the status need to be restarted.
	// If the operation is present in the status and podList is empty, no pods need to be restarted.
	for _, statusOp := range statusOps {
		if statusOp.ID != specOp.ID {
			continue
		}

		var statusPods sets.Set[string]
		if len(statusOp.PodList) == 0 {
			statusPods = allPodNames
		} else {
			statusPods = sets.New(statusOp.PodList...)
		}

		podsToRestart = specPods.Difference(statusPods)
		opFound = true

		break
	}

	if !opFound {
		podsToRestart = specPods
	}

	// Separate pods to be restarted based on operation type
	if podsToRestart != nil && podsToRestart.Len() > 0 {
		switch specOp.Kind {
		case asdbv1.OperationWarmRestart:
			quickRestarts.Insert(podsToRestart.UnsortedList()...)
		case asdbv1.OperationPodRestart:
			podRestarts.Insert(podsToRestart.UnsortedList()...)
		}
	}

	return quickRestarts, podRestarts
}

// shouldSetMigrateFillDelay determines if migrate-fill-delay should be set.
// It only returns true if the following conditions are met:
// 1. DeleteLocalStorageOnRestart is set to true.
// 2. At least one pod needs to be restarted.
// 3. At least one persistent volume is using a local storage class.
func (r *SingleClusterReconciler) shouldSetMigrateFillDelay(rackState *RackState,
	podsToRestart []*corev1.Pod, restartTypeMap map[string]RestartType) bool {
	if !asdbv1.GetBool(rackState.Rack.Storage.DeleteLocalStorageOnRestart) {
		return false
	}

	var podRestartNeeded bool

	// If restartTypeMap is nil, we assume that a pod restart is needed.
	if restartTypeMap == nil {
		podRestartNeeded = true
	} else {
		for idx := range podsToRestart {
			pod := podsToRestart[idx]
			restartType := restartTypeMap[pod.Name]

			if restartType == podRestart {
				podRestartNeeded = true
				break
			}
		}
	}

	if !podRestartNeeded {
		return false
	}

	localStorageClassSet := sets.NewString(rackState.Rack.Storage.LocalStorageClasses...)

	for idx := range rackState.Rack.Storage.Volumes {
		volume := &rackState.Rack.Storage.Volumes[idx]
		if volume.Source.PersistentVolume != nil &&
			localStorageClassSet.Has(volume.Source.PersistentVolume.StorageClass) {
			return true
		}
	}

	return false
}

// isAnyPodSpecUpdated checks if any pod spec has been updated indirectly based on
// aerospikeConfig or aerospikeNetworkPolicy change
func (r *SingleClusterReconciler) isAnyPodSpecUpdated(rackState *RackState,
	pod *corev1.Pod) (bool, error) {
	// Creating a local copy of the statefulset to avoid modifying the original object
	sts, err := r.getSTS(rackState)
	if err != nil {
		return false, err
	}

	// Currently just checking the server container ports, but can be extended to other fields as needed
	return r.checkForPortsUpdate(sts, pod)
}

func (r *SingleClusterReconciler) checkForPortsUpdate(sts *appsv1.StatefulSet, pod *corev1.Pod,
) (bool, error) {
	r.updateSTSPorts(sts)

	stsServerContainer := getContainer(sts.Spec.Template.Spec.Containers, asdbv1.AerospikeServerContainerName)
	serverContainer := getContainer(pod.Spec.Containers, asdbv1.AerospikeServerContainerName)

	if serverContainer == nil || stsServerContainer == nil {
		return false, fmt.Errorf("server container not found in pod or statefulset")
	}

	desiredContainerPorts := stsServerContainer.Ports
	currentContainerPorts := serverContainer.Ports

	for idx := range desiredContainerPorts {
		desiredPort := desiredContainerPorts[idx]

		var portFound bool

		for jdx := range currentContainerPorts {
			currentPort := currentContainerPorts[jdx]

			if desiredPort.Name != currentPort.Name {
				continue
			}

			portFound = true

			// Check for
			// 1. Container port change
			// 2. Host port change from 0 to non-zero (indicating a change from not exposed to exposed)
			// Ignore the case where host Port is disabled (0).
			if desiredPort.ContainerPort != currentPort.ContainerPort ||
				(desiredPort.HostPort != 0 && currentPort.HostPort == 0) {
				r.Log.Info(
					"Pod spec is updated, container port changed",
					"podName", pod.Name, "desiredPort", desiredPort, "currentPort", currentPort,
				)

				return true, nil
			}
		}

		if !portFound {
			return true, nil
		}
	}

	return false, nil
}
