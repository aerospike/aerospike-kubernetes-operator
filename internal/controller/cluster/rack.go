package cluster

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/asconfig"
)

type scaledDownRack struct {
	rackSTS   *appsv1.StatefulSet
	rackState *RackState
}

func (r *SingleClusterReconciler) reconcileRacks() common.ReconcileResult {
	r.Log.Info("Reconciling rack for AerospikeCluster")

	var (
		scaledDownRackList []scaledDownRack
		res                common.ReconcileResult
	)

	rackStateList := getConfiguredRackStateList(r.aeroCluster)

	racksToDelete, err := r.getRacksToDelete(rackStateList)
	if err != nil {
		return common.ReconcileError(err)
	}

	rackIDsToDelete := make([]int, 0, len(racksToDelete))
	for idx := range racksToDelete {
		rackIDsToDelete = append(rackIDsToDelete, racksToDelete[idx].ID)
	}

	ignorablePodNames, err := r.getIgnorablePods(racksToDelete, rackStateList)
	if err != nil {
		return common.ReconcileError(err)
	}

	r.Log.Info(
		"Rack changes", "racksToDelete", rackIDsToDelete, "ignorablePods",
		ignorablePodNames.UnsortedList(),
	)

	// Handle failed racks
	for idx := range rackStateList {
		var podList []*corev1.Pod

		state := &rackStateList[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, state.Rack.ID)

		if err = r.Client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			continue
		}

		// 1. Fetch the pods for the rack and if there are failed pods then reconcile rack
		podList, err = r.getOrderedRackPodList(state.Rack.ID)
		if err != nil {
			return common.ReconcileError(
				fmt.Errorf(
					"failed to list pods: %v", err,
				),
			)
		}

		failedPods, _ := getFailedAndActivePods(podList)
		// remove ignorable pods from failedPods
		failedPods = getNonIgnorablePods(failedPods, ignorablePodNames)
		if len(failedPods) != 0 {
			r.Log.Info(
				"Reconcile the failed pods in the Rack", "rackID", state.Rack.ID, "failedPods", getPodNames(failedPods),
			)

			if res = r.reconcileRack(
				found, state, ignorablePodNames, failedPods,
			); !res.IsSuccess {
				return res
			}

			r.Log.Info(
				"Reconciled the failed pods in the Rack", "rackID", state.Rack.ID, "failedPods", getPodNames(failedPods),
			)
		}

		// 2. Again, fetch the pods for the rack and if there are failed pods then restart them.
		// This is needed in cases where hash values generated from CR spec are same as hash values in pods.
		// But, pods are in failed state due to their bad spec.
		// e.g. configuring unschedulable resources in CR podSpec and reverting them to old value.
		podList, err = r.getOrderedRackPodList(state.Rack.ID)
		if err != nil {
			return common.ReconcileError(
				fmt.Errorf(
					"failed to list pods: %v", err,
				),
			)
		}

		failedPods, _ = getFailedAndActivePods(podList)
		// remove ignorable pods from failedPods
		failedPods = getNonIgnorablePods(failedPods, ignorablePodNames)
		if len(failedPods) != 0 {
			r.Log.Info(
				"Restart the failed pods in the Rack", "rackID", state.Rack.ID, "failedPods", getPodNames(failedPods),
			)

			if _, res = r.rollingRestartRack(
				found, state, ignorablePodNames, nil,
				failedPods,
			); !res.IsSuccess {
				return res
			}

			r.Log.Info(
				"Restarted the failed pods in the Rack", "rackID", state.Rack.ID, "failedPods", getPodNames(failedPods),
			)
			// Requeue after 1 second to fetch latest CR object with updated pod status
			return common.ReconcileRequeueAfter(1)
		}
	}

	for idx := range rackStateList {
		state := &rackStateList[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, state.Rack.ID)

		if err = r.Client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in Reconcile
			zeroSizedRack := &RackState{Rack: state.Rack, Size: 0}

			found, res = r.createEmptyRack(zeroSizedRack)
			if !res.IsSuccess {
				return res
			}
		}

		// Get list of scaled down racks
		if *found.Spec.Replicas > int32(state.Size) {
			scaledDownRackList = append(scaledDownRackList, scaledDownRack{rackSTS: found, rackState: state})
		} else {
			// Reconcile other statefulset
			if res = r.reconcileRack(
				found, state, ignorablePodNames, nil,
			); !res.IsSuccess {
				return res
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx := range scaledDownRackList {
		state := scaledDownRackList[idx].rackState
		sts := scaledDownRackList[idx].rackSTS

		if res = r.reconcileRack(sts, state, ignorablePodNames, nil); !res.IsSuccess {
			return res
		}
	}

	if len(r.aeroCluster.Status.RackConfig.Racks) != 0 {
		// Remove removed racks
		if res = r.deleteRacks(racksToDelete, ignorablePodNames); !res.IsSuccess {
			if res.Err != nil {
				r.Log.Error(
					err, "Failed to remove statefulset for removed racks",
					"err", res.Err,
				)
			}

			return res
		}
	}

	// Wait for pods across all racks to get ready before completing
	// reconcile for the racks. The STS may be correctly updated but the pods
	// might not be ready if they are running long-running init scripts or
	// aerospike index load.
	for idx := range rackStateList {
		state := &rackStateList[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, state.Rack.ID)

		if err := r.Client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in Reconcile
			zeroSizedRack := &RackState{Rack: state.Rack, Size: 0}
			found, res = r.createEmptyRack(zeroSizedRack)

			if !res.IsSuccess {
				return res
			}
		}

		// Wait for pods to be ready.
		if err := r.waitForSTSToBeReady(found, ignorablePodNames); err != nil {
			// If the wait times out try again.
			// The wait is required in cases where scale up waits for a pod to
			// terminate times out and event is re-queued.
			// Next reconcile will not invoke scale up or down and will
			// fall through,
			// and might run reconcile steps common to all racks before the racks
			// have scaled up.
			r.Log.Error(
				err, "Failed to wait for statefulset to be ready",
				"STS", stsName,
			)

			return common.ReconcileRequeueAfter(1)
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) createEmptyRack(rackState *RackState) (
	*appsv1.StatefulSet, common.ReconcileResult,
) {
	r.Log.Info("Create new Aerospike cluster if needed")

	// NoOp if already exist
	r.Log.Info("AerospikeCluster", "Spec", r.aeroCluster.Spec)

	// Bad config should not come here. It should be validated in validation hook
	cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rackState.Rack.ID)
	if err := r.buildSTSConfigMap(cmName, rackState.Rack); err != nil {
		r.Log.Error(err, "Failed to create configMap from AerospikeConfig")
		return nil, common.ReconcileError(err)
	}

	stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rackState.Rack.ID)

	found, err := r.createSTS(stsName, rackState)
	if err != nil {
		r.Log.Error(
			err, "Statefulset setup failed. Deleting statefulset", "name",
			stsName, "err", err,
		)

		// Delete statefulset and everything related so that it can be properly created and updated in next run
		_ = r.deleteSTS(found)

		return nil, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackCreated",
		"[rack-%d] Created Rack", rackState.Rack.ID,
	)

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) getRacksToDelete(rackStateList []RackState) (
	[]asdbv1.Rack, error,
) {
	oldRacks, err := r.getCurrentRackList()
	if err != nil {
		return nil, err
	}

	var toDelete []asdbv1.Rack

	for oldRackIdx := range oldRacks {
		var rackFound bool

		for rackStateIdx := range rackStateList {
			if oldRacks[oldRackIdx].ID == rackStateList[rackStateIdx].Rack.ID {
				rackFound = true
				break
			}
		}

		if !rackFound {
			toDelete = append(toDelete, oldRacks[oldRackIdx])
		}
	}

	return toDelete, nil
}

