package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	lib "github.com/aerospike/aerospike-management-lib"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
)

func (r *AerospikeClusterReconciler) reconcileRacks(aeroCluster *asdbv1alpha1.AerospikeCluster) reconcileResult {

	r.Log.Info("Reconciling rack for AerospikeCluster")

	var scaledDownRackSTSList []appsv1.StatefulSet
	var scaledDownRackList []RackState
	var res reconcileResult

	rackStateList := getNewRackStateList(aeroCluster)
	racksToDelete, err := r.getRacksToDelete(aeroCluster, rackStateList)
	if err != nil {
		return reconcileError(err)
	}

	rackIDsToDelete := []int{}
	for _, rack := range racksToDelete {
		rackIDsToDelete = append(rackIDsToDelete, rack.ID)
	}

	ignorablePods, err := r.getIgnorablePods(aeroCluster, racksToDelete)
	if err != nil {
		return reconcileError(err)
	}

	ignorablePodNames := []string{}
	for _, pod := range ignorablePods {
		ignorablePodNames = append(ignorablePodNames, pod.Name)
	}

	r.Log.Info("Rack changes", "racksToDelete", rackIDsToDelete, "ignorablePods", ignorablePodNames)

	for _, state := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(aeroCluster, state.Rack.ID)
		if err := r.Client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return reconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in reconcile
			zeroSizedRack := RackState{Rack: state.Rack, Size: 0}
			found, res = r.createRack(aeroCluster, zeroSizedRack)
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
			if res := r.reconcileRack(aeroCluster, found, state, ignorablePods); !res.isSuccess {
				return res
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx, state := range scaledDownRackList {
		if res := r.reconcileRack(aeroCluster, &scaledDownRackSTSList[idx], state, ignorablePods); !res.isSuccess {
			return res
		}
	}

	if len(aeroCluster.Status.RackConfig.Racks) != 0 {
		// Remove removed racks
		if res := r.deleteRacks(aeroCluster, racksToDelete, ignorablePods); !res.isSuccess {
			if res.err != nil {
				r.Log.Error(err, "Failed to remove statefulset for removed racks", "err", res.err)
			}
			return res
		}
	}

	return reconcileSuccess()
}

func (r *AerospikeClusterReconciler) createRack(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {

	r.Log.Info("Create new Aerospike cluster if needed")

	// NoOp if already exist
	r.Log.Info("AerospikeCluster", "Spec", aeroCluster.Spec)
	if err := r.createSTSHeadlessSvc(aeroCluster); err != nil {
		r.Log.Error(err, "Failed to create headless service")
		return nil, reconcileError(err)
	}

	// Bad config should not come here. It should be validated in validation hook
	cmName := getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID)
	if err := r.buildSTSConfigMap(aeroCluster, cmName, rackState.Rack); err != nil {
		r.Log.Error(err, "Failed to create configMap from AerospikeConfig")
		return nil, reconcileError(err)
	}

	stsName := getNamespacedNameForSTS(aeroCluster, rackState.Rack.ID)
	found, err := r.createSTS(aeroCluster, stsName, rackState)
	if err != nil {
		r.Log.Error(err, "Statefulset setup failed. Deleting statefulset", "name", stsName, "err", err)
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		r.deleteSTS(aeroCluster, found)
		return nil, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *AerospikeClusterReconciler) getRacksToDelete(aeroCluster *asdbv1alpha1.AerospikeCluster, rackStateList []RackState) ([]asdbv1alpha1.Rack, error) {
	oldRacks, err := r.getOldRackList(aeroCluster)

	if err != nil {
		return nil, err
	}

	toDelete := []asdbv1alpha1.Rack{}
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

func (r *AerospikeClusterReconciler) deleteRacks(aeroCluster *asdbv1alpha1.AerospikeCluster, racksToDelete []asdbv1alpha1.Rack, ignorablePods []corev1.Pod) reconcileResult {
	for _, rack := range racksToDelete {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(aeroCluster, rack.ID)
		err := r.Client.Get(context.TODO(), stsName, found)
		if err != nil {
			// If not found then go to next
			if errors.IsNotFound(err) {
				continue
			}
			return reconcileError(err)
		}
		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		found, res := r.scaleDownRack(aeroCluster, found, RackState{Size: 0, Rack: rack}, ignorablePods)
		if !res.isSuccess {
			return res
		}

		// Delete sts
		if err := r.deleteSTS(aeroCluster, found); err != nil {
			return reconcileError(err)
		}
	}
	return reconcileSuccess()
}

func (r *AerospikeClusterReconciler) reconcileRack(aeroCluster *asdbv1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod) reconcileResult {

	r.Log.Info("Reconcile existing Aerospike cluster statefulset")

	var err error
	var res reconcileResult

	r.Log.Info("Ensure rack StatefulSet size is the same as the spec")
	desiredSize := int32(rackState.Size)
	// Scale down
	if *found.Spec.Replicas > desiredSize {
		found, res = r.scaleDownRack(aeroCluster, found, rackState, ignorablePods)
		if !res.isSuccess {
			if res.err != nil {
				r.Log.Error(err, "Failed to scaleDown StatefulSet pods", "err", res.err)
			}
			return res
		}
	}

	// Always update configMap. We won't be able to find if a rack's config and it's pod config is in sync or not
	// Checking rack.spec, rack.status will not work.
	// We may change config, let some pods restart with new config and then change config back to original value.
	// Now rack.spec, rack.status will be same but few pods will have changed config.
	// So a check based on spec and status will skip configMap update.
	// Hence a rolling restart of pod will never bring pod to desired config
	if err := r.updateSTSConfigMap(aeroCluster, getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID), rackState.Rack); err != nil {
		r.Log.Error(err, "Failed to update configMap from AerospikeConfig")
		return reconcileError(err)
	}

	// Upgrade
	upgradeNeeded, err := r.isRackUpgradeNeeded(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return reconcileError(err)
	}

	if upgradeNeeded {
		found, res = r.upgradeRack(aeroCluster, found, aeroCluster.Spec.Image, rackState, ignorablePods)
		if !res.isSuccess {
			if res.err != nil {
				r.Log.Error(err, "Failed to update StatefulSet image", "err", res.err)
			}
			return res
		}
	} else {
		needRollingRestartRack, err := r.needRollingRestartRack(aeroCluster, rackState)
		if err != nil {
			return reconcileError(err)
		}
		if needRollingRestartRack {
			found, res = r.rollingRestartRack(aeroCluster, found, rackState, ignorablePods)
			if !res.isSuccess {
				if res.err != nil {
					r.Log.Error(err, "Failed to do rolling restart", "err", res.err)
				}
				return res
			}
		}
	}

	// Scale up after upgrading, so that new pods comeup with new image
	if *found.Spec.Replicas < desiredSize {
		found, res = r.scaleUpRack(aeroCluster, found, rackState)
		if !res.isSuccess {
			r.Log.Error(err, "Failed to scaleUp StatefulSet pods", "err", res.err)
			return res
		}
	}

	// All regular operation are complete. Take time and cleanup dangling nodes that have not been cleaned up previously due to errors.
	if err = r.cleanupDanglingPodsRack(aeroCluster, found, rackState); err != nil {
		return reconcileError(err)
	}

	// TODO: check if all the pods are up or not
	return reconcileSuccess()
}

func (r *AerospikeClusterReconciler) needRollingRestartRack(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState) (bool, error) {
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return false, fmt.Errorf("failed to list pods: %v", err)
	}
	for _, pod := range podList {
		// Check if this pod need restart
		needRollingRestart, err := r.needRollingRestartPod(aeroCluster, rackState, pod)
		if err != nil {
			return false, err
		}
		if needRollingRestart {
			return true, nil
		}
	}
	return false, nil
}

func (r *AerospikeClusterReconciler) scaleUpRack(aeroCluster *asdbv1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {

	desiredSize := int32(rackState.Size)

	oldSz := *found.Spec.Replicas
	found.Spec.Replicas = &desiredSize

	r.Log.Info("Scaling up pods", "currentSz", oldSz, "desiredSz", desiredSize)

	// No need for this? But if image is bad then new pod will also comeup with bad node.
	podList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(aeroCluster, podList.Items) {
		return found, reconcileError(fmt.Errorf("cannot scale up AerospikeCluster. A pod is already in failed state"))
	}

	newPodNames := []string{}
	for i := oldSz; i < desiredSize; i++ {
		newPodNames = append(newPodNames, getSTSPodName(found.Name, i))
	}

	// Ensure none of the to be launched pods are active.
	for _, newPodName := range newPodNames {
		for _, pod := range podList.Items {
			if pod.Name == newPodName {
				return found, reconcileError(fmt.Errorf("pod %s yet to be launched is still present", newPodName))
			}
		}
	}

	if err := r.cleanupDanglingPodsRack(aeroCluster, found, rackState); err != nil {
		return found, reconcileError(fmt.Errorf("failed scale up pre-check: %v", err))
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for each pod
		for _, podName := range newPodNames {
			if err := r.createPodService(aeroCluster, podName, aeroCluster.Namespace); err != nil {
				return found, reconcileError(err)
			}
		}
	}

	// Scale up the statefulset
	if err := r.Client.Update(context.TODO(), found, updateOption); err != nil {
		return found, reconcileError(fmt.Errorf("failed to update StatefulSet pods: %v", err))
	}

	if err := r.waitForSTSToBeReady(found); err != nil {
		return found, reconcileError(fmt.Errorf("failed to wait for statefulset to be ready: %v", err))
	}

	// return a fresh copy
	found, err = r.getSTS(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *AerospikeClusterReconciler) upgradeRack(aeroCluster *asdbv1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredImage string, rackState RackState, ignorablePods []corev1.Pod) (*appsv1.StatefulSet, reconcileResult) {

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	// Update strategy for statefulSet is OnDelete, so client.Update will not start update.
	// Update will happen only when a pod is deleted.
	// So first update image and then delete a pod. Pod will come up with new image.
	// Repeat the above process.
	needsUpdate := false
	for i, container := range found.Spec.Template.Spec.Containers {
		desiredImage, err := utils.GetDesiredImage(aeroCluster, container.Name)

		if err != nil {
			// Maybe a deleted sidecar.
			continue
		}

		if !utils.IsImageEqual(container.Image, desiredImage) {
			r.Log.Info("Updating image in statefulset spec", "container", container.Name, "desiredImage", desiredImage, "currentImage", container.Image)
			found.Spec.Template.Spec.Containers[i].Image = desiredImage
			needsUpdate = true
		}
	}

	if needsUpdate {
		err = r.Client.Update(context.TODO(), found, updateOption)
		if err != nil {
			return found, reconcileError(fmt.Errorf("failed to update image for StatefulSet %s: %v", found.Name, err))
		}
	}

	for _, p := range podList {
		r.Log.Info("Check if pod needs upgrade or not")
		var needPodUpgrade bool
		for _, ps := range p.Spec.Containers {
			desiredImage, err := utils.GetDesiredImage(aeroCluster, ps.Name)
			if err != nil {
				continue
			}

			if !utils.IsImageEqual(ps.Image, desiredImage) {
				r.Log.Info("Upgrading/downgrading pod", "podName", p.Name, "currentImage", ps.Image, "desiredImage", desiredImage)
				needPodUpgrade = true
				break
			}
		}

		if !needPodUpgrade {
			r.Log.Info("Pod doesn't need upgrade")
			continue
		}

		// Also check if statefulSet is in stable condition
		// Check for all containers. Status.ContainerStatuses doesn't include init container
		res := r.ensurePodImageUpdated(aeroCluster, desiredImage, rackState, p, ignorablePods)
		if !res.isSuccess {
			return found, res
		}

		// Handle the next pod in subsequent reconcile.
		return found, reconcileRequeueAfter(0)
	}

	// return a fresh copy
	found, err = r.getSTS(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *AerospikeClusterReconciler) scaleDownRack(aeroCluster *asdbv1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod) (*appsv1.StatefulSet, reconcileResult) {

	desiredSize := int32(rackState.Size)

	r.Log.Info("ScaleDown AerospikeCluster statefulset", "desiredSz", desiredSize, "currentSz", *found.Spec.Replicas)

	// Continue if scaleDown is not needed
	if *found.Spec.Replicas <= desiredSize {
		return found, reconcileSuccess()
	}

	oldPodList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}

	if r.isAnyPodInFailedState(aeroCluster, oldPodList.Items) {
		return found, reconcileError(fmt.Errorf("cannot scale down AerospikeCluster. A pod is already in failed state"))
	}

	var pod *corev1.Pod

	if *found.Spec.Replicas > desiredSize {

		// maintain list of removed pods. It will be used for alumni-reset and tip-clear
		podName := getSTSPodName(found.Name, *found.Spec.Replicas-1)

		pod = utils.GetPod(podName, oldPodList.Items)

		// Ignore safe stop check on pod not in running state.
		if utils.IsPodRunningAndReady(pod) {
			if res := r.waitForNodeSafeStopReady(aeroCluster, pod, ignorablePods); !res.isSuccess {
				// The pod is running and is unsafe to terminate.
				return found, res
			}
		}

		// Update new object with new size
		newSize := *found.Spec.Replicas - 1
		found.Spec.Replicas = &newSize
		if err := r.Client.Update(context.TODO(), found, updateOption); err != nil {
			return found, reconcileError(fmt.Errorf("failed to update pod size %d StatefulSet pods: %v", newSize, err))
		}

		// Wait for pods to get terminated
		if err := r.waitForSTSToBeReady(found); err != nil {
			return found, reconcileError(fmt.Errorf("failed to wait for statefulset to be ready: %v", err))
		}

		// Fetch new object
		nFound, err := r.getSTS(aeroCluster, rackState)
		if err != nil {
			return found, reconcileError(fmt.Errorf("failed to get StatefulSet pods: %v", err))
		}
		found = nFound

		err = r.cleanupPods(aeroCluster, []string{podName}, rackState)
		if err != nil {
			return nFound, reconcileError(fmt.Errorf("failed to cleanup pod %s: %v", podName, err))
		}

		r.Log.Info("Pod Removed", "podName", podName)
	}

	return found, reconcileRequeueAfter(0)
}

func (r *AerospikeClusterReconciler) rollingRestartRack(aeroCluster *asdbv1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod) (*appsv1.StatefulSet, reconcileResult) {

	r.Log.Info("Rolling restart AerospikeCluster statefulset nodes with new config")

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(aeroCluster, podList) {
		return found, reconcileError(fmt.Errorf("cannot Rolling restart AerospikeCluster. A pod is already in failed state"))
	}

	ls := utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackState.Rack.ID)

	// Can we optimize this? Update stateful set only if there is any update for it.
	r.updateSTSPodSpec(aeroCluster, found, ls, rackState)

	// This should be called before updating storage
	initializeSTSStorage(aeroCluster, found, rackState)

	// TODO: Add validation. device, file, both should not exist in same storage class
	r.updateSTSStorage(aeroCluster, found, rackState)

	r.updateSTSContainerResources(aeroCluster, found)

	r.Log.Info("Updating statefulset spec")

	if err := r.Client.Update(context.TODO(), found, updateOption); err != nil {
		return found, reconcileError(fmt.Errorf("failed to update StatefulSet %s: %v", found.Name, err))
	}
	r.Log.Info("Statefulset spec updated. Doing rolling restart with new config")

	for _, pod := range podList {
		// Check if this pod need restart
		needRollingRestart, err := r.needRollingRestartPod(aeroCluster, rackState, pod)
		if err != nil {
			return found, reconcileError(err)
		}
		if !needRollingRestart {
			r.Log.Info("This Pod doesn't need rolling restart, Skip this", "pod", pod.Name)
			continue
		}

		res := r.rollingRestartPod(aeroCluster, rackState, pod, ignorablePods)
		if !res.isSuccess {
			return found, res
		}

		// Handle next pod in subsequent reconcile.
		return found, reconcileRequeueAfter(0)
	}

	// return a fresh copy
	found, err = r.getSTS(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *AerospikeClusterReconciler) isRackUpgradeNeeded(aeroCluster *asdbv1alpha1.AerospikeCluster, rackID int) (bool, error) {

	podList, err := r.getRackPodList(aeroCluster, rackID)
	if err != nil {
		return true, fmt.Errorf("failed to list pods: %v", err)
	}
	for _, p := range podList.Items {
		if !utils.IsPodOnDesiredImage(&p, aeroCluster) {
			r.Log.Info("Pod need upgrade/downgrade")
			return true, nil
		}

	}
	return false, nil
}

func (r *AerospikeClusterReconciler) isRackConfigMapsUpdatedInAeroCluster(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) bool {
	configMaps := rackState.Rack.Storage.GetConfigMaps()

	var volumesToBeMatched []corev1.Volume
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap == nil ||
			volume.Name == confDirName ||
			volume.Name == initConfDirName {
			continue
		}
		volumesToBeMatched = append(volumesToBeMatched, volume)
	}

	if len(configMaps) != len(volumesToBeMatched) {
		return true
	}

	for _, configMapVolume := range configMaps {
		pvcName := configMapVolume.Name
		found := false
		for _, volume := range volumesToBeMatched {
			if volume.Name == pvcName {
				found = true
				break
			}
		}

		if !found {
			return true
		}

	}
	return false
}

func (r *AerospikeClusterReconciler) isRackPVStorageUpdatedInAeroCluster(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) bool {

	PVs := rackState.Rack.Storage.GetPVs()

	// Check for Added/Updated volumeMounts
	for _, volume := range PVs {

		// aerospike cont
		// sidecars
		// additionalInitCont
		var newMounts []v1alpha1.VolumeAttachment
		newMounts = append(newMounts, volume.Sidecars...)
		if volume.Aerospike.Path != "" {
			newMounts = append(newMounts, v1alpha1.VolumeAttachment{
				ContainerName: v1alpha1.AerospikeServerContainerName,
				// TODO fix path name
				Path: volume.Aerospike.Path,
			})
		}

		if r.isPVAttachmentAddedOrUpdated(volume.Name, volume.Source.PersistentVolume.VolumeMode, newMounts, pod.Spec.Containers, false) ||
			r.isPVAttachmentAddedOrUpdated(volume.Name, volume.Source.PersistentVolume.VolumeMode, volume.InitContainers, pod.Spec.InitContainers, true) {
			r.Log.Info("New volumeMount added/updated in rack storage. Pod Need rolling restart")
			return true
		}

	}

	// Check for removed volumeMounts
	if r.isPVAttachmentRemoved(PVs, pod.Spec.Containers, false) ||
		r.isPVAttachmentRemoved(PVs, pod.Spec.InitContainers, true) {
		r.Log.Info("volumeMount removed rack storage. Pod Need rolling restart")
		return true
	}

	return false
}

func (r *AerospikeClusterReconciler) isPVAttachmentAddedOrUpdated(volumeName string, volumeMode v1.PersistentVolumeMode, volumeAttachments []v1alpha1.VolumeAttachment, podContainers []v1.Container, isInitContainers bool) bool {

	for _, attachment := range volumeAttachments {
		for _, container := range podContainers {
			var pathPrefix string
			// Find container in the pod and then look for volumeAttachments
			if attachment.ContainerName == container.Name {
				if volumeMode == v1.PersistentVolumeBlock {
					if isInitContainers {
						pathPrefix = "/block-volumes"
					}

					for _, volumeDevice := range container.VolumeDevices {

						var found bool
						if volumeDevice.Name == volumeName {
							found = true
							if volumeDevice.DevicePath != pathPrefix+attachment.Path {
								r.Log.Info("volumeMount path updated in rack storage", "oldpath", volumeDevice.DevicePath, "newpath", pathPrefix+attachment.Path)
								// Updated volume
								return true
							}
							break
						}
						if !found {
							// Added volume
							r.Log.Info("volumeMount added in rack storage", "volume", volumeName, "containerName", container.Name)
							return true
						}
					}

				} else if volumeMode == v1.PersistentVolumeFilesystem {
					if isInitContainers {
						pathPrefix = "/filesystem-volumes"
					}

					var found bool
					for _, volumeMount := range container.VolumeMounts {
						if volumeMount.Name == volumeName {
							found = true
							if volumeMount.MountPath != pathPrefix+attachment.Path {
								// Updated volume
								r.Log.Info("volumeMount path updated in rack storage", "oldpath", volumeMount.MountPath, "newpath", pathPrefix+attachment.Path)
								return true
							}
							break
						}
					}
					if !found {
						// Added volume
						r.Log.Info("volumeMount added in rack storage", "volume", volumeName, "containerName", container.Name)
						return true
					}
				} else {
					// Nothing should come here
					continue
				}
				break
			}
		}
	}

	return false
}

func (r *AerospikeClusterReconciler) isPVAttachmentRemoved(volumes []v1alpha1.VolumeSpec, podContainers []v1.Container, isInitContainers bool) bool {
	var storageVDevices []v1alpha1.VolumeSpec
	var storageVMounts []v1alpha1.VolumeSpec

	for _, volume := range volumes {
		if volume.Source.PersistentVolume == nil {
			continue
		}

		if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
			storageVDevices = append(storageVDevices, volume)
		} else if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
			storageVMounts = append(storageVMounts, volume)
		} else {
			continue
		}
	}

	for _, container := range podContainers {
		if isInitContainers && container.Name == v1alpha1.AerospikeServerInitContainerName {
			// Initcontainer has all the volumes mounted, there is no specific entry in storage for initcontainer
			continue
		}
		for _, volumeDevice := range container.VolumeDevices {
			if volumeDevice.Name == confDirName ||
				volumeDevice.Name == initConfDirName {
				continue
			}
			if !r.isContainerVolumeInStorage(storageVDevices, volumeDevice.Name, container.Name, isInitContainers) {
				r.Log.Info("volumeMount removed from rack storage", "volumeDevice", volumeDevice.Name, "containerName", container.Name)
				return true
			}
		}

		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == confDirName ||
				volumeMount.Name == initConfDirName {
				continue
			}
			if !r.isContainerVolumeInStorage(storageVMounts, volumeMount.Name, container.Name, isInitContainers) {
				r.Log.Info("volumeMount removed from rack storage", "volumeDevice", volumeMount.Name, "containerName", container.Name)
				return true
			}
		}
	}
	return false
}

