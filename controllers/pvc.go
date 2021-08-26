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

func (r *AerospikeClusterReconciler) removePVCs(aeroCluster *asdbv1beta1.AerospikeCluster, storage *asdbv1beta1.AerospikeStorageSpec, pvcItems []corev1.PersistentVolumeClaim) error {
	deletedPVCs, err := r.removePVCsAsync(aeroCluster, storage, pvcItems)
	if err != nil {
		return err
	}

	return r.waitForPVCTermination(aeroCluster, deletedPVCs)
}

func (r *AerospikeClusterReconciler) removePVCsAsync(aeroCluster *asdbv1beta1.AerospikeCluster, storage *asdbv1beta1.AerospikeStorageSpec, pvcItems []corev1.PersistentVolumeClaim) ([]corev1.PersistentVolumeClaim, error) {
	// aeroClusterNamespacedName := getNamespacedNameForCluster(aeroCluster)

	deletedPVCs := []corev1.PersistentVolumeClaim{}

	for _, pvc := range pvcItems {
		if utils.IsPVCTerminating(&pvc) {
			continue
		}
		// Should we wait for delete?
		// Can we do it async in scaleDown

		// Check for path in pvc annotations. We put path annotation while creating statefulset
		path, ok := pvc.Annotations[storagePathAnnotationKey]
		if !ok {
			err := fmt.Errorf("PVC can not be removed, it does not have storage-path annotation")
			r.Log.Error(err, "Failed to remove PVC", "PVC", pvc.Name, "annotations", pvc.Annotations)
			continue
		}

		var cascadeDelete bool
		v := getPVCVolumeConfig(storage, path)
		if v == nil {
			if *pvc.Spec.VolumeMode == corev1.PersistentVolumeBlock {
				cascadeDelete = storage.BlockVolumePolicy.CascadeDelete
			} else {
				cascadeDelete = storage.FileSystemVolumePolicy.CascadeDelete
			}
			r.Log.Info("PVC path not found in configured storage volumes. Use storage level cascadeDelete policy", "PVC", pvc.Name, "path", path, "cascadeDelete", cascadeDelete)

		} else {
			cascadeDelete = v.CascadeDelete
		}

		if cascadeDelete {
			deletedPVCs = append(deletedPVCs, pvc)
			if err := r.Client.Delete(context.TODO(), &pvc); err != nil {
				return nil, fmt.Errorf("could not delete pvc %s: %v", pvc.Name, err)
			}
			r.Log.Info("PVC removed", "PVC", pvc.Name, "PVCCascadeDelete", cascadeDelete)
		} else {
			r.Log.Info("PVC not removed", "PVC", pvc.Name, "PVCCascadeDelete", cascadeDelete)
		}
	}

	return deletedPVCs, nil
}

func (r *AerospikeClusterReconciler) waitForPVCTermination(aeroCluster *asdbv1beta1.AerospikeCluster, deletedPVCs []corev1.PersistentVolumeClaim) error {
	if len(deletedPVCs) == 0 {
		return nil
	}

	// aeroClusterNamespacedName := getNamespacedNameForCluster(aeroCluster)

	// Wait for the PVCs to actually be deleted.
	pollAttempts := 15
	sleepInterval := time.Second * 20

	pending := false
	for i := 0; i < pollAttempts; i++ {
		pending = false
		existingPVCs, err := r.getClusterPVCList(aeroCluster)
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

func (r *AerospikeClusterReconciler) getClusterPVCList(aeroCluster *asdbv1beta1.AerospikeCluster) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.Client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func (r *AerospikeClusterReconciler) getRackPVCList(aeroCluster *asdbv1beta1.AerospikeCluster, rackID int) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackID))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.Client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func getPVCVolumeConfig(storage *asdbv1beta1.AerospikeStorageSpec, pvcPathAnnotation string) *asdbv1beta1.VolumeSpec {
	volumes := storage.Volumes
	for _, v := range volumes {
		if pvcPathAnnotation == v.Name {
			return &v
		}
	}
	return nil
}