func (r *SingleClusterReconciler) deleteRacks(
	racksToDelete []asdbv1.Rack, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	for idx := range racksToDelete {
		rack := &racksToDelete[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rack.ID)

		err := r.Client.Get(context.TODO(), stsName, found)
		if err != nil {
			// If not found then go to next
			if errors.IsNotFound(err) {
				continue
			}

			return common.ReconcileError(err)
		}

		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		rackState := &RackState{Size: 0, Rack: rack}

		found, res := r.scaleDownRack(found, rackState, ignorablePodNames)
		if !res.IsSuccess {
			return res
		}

		// Delete sts
		if err = r.deleteSTS(found); err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, "STSDeleteFailed",
				"[rack-%d] Failed to delete {STS: %s/%s}", rack.ID,
				found.Namespace, found.Name,
			)

			return common.ReconcileError(err)
		}

		// Delete configMap
		cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rack.ID)
		if err = r.deleteRackConfigMap(cmName); err != nil {
			return common.ReconcileError(err)
		}

		// Rack cleanup is done. Take time and cleanup dangling nodes and related resources that may not have been
		// cleaned up previously due to errors.
		if err = r.cleanupDanglingPodsRack(found, rackState); err != nil {
			return common.ReconcileError(err)
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "RackDeleted",
			"[rack-%d] Deleted Rack", rack.ID,
		)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) upgradeOrRollingRestartRack(
	found *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	var res common.ReconcileResult
	// Always update configMap. We won't be able to find if a rack's config, and it's pod config is in sync or not
	// Checking rack.spec, rack.status will not work.
	// We may change config, let some pods restart with new config and then change config back to original value.
	// Now rack.spec, rack.status will be the same, but few pods will have changed config.
	// So a check based on spec and status will skip configMap update.
	// Hence, a rolling restart of pod will never bring pod to desired config
	if err := r.updateSTSConfigMap(
		utils.GetNamespacedNameForSTSOrConfigMap(
			r.aeroCluster, rackState.Rack.ID,
		), rackState.Rack,
	); err != nil {
		r.Log.Error(
			err, "Failed to update configMap from AerospikeConfig", "stsName",
			found.Name,
		)

		return found, common.ReconcileError(err)
	}

	// Handle enable security just after updating configMap.
	// This code will run only when security is being enabled in an existing cluster
	// Update for security is verified by checking the config hash of the pod with the
	// config hash present in config map
	if err := r.handleEnableSecurity(rackState, ignorablePodNames); err != nil {
		return found, common.ReconcileError(err)
	}

	// Upgrade
	upgradeNeeded, err := r.isRackUpgradeNeeded(rackState.Rack.ID, ignorablePodNames)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	if upgradeNeeded {
		found, res = r.upgradeRack(found, rackState, ignorablePodNames, failedPods)
		if !res.IsSuccess {
			if res.Err != nil {
				r.Log.Error(
					res.Err, "Failed to update StatefulSet image", "stsName",
					found.Name,
				)

				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning,
					"RackImageUpdateFailed",
					"[rack-%d] Failed to update Image {STS: %s/%s}",
					rackState.Rack.ID, found.Namespace, found.Name,
				)
			}

			return found, res
		}
	} else {
		var rollingRestartInfo, nErr = r.getRollingRestartInfo(rackState, ignorablePodNames)
		if nErr != nil {
			return found, common.ReconcileError(nErr)
		}

		if rollingRestartInfo.needRestart {
			found, res = r.rollingRestartRack(
				found, rackState, ignorablePodNames, rollingRestartInfo.restartTypeMap, failedPods,
			)
			if !res.IsSuccess {
				if res.Err != nil {
					r.Log.Error(
						res.Err, "Failed to do rolling restart", "stsName",
						found.Name,
					)

					r.Recorder.Eventf(
						r.aeroCluster, corev1.EventTypeWarning,
						"RackRollingRestartFailed",
						"[rack-%d] Failed to do rolling restart {STS: %s/%s}",
						rackState.Rack.ID, found.Namespace, found.Name,
					)
				}

				return found, res
			}
		}

		if len(failedPods) == 0 && rollingRestartInfo.needUpdateConf {
			res = r.updateDynamicConfig(
				rackState, ignorablePodNames,
				rollingRestartInfo.restartTypeMap, rollingRestartInfo.dynamicConfDiffPerPod,
			)
			if !res.IsSuccess {
				if res.Err != nil {
					r.Log.Error(
						res.Err, "Failed to do dynamic update", "stsName",
						found.Name,
					)

					r.Recorder.Eventf(
						r.aeroCluster, corev1.EventTypeWarning,
						"RackDynamicConfigUpdateFailed",
						"[rack-%d] Failed to update aerospike config dynamically {STS: %s/%s}",
						rackState.Rack.ID, found.Namespace, found.Name,
					)
				}

				return found, res
			}
		}
	}

	if r.aeroCluster.Spec.RackConfig.MaxIgnorablePods != nil {
		if res = r.handleNSOrDeviceRemovalForIgnorablePods(rackState, ignorablePodNames); !res.IsSuccess {
			return found, res
		}
	}

	// handle k8sNodeBlockList pods only if it is changed
	if !reflect.DeepEqual(r.aeroCluster.Spec.K8sNodeBlockList, r.aeroCluster.Status.K8sNodeBlockList) {
		found, res = r.handleK8sNodeBlockListPods(found, rackState, ignorablePodNames, failedPods)
		if !res.IsSuccess {
			return found, res
		}
	}

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) updateDynamicConfig(
	rackState *RackState,
	ignorablePodNames sets.Set[string], restartTypeMap map[string]RestartType,
	dynamicConfDiffPerPod map[string]asconfig.DynamicConfigMap,
) common.ReconcileResult {
	r.Log.Info("Update dynamic config in Aerospike pods")

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "DynamicConfigUpdate",
		"[rack-%d] Started dynamic config update", rackState.Rack.ID,
	)

	var (
		err     error
		podList []*corev1.Pod
	)

	// List the pods for this aeroCluster's statefulset
	podList, err = r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	// Find pods which needs restart
	podsToUpdate := make([]*corev1.Pod, 0, len(podList))

	for idx := range podList {
		pod := podList[idx]

		restartType := restartTypeMap[pod.Name]
		if restartType != noRestartUpdateConf {
			r.Log.Info("This Pod doesn't need any update, Skip this", "pod", pod.Name)
			continue
		}

		podsToUpdate = append(podsToUpdate, pod)
	}

	if res := r.setDynamicConfig(dynamicConfDiffPerPod, podsToUpdate, ignorablePodNames); !res.IsSuccess {
		return res
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "DynamicConfigUpdate",
		"[rack-%d] Finished Dynamic config update", rackState.Rack.ID,
	)

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) handleNSOrDeviceRemovalForIgnorablePods(
	rackState *RackState, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	podList, err := r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
	}
	// Filter ignoredPods to update their dirtyVolumes in the status.
	// IgnoredPods are skipped from upgrade/rolling restart, and as a result in case of device removal, dirtyVolumes
	// are not updated in their pod status. This makes devices un-reusable as they cannot be cleaned up during init phase.
	// So, explicitly add dirtyVolumes for ignoredPods, so that they can be cleaned in the init phase.
	var ignoredPod []*corev1.Pod

	for idx := range podList {
		pod := podList[idx]
		// Pods, that are not in status are not even initialized, so no need to update dirtyVolumes.
		if _, ok := r.aeroCluster.Status.Pods[pod.Name]; ok {
			if ignorablePodNames.Has(pod.Name) {
				ignoredPod = append(ignoredPod, pod)
			}
		}
	}

	if len(ignoredPod) > 0 {
		if err := r.handleNSOrDeviceRemoval(rackState, ignoredPod); err != nil {
			return common.ReconcileError(err)
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) reconcileRack(
	found *appsv1.StatefulSet, rackState *RackState, ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) common.ReconcileResult {
	r.Log.Info(
		"Reconcile existing Aerospike cluster statefulset", "stsName",
		found.Name,
	)

	var res common.ReconcileResult

	r.Log.Info(
		"Ensure rack StatefulSet size is the same as the spec", "stsName",
		found.Name,
	)

	desiredSize := int32(rackState.Size)
	currentSize := *found.Spec.Replicas

	// Scale down
	if currentSize > desiredSize {
		found, res = r.scaleDownRack(found, rackState, ignorablePodNames)
		if !res.IsSuccess {
			if res.Err != nil {
				r.Log.Error(
					res.Err, "Failed to scaleDown StatefulSet pods", "stsName",
					found.Name,
				)

				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning,
					"RackScaleDownFailed",
					"[rack-%d] Failed to scale-down {STS %s/%s, currentSize: %d desiredSize: %d}: %s",
					rackState.Rack.ID, found.Namespace, found.Name, currentSize,
					desiredSize, res.Err,
				)
			}

			return res
		}
	}

	if failedPods == nil {
		// Revert migrate-fill-delay to original value if it was set to 0 during scale down.
		// Reset will be done if there is scale-down or Rack redistribution.
		// This check won't cover a scenario where scale-down operation was done and then reverted to previous value
		// before the scale down could complete.
		if (r.aeroCluster.Status.Size > r.aeroCluster.Spec.Size) ||
			(!r.IsStatusEmpty() && len(r.aeroCluster.Status.RackConfig.Racks) != len(r.aeroCluster.Spec.RackConfig.Racks)) {
			if res = r.setMigrateFillDelay(
				r.getClientPolicy(), &rackState.Rack.AerospikeConfig, false,
				nil,
			); !res.IsSuccess {
				r.Log.Error(res.Err, "Failed to revert migrate-fill-delay after scale down")
				return res
			}
		}
	}

	if err := r.updateAerospikeInitContainerImage(found); err != nil {
		r.Log.Error(
			err, "Failed to update Aerospike Init container", "stsName",
			found.Name,
		)

		return common.ReconcileError(err)
	}

	found, res = r.upgradeOrRollingRestartRack(found, rackState, ignorablePodNames, failedPods)
	if !res.IsSuccess {
		return res
	}

	// Scale up after upgrading, so that new pods come up with new image
	currentSize = *found.Spec.Replicas
	if currentSize < desiredSize {
		found, res = r.scaleUpRack(found, rackState, ignorablePodNames)
		if !res.IsSuccess {
			r.Log.Error(
				res.Err, "Failed to scaleUp StatefulSet pods", "stsName",
				found.Name,
			)

			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, "RackScaleUpFailed",
				"[rack-%d] Failed to scale-up {STS %s/%s, currentSize: %d desiredSize: %d}: %s",
				rackState.Rack.ID, found.Namespace, found.Name, currentSize,
				desiredSize, res.Err,
			)

			return res
		}
	}

	// All regular operations are complete. Take time and cleanup dangling nodes that have not been cleaned up
	// previously due to errors.
	if err := r.cleanupDanglingPodsRack(found, rackState); err != nil {
		return common.ReconcileError(err)
	}

	if err := r.reconcilePodService(rackState); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) scaleUpRack(
	found *appsv1.StatefulSet, rackState *RackState, ignorablePodNames sets.Set[string],
) (
	*appsv1.StatefulSet, common.ReconcileResult,
) {
	desiredSize := int32(rackState.Size)

	oldSz := *found.Spec.Replicas

	r.Log.Info("Scaling up pods", "currentSz", oldSz, "desiredSz", desiredSize)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackScaleUp",
		"[rack-%d] Scaling-up {STS %s/%s, currentSize: %d desiredSize: %d}",
		rackState.Rack.ID, found.Namespace, found.Name, oldSz, desiredSize,
	)

	// No need for this? But if the image is bad, then new pod will also come up
	// with bad node.
	podList, err := r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return found, common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	if r.isAnyPodInImageFailedState(podList, ignorablePodNames) {
		return found, common.ReconcileError(fmt.Errorf("cannot scale up AerospikeCluster. A pod is already in failed state"))
	}

	var newPodNames []string
	for i := oldSz; i < desiredSize; i++ {
		newPodNames = append(newPodNames, getSTSPodName(found.Name, i))
	}

	// Ensure none of the to be launched pods are active.
	for _, newPodName := range newPodNames {
		for idx := range podList {
			if podList[idx].Name == newPodName {
				return found, common.ReconcileError(
					fmt.Errorf(
						"pod %s yet to be launched is still present",
						newPodName,
					),
				)
			}
		}
	}

	if err = r.cleanupDanglingPodsRack(found, rackState); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"failed scale up pre-check: %v", err,
			),
		)
	}

	// update replicas here to avoid new replicas count comparison while cleaning up dangling pods of rack
	found.Spec.Replicas = &desiredSize

	// Scale up the statefulset
	if err = r.Client.Update(context.TODO(), found, common.UpdateOption); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"failed to update StatefulSet pods: %v", err,
			),
		)
	}

	// return a fresh copy
	found, err = r.getSTS(rackState)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackScaledUp",
		"[rack-%d] Scaled-up {STS: %s/%s, currentSize: %d desiredSize: %d}",
		rackState.Rack.ID, found.Namespace, found.Name, *found.Spec.Replicas,
		desiredSize,
	)

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) upgradeRack(
	statefulSet *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	var (
		err     error
		podList []*corev1.Pod
	)

	if len(failedPods) != 0 {
		podList = failedPods
	} else {
		// List the pods for this aeroCluster's statefulset
		podList, err = r.getOrderedRackPodList(rackState.Rack.ID)
		if err != nil {
			return statefulSet, common.ReconcileError(
				fmt.Errorf(
					"failed to list pods: %v", err,
				),
			)
		}
	}

	// Update STS definition. The operation is idempotent, so it's ok to call
	// it without checking for a change in the spec.
	//
	// Update strategy for statefulSet is OnDelete, so client.Update will not start update.
	// Update will happen only when a pod is deleted.
	// So first update image in STS and then delete a pod.
	// Pod will come up with new image.
	// Repeat the above process.
	err = r.updateSTS(statefulSet, rackState)
	if err != nil {
		return statefulSet, common.ReconcileError(
			fmt.Errorf("upgrade rack : %v", err),
		)
	}

	// Find pods which need to be updated
	podsToUpgrade := make([]*corev1.Pod, 0, len(podList))

	for idx := range podList {
		pod := podList[idx]
		r.Log.Info("Check if pod needs upgrade or not", "podName", pod.Name)

		if ignorablePodNames.Has(pod.Name) {
			r.Log.Info("Pod found in ignore pod list, skipping", "podName", pod.Name)
			continue
		}

		if r.isPodUpgraded(pod) {
			r.Log.Info("Pod doesn't need upgrade", "podName", pod.Name)
			continue
		}

		podsToUpgrade = append(podsToUpgrade, pod)
	}

	var podsBatchList [][]*corev1.Pod

	if len(failedPods) != 0 {
		// creating a single batch of all failed pods in a rack, irrespective of batch size
		r.Log.Info("Skipping batchSize for failed pods")

		podsBatchList = make([][]*corev1.Pod, 1)
		podsBatchList[0] = podsToUpgrade
	} else {
		// Create batch of pods
		podsBatchList = getPodsBatchList(
			r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize, podsToUpgrade, len(podList),
		)
	}

	if len(podsBatchList) > 0 {
		// Handle one batch
		podsBatch := podsBatchList[0]

		r.Log.Info(
			"Calculated batch for doing rolling upgrade",
			"rackPodList", getPodNames(podList),
			"rearrangedPods", getPodNames(podsToUpgrade),
			"podsBatch", getPodNames(podsBatch),
			"rollingUpdateBatchSize", r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize,
		)

		podNames := getPodNames(podsBatch)
		if err = r.createOrUpdatePodServiceIfNeeded(podNames); err != nil {
			return nil, common.ReconcileError(err)
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "PodImageUpdate",
			"[rack-%d] Updating Containers on Pods %v", rackState.Rack.ID, podNames,
		)

		res := r.safelyDeletePodsAndEnsureImageUpdated(rackState, podsBatch, ignorablePodNames)
		if !res.IsSuccess {
			return statefulSet, res
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "PodImageUpdated",
			"[rack-%d] Updated Containers on Pods %v", rackState.Rack.ID, podNames,
		)

		// Handle the next batch in subsequent Reconcile.
		if len(podsBatchList) > 1 {
			return statefulSet, common.ReconcileRequeueAfter(1)
		}
	}

	// If it was last batch then go ahead return a fresh copy
	statefulSet, err = r.getSTS(rackState)
	if err != nil {
		return statefulSet, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackImageUpdated",
		"[rack-%d] Image Updated {STS: %s/%s}", rackState.Rack.ID, statefulSet.Namespace, statefulSet.Name,
	)

	return statefulSet, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) scaleDownRack(
	found *appsv1.StatefulSet, rackState *RackState, ignorablePodNames sets.Set[string],
) (*appsv1.StatefulSet, common.ReconcileResult) {
	desiredSize := int32(rackState.Size)

	// Continue if scaleDown is not needed
	if *found.Spec.Replicas <= desiredSize {
		return found, common.ReconcileSuccess()
	}

	r.Log.Info(
		"ScaleDown AerospikeCluster statefulset", "desiredSize", desiredSize,
		"currentSize", *found.Spec.Replicas,
	)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackScaleDown",
		"[rack-%d] Scaling-down {STS:%s/%s, currentSize: %d desiredSize: %d",
		rackState.Rack.ID, found.Namespace, found.Name, *found.Spec.Replicas,
		desiredSize,
	)

	oldPodList, err := r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return found, common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	if r.isAnyPodInImageFailedState(oldPodList, ignorablePodNames) {
		return found, common.ReconcileError(
			fmt.Errorf("cannot scale down AerospikeCluster. A pod is already in failed state"))
	}

	// Code flow will reach this stage only when found.Spec.Replicas > desiredSize
	// Maintain a list of removed pods. It will be used for alumni-reset and tip-clear

	policy := r.getClientPolicy()
	diffPods := *found.Spec.Replicas - desiredSize

	podsBatchList := getPodsBatchList(
		r.aeroCluster.Spec.RackConfig.ScaleDownBatchSize, oldPodList[:diffPods], len(oldPodList),
	)

	// Handle one batch
	podsBatch := podsBatchList[0]

	r.Log.Info(
		"Calculated batch for Pod scale-down",
		"rackPodList", getPodNames(oldPodList),
		"podsBatch", getPodNames(podsBatch),
		"scaleDownBatchSize", r.aeroCluster.Spec.RackConfig.ScaleDownBatchSize,
	)

	var (
		runningPods             []*corev1.Pod
		isAnyPodRunningAndReady bool
	)

	for idx := range podsBatch {
		if utils.IsPodRunningAndReady(podsBatch[idx]) {
			runningPods = append(runningPods, podsBatch[idx])
			isAnyPodRunningAndReady = true

			continue
		}

		ignorablePodNames.Insert(podsBatch[idx].Name)
	}

	// Ignore safe stop check if all pods in the batch are not running.
	// Ignore migrate-fill-delay if pod is not running. Deleting this pod will not lead to any migration.
	if isAnyPodRunningAndReady {
		if res := r.waitForMultipleNodesSafeStopReady(runningPods, ignorablePodNames); !res.IsSuccess {
			// The pod is running and is unsafe to terminate.
			return found, res
		}

		// set migrate-fill-delay to 0 across all nodes of cluster to scale down fast
		// setting migrate-fill-delay only if pod is running and ready.
		// This check ensures that migrate-fill-delay is not set while processing failed racks.
		// setting migrate-fill-delay will fail if there are any failed pod
		if res := r.setMigrateFillDelay(
			policy, &rackState.Rack.AerospikeConfig, true, ignorablePodNames,
		); !res.IsSuccess {
			return found, res
		}
	}

	// Update new object with new size
	newSize := *found.Spec.Replicas - int32(len(podsBatch))
	found.Spec.Replicas = &newSize

	if err = r.Client.Update(
		context.TODO(), found, common.UpdateOption,
	); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"failed to update pod size %d StatefulSet pods: %v",
				newSize, err,
			),
		)
	}

	// Consider these checks if any pod in the batch is running and ready.
	// If all the pods are not running then we can safely ignore these checks.
	// These checks will fail if there is any other pod in failed state outside the batch.
	if isAnyPodRunningAndReady {
		// Wait for pods to get terminated
		if err = r.waitForSTSToBeReady(found, ignorablePodNames); err != nil {
			r.Log.Error(err, "Failed to wait for statefulset to be ready")
			return found, common.ReconcileRequeueAfter(1)
		}

		// This check is added only in scale down but not in rolling restart.
		// If scale down leads to unavailable or dead partition then we should scale up the cluster,
		// This can be left to the user but if we would do it here on our own then we can reuse
		// objects like pvc and service. These objects would have been removed if scaleup is left for the user.
		// In case of rolling restart, no pod cleanup happens, therefore rolling config back is left to the user.
		if err = r.validateSCClusterState(policy, ignorablePodNames); err != nil {
			// reset cluster size
			newSize := *found.Spec.Replicas + int32(len(podsBatch))
			found.Spec.Replicas = &newSize

			r.Log.Error(
				err, "Cluster validation failed, re-setting AerospikeCluster statefulset to previous size",
				"size", newSize,
			)

			if err = r.Client.Update(
				context.TODO(), found, common.UpdateOption,
			); err != nil {
				return found, common.ReconcileError(
					fmt.Errorf(
						"failed to update pod size %d StatefulSet pods: %v",
						newSize, err,
					),
				)
			}

			if err = r.waitForSTSToBeReady(found, ignorablePodNames); err != nil {
				r.Log.Error(err, "Failed to wait for statefulset to be ready")
			}

			return found, common.ReconcileRequeueAfter(1)
		}
	}

	// Fetch new object
	nFound, err := r.getSTS(rackState)
	if err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"failed to get StatefulSet pods: %v", err,
			),
		)
	}

	found = nFound

	podNames := getPodNames(podsBatch)

	if err := r.cleanupPods(podNames, rackState); err != nil {
		return nFound, common.ReconcileError(
			fmt.Errorf(
				"failed to cleanup pod %s: %v", podNames, err,
			),
		)
	}

	r.Log.Info("Pod Removed", "podNames", podNames)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "PodDeleted",
		"[rack-%d] Deleted Pods %s", rackState.Rack.ID, podNames,
	)

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackScaledDown",
		"[rack-%d] Scaled-down {STS:%s/%s, currentSize: %d desiredSize: %d",
		rackState.Rack.ID, found.Namespace, found.Name, *found.Spec.Replicas,
		desiredSize,
	)

	return found, common.ReconcileRequeueAfter(1)
}

