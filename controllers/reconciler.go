package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	as "github.com/ashishshinde/aerospike-client-go/v5"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
)

// SingleClusterReconciler reconciles a single AerospikeCluster
type SingleClusterReconciler struct {
	aeroCluster *asdbv1beta1.AerospikeCluster
	client.Client
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Log        logr.Logger
	Scheme     *k8sRuntime.Scheme
}

func (r *SingleClusterReconciler) Reconcile() (ctrl.Result, error) {
	r.Log.V(1).Info(
		"AerospikeCluster", "Spec", r.aeroCluster.Spec, "Status",
		r.aeroCluster.Status,
	)
	// Check DeletionTimestamp to see if cluster is being deleted
	if !r.aeroCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.V(1).Info("Deleting AerospikeCluster")
		// The cluster is being deleted
		if err := r.handleClusterDeletion(finalizerName); err != nil {
			return reconcile.Result{}, err
		}
		// Stop reconciliation as the cluster is being deleted
		return reconcile.Result{}, nil
	}

	// The cluster is not being deleted, add finalizer in not added already
	if err := r.addFinalizer(finalizerName); err != nil {
		r.Log.Error(err, "Failed to add finalizer")
		return reconcile.Result{}, err
	}

	// Handle previously failed cluster
	if err := r.handlePreviouslyFailedCluster(); err != nil {
		return reconcile.Result{}, err
	}

	// Reconcile all racks
	if res := r.reconcileRacks(); !res.isSuccess {
		return res.result, res.err
	}

	if err := r.createSTSLoadBalancerSvc(); err != nil {
		r.Log.Error(err, "Failed to create LoadBalancer service")
		return reconcile.Result{}, err
	}

	// Check if there is any node with quiesce status. We need to undo that
	// It may have been left from previous steps
	allHostConns, err := r.newAllHostConn()
	if err != nil {
		e := fmt.Errorf(
			"failed to get hostConn for aerospike cluster nodes: %v", err,
		)
		r.Log.Error(err, "Failed to get hostConn for aerospike cluster nodes")
		return reconcile.Result{}, e
	}
	if err := deployment.InfoQuiesceUndo(
		r.getClientPolicy(), allHostConns,
	); err != nil {
		r.Log.Error(err, "Failed to check for Quiesced nodes")
		return reconcile.Result{}, err
	}

	// Setup access control.
	if err := r.reconcileAccessControl(); err != nil {
		r.Log.Error(err, "Failed to Reconcile access control")
		return reconcile.Result{}, err
	}

	// Update the AerospikeCluster status.
	if err := r.updateStatus(); err != nil {
		r.Log.Error(err, "Failed to update AerospikeCluster status")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *SingleClusterReconciler) reconcileAccessControl() error {

	enabled, err := asdbv1beta1.IsSecurityEnabled(r.aeroCluster.Spec.AerospikeConfig)
	if err != nil {
		return fmt.Errorf("failed to get cluster security status: %v", err)
	}
	if !enabled {
		r.Log.Info("Cluster is not security enabled, please enable security for this cluster.")
		return nil
	}

	// Create client
	conns, err := r.newAllHostConn()
	if err != nil {
		return fmt.Errorf("failed to get host info: %v", err)
	}
	var hosts []*as.Host
	for _, conn := range conns {
		hosts = append(
			hosts, &as.Host{
				Name:    conn.ASConn.AerospikeHostName,
				TLSName: conn.ASConn.AerospikeTLSName,
				Port:    conn.ASConn.AerospikePort,
			},
		)
	}
	// Create policy using status, status has current connection info
	clientPolicy := r.getClientPolicy()
	aeroClient, err := as.NewClientWithPolicyAndHost(clientPolicy, hosts...)

	if err != nil {
		return fmt.Errorf("failed to create aerospike cluster client: %v", err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider()

	err = ReconcileAccessControl(&r.aeroCluster.Spec, aeroClient, pp, r.Log)
	return err
}

func (r *SingleClusterReconciler) updateStatus() error {

	r.Log.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	)
	if err != nil {
		return err
	}

	// TODO: FIXME: Copy only required fields, StatusSpec may not have all the fields in Spec.
	// Deepcopy at that location may create problem
	// Deep copy merges so blank out the spec part of status before copying over.
	// newAeroCluster.Status.AerospikeClusterStatusSpec = asdbv1beta1.AerospikeClusterStatusSpec{}
	// if err := lib.DeepCopy(&newAeroCluster.Status.AerospikeClusterStatusSpec, &aeroCluster.Spec); err != nil {
	// 	return err
	// }

	specToStatus, err := asdbv1beta1.CopySpecToStatus(r.aeroCluster.Spec)
	if err != nil {
		return err
	}
	newAeroCluster.Status.AerospikeClusterStatusSpec = *specToStatus

	err = r.patchStatus(newAeroCluster)
	if err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}
	r.aeroCluster = newAeroCluster

	r.Log.Info("Updated status", "status", newAeroCluster.Status)
	return nil
}

