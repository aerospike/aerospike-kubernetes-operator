package cluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

func (r *SingleClusterReconciler) removePVCs(
	ctx context.Context,
	storage *asdbv1.AerospikeStorageSpec,
	pvcItems []corev1.PersistentVolumeClaim,
) error {
	deletedPVCs, err := r.removePVCsAsync(ctx, storage, pvcItems)
	if err != nil {
		return err
	}

	return r.waitForPVCTermination(ctx, deletedPVCs)
}

func (r *SingleClusterReconciler) removePVCsAsync(
	ctx context.Context,
	storage *asdbv1.AerospikeStorageSpec,
	pvcItems []corev1.PersistentVolumeClaim,
) ([]corev1.PersistentVolumeClaim, error) {
	var deletedPVCs []corev1.PersistentVolumeClaim

	for idx := range pvcItems {
		pvc := pvcItems[idx]
		if utils.IsPVCTerminating(&pvc) {
			continue
		}
		// Should we wait for delete?
		// Can we do it async in scaleDown

		// Check for path in pvc annotations. We put path annotation while creating statefulset
		pvcStorageVolName, ok := pvc.Annotations[storageVolumeAnnotationKey]
		if !ok {
			// Try legacy annotation name.
			pvcStorageVolName, ok = pvc.Annotations[storageVolumeLegacyAnnotationKey]
		}

		if !ok {
			err := fmt.Errorf(
				"PVC can not be removed, " +
					"it does not have storage-volume annotation",
			)
			r.Log.Error(
				err, "Failed to remove PVC",
				"persistentVolumeClaim", klog.KObj(&pvc),
				"annotations", pvc.Annotations,
			)

			continue
		}

		var cascadeDelete bool

		v := getPVCVolumeConfig(storage, pvcStorageVolName)
		if v == nil {
			if *pvc.Spec.VolumeMode == corev1.PersistentVolumeBlock {
				cascadeDelete = storage.BlockVolumePolicy.CascadeDelete
			} else {
				cascadeDelete = storage.FileSystemVolumePolicy.CascadeDelete
			}

			r.Log.Info(
				"PVC's volume not found in configured storage volumes. "+
					"Use storage level cascadeDelete policy",
				"persistentVolumeClaim", klog.KObj(&pvc),
				"volume", pvcStorageVolName, "cascadeDelete", cascadeDelete,
			)
		} else {
			cascadeDelete = v.CascadeDelete
		}

		if cascadeDelete {
			deletedPVCs = append(deletedPVCs, pvc)

			if err := r.Delete(ctx, &pvc); err != nil {
				return nil, fmt.Errorf(
					"could not delete pvc %s: %w", utils.NamespacedName(pvc.Namespace, pvc.Name), err,
				)
			}

			r.Log.Info(
				"PVC removed",
				"persistentVolumeClaim", klog.KObj(&pvc),
				"pvcCascadeDelete", cascadeDelete,
			)
		} else {
			r.Log.Info(
				"PVC not removed",
				"persistentVolumeClaim", klog.KObj(&pvc),
				"pvcCascadeDelete", cascadeDelete,
			)
		}
	}

	return deletedPVCs, nil
}

// deleteLocalPVCs deletes PVCs which are created using local storage classes
// It considers the user given LocalStorageClasses list from spec to determine if a PVC is local or not.
func (r *SingleClusterReconciler) deleteLocalPVCs(ctx context.Context, rackState *RackState, pod *corev1.Pod) error {
	pvcItems, err := r.getPodsPVCList(ctx, []string{pod.Name}, rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return fmt.Errorf("could not find pvc for pod %s: %w", utils.GetNamespacedNameString(pod), err)
	}

	for idx := range pvcItems {
		pvcStorageClass := pvcItems[idx].Spec.StorageClassName
		if pvcStorageClass == nil {
			r.Log.Info(
				"PVC does not have storageClass set, no need to delete PVC",
				"persistentVolumeClaim", klog.KObj(&pvcItems[idx]),
			)

			continue
		}

		if utils.ContainsString(rackState.Rack.Storage.LocalStorageClasses, *pvcStorageClass) {
			if err := r.Delete(ctx, &pvcItems[idx]); err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf(
						"could not delete pvc %s: %w",
						utils.NamespacedName(pvcItems[idx].Namespace, pvcItems[idx].Name), err,
					)
				}

				r.Log.Info(
					"PVC not found, may have been already deleted",
					"persistentVolumeClaim", klog.KObj(&pvcItems[idx]),
				)
			} else {
				r.Log.Info(
					"Successfully deleted local PVC",
					"persistentVolumeClaim", klog.KObj(&pvcItems[idx]),
					"storageClass", *pvcStorageClass,
				)
			}
		}
	}

	return nil
}

func (r *SingleClusterReconciler) waitForPVCTermination(
	ctx context.Context, deletedPVCs []corev1.PersistentVolumeClaim) error {
	if len(deletedPVCs) == 0 {
		return nil
	}

	// Wait for the PVCs to actually be deleted.
	pollAttempts := 15
	sleepInterval := time.Second * 20

	pending := false
	for i := 0; i < pollAttempts; i++ {
		pending = false

		existingPVCs, err := r.getClusterPVCList(ctx)
		if err != nil {
			return err
		}

		for deletedIdx := range deletedPVCs {
			pvc := deletedPVCs[deletedIdx]
			found := false

			for existingIdx := range existingPVCs {
				if existingPVCs[existingIdx].Name == pvc.Name {
					r.Log.Info(
						"Waiting for PVC termination",
						"persistentVolumeClaim", klog.KObj(&pvc),
					)

					found = true

					break
				}
			}

			if found {
				pending = true
				break
			}
		}

		if !pending {
			// All to-delete PVCs are deleted.
			break
		}

		// Wait for some more time.
		time.Sleep(sleepInterval)
	}

	if pending {
		pvcNames := make([]string, 0, len(deletedPVCs))
		for idx := range deletedPVCs {
			pvcNames = append(pvcNames, utils.GetNamespacedNameString(&deletedPVCs[idx]))
		}

		return fmt.Errorf("pvc termination timed out for pvcs %s", strings.Join(pvcNames, ", "))
	}

	return nil
}

func (r *SingleClusterReconciler) getClusterPVCList(ctx context.Context) (
	[]corev1.PersistentVolumeClaim, error,
) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(r.aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := r.List(ctx, pvcList, listOps); err != nil {
		return nil, err
	}

	return pvcList.Items, nil
}

func (r *SingleClusterReconciler) getRackPVCList(ctx context.Context, rackID int, rackRevision string) (
	[]corev1.PersistentVolumeClaim, error,
) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	listOps := &client.ListOptions{
		Namespace:     r.aeroCluster.Namespace,
		LabelSelector: utils.GetAerospikeClusterRackLabelSelector(r.aeroCluster.Name, rackID, rackRevision),
	}

	if err := r.List(ctx, pvcList, listOps); err != nil {
		return nil, err
	}

	return pvcList.Items, nil
}

func getPVCVolumeConfig(
	storage *asdbv1.AerospikeStorageSpec, pvcStorageVolName string,
) *asdbv1.VolumeSpec {
	volumes := storage.Volumes
	for idx := range volumes {
		v := &volumes[idx]
		if pvcStorageVolName == v.Name {
			return v
		}
	}

	return nil
}