func (r *SingleClusterReconciler) rollingRestartRack(
	found *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], restartTypeMap map[string]RestartType,
	failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	r.Log.Info("Rolling restart AerospikeCluster statefulset pods")

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackRollingRestart",
		"[rack-%d] Started Rolling restart", rackState.Rack.ID,
	)

	var (
		err     error
		podList []*corev1.Pod
	)

	if len(failedPods) != 0 {
		podList = failedPods
		restartTypeMap = make(map[string]RestartType)

		for idx := range podList {
			restartTypeMap[podList[idx].Name] = podRestart
		}
	} else {
		// List the pods for this aeroCluster's statefulset
		podList, err = r.getOrderedRackPodList(rackState.Rack.ID)
		if err != nil {
			return found, common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
		}
	}

	err = r.updateSTS(found, rackState)
	if err != nil {
		return found, common.ReconcileError(
			fmt.Errorf("rolling restart failed: %v", err),
		)
	}

	r.Log.Info(
		"Statefulset spec updated - doing rolling restart",
	)

	// Find pods which need restart
	podsToRestart := make([]*corev1.Pod, 0, len(podList))

	for idx := range podList {
		pod := podList[idx]

		if ignorablePodNames.Has(pod.Name) {
			r.Log.Info("Pod found in ignore pod list, skipping", "podName", pod.Name)
			continue
		}

		restartType := restartTypeMap[pod.Name]
		if restartType == noRestart || restartType == noRestartUpdateConf {
			r.Log.Info("This Pod doesn't need rolling restart, Skip this", "pod", pod.Name)
			continue
		}

		podsToRestart = append(podsToRestart, pod)
	}

	var podsBatchList [][]*corev1.Pod

	if len(failedPods) != 0 {
		// Creating a single batch of all failed pods in a rack, irrespective of batch size
		r.Log.Info("Skipping batchSize for failed pods")

		podsBatchList = make([][]*corev1.Pod, 1)
		podsBatchList[0] = podsToRestart
	} else {
		// Create batch of pods
		podsBatchList = getPodsBatchList(
			r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize, podsToRestart, len(podList),
		)
	}

	// Restart batch of pods
	if len(podsBatchList) > 0 {
		// Handle one batch
		podsBatch := podsBatchList[0]

		r.Log.Info(
			"Calculated batch for doing rolling restart",
			"rackPodList", getPodNames(podList),
			"rearrangedPods", getPodNames(podsToRestart),
			"podsBatch", getPodNames(podsBatch),
			"rollingUpdateBatchSize", r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize,
		)

		podNames := getPodNames(podsBatch)
		if err = r.createOrUpdatePodServiceIfNeeded(podNames); err != nil {
			return nil, common.ReconcileError(err)
		}

		if res := r.rollingRestartPods(rackState, podsBatch, ignorablePodNames, restartTypeMap); !res.IsSuccess {
			return found, res
		}

		// Handle next batch in subsequent Reconcile.
		if len(podsBatchList) > 1 {
			return found, common.ReconcileRequeueAfter(1)
		}
	}
	// It's last batch, go ahead

	// Return a fresh copy
	found, err = r.getSTS(rackState)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "RackRollingRestarted",
		"[rack-%d] Finished Rolling restart", rackState.Rack.ID,
	)

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) handleK8sNodeBlockListPods(
	statefulSet *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	if err := r.updateSTS(statefulSet, rackState); err != nil {
		return statefulSet, common.ReconcileError(
			fmt.Errorf("k8s node block list processing failed: %v", err),
		)
	}

	var (
		podList []*corev1.Pod
		err     error
	)

	if len(failedPods) != 0 {
		podList = failedPods
	} else {
		// List the pods for this aeroCluster's statefulset
		podList, err = r.getOrderedRackPodList(rackState.Rack.ID)
		if err != nil {
			return statefulSet, common.ReconcileError(fmt.Errorf("failed to list pods: %v", err))
		}
	}

	blockedK8sNodes := sets.NewString(r.aeroCluster.Spec.K8sNodeBlockList...)

	var podsToRestart []*corev1.Pod

	restartTypeMap := make(map[string]RestartType)

	for idx := range podList {
		pod := podList[idx]

		if blockedK8sNodes.Has(pod.Spec.NodeName) {
			r.Log.Info(
				"Pod found in blocked nodes list, migrating to a different node",
				"podName", pod.Name,
			)

			podsToRestart = append(podsToRestart, pod)

			restartTypeMap[pod.Name] = podRestart
		}
	}

	podsBatchList := getPodsBatchList(
		r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize, podsToRestart, len(podList),
	)

	// Restart batch of pods
	if len(podsBatchList) > 0 {
		// Handle one batch
		podsBatch := podsBatchList[0]

		r.Log.Info(
			"Calculated batch for Pod migration to different nodes",
			"rackPodList", getPodNames(podList),
			"rearrangedPods", getPodNames(podsToRestart),
			"podsBatch", getPodNames(podsBatch),
			"rollingUpdateBatchSize", r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize,
		)

		if res := r.rollingRestartPods(rackState, podsBatch, ignorablePodNames, restartTypeMap); !res.IsSuccess {
			return statefulSet, res
		}

		// Handle next batch in subsequent Reconcile.
		if len(podsBatchList) > 1 {
			return statefulSet, common.ReconcileRequeueAfter(1)
		}
	}

	return statefulSet, common.ReconcileSuccess()
}

