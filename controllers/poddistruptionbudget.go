package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

func (r *SingleClusterReconciler) reconcilePDB() error {
	// If spec.DisablePDB is set to true, then we don't need to create PDB
	// If it exist then delete it
	if asdbv1.GetBool(r.aeroCluster.Spec.DisablePDB) {
		if !asdbv1.GetBool(r.aeroCluster.Status.DisablePDB) {
			r.Log.Info("PodDisruptionBudget is disabled. Deleting old PodDisruptionBudget")
			return r.deletePDB()
		}

		r.Log.Info("PodDisruptionBudget is disabled, skipping PodDisruptionBudget creation")

		return nil
	}

	// Create or update PodDisruptionBudget
	return r.createOrUpdatePDB()
}

func (r *SingleClusterReconciler) deletePDB() error {
	pdb := &v1.PodDisruptionBudget{}

	// Get the PodDisruptionBudget
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, pdb,
	); err != nil {
		if errors.IsNotFound(err) {
			// PodDisruptionBudget is already deleted
			return nil
		}

		return err
	}

	if !isPDBCreatedByOperator(pdb) {
		r.Log.Info("PodDisruptionBudget is not created/owned by operator. Skipping delete")
		return nil
	}

	// Delete the PodDisruptionBudget
	return r.Client.Delete(context.TODO(), pdb)
}

func (r *SingleClusterReconciler) createOrUpdatePDB() error {
	podList, err := r.getClusterPodList()
	if err != nil {
		return err
	}

	for podIdx := range podList.Items {
		pod := &podList.Items[podIdx]

		for containerIdx := range pod.Spec.Containers {
			if pod.Spec.Containers[containerIdx].Name != asdbv1.AerospikeServerContainerName {
				continue
			}

			if pod.Spec.Containers[containerIdx].ReadinessProbe == nil {
				r.Log.Info("Pod found without ReadinessProbe, skipping PodDisruptionBudget. Refer Aerospike "+
					"documentation for more details.", "name", pod.Name)
				return nil
			}
		}
	}

	ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)
	pdb := &v1.PodDisruptionBudget{}

	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, pdb,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Create PodDisruptionBudget")

		pdb.SetName(r.aeroCluster.Name)
		pdb.SetNamespace(r.aeroCluster.Namespace)
		pdb.SetLabels(ls)
		pdb.Spec.MaxUnavailable = r.aeroCluster.Spec.MaxUnavailable
		pdb.Spec.Selector = &metav1.LabelSelector{
			MatchLabels: ls,
		}

		// Set AerospikeCluster instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroCluster, pdb, r.Scheme,
		)
		if err != nil {
			return err
		}

		if err = r.Client.Create(
			context.TODO(), pdb, createOption,
		); err != nil {
			return fmt.Errorf(
				"failed to create PodDisruptionBudget: %v",
				err,
			)
		}

		r.Log.Info("Created new PodDisruptionBudget", "name",
			utils.NamespacedName(r.aeroCluster.Namespace, r.aeroCluster.Name))

		return nil
	}

	r.Log.Info(
		"PodDisruptionBudget already exist. Updating existing PodDisruptionBudget if required", "name",
		utils.NamespacedName(r.aeroCluster.Namespace, r.aeroCluster.Name),
	)

	// This will ensure that cluster is not deployed with PDB created by user
	// cluster deploy call itself will fail.
	// If PDB is not created by operator then no need to even match the spec
	if !isPDBCreatedByOperator(pdb) {
		r.Log.Info("PodDisruptionBudget is not created/owned by operator. Skipping update")

		return fmt.Errorf(
			"failed to update PodDisruptionBudget: %v",
			fmt.Errorf("PodDisruptionBudget is not created/owned by operator"),
		)
	}

	if pdb.Spec.MaxUnavailable.String() != r.aeroCluster.Spec.MaxUnavailable.String() {
		pdb.Spec.MaxUnavailable = r.aeroCluster.Spec.MaxUnavailable

		if err := r.Client.Update(
			context.TODO(), pdb, updateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to update PodDisruptionBudget: %v",
				err,
			)
		}

		r.Log.Info("Updated PodDisruptionBudget", "name",
			utils.NamespacedName(r.aeroCluster.Namespace, r.aeroCluster.Name))
	}

	return nil
}

func isPDBCreatedByOperator(pdb *v1.PodDisruptionBudget) bool {
	val, ok := pdb.GetLabels()[asdbv1.AerospikeAppLabel]
	if ok && val == asdbv1.AerospikeAppLabelValue {
		return true
	}

	return false
}
