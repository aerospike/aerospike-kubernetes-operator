package cluster

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
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

type revisionChangedRack struct {
	oldRack *RackState
	newRack *RackState
}

func (r *SingleClusterReconciler) reconcileRacks(ctx context.Context) common.ReconcileResult {
	r.Log.Info("Reconciling rack for AerospikeCluster")

	var (
		scaledDownRackList []scaledDownRack
		res                common.ReconcileResult
	)

	configuredRacks, revisionChangedRacks, racksToDelete, err := r.categoriseRacks(ctx)
	if err != nil {
		return common.ReconcileError(err)
	}

	ignorablePodNames, err := r.getIgnorablePods(ctx, racksToDelete, configuredRacks, revisionChangedRacks)
	if err != nil {
		return common.ReconcileError(err)
	}

	r.Log.Info(
		"Rack changes", "revisionChangedRacks", revisionChangedRacks, "racksToDelete",
		racksToDelete, "ignorablePods", ignorablePodNames.UnsortedList(),
	)

	// Handle failed racks (integrates both normal racks and revision-changed racks)
	for idx := range configuredRacks {
		state := &configuredRacks[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
			utils.GetRackIdentifier(state.Rack.ID, state.Rack.Revision))

		if err = r.Get(ctx, stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			continue
		}

		// Handle failed pods for this rack (two-pass: reconcile then restart if not recovered)
		if res = r.handleFailedPodsInRack(ctx, found, state, ignorablePodNames); !res.IsSuccess {
			return res
		}
	}

	for idx := range configuredRacks {
		state := &configuredRacks[idx]

		if revisionChangedRackInfo, ok := revisionChangedRacks[state.Rack.ID]; ok {
			if res = r.reconcileRevisionChangedRacks(ctx, revisionChangedRackInfo, ignorablePodNames); !res.IsSuccess {
				return res
			}

			continue
		}

		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
			utils.GetRackIdentifier(state.Rack.ID, state.Rack.Revision))

		if err = r.Get(ctx, stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in Reconcile
			zeroSizedRack := &RackState{Rack: state.Rack, Size: 0}

			found, res = r.createEmptyRack(ctx, zeroSizedRack)
			if !res.IsSuccess {
				return res
			}
		}

		// Get list of scaled down racks
		if *found.Spec.Replicas > state.Size {
			scaledDownRackList = append(scaledDownRackList, scaledDownRack{rackSTS: found, rackState: state})
		} else {
			// Reconcile other statefulset
			if res = r.reconcileRack(
				ctx, found, state, ignorablePodNames, nil,
			); !res.IsSuccess {
				return res
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx := range scaledDownRackList {
		state := scaledDownRackList[idx].rackState
		sts := scaledDownRackList[idx].rackSTS

		if res = r.reconcileRack(ctx, sts, state, ignorablePodNames, nil); !res.IsSuccess {
			return res
		}
	}

	if len(r.aeroCluster.Status.RackConfig.Racks) != 0 {
		// Remove removed racks
		if res = r.deleteRacks(ctx, racksToDelete, ignorablePodNames); !res.IsSuccess {
			if res.Err != nil {
				res.Err = fmt.Errorf(
					"could not remove StatefulSets for removed racks in cluster %s: %w",
					utils.ClusterNamespacedName(r.aeroCluster), res.Err,
				)
			}

			return res
		}
	}

	// Wait for pods across all racks to get ready before completing
	// reconcile for the racks. The STS may be correctly updated but the pods
	// might not be ready if they are running long-running init scripts or
	// aerospike index load.
	for idx := range configuredRacks {
		state := &configuredRacks[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
			utils.GetRackIdentifier(state.Rack.ID, state.Rack.Revision))

		if err := r.Get(ctx, stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return common.ReconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in Reconcile
			zeroSizedRack := &RackState{Rack: state.Rack, Size: 0}
			found, res = r.createEmptyRack(ctx, zeroSizedRack)

			if !res.IsSuccess {
				return res
			}
		}

		// Wait for pods to be ready.
		if err := r.waitForSTSToBeReady(ctx, found, ignorablePodNames); err != nil {
			// If the wait times out try again.
			// The wait is required in cases where scale up waits for a pod to
			// terminate times out and event is re-queued.
			// Next reconcile will not invoke scale up or down and will
			// fall through,
			// and might run reconcile steps common to all racks before the racks
			// have scaled up.
			r.Log.V(1).Info(
				"StatefulSet not ready, will requeue", "statefulSet", klog.KObj(found),
				"err", err,
			)

			return common.ReconcileRequeueAfter(1)
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) createEmptyRack(ctx context.Context, rackState *RackState) (
	*appsv1.StatefulSet, common.ReconcileResult,
) {
	r.Log.Info("Create new Aerospike cluster rack if needed")

	// NoOp if already exist
	r.Log.V(2).Info("AerospikeCluster", "Spec", r.aeroCluster.Spec)

	// Bad config should not come here. It should be validated in validation hook
	cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
		utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision))
	if err := r.createSTSConfigMap(ctx, cmName, rackState.Rack); err != nil {
		return nil, common.ReconcileError(fmt.Errorf("could not create ConfigMap %s for rack %d: %w",
			cmName.String(), rackState.Rack.ID, err))
	}

	stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
		utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision))

	found, err := r.createSTS(ctx, stsName, rackState)
	if err != nil {
		r.Log.V(1).Info(
			"Statefulset setup failed. Deleting statefulset", "name",
			stsName,
		)
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		_ = r.deleteSTS(ctx, found)

		return nil, common.ReconcileError(fmt.Errorf("could not create StatefulSet %s for rack %d: %w",
			stsName.String(), rackState.Rack.ID, err))
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackCreated,
		"[rack-%d] Created rack", rackState.Rack.ID,
	)

	return found, common.ReconcileSuccess()
}

// getRacksToBeBlockedFromRoster identifies racks that should have their nodes blocked from roster
// and returns a slice of racks that have ForceBlockFromRoster: true
func getRacksToBeBlockedFromRoster(log logger, rackStateList []RackState) []asdbv1.Rack {
	var racksToBlock []asdbv1.Rack

	for _, rackState := range rackStateList {
		// Check if this rack has ForceBlockFromRoster set to true
		if asdbv1.GetBool(rackState.Rack.ForceBlockFromRoster) {
			racksToBlock = append(racksToBlock, *rackState.Rack)
			log.Info("Rack marked for roster blocking",
				"rackID", rackState.Rack.ID)
		}
	}

	return racksToBlock
}