type rollingRestartInfo struct {
	restartTypeMap              map[string]RestartType
	dynamicConfDiffPerPod       map[string]asconfig.DynamicConfigMap
	needRestart, needUpdateConf bool
}

func (r *SingleClusterReconciler) getRollingRestartInfo(rackState *RackState, ignorablePodNames sets.Set[string]) (
	info *rollingRestartInfo, err error,
) {
	restartTypeMap, dynamicConfDiffPerPod, err := r.getRollingRestartTypeMap(rackState, ignorablePodNames)
	if err != nil {
		return nil, err
	}

	needRestart, needUpdateConf := false, false

	for _, restartType := range restartTypeMap {
		switch restartType {
		case noRestart:
			// Do nothing
		case noRestartUpdateConf:
			needUpdateConf = true
		case podRestart, quickRestart:
			needRestart = true
		}
	}

	info = &rollingRestartInfo{
		needRestart:           needRestart,
		needUpdateConf:        needUpdateConf,
		restartTypeMap:        restartTypeMap,
		dynamicConfDiffPerPod: dynamicConfDiffPerPod,
	}

	return info, nil
}

func (r *SingleClusterReconciler) isRackUpgradeNeeded(rackID int, ignorablePodNames sets.Set[string]) (
	bool, error,
) {
	podList, err := r.getRackPodList(rackID)
	if err != nil {
		return true, fmt.Errorf("failed to list pods: %v", err)
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		if !r.isPodOnDesiredImage(pod, true) {
			r.Log.Info("Pod needs upgrade/downgrade", "podName", pod.Name)
			return true, nil
		}
	}

	return false, nil
}