func (r *AerospikeClusterReconciler) isContainerVolumeMountInStorage(volumes []v1alpha1.VolumeSpec, volumeMount v1.VolumeMount, containerName string, isInitContainers bool) bool {

	for _, volume := range volumes {
		if volumeMount.Name == volume.Name {
			if isInitContainers {
				if !isContainerNameInStorageVolumeAttachments(containerName, volume.InitContainers) {
					return false
				}
			} else {
				if containerName == v1alpha1.AerospikeServerContainerName {
					if volume.Aerospike == nil {
						return false
					}
				} else {
					if !isContainerNameInStorageVolumeAttachments(containerName, volume.Sidecars) {
						return false
					}
				}
			}
			return true
		}
	}
	// volumeMount should be in storage volumes, if it is not there then it was not created by storage volumes.
	// because storage volumes can not be removed, only mounts can be added or removed inside them
	return true
}

func (r *AerospikeClusterReconciler) isRackStorageUpdatedInAeroCluster(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) bool {

	volumes := rackState.Rack.Storage.Volumes

	for _, volume := range volumes {

		// Check for Updated volumeSource
		if r.isStorageVolumeSourceUpdated(volume, pod) {
			r.Log.Info("Volume added or volume source updated in rack storage. Pod Need rolling restart")
			return true
		}

		// Check for Added/Updated volumeAttachments
		var containerAttachments []v1alpha1.VolumeAttachment
		containerAttachments = append(containerAttachments, volume.Sidecars...)
		if volume.Aerospike != nil {
			containerAttachments = append(containerAttachments, v1alpha1.VolumeAttachment{
				ContainerName: v1alpha1.AerospikeServerContainerName,
				Path:          volume.Aerospike.Path,
			})
		}

		if r.isVolumeAttachmentAddedOrUpdated(volume.Name, containerAttachments, pod.Spec.Containers) ||
			r.isVolumeAttachmentAddedOrUpdated(volume.Name, volume.InitContainers, pod.Spec.InitContainers) {
			r.Log.Info("New volume or VolumeAttachment added/updated in rack storage. Pod Need rolling restart")
			return true
		}

	}

	// Check for removed volumeAttachments
	if r.isVolumeAttachmentRemoved(volumes, pod.Spec.Containers, false) ||
		r.isVolumeAttachmentRemoved(volumes, pod.Spec.InitContainers, true) {
		r.Log.Info("volume or VolumeAttachment removed from rack storage. Pod Need rolling restart")
		return true
	}

	return false
}

