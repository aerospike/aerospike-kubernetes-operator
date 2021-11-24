package controllers

import (
	"context"
	"fmt"
	"time"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *SingleClusterReconciler) removePVCs(
	storage *asdbv1beta1.AerospikeStorageSpec,
	pvcItems []corev1.PersistentVolumeClaim,
) error {
	deletedPVCs, err := r.removePVCsAsync(storage, pvcItems)
	if err != nil {
		return err
	}

	return r.waitForPVCTermination(deletedPVCs)
}

func (r *SingleClusterReconciler) removePVCsAsync(
	storage *asdbv1beta1.AerospikeStorageSpec,
	pvcItems []corev1.PersistentVolumeClaim,
) ([]corev1.PersistentVolumeClaim, error) {
	// aeroClusterNamespacedName := getNamespacedNameForCluster(r.aeroCluster)

	var deletedPVCs []corev1.PersistentVolumeClaim

	for _, pvc := range pvcItems {
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
				err, "Failed to remove PVC", "PVC", pvc.Name, "annotations",
				pvc.Annotations,
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
				"PVC", pvc.Name, "volume", pvcStorageVolName, "cascadeDelete",
				cascadeDelete,
			)

		} else {
			cascadeDelete = v.CascadeDelete
		}

		if cascadeDelete {
			deletedPVCs = append(deletedPVCs, pvc)
			if err := r.Client.Delete(context.TODO(), &pvc); err != nil {
				return nil, fmt.Errorf(
					"could not delete pvc %s: %v", pvc.Name, err,
				)
			}
			r.Log.Info(
				"PVC removed", "PVC", pvc.Name, "PVCCascadeDelete",
				cascadeDelete,
			)
		} else {
			r.Log.Info(
				"PVC not removed", "PVC", pvc.Name, "PVCCascadeDelete",
				cascadeDelete,
			)
		}
	}

	return deletedPVCs, nil
}

func (r *SingleClusterReconciler) waitForPVCTermination(deletedPVCs []corev1.PersistentVolumeClaim) error {
	if len(deletedPVCs) == 0 {
		return nil
	}

	// aeroClusterNamespacedName := getNamespacedNameForCluster(r.aeroCluster)

	// Wait for the PVCs to actually be deleted.
	pollAttempts := 15
	sleepInterval := time.Second * 20

	pending := false
	for i := 0; i < pollAttempts; i++ {
		pending = false
		existingPVCs, err := r.getClusterPVCList()
		if err != nil {
			return err
		}

		for _, pvc := range deletedPVCs {
			found := false
			for _, existing := range existingPVCs {
				if existing.Name == pvc.Name {
					r.Log.Info("Waiting for PVC termination", "PVC", pvc.Name)
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
		return fmt.Errorf("PVC termination timed out PVC: %v", deletedPVCs)
	}

	return nil
}

func (r *SingleClusterReconciler) getClusterPVCList() (
	[]corev1.PersistentVolumeClaim, error,
) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(r.aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := r.Client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func (r *SingleClusterReconciler) getRackPVCList(rackID int) (
	[]corev1.PersistentVolumeClaim, error,
) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(
		utils.LabelsForAerospikeClusterRack(
			r.aeroCluster.Name, rackID,
		),
	)
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := r.Client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func getPVCVolumeConfig(
	storage *asdbv1beta1.AerospikeStorageSpec, pvcStorageVolName string,
) *asdbv1beta1.VolumeSpec {
	volumes := storage.Volumes
	for _, v := range volumes {
		if pvcStorageVolName == v.Name {
			return &v
		}
	}
	return nil
}