func (r *SingleClusterReconciler) isRackStorageUpdatedInAeroCluster(
	rackState *RackState, pod *corev1.Pod,
) bool {
	volumes := rackState.Rack.Storage.Volumes

	for idx := range volumes {
		volume := &volumes[idx]
		// Check for Updated volumeSource
		if r.isStorageVolumeSourceUpdated(volume, pod) {
			r.Log.Info(
				"Volume added or volume source updated in rack storage" +
					" - pod needs rolling restart",
			)

			return true
		}

		// Check for Added/Updated volumeAttachments
		_, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		if r.isVolumeAttachmentAddedOrUpdated(
			volume.Name, containerAttachments, pod.Spec.Containers,
		) ||
			r.isVolumeAttachmentAddedOrUpdated(
				volume.Name, volume.InitContainers, pod.Spec.InitContainers,
			) {
			r.Log.Info(
				"New volume or volume attachment added/updated in rack" +
					" storage - pod needs rolling restart",
			)

			return true
		}
	}

	// Check for removed volumeAttachments
	allConfiguredInitContainers := []string{
		asdbv1.
			AerospikeInitContainerName,
	}
	allConfiguredContainers := []string{asdbv1.AerospikeServerContainerName}

	for idx := range r.aeroCluster.Spec.PodSpec.Sidecars {
		allConfiguredContainers = append(allConfiguredContainers, r.aeroCluster.Spec.PodSpec.Sidecars[idx].Name)
	}

	for idx := range r.aeroCluster.Spec.PodSpec.InitContainers {
		allConfiguredInitContainers = append(
			allConfiguredInitContainers, r.aeroCluster.Spec.PodSpec.InitContainers[idx].Name,
		)
	}

	rackStatusVolumes := r.getRackStatusVolumes(rackState)

	if r.isVolumeAttachmentRemoved(
		volumes, rackStatusVolumes, allConfiguredContainers,
		pod.Spec.Containers, false,
	) ||
		r.isVolumeAttachmentRemoved(
			volumes, rackStatusVolumes, allConfiguredInitContainers,
			pod.Spec.InitContainers, true,
		) {
		r.Log.Info(
			"Volume or volume attachment removed from rack storage" +
				" - pod needs rolling restart",
		)

		return true
	}

	return false
}