func (r *SingleClusterReconciler) getRacksToDelete(ctx context.Context, rackStateList []RackState) (
	[]asdbv1.Rack, error,
) {
	oldRacks, err := r.getCurrentRackList(ctx)
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
	ctx context.Context, racksToDelete []asdbv1.Rack, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	for idx := range racksToDelete {
		rack := &racksToDelete[idx]
		found := &appsv1.StatefulSet{}
		stsName := utils.GetNamespacedNameForSTSOrConfigMap(
			r.aeroCluster, utils.GetRackIdentifier(rack.ID, rack.Revision),
		)

		err := r.Get(ctx, stsName, found)
		if err != nil {
			// If not found, then go to the next
			if errors.IsNotFound(err) {
				continue
			}

			return common.ReconcileError(err)
		}

		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		rackState := &RackState{Size: 0, Rack: rack}

		found, res := r.scaleDownRack(ctx, found, rackState, ignorablePodNames, nil)
		if !res.IsSuccess {
			return res
		}

		// Delete sts
		if err = r.deleteSTS(ctx, found); err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, EventReasonStatefulSetDeleteFailed,
				"[rack-%d] Failed to delete StatefulSet %s", rack.ID,
				utils.GetNamespacedNameString(found),
			)

			return common.ReconcileError(err)
		}

		// Delete configMap
		cmName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
			utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision))
		if err = r.deleteRackConfigMap(ctx, cmName); err != nil {
			return common.ReconcileError(err)
		}

		// Rack cleanup is done. Take time and cleanup dangling nodes and related resources that may not have been
		// cleaned up previously due to errors.
		if err = r.cleanupDanglingPodsRack(ctx, found, rackState); err != nil {
			return common.ReconcileError(err)
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, EventReasonRackDeleted,
			"[rack-%d] Deleted rack", rack.ID,
		)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) upgradeOrRollingRestartRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
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
		ctx,
		utils.GetNamespacedNameForSTSOrConfigMap(
			r.aeroCluster, utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision),
		), rackState.Rack,
	); err != nil {
		return found, common.ReconcileError(fmt.Errorf("could not update ConfigMap for rack %d, stsName %s: %w",
			rackState.Rack.ID, utils.GetNamespacedNameString(found), err))
	}

	// Handle enable security just after updating configMap.
	// This code will run only when security is being enabled in an existing cluster
	// Update for security is verified by checking the config hash of the pod with the
	// config hash present in config map
	if err := r.handleEnableSecurity(ctx, rackState, ignorablePodNames); err != nil {
		return found, common.ReconcileError(err)
	}

	// Upgrade
	upgradeNeeded, err := r.isRackUpgradeNeeded(ctx, rackState.Rack.ID, rackState.Rack.Revision, ignorablePodNames)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	if upgradeNeeded {
		found, res = r.upgradeRack(ctx, found, rackState, ignorablePodNames, failedPods)
		if !res.IsSuccess {
			if res.Err != nil {
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning,
					EventReasonRackImageUpdateFailed,
					"[rack-%d] Failed to update image for StatefulSet %s",
					rackState.Rack.ID, utils.GetNamespacedNameString(found),
				)
				res.Err = fmt.Errorf("could not upgrade rack %d StatefulSet %s: %w",
					rackState.Rack.ID, utils.GetNamespacedNameString(found), res.Err)
			}

			return found, res
		}
	} else {
		var rollingRestartInfo, nErr = r.getRollingRestartInfo(ctx, rackState, ignorablePodNames)
		if nErr != nil {
			return found, common.ReconcileError(nErr)
		}

		if rollingRestartInfo.needRestart {
			found, res = r.rollingRestartRack(
				ctx, found, rackState, ignorablePodNames, rollingRestartInfo.restartTypeMap, failedPods,
			)
			if !res.IsSuccess {
				if res.Err != nil {
					r.Recorder.Eventf(
						r.aeroCluster, corev1.EventTypeWarning,
						EventReasonRackRollingRestartFailed,
						"[rack-%d] Failed to roll StatefulSet %s",
						rackState.Rack.ID, utils.GetNamespacedNameString(found),
					)
					res.Err = fmt.Errorf("could not complete rolling restart for rack %d StatefulSet %s: %w",
						rackState.Rack.ID, utils.GetNamespacedNameString(found), res.Err)
				}

				return found, res
			}
		}

		if len(failedPods) == 0 && rollingRestartInfo.needUpdateConf {
			res = r.updateDynamicConfig(
				ctx, rackState, ignorablePodNames,
				rollingRestartInfo.restartTypeMap, rollingRestartInfo.dynamicConfDiffPerPod,
			)
			if !res.IsSuccess {
				if res.Err != nil {
					r.Recorder.Eventf(
						r.aeroCluster, corev1.EventTypeWarning,
						EventReasonRackDynamicConfigFailed,
						"[rack-%d] Failed to update dynamic config for StatefulSet %s",
						rackState.Rack.ID, utils.GetNamespacedNameString(found),
					)
					res.Err = fmt.Errorf("could not apply dynamic configuration update for rack %d StatefulSet %s: %w",
						rackState.Rack.ID, utils.GetNamespacedNameString(found), res.Err)
				}

				return found, res
			}
		}
	}

	if r.aeroCluster.Spec.RackConfig.MaxIgnorablePods != nil {
		if res = r.handleNSOrDeviceRemovalForIgnorablePods(ctx, rackState, ignorablePodNames); !res.IsSuccess {
			return found, res
		}
	}

	// handle k8sNodeBlockList pods only if it is changed
	if !reflect.DeepEqual(r.aeroCluster.Spec.K8sNodeBlockList, r.aeroCluster.Status.K8sNodeBlockList) {
		found, res = r.handleK8sNodeBlockListPods(ctx, found, rackState, ignorablePodNames, failedPods)
		if !res.IsSuccess {
			return found, res
		}
	}

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) updateDynamicConfig(
	ctx context.Context, rackState *RackState,
	ignorablePodNames sets.Set[string], restartTypeMap map[string]RestartType,
	dynamicConfDiffPerPod map[string]asconfig.DynamicConfigMap,
) common.ReconcileResult {
	r.Log.Info("Update dynamic config in Aerospike pods")

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonDynamicConfigUpdate,
		"[rack-%d] Started dynamic config update", rackState.Rack.ID,
	)

	var (
		err     error
		podList []*corev1.Pod
	)

	// List the pods for this aeroCluster's statefulset
	podList, err = r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return common.ReconcileError(fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
		))
	}

	// Find pods which needs restart
	podsToUpdate := make([]*corev1.Pod, 0, len(podList))

	for idx := range podList {
		pod := podList[idx]

		restartType := restartTypeMap[pod.Name]
		if restartType != noRestartUpdateConf {
			r.Log.Info("This Pod doesn't need any update, Skip this", "pod", klog.KObj(pod))
			continue
		}

		podsToUpdate = append(podsToUpdate, pod)
	}

	if res := r.setDynamicConfig(ctx, dynamicConfDiffPerPod, podsToUpdate, ignorablePodNames); !res.IsSuccess {
		return res
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonDynamicConfigUpdated,
		"[rack-%d] Finished dynamic config update", rackState.Rack.ID,
	)

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) handleNSOrDeviceRemovalForIgnorablePods(
	ctx context.Context, rackState *RackState, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	podList, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return common.ReconcileError(fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
		))
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
		if err := r.handleNSOrDeviceRemoval(ctx, rackState, ignoredPod); err != nil {
			return common.ReconcileError(err)
		}
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) reconcileRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) common.ReconcileResult {
	r.Log.Info(
		"Reconcile existing Aerospike cluster statefulset", "statefulSet",
		klog.KObj(found),
	)

	var res common.ReconcileResult

	r.Log.Info(
		"Ensure rack StatefulSet size is the same as the spec", "statefulSet",
		klog.KObj(found),
	)

	desiredSize := rackState.Size
	currentSize := *found.Spec.Replicas

	// Scale down
	if currentSize > desiredSize {
		found, res = r.scaleDownRack(ctx, found, rackState, ignorablePodNames, nil)
		if !res.IsSuccess {
			if res.Err != nil {
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning,
					EventReasonRackScaleDownFailed,
					eventRackScaleFailureMessageWithCause(
						"scale down", rackState.Rack.ID,
						utils.GetNamespacedNameString(found), currentSize, desiredSize, res.Err,
					),
				)
				res.Err = fmt.Errorf(
					"could not scale down StatefulSet %s for rack %d (current %d, desired %d replicas): %w",
					utils.GetNamespacedNameString(found), rackState.Rack.ID, currentSize, desiredSize, res.Err,
				)
			}

			return res
		}
	}

	if failedPods == nil {
		// Revert migrate-fill-delay to the original value if it was set to 0 during scale down.
		// Reset will be done if there is scale-down or Rack redistribution.
		// This check won't cover a scenario where a scale-down operation was done and then reverted to the previous
		// value before the scale down could complete.
		if (r.aeroCluster.Status.Size > r.aeroCluster.Spec.Size) ||
			(!r.IsStatusEmpty() && len(r.aeroCluster.Status.RackConfig.Racks) != len(r.aeroCluster.Spec.RackConfig.Racks)) {
			if res = r.setMigrateFillDelay(
				ctx, r.getClientPolicy(ctx), &rackState.Rack.AerospikeConfig, false,
				nil,
			); !res.IsSuccess {
				if res.Err != nil {
					res.Err = fmt.Errorf("could not revert migrate-fill-delay after scale down for rack %d: %w",
						rackState.Rack.ID, res.Err)
				}

				return res
			}
		}
	}

	if err := r.updateAerospikeInitContainerImage(ctx, found); err != nil {
		return common.ReconcileError(fmt.Errorf("could not update init container image for StatefulSet %s: %w",
			utils.GetNamespacedNameString(found), err))
	}

	found, res = r.upgradeOrRollingRestartRack(ctx, found, rackState, ignorablePodNames, failedPods)
	if !res.IsSuccess {
		return res
	}

	// Scale up after upgrading, so that new pods come up with new image
	currentSize = *found.Spec.Replicas
	if currentSize < desiredSize {
		found, res = r.scaleUpRack(ctx, found, rackState, ignorablePodNames)
		if !res.IsSuccess {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, EventReasonRackScaleUpFailed,
				eventRackScaleFailureMessageWithCause(
					"scale up", rackState.Rack.ID,
					utils.GetNamespacedNameString(found), currentSize, desiredSize, res.Err,
				),
			)

			if res.Err != nil {
				res.Err = fmt.Errorf(
					"could not scale up StatefulSet %s for rack %d (current %d, desired %d replicas): %w",
					utils.GetNamespacedNameString(found), rackState.Rack.ID, currentSize, desiredSize, res.Err,
				)
			}

			return res
		}
	}

	// All regular operations are complete. Take time and cleanup dangling nodes that have not been cleaned up
	// previously due to errors.
	if err := r.cleanupDanglingPodsRack(ctx, found, rackState); err != nil {
		return common.ReconcileError(err)
	}

	if err := r.reconcilePodService(ctx, rackState); err != nil {
		return common.ReconcileError(err)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) scaleUpRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState, ignorablePodNames sets.Set[string],
) (
	*appsv1.StatefulSet, common.ReconcileResult,
) {
	desiredSize := rackState.Size

	oldSz := *found.Spec.Replicas

	r.Log.Info("Scaling up pods", "currentSz", oldSz, "desiredSz", desiredSize)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackScaleUp,
		eventRackScaleMessage(
			"Scaling up", rackState.Rack.ID,
			utils.GetNamespacedNameString(found), oldSz, desiredSize,
		),
	)

	// No need for this? But if the image is bad, then new pod will also come up
	// with bad node.
	podList, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return found, common.ReconcileError(fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
		))
	}

	if r.isAnyPodInImageFailedState(podList, ignorablePodNames) {
		return found, common.ReconcileError(fmt.Errorf(
			"cannot scale up cluster %s rack %d: a pod is already in failed state",
			utils.ClusterNamespacedName(r.aeroCluster), rackState.Rack.ID))
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
						utils.NamespacedName(r.aeroCluster.Namespace, newPodName),
					),
				)
			}
		}
	}

	if err = r.cleanupDanglingPodsRack(ctx, found, rackState); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"could not run scale-up pre-check for rack %d in cluster %s: %w",
				rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
			),
		)
	}

	// Create pod service for the scaled up pod when node network is used in network policy
	if err = r.createOrUpdatePodServiceIfNeeded(ctx, newPodNames); err != nil {
		return nil, common.ReconcileError(err)
	}

	// update replicas here to avoid new replicas count comparison while cleaning up dangling pods of rack
	found.Spec.Replicas = &desiredSize

	// Scale up the statefulset
	if err = r.Update(ctx, found, common.UpdateOption); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"could not scale up StatefulSet %s: %w", utils.GetNamespacedNameString(found), err,
			),
		)
	}

	// return a fresh copy
	found, err = r.getSTS(ctx, rackState)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackScaledUp,
		eventRackScaleMessage(
			"Scaled up", rackState.Rack.ID,
			utils.GetNamespacedNameString(found), *found.Spec.Replicas, desiredSize,
		),
	)

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) upgradeRack(
	ctx context.Context, statefulSet *appsv1.StatefulSet, rackState *RackState,
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
		podList, err = r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
		if err != nil {
			return statefulSet, common.ReconcileError(
				fmt.Errorf(
					"could not list pods for rack %d in cluster %s: %w",
					rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
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
	err = r.updateSTS(ctx, statefulSet, rackState)
	if err != nil {
		return statefulSet, common.ReconcileError(
			fmt.Errorf("could not update StatefulSet spec for rack %d in cluster %s: %w",
				rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err),
		)
	}

	// Find pods which need to be updated
	podsToUpgrade := make([]*corev1.Pod, 0, len(podList))

	for idx := range podList {
		pod := podList[idx]
		r.Log.Info("Check if pod needs upgrade or not", "pod", klog.KObj(pod))

		if ignorablePodNames.Has(pod.Name) {
			r.Log.Info("Pod found in ignore pod list, skipping", "pod", klog.KObj(pod))
			continue
		}

		if r.isPodUpgraded(pod) {
			r.Log.Info("Pod doesn't need upgrade", "pod", klog.KObj(pod))
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
		if err = r.createOrUpdatePodServiceIfNeeded(ctx, podNames); err != nil {
			return nil, common.ReconcileError(err)
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, EventReasonPodImageUpdate,
			"[rack-%d] Updating containers on Pods %s",
			rackState.Rack.ID, eventNamespacedNames(r.aeroCluster.Namespace, podNames),
		)

		res := r.safelyDeletePodsAndEnsureImageUpdated(ctx, rackState, podsBatch, ignorablePodNames)
		if !res.IsSuccess {
			return statefulSet, res
		}

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, EventReasonPodImageUpdated,
			"[rack-%d] Updated containers on Pods %s",
			rackState.Rack.ID, eventNamespacedNames(r.aeroCluster.Namespace, podNames),
		)

		// Handle the next batch in subsequent Reconcile.
		if len(podsBatchList) > 1 {
			return statefulSet, common.ReconcileRequeueAfter(1)
		}
	}

	// If it was last batch then go ahead return a fresh copy
	statefulSet, err = r.getSTS(ctx, rackState)
	if err != nil {
		return statefulSet, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackImageUpdated,
		"[rack-%d] Updated image for StatefulSet %s",
		rackState.Rack.ID, utils.GetNamespacedNameString(statefulSet),
	)

	return statefulSet, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) scaleDownRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], customBatchSize *intstr.IntOrString,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	desiredSize := rackState.Size

	// Continue if scaleDown is not needed
	if *found.Spec.Replicas <= desiredSize {
		return found, common.ReconcileSuccess()
	}

	r.Log.Info(
		"ScaleDown AerospikeCluster statefulset", "desiredSize", desiredSize,
		"currentSize", *found.Spec.Replicas, "rackID", rackState.Rack.ID, "rackRevision", rackState.Rack.Revision,
	)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackScaleDown,
		eventRackScaleMessage(
			"Scaling down", rackState.Rack.ID,
			utils.GetNamespacedNameString(found), *found.Spec.Replicas, desiredSize,
		),
	)

	oldPodList, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return found, common.ReconcileError(fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
		))
	}

	if r.isAnyPodInImageFailedState(oldPodList, ignorablePodNames) {
		return found, common.ReconcileError(
			fmt.Errorf("cannot scale down cluster %s rack %d: a pod is already in failed state",
				utils.ClusterNamespacedName(r.aeroCluster), rackState.Rack.ID))
	}

	// Code flow will reach this stage only when found.Spec.Replicas > desiredSize
	// Maintain a list of removed pods. It will be used for alumni-reset and tip-clear

	policy := r.getClientPolicy(ctx)
	diffPods := *found.Spec.Replicas - desiredSize

	// Use custom batch size if provided, otherwise use ScaleDownBatchSize
	batchSizeConfig := r.aeroCluster.Spec.RackConfig.ScaleDownBatchSize
	if customBatchSize != nil {
		batchSizeConfig = customBatchSize
	}

	podsBatchList := getPodsBatchList(
		batchSizeConfig, oldPodList[:diffPods], len(oldPodList),
	)

	// Handle one batch
	podsBatch := podsBatchList[0]

	r.Log.Info(
		"Calculated batch for Pod scale-down",
		"rackPodList", getPodNames(oldPodList),
		"podsBatch", getPodNames(podsBatch),
		"scaleDownBatchSize", batchSizeConfig,
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
		if res := r.waitForMultipleNodesSafeStopReady(ctx, runningPods, ignorablePodNames); !res.IsSuccess {
			// The pod is running and is unsafe to terminate.
			return found, res
		}

		// set migrate-fill-delay to 0 across all nodes of cluster to scale down fast
		// setting migrate-fill-delay only if pod is running and ready.
		// This check ensures that migrate-fill-delay is not set while processing failed racks.
		// setting migrate-fill-delay will fail if there are any failed pod
		if res := r.setMigrateFillDelay(
			ctx, policy, &rackState.Rack.AerospikeConfig, true, ignorablePodNames,
		); !res.IsSuccess {
			if res.Err != nil {
				res.Err = fmt.Errorf("could not set migrate-fill-delay to 0 for rack %d: %w", rackState.Rack.ID, res.Err)
			}

			return found, res
		}

		// Wait for migration to complete before deleting the pods.
		if res := r.waitForMigrationToComplete(ctx, policy,
			ignorablePodNames,
		); !res.IsSuccess {
			if res.Err != nil {
				res.Err = fmt.Errorf("could not wait for migrations to complete before deleting pods for rack %d: %w",
					rackState.Rack.ID, res.Err)
			}

			return found, res
		}
	}

	// Update new object with new size
	newSize := *found.Spec.Replicas - utils.Len32(podsBatch)
	found.Spec.Replicas = &newSize

	if err = r.Update(
		ctx, found, common.UpdateOption,
	); err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"could not scale StatefulSet %s to %d replicas: %w",
				utils.GetNamespacedNameString(found), newSize, err,
			),
		)
	}

	// Consider these checks if any pod in the batch is running and ready.
	// If all the pods are not running then we can safely ignore these checks.
	// These checks will fail if there is any other pod in failed state outside the batch.
	if isAnyPodRunningAndReady {
		// Wait for pods to get terminated
		if err = r.waitForSTSToBeReady(ctx, found, ignorablePodNames); err != nil {
			r.Log.V(1).Info("StatefulSet not ready, will requeue", "statefulSet", klog.KObj(found), "err", err)
			return found, common.ReconcileRequeueAfter(1)
		}

		// This check is added only in scale down but not in rolling restart.
		// If scale down leads to unavailable or dead partition then we should scale up the cluster,
		// This can be left to the user but if we would do it here on our own then we can reuse
		// objects like pvc and service. These objects would have been removed if scaleup is left for the user.
		// In case of rolling restart, no pod cleanup happens, therefore rolling config back is left to the user.
		if err = r.validateSCClusterState(ctx, policy, ignorablePodNames); err != nil {
			// reset cluster size
			newSize := *found.Spec.Replicas + utils.Len32(podsBatch)
			found.Spec.Replicas = &newSize

			r.Log.V(1).Info(
				"Cluster validation failed, re-setting AerospikeCluster statefulset to previous size",
				"statefulSet", klog.KObj(found), "size", newSize, "err", err,
			)

			if err = r.Update(
				ctx, found, common.UpdateOption,
			); err != nil {
				return found, common.ReconcileError(
					fmt.Errorf(
						"could not scale StatefulSet %s to %d replicas: %w",
						utils.GetNamespacedNameString(found), newSize, err,
					),
				)
			}

			if err = r.waitForSTSToBeReady(ctx, found, ignorablePodNames); err != nil {
				r.Log.V(1).Info("StatefulSet not ready after reset, will requeue", "statefulSet", klog.KObj(found), "err", err)
			}

			return found, common.ReconcileRequeueAfter(1)
		}
	}

	// Fetch new object
	nFound, err := r.getSTS(ctx, rackState)
	if err != nil {
		return found, common.ReconcileError(
			fmt.Errorf(
				"could not get StatefulSet for rack %d in cluster %s: %w",
				rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
			),
		)
	}

	found = nFound

	podNames := getPodNames(podsBatch)

	if err := r.cleanupPods(ctx, podNames, rackState); err != nil {
		return nFound, common.ReconcileError(
			fmt.Errorf(
				"could not clean up pods %s: %w",
				strings.Join(utils.NamespacedNames(r.aeroCluster.Namespace, podNames), ", "), err,
			),
		)
	}

	r.Log.Info("Pod Removed", "podNames", podNames)
	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonPodDeleted,
		"[rack-%d] Deleted Pods %s",
		rackState.Rack.ID, eventNamespacedNames(r.aeroCluster.Namespace, podNames),
	)

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackScaledDown,
		eventRackScaleMessage(
			"Scaled down", rackState.Rack.ID,
			utils.GetNamespacedNameString(found), *found.Spec.Replicas, desiredSize,
		),
	)

	return found, common.ReconcileRequeueAfter(1)
}