func (r *SingleClusterReconciler) createStatus() error {

	r.Log.Info("Creating status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	)
	if err != nil {
		return err
	}

	if newAeroCluster.Status.Pods == nil {
		newAeroCluster.Status.Pods = map[string]asdbv1beta1.AerospikePodStatus{}
	}

	if err = r.Client.Status().Update(
		context.TODO(), newAeroCluster,
	); err != nil {
		return fmt.Errorf("error creating status: %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) isNewCluster() (bool, error) {
	if r.aeroCluster.Status.AerospikeConfig != nil {
		// We have valid status, cluster cannot be new.
		return false, nil
	}

	statefulSetList, err := r.getClusterSTSList()

	if err != nil {
		return false, err
	}

	// Cluster can have status nil and still have pods on failures.
	// For cluster to be new there should be no pods in the cluster.
	return len(statefulSetList.Items) == 0, nil
}

func (r *SingleClusterReconciler) hasClusterFailed() (bool, error) {
	isNew, err := r.isNewCluster()

	if err != nil {
		// Checking cluster status failed.
		return false, err
	}

	return !isNew && r.aeroCluster.Status.AerospikeConfig == nil, nil
}

func (r *SingleClusterReconciler) patchStatus(newAeroCluster *asdbv1beta1.AerospikeCluster) error {
	oldAeroCluster := r.aeroCluster
	oldJSON, err := json.Marshal(oldAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling old status: %v", err)
	}

	newJSON, err := json.Marshal(newAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling new status: %v", err)
	}

	jsonPatchPatch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("error creating json patch: %v", err)
	}

	// Pick changes to the status object only.
	var filteredPatch []jsonpatch.JsonPatchOperation
	for _, operation := range jsonPatchPatch {
		// pods should never be updated here
		// pods is updated only from 2 places
		// 1: While pod init, it will add pod in pods
		// 2: While pod cleanup, it will remove pod from pods
		if strings.HasPrefix(
			operation.Path, "/status",
		) && !strings.HasPrefix(operation.Path, "/status/pods") {
			filteredPatch = append(filteredPatch, operation)
		}
	}

	if len(filteredPatch) == 0 {
		r.Log.Info("No status change required")
		return nil
	}
	r.Log.V(1).Info(
		"Filtered status patch ", "patch", filteredPatch, "oldObj.status",
		oldAeroCluster.Status, "newObj.status", newAeroCluster.Status,
	)

	jsonPatchJSON, err := json.Marshal(filteredPatch)

	if err != nil {
		return fmt.Errorf("error marshalling json patch: %v", err)
	}

	patch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

	if err = r.Client.Status().Patch(
		context.TODO(), oldAeroCluster, patch,
		client.FieldOwner(patchFieldOwner),
	); err != nil {
		return fmt.Errorf("error patching status: %v", err)
	}

	// FIXME: Json unmarshal used by above client.Status(),Patch()  does not convert empty lists in the new JSON to empty lists in the target. Seems like a bug in encoding/json/Unmarshall.
	//
	// Workaround by force copying new object's status to old object's status.
	return lib.DeepCopy(&oldAeroCluster.Status, &newAeroCluster.Status)
}

// recoverFailedCreate deletes the stateful sets for every rack and retries creating the cluster again when the first cluster create has failed.
//
// The cluster is not new but maybe unreachable or down. There could be an Aerospike configuration
// error that passed the operator validation but is invalid on the server. This will happen for
// example where deeper paramter or value of combination of parameter values need validation which
// is missed by the operator. For e.g. node-address-port values in xdr datacenter section needs better
// validation for ip and port.
//
// Such cases warrant a cluster recreate to recover after the user corrects the configuration.
func (r *SingleClusterReconciler) recoverFailedCreate() error {

	r.Log.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration.")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterSTSList()
	if err != nil {
		return fmt.Errorf(
			"error getting statefulsets while forcing recreate of the cluster as status is nil: %v",
			err,
		)
	}

	r.Log.V(1).Info(
		"Found statefulset for cluster. Need to delete them", "nSTS",
		len(statefulSetList.Items),
	)
	for _, statefulset := range statefulSetList.Items {
		if err := r.deleteSTS(&statefulset); err != nil {
			return fmt.Errorf(
				"error deleting statefulset while forcing recreate of the cluster as status is nil: %v",
				err,
			)
		}
	}

	// Clear pod status as well in status since we want to be re-initializing or cascade deleting devices if any.
	// This is not necessary since scale-up would clean dangling pod status. However, done here for general
	// cleanliness.
	rackStateList := getConfiguredRackStateList(r.aeroCluster)
	for _, state := range rackStateList {
		pods, err := r.getRackPodList(state.Rack.ID)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %v", err)
		}

		newPodNames := make([]string, 0)
		for i := 0; i < len(pods.Items); i++ {
			newPodNames = append(newPodNames, pods.Items[i].Name)
		}

		err = r.cleanupPods(newPodNames, state)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %v", err)
		}
	}

	return fmt.Errorf("forcing recreate of the cluster as status is nil")
}

