package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	webhookv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/internal/webhook/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// volumeExpansionRestartAnnotation marks a pod whose block device was expanded: asd reads the device size
// at start, so the pod needs a warm restart once the expansion completes. It is set before the PVC patch
// and cleared after the restart, so an operator restart in between never skips it.
const volumeExpansionRestartAnnotation = "aerospike.com/volume-expansion-restart"

const volumeExpansionRequeueInterval = 10

// reconcileRackVolumeExpansion grows the rack's PVCs to the persistent volume sizes of the spec, waits
// for the expansion to complete, then realigns the StatefulSet volumeClaimTemplates so that PVCs created
// later get the new size. PVCs are never shrunk.
func (r *SingleClusterReconciler) reconcileRackVolumeExpansion(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	volumes := webhookv1.GetPVsVolumesFromStorage(&rackState.Rack.Storage)
	if len(volumes) == 0 {
		return found, common.ReconcileSuccess()
	}

	pvcs, err := r.getRackPVCList(ctx, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return found, common.ReconcileError(fmt.Errorf("list PVCs for rack %d: %w", rackState.Rack.ID, err))
	}

	var inProgress []string

	for idx := range pvcs {
		pvc := &pvcs[idx]

		volume := getPVCVolumeConfig(&rackState.Rack.Storage, pvc.Annotations[storageVolumeAnnotationKey])
		if volume == nil || volume.Source.PersistentVolume == nil || pvc.DeletionTimestamp != nil {
			continue
		}

		desired := volume.Source.PersistentVolume.Size

		requested := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if requested.Cmp(desired) < 0 {
			if err := r.expandPVC(ctx, rackState, pvc, volume, desired); err != nil {
				return found, common.ReconcileError(err)
			}
		}

		capacity := pvc.Status.Capacity[corev1.ResourceStorage]
		if capacity.Cmp(desired) < 0 {
			if reason := getPVCResizeFailure(pvc); reason != "" {
				r.Recorder.Eventf(
					r.aeroCluster, corev1.EventTypeWarning, "VolumeExpansionFailed",
					"[rack-%d] Expansion of PVC %s to %s is failing: %s",
					rackState.Rack.ID, utils.GetNamespacedNameString(pvc), desired.String(), reason,
				)
			}

			inProgress = append(inProgress, pvc.Name)
		}
	}

	if len(inProgress) > 0 {
		r.Log.Info("Waiting for PVC expansion to complete", "rackID", rackState.Rack.ID, "pvcs", inProgress)

		return found, common.ReconcileRequeueAfter(volumeExpansionRequeueInterval)
	}

	if !isSTSVolumeClaimTemplateOutdated(found, volumes) {
		return found, common.ReconcileSuccess()
	}

	return r.realignSTSVolumeClaimTemplates(ctx, found, rackState)
}

// expandPVC patches the PVC storage request, after marking the pod for a warm restart when asd uses the
// volume as a block device.
func (r *SingleClusterReconciler) expandPVC(
	ctx context.Context, rackState *RackState, pvc *corev1.PersistentVolumeClaim, volume *asdbv1.VolumeSpec,
	desired resource.Quantity,
) error {
	var markedPod *corev1.Pod

	if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock && volume.Aerospike != nil {
		var err error

		podName := strings.TrimPrefix(pvc.Name, volume.Name+"-")
		if markedPod, err = r.markPodForVolumeExpansionRestart(ctx, podName); err != nil {
			return err
		}
	}

	r.Log.Info("Expanding PVC", "pvc", utils.GetNamespacedName(pvc), "size", desired.String())

	patch := client.MergeFrom(pvc.DeepCopy())

	if pvc.Spec.Resources.Requests == nil {
		pvc.Spec.Resources.Requests = corev1.ResourceList{}
	}

	pvc.Spec.Resources.Requests[corev1.ResourceStorage] = desired

	if err := r.Patch(ctx, pvc, patch); err != nil {
		// Nothing grew: drop the mark set above, or the pod would get a needless warm restart.
		if markedPod != nil {
			if clearErr := r.clearVolumeExpansionRestartMark(ctx, markedPod); clearErr != nil {
				r.Log.Error(clearErr, "Failed to clear volume expansion mark", "pod", utils.GetNamespacedName(markedPod))
			}
		}

		// The API server refuses the expansion when the StorageClass does not allow it.
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "VolumeExpansionFailed",
			"[rack-%d] Cannot expand PVC %s to %s: %v",
			rackState.Rack.ID, utils.GetNamespacedNameString(pvc), desired.String(), err,
		)

		return fmt.Errorf("expand PVC %s to %s: %w", utils.GetNamespacedNameString(pvc), desired.String(), err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "VolumeExpansionStarted",
		"[rack-%d] Expanding PVC %s to %s",
		rackState.Rack.ID, utils.GetNamespacedNameString(pvc), desired.String(),
	)

	return nil
}

// markPodForVolumeExpansionRestart marks the pod and returns it, or returns nil when the pod does not
// exist or was already marked by an earlier expansion.
func (r *SingleClusterReconciler) markPodForVolumeExpansionRestart(
	ctx context.Context, podName string,
) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.aeroCluster.Namespace, Name: podName}, pod); err != nil {
		if k8serrors.IsNotFound(err) {
			// A pod created later starts asd on the expanded device.
			return nil, nil
		}

		return nil, fmt.Errorf("get Pod %s: %w", podName, err)
	}

	if _, ok := pod.Annotations[volumeExpansionRestartAnnotation]; ok {
		return nil, nil
	}

	patch := client.MergeFrom(pod.DeepCopy())

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}

	pod.Annotations[volumeExpansionRestartAnnotation] = "true"

	if err := r.Patch(ctx, pod, patch); err != nil {
		return nil, fmt.Errorf("mark Pod %s for warm restart after volume expansion: %w",
			utils.GetNamespacedNameString(pod), err)
	}

	return pod, nil
}