func (r *AerospikeClusterReconciler) isStorageVolumeSourceUpdated(volume v1alpha1.VolumeSpec, pod corev1.Pod) bool {
	podVolume := getPodVolume(pod, volume.Name)
	if podVolume == nil {
		// Volume not found in pod.volumes. This is newly aaded volume
		r.Log.Info("New volume added in rack storage. Pod Need rolling restart")
		return true
	}

	var volumeCopy v1alpha1.VolumeSpec
	lib.DeepCopy(&volumeCopy, &volume)

	if volumeCopy.Source.Secret != nil {
		setDefaults_SecretVolumeSource(volumeCopy.Source.Secret)
	}
	if volumeCopy.Source.ConfigMap != nil {
		setDefaults_ConfigMapVolumeSource(volumeCopy.Source.ConfigMap)
	}

	if !reflect.DeepEqual(podVolume.Secret, volumeCopy.Source.Secret) {
		r.Log.Info("volume source updated", "old volume.source ", podVolume.VolumeSource, "new volume.source", volume.Source)
		return true
	}
	if !reflect.DeepEqual(podVolume.ConfigMap, volumeCopy.Source.ConfigMap) {
		r.Log.Info("volume source updated", "old volume.source ", podVolume.VolumeSource, "new volume.source", volume.Source)
		return true
	}
	if !reflect.DeepEqual(podVolume.EmptyDir, volumeCopy.Source.EmptyDir) {
		r.Log.Info("volume source updated", "old volume.source ", podVolume.VolumeSource, "new volume.source", volume.Source)
		return true
	}
	return false
}