func (r *SingleClusterReconciler) addFinalizer(finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(
		r.aeroCluster.ObjectMeta.Finalizers, finalizerName,
	) {
		r.aeroCluster.ObjectMeta.Finalizers = append(
			r.aeroCluster.ObjectMeta.Finalizers, finalizerName,
		)
		if err := r.Client.Update(context.TODO(), r.aeroCluster); err != nil {
			return err
		}
	}
	return nil
}

func (r *SingleClusterReconciler) cleanUpAndRemoveFinalizer(finalizerName string) error {
	// The object is being deleted
	if utils.ContainsString(
		r.aeroCluster.ObjectMeta.Finalizers, finalizerName,
	) {
		// Handle any external dependency
		if err := r.deleteExternalResources(); err != nil {
			// If fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// Remove finalizer from the list
		r.aeroCluster.ObjectMeta.Finalizers = utils.RemoveString(
			r.aeroCluster.ObjectMeta.Finalizers, finalizerName,
		)
		if err := r.Client.Update(context.TODO(), r.aeroCluster); err != nil {
			return err
		}
	}

	// Stop reconciliation as the item is being deleted
	return nil
}

func (r *SingleClusterReconciler) deleteExternalResources() error {
	// Delete should be idempotent

	r.Log.Info("Removing pvc for removed cluster")

	// Delete pvc for all rack storage
	for _, rack := range r.aeroCluster.Spec.RackConfig.Racks {
		rackPVCItems, err := r.getRackPVCList(rack.ID)
		if err != nil {
			return fmt.Errorf("could not find pvc for rack: %v", err)
		}
		storage := rack.Storage

		if _, err := r.removePVCsAsync(&storage, rackPVCItems); err != nil {
			return fmt.Errorf("failed to remove cluster PVCs: %v", err)
		}
	}

	// Delete PVCs for any remaining old removed racks
	pvcItems, err := r.getClusterPVCList()
	if err != nil {
		return fmt.Errorf("could not find pvc for cluster: %v", err)
	}

	// removePVCs should be passed only filtered pvc otherwise rack pvc may be removed using global storage cascadeDelete
	var filteredPVCItems []corev1.PersistentVolumeClaim
	for _, pvc := range pvcItems {
		var found bool
		for _, rack := range r.aeroCluster.Spec.RackConfig.Racks {
			rackLabels := utils.LabelsForAerospikeClusterRack(
				r.aeroCluster.Name, rack.ID,
			)
			if reflect.DeepEqual(pvc.Labels, rackLabels) {
				found = true
				break
			}
		}
		if !found {
			filteredPVCItems = append(filteredPVCItems, pvc)
		}
	}

	// Delete pvc for commmon storage.
	if _, err := r.removePVCsAsync(
		&r.aeroCluster.Spec.Storage, filteredPVCItems,
	); err != nil {
		return fmt.Errorf("failed to remove cluster PVCs: %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) isResourceUpdatedInAeroCluster(pod corev1.Pod) bool {
	res := r.aeroCluster.Spec.Resources
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}

	if !isClusterResourceListEqual(
		pod.Spec.Containers[0].Resources.Requests, res.Requests,
	) ||
		!isClusterResourceListEqual(
			pod.Spec.Containers[0].Resources.Limits, res.Limits,
		) {
		return true
	}
	return false
}

func isClusterResourceListEqual(res1, res2 corev1.ResourceList) bool {
	if len(res1) != len(res2) {
		return false
	}
	for k := range res1 {
		if v2, ok := res2[k]; !ok || !res1[k].Equal(v2) {
			return false
		}
	}
	return true
}

func (r *SingleClusterReconciler) handleClusterDeletion(finalizerName string) error {

	r.Log.Info("Handle cluster deletion")

	// The cluster is being deleted
	if err := r.cleanUpAndRemoveFinalizer(finalizerName); err != nil {
		r.Log.Error(err, "Failed to remove finalizer")
		return err
	}
	return nil
}

func (r *SingleClusterReconciler) handlePreviouslyFailedCluster() error {

	r.Log.Info("Handle previously failed cluster")

	isNew, err := r.isNewCluster()
	if err != nil {
		return fmt.Errorf("error determining if cluster is new: %v", err)
	}

	if isNew {
		r.Log.V(1).Info("It's new cluster, create empty status object")
		if err := r.createStatus(); err != nil {
			return err
		}
	} else {
		r.Log.V(1).Info("It's not a new cluster, check if it is failed and needs recovery")
		hasFailed, err := r.hasClusterFailed()
		if err != nil {
			return fmt.Errorf(
				"error determining if cluster has failed: %v", err,
			)
		}

		if hasFailed {
			return r.recoverFailedCreate()
		}
	}
	return nil
}
