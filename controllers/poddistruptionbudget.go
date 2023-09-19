package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

func (r *SingleClusterReconciler) createOrUpdatePDB() error {
	ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)
	pdb := &v1.PodDisruptionBudget{}

	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, pdb,
	)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Create PodDisruptionBudget")

		pdb = &v1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.aeroCluster.Name,
				Namespace: r.aeroCluster.Namespace,
				Labels:    ls,
			},
			Spec: v1.PodDisruptionBudgetSpec{
				MaxUnavailable: r.aeroCluster.Spec.MaxUnavailable,
				Selector: &metav1.LabelSelector{
					MatchLabels: ls,
				},
			},
		}

		// This will be true only for old existing CRs. For new operator versions, this field will be
		// set by default to 1 by mutating webhook.
		if pdb.Spec.MaxUnavailable == nil {
			maxUnavailable := intstr.FromInt(1)
			pdb.Spec.MaxUnavailable = &maxUnavailable
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

	if pdb.Spec.MaxUnavailable.String() != r.aeroCluster.Spec.MaxUnavailable.String() {
		pdb.Spec.MaxUnavailable = r.aeroCluster.Spec.MaxUnavailable
		if err = r.Client.Update(
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