func (r *AerospikeClusterReconciler) isVolumeAttachmentAddedOrUpdated(volumeName string, volumeAttachments []v1alpha1.VolumeAttachment, podContainers []corev1.Container) bool {

	for _, attachment := range volumeAttachments {
		container := getContainer(podContainers, attachment.ContainerName)
		// Not possible, only valid containerName should be there in attachment
		if container == nil {
			continue
		}

		volumeDevice := getContainerVolumeDevice(container.VolumeDevices, volumeName)
		if volumeDevice != nil {
			// Found, check for updated
			if getOriginalPath(volumeDevice.DevicePath) != attachment.Path {
				r.Log.Info("volume updated in rack storage", "old", volumeDevice, "new", attachment)
				return true
			}
			continue
		}

		volumeMount := getContainerVolumeMounts(container.VolumeMounts, volumeName)
		if volumeMount != nil {
			// Found, check for updated
			if getOriginalPath(volumeMount.MountPath) != attachment.Path ||
				volumeMount.ReadOnly != attachment.ReadOnly ||
				volumeMount.SubPath != attachment.SubPath ||
				volumeMount.SubPathExpr != attachment.SubPathExpr ||
				!reflect.DeepEqual(volumeMount.MountPropagation, attachment.MountPropagation) {

				r.Log.Info("volume updated in rack storage", "old", volumeMount, "new", attachment)
				return true
			}
			continue
		}

		// Added volume
		r.Log.Info("volume added in rack storage", "volume", volumeName, "containerName", container.Name)
		return true
	}

	return false
}