func (r *SingleClusterReconciler) clearVolumeExpansionRestartMark(ctx context.Context, pod *corev1.Pod) error {
	if _, ok := pod.Annotations[volumeExpansionRestartAnnotation]; !ok {
		return nil
	}

	patch := client.MergeFrom(pod.DeepCopy())
	delete(pod.Annotations, volumeExpansionRestartAnnotation)

	if err := r.Patch(ctx, pod, patch); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("clear volume expansion mark on Pod %s: %w", utils.GetNamespacedNameString(pod), err)
	}

	return nil
}

// isVolumeExpansionRestartNeeded reports whether the pod is marked for a warm restart and every PVC of
// the pod has reached its requested size, so asd will see the grown device.
func (r *SingleClusterReconciler) isVolumeExpansionRestartNeeded(
	ctx context.Context, rackState *RackState, pod *corev1.Pod,
) (bool, error) {
	if _, ok := pod.Annotations[volumeExpansionRestartAnnotation]; !ok {
		return false, nil
	}

	pvcs, err := r.getPodsPVCList(ctx, []string{pod.Name}, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return false, fmt.Errorf("list PVCs of Pod %s: %w", utils.GetNamespacedNameString(pod), err)
	}

	for idx := range pvcs {
		requested := pvcs[idx].Spec.Resources.Requests[corev1.ResourceStorage]
		capacity := pvcs[idx].Status.Capacity[corev1.ResourceStorage]

		if capacity.Cmp(requested) < 0 {
			return false, nil
		}
	}

	return true, nil
}

// getPVCResizeFailure returns why the PVC expansion is failing, or "" when it is not.
func getPVCResizeFailure(pvc *corev1.PersistentVolumeClaim) string {
	if status := pvc.Status.AllocatedResourceStatuses[corev1.ResourceStorage]; status ==
		corev1.PersistentVolumeClaimControllerResizeInfeasible || status == corev1.PersistentVolumeClaimNodeResizeInfeasible {
		return string(status)
	}

	for idx := range pvc.Status.Conditions {
		condition := pvc.Status.Conditions[idx]
		if (condition.Type == corev1.PersistentVolumeClaimControllerResizeError ||
			condition.Type == corev1.PersistentVolumeClaimNodeResizeError) &&
			condition.Status == corev1.ConditionTrue {
			return fmt.Sprintf("%s: %s", condition.Type, condition.Message)
		}
	}

	return ""
}

// isSTSVolumeClaimTemplateOutdated reports whether a volumeClaimTemplate requests less than its volume size.
func isSTSVolumeClaimTemplateOutdated(sts *appsv1.StatefulSet, volumes []asdbv1.VolumeSpec) bool {
	for idx := range sts.Spec.VolumeClaimTemplates {
		template := &sts.Spec.VolumeClaimTemplates[idx]

		for volIdx := range volumes {
			if volumes[volIdx].Name != template.Name {
				continue
			}

			requested := template.Spec.Resources.Requests[corev1.ResourceStorage]
			if requested.Cmp(volumes[volIdx].Source.PersistentVolume.Size) < 0 {
				return true
			}
		}
	}

	return false
}

// realignSTSVolumeClaimTemplates recreates the rack StatefulSet from the spec, since volumeClaimTemplates
// are immutable. The StatefulSet is deleted with orphan propagation and recreated at the size that
// re-adopts every pod (createMissingRack): no pod is restarted and no PVC is touched.
func (r *SingleClusterReconciler) realignSTSVolumeClaimTemplates(
	ctx context.Context, found *appsv1.StatefulSet, rackState *RackState,
) (*appsv1.StatefulSet, common.ReconcileResult) {
	r.Log.Info(
		"Recreating StatefulSet to realign volumeClaimTemplates with expanded volumes",
		"statefulSet", utils.GetNamespacedName(found),
	)

	if err := r.Delete(ctx, found, client.PropagationPolicy(metav1.DeletePropagationOrphan)); err != nil &&
		!k8serrors.IsNotFound(err) {
		return found, common.ReconcileError(fmt.Errorf("delete StatefulSet %s with orphan propagation: %w",
			utils.GetNamespacedNameString(found), err))
	}

	const (
		deleteMaxRetry      = 30
		deleteRetryInterval = time.Second
	)

	for i := 0; ; i++ {
		err := r.Get(ctx, utils.GetNamespacedName(found), &appsv1.StatefulSet{})
		if k8serrors.IsNotFound(err) {
			break
		}

		if err != nil {
			return found, common.ReconcileError(err)
		}

		if i == deleteMaxRetry {
			return found, common.ReconcileRequeueAfter(1)
		}

		time.Sleep(deleteRetryInterval)
	}

	newSTS, res := r.createMissingRack(ctx, rackState.Rack)
	if !res.IsSuccess {
		return found, res
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "StatefulSetVolumeClaimTemplatesUpdated",
		"[rack-%d] Recreated StatefulSet %s with the expanded volume sizes",
		rackState.Rack.ID, utils.GetNamespacedNameString(newSTS),
	)

	return newSTS, common.ReconcileSuccess()
}
