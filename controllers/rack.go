package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	lib "github.com/aerospike/aerospike-management-lib"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
)

func (r *SingleClusterReconciler) reconcileRacks() reconcileResult {

	r.Log.Info("Reconciling rack for AerospikeCluster")

	var scaledDownRackSTSList []appsv1.StatefulSet
	var scaledDownRackList []RackState
	var res reconcileResult

	rackStateList := getConfiguredRackStateList(r.aeroCluster)
	racksToDelete, err := r.getRacksToDelete(rackStateList)
	if err != nil {
		return reconcileError(err)
	}

	var rackIDsToDelete []int
	for _, rack := range racksToDelete {
		rackIDsToDelete = append(rackIDsToDelete, rack.ID)
	}

	ignorablePods, err := r.getIgnorablePods(racksToDelete)
	if err != nil {
		return reconcileError(err)
	}

	var ignorablePodNames []string
	for _, pod := range ignorablePods {
		ignorablePodNames = append(ignorablePodNames, pod.Name)
	}

	r.Log.Info(
		"Rack changes", "racksToDelete", rackIDsToDelete, "ignorablePods",
		ignorablePodNames,
	)

	for _, state := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(r.aeroCluster, state.Rack.ID)
		if err := r.Client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return reconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in Reconcile
			zeroSizedRack := RackState{Rack: state.Rack, Size: 0}
			found, res = r.createRack(zeroSizedRack)
			if !res.isSuccess {
				return res
			}
		}

		// Get list of scaled down racks
		if *found.Spec.Replicas > int32(state.Size) {
			scaledDownRackSTSList = append(scaledDownRackSTSList, *found)
			scaledDownRackList = append(scaledDownRackList, state)
		} else {
			// Reconcile other statefulset
			if res := r.reconcileRack(
				found, state, ignorablePods,
			); !res.isSuccess {
				return res
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx, state := range scaledDownRackList {
		if res := r.reconcileRack(
			&scaledDownRackSTSList[idx], state, ignorablePods,
		); !res.isSuccess {
			return res
		}
	}

	if len(r.aeroCluster.Status.RackConfig.Racks) != 0 {
		// Remove removed racks
		if res := r.deleteRacks(racksToDelete, ignorablePods); !res.isSuccess {
			if res.err != nil {
				r.Log.Error(
					err, "Failed to remove statefulset for removed racks",
					"err", res.err,
				)
			}
			return res
		}
	}

	return reconcileSuccess()
}

func (r *SingleClusterReconciler) createRack(rackState RackState) (
	*appsv1.StatefulSet, reconcileResult,
) {

	r.Log.Info("Create new Aerospike cluster if needed")

	// NoOp if already exist
	r.Log.Info("AerospikeCluster", "Spec", r.aeroCluster.Spec)
	if err := r.createSTSHeadlessSvc(); err != nil {
		r.Log.Error(err, "Failed to create headless service")
		return nil, reconcileError(err)
	}

	// Bad config should not come here. It should be validated in validation hook
	cmName := getNamespacedNameForSTSConfigMap(r.aeroCluster, rackState.Rack.ID)
	if err := r.buildSTSConfigMap(cmName, rackState.Rack); err != nil {
		r.Log.Error(err, "Failed to create configMap from AerospikeConfig")
		return nil, reconcileError(err)
	}

	stsName := getNamespacedNameForSTS(r.aeroCluster, rackState.Rack.ID)
	found, err := r.createSTS(stsName, rackState)
	if err != nil {
		r.Log.Error(
			err, "Statefulset setup failed. Deleting statefulset", "name",
			stsName, "err", err,
		)
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		_ = r.deleteSTS(found)
		return nil, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *SingleClusterReconciler) getRacksToDelete(rackStateList []RackState) (
	[]asdbv1beta1.Rack, error,
) {
	oldRacks, err := r.getCurrentRackList()

	if err != nil {
		return nil, err
	}

	var toDelete []asdbv1beta1.Rack
	for _, oldRack := range oldRacks {
		var rackFound bool
		for _, newRack := range rackStateList {
			if oldRack.ID == newRack.Rack.ID {
				rackFound = true
				break
			}
		}

		if !rackFound {
			toDelete = append(toDelete, oldRack)
		}
	}

	return toDelete, nil
}

func (r *SingleClusterReconciler) deleteRacks(
	racksToDelete []asdbv1beta1.Rack, ignorablePods []corev1.Pod,
) reconcileResult {
	for _, rack := range racksToDelete {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(r.aeroCluster, rack.ID)
		err := r.Client.Get(context.TODO(), stsName, found)
		if err != nil {
			// If not found then go to next
			if errors.IsNotFound(err) {
				continue
			}
			return reconcileError(err)
		}
		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		found, res := r.scaleDownRack(
			found, RackState{Size: 0, Rack: rack}, ignorablePods,
		)
		if !res.isSuccess {
			return res
		}

		// Delete sts
		if err := r.deleteSTS(found); err != nil {
			return reconcileError(err)
		}
	}
	return reconcileSuccess()
}
func (r *SingleClusterReconciler) reconcileRack(
	found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod,
) reconcileResult {

	r.Log.Info(
		"Reconcile existing Aerospike cluster statefulset", "stsName",
		found.Name,
	)

	var res reconcileResult

	r.Log.Info(
		"Ensure rack StatefulSet size is the same as the spec", "stsName",
		found.Name,
	)
	desiredSize := int32(rackState.Size)
	// Scale down
	if *found.Spec.Replicas > desiredSize {
		found, res = r.scaleDownRack(found, rackState, ignorablePods)
		if !res.isSuccess {
			if res.err != nil {
				r.Log.Error(
					res.err, "Failed to scaleDown StatefulSet pods", "stsName",
					found.Name,
				)
			}
			return res
		}
	}

	// Always update configMap. We won't be able to find if a rack's config, and it's pod config is in sync or not
	// Checking rack.spec, rack.status will not work.
	// We may change config, let some pods restart with new config and then change config back to original value.
	// Now rack.spec, rack.status will be same but few pods will have changed config.
	// So a check based on spec and status will skip configMap update.
	// Hence, a rolling restart of pod will never bring pod to desired config
	if err := r.updateSTSConfigMap(
		getNamespacedNameForSTSConfigMap(
			r.aeroCluster, rackState.Rack.ID,
		), rackState.Rack,
	); err != nil {
		r.Log.Error(
			err, "Failed to update configMap from AerospikeConfig", "stsName",
			found.Name,
		)
		return reconcileError(err)
	}

	// Upgrade
	upgradeNeeded, err := r.isRackUpgradeNeeded(rackState.Rack.ID)
	if err != nil {
		return reconcileError(err)
	}

	if upgradeNeeded {
		found, res = r.upgradeRack(found, rackState, ignorablePods)
		if !res.isSuccess {
			if res.err != nil {
				r.Log.Error(
					res.err, "Failed to update StatefulSet image", "stsName",
					found.Name,
				)
			}
			return res
		}
	} else {
		needRollingRestartRack, err := r.needRollingRestartRack(rackState)
		if err != nil {
			return reconcileError(err)
		}
		if needRollingRestartRack {
			found, res = r.rollingRestartRack(found, rackState, ignorablePods)
			if !res.isSuccess {
				if res.err != nil {
					r.Log.Error(
						res.err, "Failed to do rolling restart", "stsName",
						found.Name,
					)
				}
				return res
			}
		}
	}

	// Scale up after upgrading, so that new pods come up with new image
	if *found.Spec.Replicas < desiredSize {
		found, res = r.scaleUpRack(found, rackState)
		if !res.isSuccess {
			r.Log.Error(
				res.err, "Failed to scaleUp StatefulSet pods", "stsName",
				found.Name,
			)
			return res
		}
	}

	// All regular operation are complete. Take time and cleanup dangling nodes that have not been cleaned up previously due to errors.
	if err = r.cleanupDanglingPodsRack(found, rackState); err != nil {
		return reconcileError(err)
	}

	// TODO: check if all the pods are up or not
	return reconcileSuccess()
}

func (r *SingleClusterReconciler) needRollingRestartRack(rackState RackState) (
	bool, error,
) {
	podList, err := r.getTargetPodList(rackState.Rack.ID)
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %v", err)
	}
	for _, pod := range podList {
		// Check if this pod needs restart
		restartType, err := r.getRollingRestartTypePod(rackState, pod)
		if err != nil {
			return false, err
		}
		if restartType != NoRestart {
			return true, nil
		}
	}
	return false, nil
}

func (r *SingleClusterReconciler) scaleUpRack(
	found *appsv1.StatefulSet, rackState RackState,
) (*appsv1.StatefulSet, reconcileResult) {

	desiredSize := int32(rackState.Size)

	oldSz := *found.Spec.Replicas
	found.Spec.Replicas = &desiredSize

	r.Log.Info("Scaling up pods", "currentSz", oldSz, "desiredSz", desiredSize)

	// No need for this? But if image is bad then new pod will also come up
	//with bad node.
	podList, err := r.getRackPodList(rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(podList.Items) {
		return found, reconcileError(fmt.Errorf("cannot scale up AerospikeCluster. A pod is already in failed state"))
	}

	var newPodNames []string
	for i := oldSz; i < desiredSize; i++ {
		newPodNames = append(newPodNames, getSTSPodName(found.Name, i))
	}

	// Ensure none of the to be launched pods are active.
	for _, newPodName := range newPodNames {
		for _, pod := range podList.Items {
			if pod.Name == newPodName {
				return found, reconcileError(
					fmt.Errorf(
						"pod %s yet to be launched is still present",
						newPodName,
					),
				)
			}
		}
	}

	if err := r.cleanupDanglingPodsRack(found, rackState); err != nil {
		return found, reconcileError(
			fmt.Errorf(
				"failed scale up pre-check: %v", err,
			),
		)
	}

	if r.aeroCluster.Spec.PodSpec.MultiPodPerHost {
		// Create services for each pod
		for _, podName := range newPodNames {
			if err := r.createPodService(
				podName, r.aeroCluster.Namespace,
			); err != nil {
				return found, reconcileError(err)
			}
		}
	}

	// Scale up the statefulset
	if err := r.Client.Update(context.TODO(), found, updateOption); err != nil {
		return found, reconcileError(
			fmt.Errorf(
				"failed to update StatefulSet pods: %v", err,
			),
		)
	}

	if err := r.waitForSTSToBeReady(found); err != nil {
		return found, reconcileError(
			fmt.Errorf(
				"failed to wait for statefulset to be ready: %v", err,
			),
		)
	}

	// return a fresh copy
	found, err = r.getSTS(rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *SingleClusterReconciler) needsToUpdateContainers(
	containers []corev1.Container, podName string,
) bool {
	for _, container := range containers {
		desiredImage, err := utils.GetDesiredImage(
			r.aeroCluster, container.Name,
		)
		if err != nil {
			// Delete or auto-injected container that is safe to delete.
			continue
		}

		if !utils.IsImageEqual(container.Image, desiredImage) {
			r.Log.Info(
				"Found container for upgrading/downgrading in pod", "pod",
				podName, "container", container.Name, "currentImage",
				container.Image, "desiredImage", desiredImage,
			)
			return true
		}
	}
	return false
}

func (r *SingleClusterReconciler) upgradeRack(
	statefulSet *appsv1.StatefulSet, rackState RackState,
	ignorablePods []corev1.Pod,
) (*appsv1.StatefulSet, reconcileResult) {
	// List the pods for this aeroCluster's statefulset
	podList, err := r.getTargetPodList(rackState.Rack.ID)
	if err != nil {
		return statefulSet, reconcileError(
			fmt.Errorf(
				"failed to list pods: %v", err,
			),
		)
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
		return statefulSet, reconcileError(
			fmt.Errorf("upgrade rack : %v", err),
		)
	}

	for _, p := range podList {
		r.Log.Info("Check if pod needs upgrade or not", "podName", p.Name)
		needPodUpgrade := r.needsToUpdateContainers(
			p.Spec.Containers, p.Name,
		) ||
			r.needsToUpdateContainers(p.Spec.InitContainers, p.Name)

		if !needPodUpgrade {
			r.Log.Info("Pod doesn't need upgrade", "podName", p.Name)
			continue

		}

		// Also check if statefulSet is in stable condition
		// Check for all containers. Status.ContainerStatuses doesn't include init container
		res := r.deletePodAndEnsureImageUpdated(rackState, p, ignorablePods)
		if !res.isSuccess {
			return statefulSet, res
		}
		// Handle the next pod in subsequent Reconcile.
		return statefulSet, reconcileRequeueAfter(0)
	}
	// return a fresh copy
	statefulSet, err = r.getSTS(rackState)
	if err != nil {
		return statefulSet, reconcileError(err)
	}
	return statefulSet, reconcileSuccess()
}

func (r *SingleClusterReconciler) scaleDownRack(
	found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod,
) (*appsv1.StatefulSet, reconcileResult) {

	desiredSize := int32(rackState.Size)

	r.Log.Info(
		"ScaleDown AerospikeCluster statefulset", "desiredSz", desiredSize,
		"currentSz", *found.Spec.Replicas,
	)

	// Continue if scaleDown is not needed
	if *found.Spec.Replicas <= desiredSize {
		return found, reconcileSuccess()
	}

	oldPodList, err := r.getRackPodList(rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	if r.isAnyPodInFailedState(oldPodList.Items) {
		return found, reconcileError(fmt.Errorf("cannot scale down AerospikeCluster. A pod is already in failed state"))
	}

	var pod *corev1.Pod

	if *found.Spec.Replicas > desiredSize {

		// maintain list of removed pods. It will be used for alumni-reset and tip-clear
		podName := getSTSPodName(found.Name, *found.Spec.Replicas-1)

		pod = utils.GetPod(podName, oldPodList.Items)

		// Ignore safe stop check on pod not in running state.
		if utils.IsPodRunningAndReady(pod) {
			if res := r.waitForNodeSafeStopReady(
				pod, ignorablePods,
			); !res.isSuccess {
				// The pod is running and is unsafe to terminate.
				return found, res
			}
		}

		// Update new object with new size
		newSize := *found.Spec.Replicas - 1
		found.Spec.Replicas = &newSize
		if err := r.Client.Update(
			context.TODO(), found, updateOption,
		); err != nil {
			return found, reconcileError(
				fmt.Errorf(
					"failed to update pod size %d StatefulSet pods: %v",
					newSize, err,
				),
			)
		}

		// Wait for pods to get terminated
		if err := r.waitForSTSToBeReady(found); err != nil {
			return found, reconcileError(
				fmt.Errorf(
					"failed to wait for statefulset to be ready: %v", err,
				),
			)
		}

		// Fetch new object
		nFound, err := r.getSTS(rackState)
		if err != nil {
			return found, reconcileError(
				fmt.Errorf(
					"failed to get StatefulSet pods: %v", err,
				),
			)
		}
		found = nFound

		err = r.cleanupPods([]string{podName}, rackState)
		if err != nil {
			return nFound, reconcileError(
				fmt.Errorf(
					"failed to cleanup pod %s: %v", podName, err,
				),
			)
		}

		r.Log.Info("Pod Removed", "podName", podName)
	}

	return found, reconcileRequeueAfter(0)
}

func (r *SingleClusterReconciler) rollingRestartRack(
	found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod,
) (*appsv1.StatefulSet, reconcileResult) {

	r.Log.Info("Rolling restart AerospikeCluster statefulset nodes with new config")

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getTargetPodList(rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(podList) {
		return found, reconcileError(fmt.Errorf("cannot Rolling restart AerospikeCluster. A pod is already in failed state"))
	}

	err = r.updateSTS(found, rackState)
	if err != nil {
		return found, reconcileError(
			fmt.Errorf("rolling restart failed: %v", err),
		)
	}

	r.Log.Info(
		"Statefulset spec updated - doing rolling restart with new" +
			" config",
	)

	for _, pod := range podList {
		// Check if this pod needs restart
		restartType, err := r.getRollingRestartTypePod(rackState, pod)
		if err != nil {
			return found, reconcileError(err)
		}

		if restartType == NoRestart {
			r.Log.Info(
				"This Pod doesn't need rolling restart, Skip this", "pod",
				pod.Name,
			)
			continue
		}

		res := r.rollingRestartPod(rackState, pod, restartType, ignorablePods)
		if !res.isSuccess {
			return found, res
		}

		// Handle next pod in subsequent Reconcile.
		return found, reconcileRequeueAfter(0)
	}

	// return a fresh copy
	found, err = r.getSTS(rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *SingleClusterReconciler) isRackUpgradeNeeded(rackID int) (
	bool, error,
) {

	podList, err := r.getRackPodList(rackID)
	if err != nil {
		return true, fmt.Errorf("failed to list pods: %v", err)
	}
	for _, p := range podList.Items {
		if !utils.IsPodOnDesiredImage(&p, r.aeroCluster) {
			r.Log.Info("Pod needs upgrade/downgrade", "podName", p.Name)
			return true, nil
		}

	}
	return false, nil
}

func (r *SingleClusterReconciler) isRackStorageUpdatedInAeroCluster(
	rackState RackState, pod corev1.Pod,
) bool {

	volumes := rackState.Rack.Storage.Volumes

	for _, volume := range volumes {

		// Check for Updated volumeSource
		if r.isStorageVolumeSourceUpdated(volume, pod) {
			r.Log.Info(
				"Volume added or volume source updated in rack storage" +
					" - pod needs rolling restart",
			)
			return true
		}

		// Check for Added/Updated volumeAttachments
		var containerAttachments []asdbv1beta1.VolumeAttachment
		containerAttachments = append(containerAttachments, volume.Sidecars...)
		if volume.Aerospike != nil {
			containerAttachments = append(
				containerAttachments, asdbv1beta1.VolumeAttachment{
					ContainerName: asdbv1beta1.AerospikeServerContainerName,
					Path:          volume.Aerospike.Path,
				},
			)
		}

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
		asdbv1beta1.
			AerospikeServerInitContainerName,
	}
	allConfiguredContainers := []string{asdbv1beta1.AerospikeServerContainerName}

	for _, c := range r.aeroCluster.Spec.PodSpec.Sidecars {
		allConfiguredContainers = append(allConfiguredContainers, c.Name)
	}

	for _, c := range r.aeroCluster.Spec.PodSpec.InitContainers {
		allConfiguredInitContainers = append(
			allConfiguredInitContainers, c.Name,
		)
	}

	if r.isVolumeAttachmentRemoved(
		volumes, allConfiguredContainers,
		pod.Spec.Containers, false,
	) ||
		r.isVolumeAttachmentRemoved(
			volumes, allConfiguredInitContainers,
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

func (r *SingleClusterReconciler) isStorageVolumeSourceUpdated(
	volume asdbv1beta1.VolumeSpec, pod corev1.Pod,
) bool {
	podVolume := getPodVolume(pod, volume.Name)
	if podVolume == nil {
		// Volume not found in pod.volumes. This is newly added volume.
		r.Log.Info(
			"New volume added in rack storage - pod needs rolling" +
				" restart",
		)
		return true
	}

	var volumeCopy asdbv1beta1.VolumeSpec
	lib.DeepCopy(&volumeCopy, &volume)

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
	volumeName string, volumeAttachments []asdbv1beta1.VolumeAttachment,
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
				volumeMount.ReadOnly != attachment.ReadOnly ||
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
	volumes []asdbv1beta1.VolumeSpec, configuredContainers []string,
	podContainers []corev1.Container, isInitContainers bool,
) bool {
	// TODO: Deal with injected volumes later.
	for _, container := range podContainers {
		if isInitContainers && container.Name == asdbv1beta1.AerospikeServerInitContainerName {
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
	volumes []asdbv1beta1.VolumeSpec, containerVolumeName string,
	containerName string, isInitContainers bool,
) bool {
	volume := getStorageVolume(volumes, containerVolumeName)
	if volume == nil {
		// Volume may have been removed, we allow removal of all volumes (except pv type)
		r.Log.Info(
			"Volume removed from rack storage", "volumeName",
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
		if containerName == asdbv1beta1.AerospikeServerContainerName {
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

func (r *SingleClusterReconciler) getOrderedRackPodList(rackID int) ([]corev1.Pod, error) {
	podList, err := r.getRackPodList(rackID)
	if err != nil {
		return nil, err
	}
	sortedList := make([]corev1.Pod, len(podList.Items))
	for _, p := range podList.Items {
		indexStr := strings.Split(p.Name, "-")
		// Index is last, [1] can be rackID
		indexInt, _ := strconv.Atoi(indexStr[len(indexStr)-1])
		if indexInt >= len(podList.Items) {
			// Happens if we do not get full list of pods due to a crash,
			return nil, fmt.Errorf("error get pod list for rack:%v", rackID)
		}
		sortedList[(len(podList.Items)-1)-indexInt] = p
	}
	return sortedList, nil
}

func (r *SingleClusterReconciler) getTargetPodList(rackID int) ([]corev1.Pod, error) {
	podList, err := r.getOrderedRackPodList(rackID)
	if err != nil {
		return nil, err
	}
	if len(podList) <= 1 {
		return podList, nil
	}
	tmp := podList[:r.getRollOutPods(len(podList))]
	return tmp, nil
}

func (r *SingleClusterReconciler) getCurrentRackList() (
	[]asdbv1beta1.Rack, error,
) {
	var rackList []asdbv1beta1.Rack
	rackList = append(rackList, r.aeroCluster.Status.RackConfig.Racks...)

	// Create dummy rack structures for dangling racks that have stateful sets but were deleted later because rack before status was updated.
	statefulSetList, err := r.getClusterSTSList()
	if err != nil {
		return nil, err
	}

	for _, sts := range statefulSetList.Items {
		rackID, err := utils.GetRackIDFromSTSName(sts.Name)
		if err != nil {
			return nil, err
		}

		found := false
		for _, rack := range r.aeroCluster.Status.RackConfig.Racks {
			if rack.ID == *rackID {
				found = true
				break
			}
		}

		if !found {
			// Create a dummy rack config using globals.
			// TODO: Refactor and reuse code in mutate setting.
			dummyRack := asdbv1beta1.Rack{
				ID: *rackID, Storage: r.aeroCluster.Spec.Storage,
				AerospikeConfig: *r.aeroCluster.Spec.AerospikeConfig,
			}

			rackList = append(rackList, dummyRack)
		}
	}

	return rackList, nil
}

func isContainerNameInStorageVolumeAttachments(
	containerName string, mounts []asdbv1beta1.VolumeAttachment,
) bool {
	for _, mount := range mounts {
		if mount.ContainerName == containerName {
			return true
		}
	}
	return false
}

func splitRacks(nodes, racks int) []int {
	nodesPerRack, extraNodes := nodes/racks, nodes%racks

	// Distributing nodes in given racks
	var topology []int

	for rackIdx := 0; rackIdx < racks; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}
		topology = append(topology, nodesForThisRack)
	}

	return topology
}

func getConfiguredRackStateList(aeroCluster *asdbv1beta1.AerospikeCluster) []RackState {
	topology := splitRacks(
		int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks),
	)
	var rackStateList []RackState
	for idx, rack := range aeroCluster.Spec.RackConfig.Racks {
		if topology[idx] == 0 {
			// Skip the rack, if it's size is 0
			continue
		}
		rackStateList = append(
			rackStateList, RackState{
				Rack: rack,
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

func getPodVolume(pod corev1.Pod, name string) *corev1.Volume {
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == name {
			return &volume
		}
	}
	return nil
}

func getStorageVolume(
	volumes []asdbv1beta1.VolumeSpec, name string,
) *asdbv1beta1.VolumeSpec {
	for _, volume := range volumes {
		if name == volume.Name {
			return &volume
		}
	}
	return nil
}

func getContainer(
	podContainers []corev1.Container, name string,
) *corev1.Container {
	for _, container := range podContainers {
		if name == container.Name {
			return &container
		}
	}
	return nil
}

func getOriginalPath(path string) string {
	path = strings.TrimPrefix(path, "/workdir/filesystem-volumes")
	path = strings.TrimPrefix(path, "/workdir/block-volumes")
	return path
}
