package controllers

import (
	"context"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

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
				r.Log.Info("Pod found without ReadinessProbe, skipping PodDisruptionBudget",
					"name", pod.Name)
				return nil
			}
		}
	}

	ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)
	// TODO: Move to concrete object when minimum supported k8s version is 1.21
	pdb := &unstructured.Unstructured{}
	pdb.SetGroupVersionKind(PDBbGvk)

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
		pdb.Object["spec"] = map[string]interface{}{
			"maxUnavailable": r.aeroCluster.Spec.MaxUnavailable,
			"selector": &metav1.LabelSelector{
				MatchLabels: ls,
			},
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

		r.Log.Info("Created new PodDisruptionBudget")

		return nil
	}

	r.Log.Info(
		"PodDisruptionBudget already exist. Updating existing PodDisruptionBudget if required", "name",
		utils.NamespacedName(r.aeroCluster.Namespace, r.aeroCluster.Name),
	)

	var value string

	maxUnavailable := pdb.Object["spec"].(map[string]interface{})["maxUnavailable"]

	// Type casting is required because of unstructured object
	if val, ok := maxUnavailable.(string); ok {
		value = val
	} else {
		value = strconv.Itoa(int(maxUnavailable.(int64)))
	}

	if value != r.aeroCluster.Spec.MaxUnavailable.String() {
		spec := pdb.Object["spec"].(map[string]interface{})
		spec["maxUnavailable"] = r.aeroCluster.Spec.MaxUnavailable
		pdb.Object["spec"] = spec

		if err := r.Client.Update(
			context.TODO(), pdb, updateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to update PodDisruptionBudget: %v",
				err,
			)
		}

		r.Log.Info("Updated PodDisruptionBudget")
	}

	return nil
}