func (r *AerospikeClusterReconciler) isVolumeAttachmentRemoved(volumes []v1alpha1.VolumeSpec, podContainers []corev1.Container, isInitContainers bool) bool {
	for _, container := range podContainers {
		if isInitContainers && container.Name == v1alpha1.AerospikeServerInitContainerName {
			// Initcontainer has all the volumes mounted, there is no specific entry in storage for initcontainer
			continue
		}
		for _, volumeDevice := range container.VolumeDevices {
			if volumeDevice.Name == confDirName ||
				volumeDevice.Name == initConfDirName {
				continue
			}
			if !r.isContainerVolumeInStorage(volumes, volumeDevice.Name, container.Name, isInitContainers) {
				r.Log.Info("volume for container.volumeDevice removed from rack storage", "container.volumeDevice", volumeDevice.Name, "containerName", container.Name)
				return true
			}
		}

		for _, volumeMount := range container.VolumeMounts {
			if volumeMount.Name == confDirName ||
				volumeMount.Name == initConfDirName ||
				volumeMount.Name == secretVolumeName ||
				volumeMount.MountPath == podServiceAccountMountPath {
				continue
			}
			if !r.isContainerVolumeInStorage(volumes, volumeMount.Name, container.Name, isInitContainers) {
				r.Log.Info("volume for container.volumeMount removed from rack storage", "container.volumeMount", volumeMount.Name, "containerName", container.Name)
				return true
			}
		}
	}
	return false
}