func (r *SingleClusterReconciler) getRackStatusVolumes(rackState *RackState) []asdbv1.VolumeSpec {
	for idx := range r.aeroCluster.Status.RackConfig.Racks {
		rack := &r.aeroCluster.Status.RackConfig.Racks[idx]
		if rack.ID == rackState.Rack.ID {
			return rack.Storage.Volumes
		}
	}

	return nil
}

func (r *SingleClusterReconciler) isStorageVolumeSourceUpdated(volume *asdbv1.VolumeSpec, pod *corev1.Pod) bool {
	podVolume := getPodVolume(pod, volume.Name)
	if podVolume == nil {
		// Volume not found in pod.volumes. This is newly added volume.
		r.Log.Info(
			"New volume added in rack storage - pod needs rolling" +
				" restart",
		)

		return true
	}

	volumeCopy := lib.DeepCopy(volume).(*asdbv1.VolumeSpec)

	if volumeCopy.Source.Secret != nil {
		setDefaultsSecretVolumeSource(volumeCopy.Source.Secret)
	}

	if volumeCopy.Source.ConfigMap != nil {
		setDefaultsConfigMapVolumeSource(volumeCopy.Source.ConfigMap)
	}

	if !reflect.DeepEqual(podVolume.Secret, volumeCopy.Source.Secret) {
		r.Log.Info(
			"Volume source updated", "old volume.source ",
			podVolume.VolumeSource, "new volume.source", volume.Source,
		)

		return true
	}

	if !reflect.DeepEqual(podVolume.ConfigMap, volumeCopy.Source.ConfigMap) {
		r.Log.Info(
			"Volume source updated", "old volume.source ",
			podVolume.VolumeSource, "new volume.source", volume.Source,
		)

		return true
	}

	if !reflect.DeepEqual(podVolume.EmptyDir, volumeCopy.Source.EmptyDir) {
		r.Log.Info(
			"Volume source updated", "old volume.source ",
			podVolume.VolumeSource, "new volume.source", volume.Source,
		)

		return true
	}

	return false
}