func (r *SingleClusterReconciler) rollingRestartRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], restartTypeMap map[string]RestartType,
	failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	r.Log.Info("Rolling restart AerospikeCluster statefulset pods", "statefulSet", klog.KObj(found))

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackRollingRestart,
		"[rack-%d] Started rolling restart", rackState.Rack.ID,
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
		podList, err = r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
		if err != nil {
			return found, common.ReconcileError(fmt.Errorf(
				"could not list pods for rack %d in cluster %s: %w",
				rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
			))
		}
	}

	err = r.updateSTS(ctx, found, rackState)
	if err != nil {
		return found, common.ReconcileError(
			fmt.Errorf("rolling restart failed: %w", err),
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
			r.Log.Info("Pod found in ignore pod list, skipping", "pod", klog.KObj(pod))
			continue
		}

		restartType := restartTypeMap[pod.Name]
		if restartType == noRestart || restartType == noRestartUpdateConf {
			r.Log.Info("This Pod doesn't need rolling restart, Skip this", "pod", klog.KObj(pod))
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
		if err = r.createOrUpdatePodServiceIfNeeded(ctx, podNames); err != nil {
			return nil, common.ReconcileError(err)
		}

		if res := r.rollingRestartPods(ctx, rackState, podsBatch, ignorablePodNames, restartTypeMap); !res.IsSuccess {
			return found, res
		}

		// Handle next batch in subsequent Reconcile.
		if len(podsBatchList) > 1 {
			return found, common.ReconcileRequeueAfter(1)
		}
	}
	// It's last batch, go ahead

	// Return a fresh copy
	found, err = r.getSTS(ctx, rackState)
	if err != nil {
		return found, common.ReconcileError(err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonRackRollingRestarted,
		"[rack-%d] Finished rolling restart", rackState.Rack.ID,
	)

	return found, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) handleK8sNodeBlockListPods(
	ctx context.Context, statefulSet *appsv1.StatefulSet, rackState *RackState,
	ignorablePodNames sets.Set[string], failedPods []*corev1.Pod,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	if err := r.updateSTS(ctx, statefulSet, rackState); err != nil {
		return statefulSet, common.ReconcileError(
			fmt.Errorf("k8s node block list processing failed: %w", err),
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
		podList, err = r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
		if err != nil {
			return statefulSet, common.ReconcileError(fmt.Errorf(
				"could not list pods for rack %d in cluster %s: %w",
				rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
			))
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
				"pod", klog.KObj(pod),
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

		if res := r.rollingRestartPods(ctx, rackState, podsBatch, ignorablePodNames, restartTypeMap); !res.IsSuccess {
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

func (r *SingleClusterReconciler) getRollingRestartInfo(
	ctx context.Context, rackState *RackState, ignorablePodNames sets.Set[string]) (
	info *rollingRestartInfo, err error,
) {
	restartTypeMap, dynamicConfDiffPerPod, err := r.getRollingRestartTypeMap(ctx, rackState, ignorablePodNames)
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

func (r *SingleClusterReconciler) isRackUpgradeNeeded(
	ctx context.Context, rackID int, rackRevision string, ignorablePodNames sets.Set[string]) (
	bool, error,
) {
	podList, err := r.getRackPodList(ctx, rackID, rackRevision)
	if err != nil {
		return true, fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackID, utils.ClusterNamespacedName(r.aeroCluster), err,
		)
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		if ignorablePodNames.Has(pod.Name) {
			continue
		}

		if !r.isPodOnDesiredImage(pod, true) {
			r.Log.Info("Pod needs upgrade/downgrade", "pod", klog.KObj(pod))
			return true, nil
		}
	}

	return false, nil
}

func (r *SingleClusterReconciler) isRackStorageUpdatedInAeroCluster(
	rackState *RackState, pod *corev1.Pod,
) bool {
	volumes := rackState.Rack.Storage.Volumes
	workDir := asdbv1.GetWorkDirectory(rackState.Rack.AerospikeConfig)

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
		var containerAttachments []asdbv1.VolumeAttachment

		containerAttachments = append(containerAttachments, volume.Sidecars...)

		if volume.Aerospike != nil {
			// Workdir volume is always mounted as subPath volumes for smd and usr subPath.
			// So, we need to handle workdir volume mount separately here.
			// Add different subPath volume mounts for workdir volume for storage update check
			if volume.Aerospike.Path == workDir {
				containerAttachments = append(
					containerAttachments, getWorkDirSubPathVolumeMounts(volume.Aerospike.Path)...)
			} else {
				containerAttachment := asdbv1.VolumeAttachment{
					ContainerName: asdbv1.AerospikeServerContainerName,
					Path:          volume.Aerospike.Path,
				}

				// Mount options (ReadOnly, SubPath, SubPathExpr, MountPropagation...) are only applicable
				// for hostPath volumes in aerospike containers, so we only include them in that case
				if volume.Source.HostPath != nil {
					containerAttachment.AttachmentOptions = asdbv1.AttachmentOptions{
						MountOptions: volume.Aerospike.MountOptions,
					}
				}

				containerAttachments = append(containerAttachments, containerAttachment)
			}
		}

		if r.isVolumeAttachmentAddedOrUpdated(
			volume.Name, containerAttachments, pod.Spec.Containers, workDir,
		) ||
			r.isVolumeAttachmentAddedOrUpdated(
				volume.Name, volume.InitContainers, pod.Spec.InitContainers, workDir,
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

	// Include InitContainers (excluding placeholder)
	for idx := range r.aeroCluster.Spec.PodSpec.InitContainers {
		// Skip placeholder (aerospike-init) as it's not a real container in the spec
		if r.aeroCluster.Spec.PodSpec.InitContainers[idx].Name != asdbv1.AerospikeInitContainerName {
			allConfiguredInitContainers = append(
				allConfiguredInitContainers, r.aeroCluster.Spec.PodSpec.InitContainers[idx].Name,
			)
		}
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
	podContainers []corev1.Container, workDir string,
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

		found := false

		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name != volumeName {
				continue
			}

			// This is to support backward compatibility for workdir volume mounts which were mounted directly
			// on workdir path earlier. Now we are mounting smd and usr subPath of workdir volume.
			// This condition will be true only for older existing clusters.
			// No need to check the mount options for this case as this is only for backward compatibility.
			if (attachment.Path == filepath.Join(workDir, asdbv1.WorkDirSubPathSmd) ||
				attachment.Path == filepath.Join(workDir, asdbv1.WorkDirSubPathUsr)) &&
				volumeMount.MountPath == workDir {
				r.Log.Info(
					"workDir volume mount found with legacy style, will be updated in next rolling restart, skipping",
				)

				found = true

				break
			}

			if getOriginalPath(volumeMount.MountPath) == attachment.Path &&
				volumeMount.SubPath == attachment.SubPath {
				// Found a matching mount, check for other options
				if volumeMount.ReadOnly != asdbv1.GetBool(attachment.ReadOnly) ||
					volumeMount.SubPathExpr != attachment.SubPathExpr ||
					!reflect.DeepEqual(volumeMount.MountPropagation, attachment.MountPropagation) {
					r.Log.Info(
						"Volume updated in rack storage", "old", volumeMount, "new", attachment,
					)

					return true
				}

				found = true

				break
			}
		}

		// If not found, this attachment is new or changed
		if !found {
			r.Log.Info(
				"Volume added or updated in rack storage", "volume", volumeName,
				"containerName", container.Name, "attachmentPath", attachment.Path, "subPath", attachment.SubPath,
			)

			return true
		}
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

func (r *SingleClusterReconciler) getRackPodList(ctx context.Context, rackID int, rackRevision string) (
	*corev1.PodList, error,
) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	listOps := &client.ListOptions{
		Namespace:     r.aeroCluster.Namespace,
		LabelSelector: utils.GetAerospikeClusterRackLabelSelector(r.aeroCluster.Name, rackID, rackRevision),
	}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := r.List(ctx, podList, listOps); err != nil {
		return nil, err
	}

	return podList, nil
}

func (r *SingleClusterReconciler) getRackPodNames(rackState *RackState) []string {
	rackIdentifier := utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision)
	stsName := utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
		rackIdentifier)
	podNames := make([]string, 0, rackState.Size)

	for i := int32(0); i < rackState.Size; i++ {
		podNames = append(podNames, getSTSPodName(stsName.Name, i))
	}

	return podNames
}

func (r *SingleClusterReconciler) getOrderedRackPodList(ctx context.Context, rackID int, rackRevision string) (
	[]*corev1.Pod, error,
) {
	podList, err := r.getRackPodList(ctx, rackID, rackRevision)
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
			return nil, fmt.Errorf("error get pod list for rack:%v, rackRevision:%v", rackID, rackRevision)
		}

		sortedList[(len(podList.Items)-1)-indexInt] = &podList.Items[idx]
	}

	return sortedList, nil
}

func (r *SingleClusterReconciler) getCurrentRackList(ctx context.Context) (
	[]asdbv1.Rack, error,
) {
	var rackList []asdbv1.Rack

	rackList = append(rackList, r.aeroCluster.Status.RackConfig.Racks...)

	// Create dummy rack structures for dangling racks that have stateful sets but were deleted later because rack
	// before status was updated.
	statefulSetList, err := r.getClusterSTSList(ctx)
	if err != nil {
		return nil, err
	}

	for stsIdx := range statefulSetList.Items {
		stsName := statefulSetList.Items[stsIdx].Name

		rackID, rackRevision, err := utils.GetRackIDAndRevisionFromSTSName(r.aeroCluster.Name, stsName)
		if err != nil {
			return nil, err
		}

		found := false

		racks := r.aeroCluster.Status.RackConfig.Racks
		for rackIdx := range racks {
			if racks[rackIdx].ID == rackID && racks[rackIdx].Revision == rackRevision {
				found = true
				break
			}
		}

		if !found {
			// Create a dummy rack config using globals.
			// TODO: Refactor and reuse code in mutate setting.
			dummyRack := asdbv1.Rack{
				ID:              rackID,
				Revision:        rackRevision,
				Storage:         r.aeroCluster.Spec.Storage,
				AerospikeConfig: *r.aeroCluster.Spec.AerospikeConfig,
			}

			rackList = append(rackList, dummyRack)
		}
	}

	return rackList, nil
}

func (r *SingleClusterReconciler) handleEnableSecurity(
	ctx context.Context, rackState *RackState, ignorablePodNames sets.Set[string]) error {
	if !r.enablingSecurity() {
		// No need to proceed if security is not to be enabling
		return nil
	}

	// Get pods where security-enabled config is applied
	securityEnabledPods, err := r.getPodsWithUpdatedConfigForRack(ctx, rackState)
	if err != nil {
		return err
	}

	if len(securityEnabledPods) == 0 {
		// No security-enabled pods found
		return nil
	}

	// Setup access control.
	if err := r.validateAndReconcileAccessControl(ctx, securityEnabledPods, ignorablePodNames); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonAccessControlUpdateFailed,
			"Failed to set up access control for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		return err
	}

	return nil
}

func (r *SingleClusterReconciler) enablingSecurity() bool {
	return r.aeroCluster.Spec.AerospikeAccessControl != nil && r.aeroCluster.Status.AerospikeAccessControl == nil
}

func (r *SingleClusterReconciler) getPodsWithUpdatedConfigForRack(
	ctx context.Context, rackState *RackState) ([]corev1.Pod, error) {
	pods, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return nil, fmt.Errorf(
			"could not list pods for rack %d in cluster %s: %w",
			rackState.Rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err,
		)
	}

	if len(pods) == 0 {
		// No pod found for the rack
		return nil, nil
	}

	confMap, err := r.getConfigMap(ctx, utils.GetRackIdentifier(rackState.Rack.ID, rackState.Rack.Revision))
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
		aeroCluster.Spec.Size, utils.Len32(aeroCluster.Spec.RackConfig.Racks),
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

func getContainerVolumeMount(
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

func (r *SingleClusterReconciler) reconcileRevisionChangedRacks(
	ctx context.Context, revisionChangedRackInfo revisionChangedRack, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	oldRack := revisionChangedRackInfo.oldRack
	newRack := revisionChangedRackInfo.newRack
	targetSize := newRack.Size

	r.Log.Info(
		"Reconciling revision-changed rack", "rackID", newRack.Rack.ID,
		"fromRevision", oldRack.Rack.Revision, "toRevision", newRack.Rack.Revision,
		"targetSize", targetSize,
	)

	newSts, err := r.getSTS(ctx, newRack)
	if err != nil {
		if !errors.IsNotFound(err) {
			return common.ReconcileError(err)
		}
		// Create new STS
		found, res := r.createEmptyRack(ctx, &RackState{Rack: newRack.Rack, Size: 0})
		if !res.IsSuccess {
			return res
		}

		newSts = found
	}

	oldSts, err := r.getSTS(ctx, oldRack)
	if err != nil {
		if !errors.IsNotFound(err) {
			return common.ReconcileError(err)
		}

		// The old STS is not found. This could happen if it was manually deleted.
		// Instead of assuming completion, we now ensure the new rack is scaled up correctly.
		r.Log.Info(
			"Old rack revision STS not found. Ensuring new rack revision STS is at the target size.",
			"oldStsName", utils.GetNamespacedNameForSTSOrConfigMap(r.aeroCluster,
				utils.GetRackIdentifier(oldRack.Rack.ID, oldRack.Rack.Revision),
			),
		)

		// Use reconcileRack to bring the new rack to its desired state.
		// This will handle the scale-up logic.
		return r.reconcileRack(ctx, newSts, newRack, ignorablePodNames, nil)
	}

	oldReplicas := *oldSts.Spec.Replicas
	newReplicas := *newSts.Spec.Replicas

	// rack revision migration completed, delete old revision rack
	if oldReplicas == 0 && newReplicas == targetSize {
		r.Log.Info("Rack migration to new revision completed, deleting old revision",
			"revision", newRack.Rack.Revision)

		return r.deleteRacks(ctx, []asdbv1.Rack{*oldRack.Rack}, ignorablePodNames)
	}

	// Handle oversized new rack
	if newReplicas > targetSize {
		r.Log.Info(
			"Migrating rack to new revisions: reconciling new revision STS to scale down",
			"statefulSet", klog.KObj(newSts),
		)

		tempState := &RackState{Rack: newRack.Rack, Size: targetSize}
		_, res := r.scaleDownRack(ctx, newSts, tempState, ignorablePodNames, nil)

		return res
	}

	if err := r.waitForAllSTSToBeReady(ctx, ignorablePodNames); err != nil {
		return common.ReconcileError(err)
	}

	// Calculate batch size
	batchSize, _ := intstr.GetScaledValueFromIntOrPercent(
		r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize, int(targetSize), true,
	)

	if batchSize < 1 {
		batchSize = 1
	}

	batchSizeInt32 := int32(batchSize)

	totalCurrentPods := oldReplicas + newReplicas

	if totalCurrentPods < targetSize && newReplicas < targetSize {
		// Scale up new rack
		podsToScaleUp := min(batchSizeInt32, targetSize-totalCurrentPods)
		tempState := &RackState{Rack: newRack.Rack, Size: newReplicas + podsToScaleUp}

		if res := r.reconcileRack(ctx, newSts, tempState, ignorablePodNames, nil); !res.IsSuccess {
			return res
		}

		return common.ReconcileRequeueAfter(1)
	}

	if oldReplicas > 0 {
		// Scale down old rack
		podsToScaleDown := min(batchSizeInt32, oldReplicas)
		r.Log.Info(
			"Migrating rack: scaling down old revision STS by a batch", "statefulSet", klog.KObj(oldSts),
			"batchSize", podsToScaleDown,
		)

		tempState := &RackState{Rack: oldRack.Rack, Size: oldReplicas - podsToScaleDown}
		_, res := r.scaleDownRack(
			ctx, oldSts, tempState, ignorablePodNames,
			r.aeroCluster.Spec.RackConfig.RollingUpdateBatchSize,
		)

		return res
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) handleFailedPodsInRack(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState, ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	// 1. Fetch the pods for the rack and if there are failed pods, then reconcile the rack
	podList, err := r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf("could not list pods for rack %d-%s in cluster %s: %w",
				rackState.Rack.ID, rackState.Rack.Revision, utils.ClusterNamespacedName(r.aeroCluster), err),
		)
	}

	failedPods, _, _ := getFailedAndActivePods(podList, false)
	// remove ignorable pods from failedPods
	failedPods = getNonIgnorablePods(failedPods, ignorablePodNames)

	if len(failedPods) != 0 {
		r.Log.Info("Reconcile the failed pods in the Rack",
			"rackID", rackState.Rack.ID, "rackRevision", rackState.Rack.Revision,
			"failedPods", getPodNames(failedPods))

		if res := r.reconcileRack(
			ctx, found, rackState, ignorablePodNames, failedPods,
		); !res.IsSuccess {
			return res
		}

		r.Log.Info("Reconciled the failed pods in the Rack",
			"rackID", rackState.Rack.ID, "rackRevision", rackState.Rack.Revision,
			"failedPods", getPodNames(failedPods))
	}

	// 2. Again, fetch the pods for the rack and if there are failed pods then restart them.
	// This is needed in cases where hash values generated from CR spec are same as hash values in pods.
	// But, pods are in failed state due to their bad spec.
	// e.g. configuring unschedulable resources in CR podSpec and reverting them to old value.
	podList, err = r.getOrderedRackPodList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return common.ReconcileError(
			fmt.Errorf("could not re-fetch pods for restart check on rack %d-%s in cluster %s: %w",
				rackState.Rack.ID, rackState.Rack.Revision, utils.ClusterNamespacedName(r.aeroCluster), err),
		)
	}

	failedPods, _, _ = getFailedAndActivePods(podList, false)
	// remove ignorable pods from failedPods
	failedPods = getNonIgnorablePods(failedPods, ignorablePodNames)

	if len(failedPods) != 0 {
		r.Log.Info("Restart the failed pods in the Rack",
			"rackID", rackState.Rack.ID, "rackRevision", rackState.Rack.Revision,
			"failedPods", getPodNames(failedPods))

		if _, res := r.rollingRestartRack(
			ctx, found, rackState, ignorablePodNames, nil,
			failedPods,
		); !res.IsSuccess {
			return res
		}

		r.Log.Info("Restarted the failed pods in the Rack",
			"rackID", rackState.Rack.ID, "rackRevision", rackState.Rack.Revision,
			"failedPods", getPodNames(failedPods))
		// Requeue after 1 second to fetch latest CR object with updated pod status
		return common.ReconcileRequeueAfter(1)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) categoriseRacks(ctx context.Context) (configuredRacks []RackState,
	revisionChangedRacks map[int]revisionChangedRack, racksToDelete []asdbv1.Rack, err error) {
	configuredRacks = getConfiguredRackStateList(r.aeroCluster)

	// Revision-changed racks are not considered in the racks to delete.
	racksToDelete, err = r.getRacksToDelete(ctx, configuredRacks)
	if err != nil {
		return configuredRacks, revisionChangedRacks, racksToDelete, err
	}

	oldRacks, err := r.getCurrentRackList(ctx)
	if err != nil {
		return configuredRacks, revisionChangedRacks, racksToDelete, err
	}

	// Find racks that are in the spec but don't have a statefulset yet.
	revisionChangedRacks = make(map[int]revisionChangedRack)

	for idx := range configuredRacks {
		state := &configuredRacks[idx]

		for jdx := range oldRacks {
			oldRack := oldRacks[jdx]
			if state.Rack.ID == oldRack.ID && state.Rack.Revision != oldRack.Revision {
				revisionChangedRacks[state.Rack.ID] = revisionChangedRack{
					oldRack: &RackState{
						Rack: &oldRack,
						Size: state.Size,
					},
					newRack: state,
				}

				break
			}
		}
	}

	return configuredRacks, revisionChangedRacks, racksToDelete, err
}

func getWorkDirSubPathVolumeMounts(basePath string) []asdbv1.VolumeAttachment {
	subDirs := []string{asdbv1.WorkDirSubPathSmd, asdbv1.WorkDirSubPathUsr}

	volumeMounts := make([]asdbv1.VolumeAttachment, 0, len(subDirs))

	for _, dir := range subDirs {
		volumeMounts = append(volumeMounts, asdbv1.VolumeAttachment{
			ContainerName: asdbv1.AerospikeServerContainerName,
			Path:          filepath.Join(basePath, dir),
			AttachmentOptions: asdbv1.AttachmentOptions{
				MountOptions: asdbv1.MountOptions{
					SubPath: dir,
				},
			},
		})
	}

	return volumeMounts
}