func (r *AerospikeClusterReconciler) isContainerVolumeInStorage(volumes []v1alpha1.VolumeSpec, containerVolumeName string, containerName string, isInitContainers bool) bool {
	volume := getStorageVolume(volumes, containerVolumeName)
	if volume == nil {
		// Volume may have been removed, we allow removal of all volumes (except pv type)
		r.Log.Info("volume removed from rack storage", "volumeName", containerVolumeName)
		return false
	}

	if isInitContainers {
		if !isContainerNameInStorageVolumeAttachments(containerName, volume.InitContainers) {
			return false
		}
	} else {
		if containerName == v1alpha1.AerospikeServerContainerName {
			if volume.Aerospike == nil {
				return false
			}
		} else {
			if !isContainerNameInStorageVolumeAttachments(containerName, volume.Sidecars) {
				return false
			}
		}
	}
	return true
}

func (r *AerospikeClusterReconciler) getRackPodList(aeroCluster *asdbv1alpha1.AerospikeCluster, rackID int) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackID))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}
	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart

	if err := r.Client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *AerospikeClusterReconciler) getOrderedRackPodList(aeroCluster *asdbv1alpha1.AerospikeCluster, rackID int) ([]corev1.Pod, error) {
	podList, err := r.getRackPodList(aeroCluster, rackID)
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

func (r *AerospikeClusterReconciler) getOldRackList(aeroCluster *asdbv1alpha1.AerospikeCluster) ([]asdbv1alpha1.Rack, error) {
	var rackList []asdbv1alpha1.Rack = []asdbv1alpha1.Rack{}
	rackList = append(rackList, aeroCluster.Status.RackConfig.Racks...)

	// Create dummy rack structures for dangling racks that have stateful sets but were deleted later because rack before status was updated.
	statefulSetList, err := r.getClusterSTSList(aeroCluster)
	if err != nil {
		return nil, err
	}

	for _, sts := range statefulSetList.Items {
		rackID, err := utils.GetRackIDFromSTSName(sts.Name)
		if err != nil {
			return nil, err
		}

		found := false
		for _, rack := range aeroCluster.Status.RackConfig.Racks {
			if rack.ID == *rackID {
				found = true
				break
			}
		}

		if !found {
			// Create a dummy rack config using globals.
			// TODO: Refactor and reuse code in mutate setting.
			dummyRack := asdbv1alpha1.Rack{ID: *rackID, Storage: aeroCluster.Spec.Storage, AerospikeConfig: *aeroCluster.Spec.AerospikeConfig}

			rackList = append(rackList, dummyRack)
		}
	}

	return rackList, nil
}

func isContainerNameInStorageVolumeAttachments(containerName string, mounts []v1alpha1.VolumeAttachment) bool {
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

func getNewRackStateList(aeroCluster *asdbv1alpha1.AerospikeCluster) []RackState {
	topology := splitRacks(int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks))
	var rackStateList []RackState
	for idx, rack := range aeroCluster.Spec.RackConfig.Racks {
		rackStateList = append(rackStateList, RackState{
			Rack: rack,
			Size: topology[idx],
		})
	}
	return rackStateList
}

// TODO: These func are available in client-go@v1.5.2, for now creating our own
func setDefaults_SecretVolumeSource(obj *corev1.SecretVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.SecretVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}

func setDefaults_ConfigMapVolumeSource(obj *corev1.ConfigMapVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.ConfigMapVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}

func getContainerVolumeDevice(devices []corev1.VolumeDevice, name string) *corev1.VolumeDevice {
	for _, device := range devices {
		if device.Name == name {
			return &device
		}
	}
	return nil
}

func getContainerVolumeMounts(mounts []corev1.VolumeMount, name string) *corev1.VolumeMount {
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

func getStorageVolume(volumes []v1alpha1.VolumeSpec, name string) *v1alpha1.VolumeSpec {
	for _, volume := range volumes {
		if name == volume.Name {
			return &volume
		}
	}
	return nil
}

func getContainer(podContainers []corev1.Container, name string) *corev1.Container {
	for _, container := range podContainers {
		if name == container.Name {
			return &container
		}
	}
	return nil
}

func getOriginalPath(path string) string {
	path = strings.TrimPrefix(path, "/filesystem-volumes")
	path = strings.TrimPrefix(path, "/block-volumes")
	return path
}