func (r *SingleClusterReconciler) isVolumeAttachmentAddedOrUpdated(
	volumeName string, volumeAttachments []asdbv1.VolumeAttachment,
	podContainers []corev1.Container,
) bool {
	for _, attachment := range volumeAttachments {
		container := getContainer(podContainers, attachment.ContainerName)
		// Not possible, only valid containerName should be there in attachment
		if container == nil {
			continue
		}

		volumeDevice := getContainerVolumeDevice(
			container.VolumeDevices, volumeName,
		)

		if volumeDevice != nil {
			// Found, check for updated
			if getOriginalPath(volumeDevice.DevicePath) != attachment.Path {
				r.Log.Info(
					"Volume updated in rack storage", "old", volumeDevice,
					"new", attachment,
				)

				return true
			}

			continue
		}

		volumeMount := getContainerVolumeMounts(
			container.VolumeMounts, volumeName,
		)
		if volumeMount != nil {
			// Found, check for updated
			if getOriginalPath(volumeMount.MountPath) != attachment.Path ||
				volumeMount.ReadOnly != ptr.Deref(attachment.ReadOnly, false) ||
				volumeMount.SubPath != attachment.SubPath ||
				volumeMount.SubPathExpr != attachment.SubPathExpr ||
				!reflect.DeepEqual(
					volumeMount.MountPropagation, attachment.MountPropagation,
				) {
				r.Log.Info(
					"Volume updated in rack storage", "old", volumeMount, "new",
					attachment,
				)

				return true
			}

			continue
		}

		// Added volume
		r.Log.Info(
			"Volume added in rack storage", "volume", volumeName,
			"containerName", container.Name,
		)

		return true
	}

	return false
}

func (r *SingleClusterReconciler) isVolumeAttachmentRemoved(
	volumes []asdbv1.VolumeSpec,
	rackStatusVolumes []asdbv1.VolumeSpec,
	configuredContainers []string, podContainers []corev1.Container,
	isInitContainers bool,
) bool {
	// TODO: Deal with injected volumes later.
	for idx := range podContainers {
		container := &podContainers[idx]
		if isInitContainers && container.Name == asdbv1.AerospikeInitContainerName {
			// InitContainer has all the volumes mounted, there is no specific entry in storage for initContainer
			continue
		}

		// Skip injected containers.
		if !utils.ContainsString(configuredContainers, container.Name) {
			continue
		}

		for _, volumeDevice := range container.VolumeDevices {
			if volumeDevice.Name == confDirName ||
				volumeDevice.Name == initConfDirName {
				continue
			}

			if !r.isContainerVolumeInStorage(
				volumes, volumeDevice.Name, container.Name, isInitContainers,
			) {
				if !r.isContainerVolumeInStorageStatus(
					rackStatusVolumes, volumeDevice.Name,
				) {
					continue
				}

				r.Log.Info(
					"Volume for container."+
						"volumeDevice removed from rack storage",
					"container.volumeDevice", volumeDevice.Name,
					"containerName", container.Name,
				)

				return true
			}
		}

		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == confDirName ||
				volumeMount.Name == initConfDirName ||
				volumeMount.MountPath == podServiceAccountMountPath {
				continue
			}

			if !r.isContainerVolumeInStorage(
				volumes, volumeMount.Name, container.Name, isInitContainers,
			) {
				if !r.isContainerVolumeInStorageStatus(
					rackStatusVolumes, volumeMount.Name,
				) {
					continue
				}

				r.Log.Info(
					"Volume for container."+
						"volumeMount removed from rack storage",
					"container.volumeMount", volumeMount.Name, "containerName",
					container.Name,
				)

				return true
			}
		}
	}

	return false
}

func (r *SingleClusterReconciler) isContainerVolumeInStorage(
	volumes []asdbv1.VolumeSpec, containerVolumeName string,
	containerName string, isInitContainers bool,
) bool {
	volume := getStorageVolume(volumes, containerVolumeName)
	if volume == nil {
		// Volume may have been removed, we allow removal of all volumes (except pv type)
		r.Log.Info(
			"Volume not found in rack storage", "volumeName",
			containerVolumeName,
		)

		return false
	}

	if isInitContainers {
		if !isContainerNameInStorageVolumeAttachments(
			containerName, volume.InitContainers,
		) {
			return false
		}
	} else {
		if containerName == asdbv1.AerospikeServerContainerName {
			if volume.Aerospike == nil {
				return false
			}
		} else {
			if !isContainerNameInStorageVolumeAttachments(
				containerName, volume.Sidecars,
			) {
				return false
			}
		}
	}

	return true
}

func (r *SingleClusterReconciler) isContainerVolumeInStorageStatus(
	volumes []asdbv1.VolumeSpec, containerVolumeName string,
) bool {
	volume := getStorageVolume(volumes, containerVolumeName)
	if volume == nil {
		r.Log.Info(
			"Volume is added from external source", "volumeName",
			containerVolumeName,
		)

		return false
	}

	return true
}

func (r *SingleClusterReconciler) getRackPodList(rackID int) (
	*corev1.PodList, error,
) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(
		utils.LabelsForAerospikeClusterRack(
			r.aeroCluster.Name, rackID,
		),
	)
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}
	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart

	if err := r.Client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}

	return podList, nil
}

func (r *SingleClusterReconciler) getRackPodNames(rackState *RackState) []string {
	stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster, rackState.Rack.ID)

	podNames := make([]string, 0, rackState.Size)

	for i := 0; i < rackState.Size; i++ {
		podNames = append(podNames, getSTSPodName(stsName.Name, int32(i)))
	}

	return podNames
}

func (r *SingleClusterReconciler) getOrderedRackPodList(rackID int) (
	[]*corev1.Pod, error,
) {
	podList, err := r.getRackPodList(rackID)
	if err != nil {
		return nil, err
	}

	sortedList := make([]*corev1.Pod, len(podList.Items))

	for idx := range podList.Items {
		indexStr := strings.Split(podList.Items[idx].Name, "-")
		// Index is last, [1] can be rackID
		indexInt, _ := strconv.Atoi(indexStr[len(indexStr)-1])

		if indexInt >= len(podList.Items) {
			// Happens if we do not get full list of pods due to a crash,
			return nil, fmt.Errorf("error get pod list for rack:%v", rackID)
		}

		sortedList[(len(podList.Items)-1)-indexInt] = &podList.Items[idx]
	}

	return sortedList, nil
}

func (r *SingleClusterReconciler) getCurrentRackList() (
	[]asdbv1.Rack, error,
) {
	var rackList []asdbv1.Rack
	rackList = append(rackList, r.aeroCluster.Status.RackConfig.Racks...)

	// Create dummy rack structures for dangling racks that have stateful sets but were deleted later because rack
	// before status was updated.
	statefulSetList, err := r.getClusterSTSList()
	if err != nil {
		return nil, err
	}

	for stsIdx := range statefulSetList.Items {
		rackID, err := utils.GetRackIDFromSTSName(statefulSetList.Items[stsIdx].Name)
		if err != nil {
			return nil, err
		}

		found := false

		racks := r.aeroCluster.Status.RackConfig.Racks
		for rackIdx := range racks {
			if racks[rackIdx].ID == *rackID {
				found = true
				break
			}
		}

		if !found {
			// Create a dummy rack config using globals.
			// TODO: Refactor and reuse code in mutate setting.
			dummyRack := asdbv1.Rack{
				ID: *rackID, Storage: r.aeroCluster.Spec.Storage,
				AerospikeConfig: *r.aeroCluster.Spec.AerospikeConfig,
			}

			rackList = append(rackList, dummyRack)
		}
	}

	return rackList, nil
}

func (r *SingleClusterReconciler) handleEnableSecurity(rackState *RackState, ignorablePodNames sets.Set[string]) error {
	if !r.enablingSecurity() {
		// No need to proceed if security is not to be enabling
		return nil
	}

	// Get pods where security-enabled config is applied
	securityEnabledPods, err := r.getPodsWithUpdatedConfigForRack(rackState)
	if err != nil {
		return err
	}

	if len(securityEnabledPods) == 0 {
		// No security-enabled pods found
		return nil
	}

	// Setup access control.
	if err := r.validateAndReconcileAccessControl(securityEnabledPods, ignorablePodNames); err != nil {
		r.Log.Error(err, "Failed to Reconcile access control")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "ACLUpdateFailed",
			"Failed to setup Access Control %s/%s", r.aeroCluster.Namespace,
			r.aeroCluster.Name,
		)

		return err
	}

	return nil
}

func (r *SingleClusterReconciler) enablingSecurity() bool {
	return r.aeroCluster.Spec.AerospikeAccessControl != nil && r.aeroCluster.Status.AerospikeAccessControl == nil
}

func (r *SingleClusterReconciler) getPodsWithUpdatedConfigForRack(rackState *RackState) ([]corev1.Pod, error) {
	pods, err := r.getOrderedRackPodList(rackState.Rack.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}

	if len(pods) == 0 {
		// No pod found for the rack
		return nil, nil
	}

	confMap, err := r.getConfigMap(rackState.Rack.ID)
	if err != nil {
		return nil, err
	}

	requiredConfHash := confMap.Data[aerospikeConfHashFileName]

	updatedPods := make([]corev1.Pod, 0, len(pods))

	for idx := range pods {
		podName := pods[idx].Name
		podStatus := r.aeroCluster.Status.Pods[podName]

		if podStatus.AerospikeConfigHash == requiredConfHash {
			// Config hash is matching, it means config has been applied
			updatedPods = append(updatedPods, *pods[idx])
		}
	}

	return updatedPods, nil
}

func isContainerNameInStorageVolumeAttachments(
	containerName string, mounts []asdbv1.VolumeAttachment,
) bool {
	for _, mount := range mounts {
		if mount.ContainerName == containerName {
			return true
		}
	}

	return false
}

func getConfiguredRackStateList(aeroCluster *asdbv1.AerospikeCluster) []RackState {
	topology := asdbv1.DistributeItems(
		int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks),
	)

	rackStateList := make([]RackState, 0, len(aeroCluster.Spec.RackConfig.Racks))

	racks := aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		if topology[idx] == 0 {
			// Skip the rack, if it's size is 0
			continue
		}

		rackStateList = append(
			rackStateList, RackState{
				Rack: &racks[idx],
				Size: topology[idx],
			},
		)
	}

	return rackStateList
}

// TODO: These func are available in client-go@v1.5.2, for now creating our own
func setDefaultsSecretVolumeSource(obj *corev1.SecretVolumeSource) {
	if obj.DefaultMode == nil {
		perm := corev1.SecretVolumeSourceDefaultMode
		obj.DefaultMode = &perm
	}
}

func setDefaultsConfigMapVolumeSource(obj *corev1.ConfigMapVolumeSource) {
	if obj.DefaultMode == nil {
		perm := corev1.ConfigMapVolumeSourceDefaultMode
		obj.DefaultMode = &perm
	}
}

func getContainerVolumeDevice(
	devices []corev1.VolumeDevice, name string,
) *corev1.VolumeDevice {
	for _, device := range devices {
		if device.Name == name {
			return &device
		}
	}

	return nil
}

func getContainerVolumeMounts(
	mounts []corev1.VolumeMount, name string,
) *corev1.VolumeMount {
	for _, mount := range mounts {
		if mount.Name == name {
			return &mount
		}
	}

	return nil
}

func getPodVolume(pod *corev1.Pod, name string) *corev1.Volume {
	volumes := pod.Spec.Volumes
	for idx := range volumes {
		if volumes[idx].Name == name {
			return &volumes[idx]
		}
	}

	return nil
}

func getStorageVolume(
	volumes []asdbv1.VolumeSpec, name string,
) *asdbv1.VolumeSpec {
	for idx := range volumes {
		if name == volumes[idx].Name {
			return &volumes[idx]
		}
	}

	return nil
}

func getContainer(
	podContainers []corev1.Container, name string,
) *corev1.Container {
	for idx := range podContainers {
		if name == podContainers[idx].Name {
			return &podContainers[idx]
		}
	}

	return nil
}

func getOriginalPath(path string) string {
	path = strings.TrimPrefix(path, "/workdir/filesystem-volumes")
	path = strings.TrimPrefix(path, "/workdir/block-volumes")

	return path
}

func getPodsBatchList(batchSize *intstr.IntOrString, podList []*corev1.Pod, rackSize int) [][]*corev1.Pod {
	// Error is already handled in validation
	rollingUpdateBatchSize, _ := intstr.GetScaledValueFromIntOrPercent(batchSize, rackSize, false)

	return chunkBy(podList, rollingUpdateBatchSize)
}

func chunkBy[T any](items []*T, chunkSize int) (chunks [][]*T) {
	if chunkSize <= 0 {
		chunkSize = 1
	}

	for chunkSize < len(items) {
		items, chunks = items[chunkSize:], append(chunks, items[0:chunkSize:chunkSize])
	}

	return append(chunks, items)
}
