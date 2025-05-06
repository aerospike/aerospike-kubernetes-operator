package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	as "github.com/aerospike/aerospike-client-go/v7"
	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
)

// SingleClusterReconciler reconciles a single AerospikeCluster
type SingleClusterReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	aeroCluster *asdbv1.AerospikeCluster
	KubeClient  *kubernetes.Clientset
	KubeConfig  *rest.Config
	Scheme      *k8sRuntime.Scheme
	Log         logr.Logger
}

func (r *SingleClusterReconciler) Reconcile() (result ctrl.Result, recErr error) {
	r.Log.V(1).Info(
		"AerospikeCluster", "Spec", r.aeroCluster.Spec, "Status",
		r.aeroCluster.Status,
	)

	// Set the status phase to Error if the recErr is not nil
	// recErr is only set when reconcile failure should result in Error phase of the cluster
	defer func() {
		if recErr != nil {
			r.Log.Error(recErr, "Reconcile failed")

			if err := r.setStatusPhase(asdbv1.AerospikeClusterError); err != nil {
				recErr = err
			}
		}
	}()

	// Check DeletionTimestamp to see if the cluster is being deleted
	if !r.aeroCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.V(1).Info("Deleting AerospikeCluster")
		// The cluster is being deleted
		if err := r.handleClusterDeletion(finalizerName); err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, "DeleteFailed",
				"Unable to handle AerospikeCluster delete operations %s/%s",
				r.aeroCluster.Namespace, r.aeroCluster.Name,
			)

			return reconcile.Result{}, err
		}

		r.removeClusterPhaseMetric()

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, "Deleted",
			"Deleted AerospikeCluster %s/%s", r.aeroCluster.Namespace,
			r.aeroCluster.Name,
		)

		// Stop reconciliation as the cluster is being deleted
		return reconcile.Result{}, nil
	}

	// Pause the reconciliation for the AerospikeCluster if the paused field is set to true.
	// Deletion of the AerospikeCluster will not be paused.
	if asdbv1.GetBool(r.aeroCluster.Spec.Paused) {
		r.Log.Info("Reconciliation is paused for this AerospikeCluster")
		return reconcile.Result{}, nil
	}

	// Set the status to AerospikeClusterInProgress before starting any operations
	if err := r.setStatusPhase(asdbv1.AerospikeClusterInProgress); err != nil {
		return reconcile.Result{}, err
	}

	// The cluster is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(finalizerName); err != nil {
		r.Log.Error(err, "Failed to add finalizer")
		return reconcile.Result{}, err
	}

	// Handle previously failed cluster
	hasFailed, chkErr := r.checkPreviouslyFailedCluster()
	if chkErr != nil {
		return reconcile.Result{}, chkErr
	}

	if r.aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel] == asdbv1.AerospikeAPIVersion {
		r.Log.Info("cluster migration is not needed")
	} else {
		if err := r.migrateAerospikeCluster(context.TODO(), hasFailed); err != nil {
			return reconcile.Result{Requeue: true}, err
		}
	}

	if err := r.createOrUpdateSTSHeadlessSvc(); err != nil {
		r.Log.Error(err, "Failed to create headless service")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "ServiceCreateFailed",
			"Failed to create Service(Headless) %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Reconcile all racks
	if res := r.reconcileRacks(); !res.IsSuccess {
		if res.Err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to reconcile Racks for cluster %s/%s",
				r.aeroCluster.Namespace, r.aeroCluster.Name,
			)

			recErr = res.Err
		}

		return res.Result, recErr
	}

	if err := r.reconcilePDB(); err != nil {
		r.Log.Error(err, "Failed to reconcile PodDisruptionBudget")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "PodDisruptionBudgetReconcileFailed",
			"Failed to reconcile PodDisruptionBudget %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	if err := r.reconcileSTSLoadBalancerSvc(); err != nil {
		r.Log.Error(err, "Failed to create LoadBalancer service")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "ServiceCreateFailed",
			"Failed to create Service(LoadBalancer) %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	ignorablePodNames, err := r.getIgnorablePods(nil, getConfiguredRackStateList(r.aeroCluster))
	if err != nil {
		r.Log.Error(err, "Failed to determine pods to be ignored")

		return reconcile.Result{}, err
	}

	// Check if there is any node with quiesce status. We need to undo that
	// It may have been left from previous steps
	allHostConns, err := r.newAllHostConnWithOption(ignorablePodNames)
	if err != nil {
		e := fmt.Errorf(
			"failed to get hostConn for aerospike cluster nodes: %v", err,
		)

		r.Log.Error(err, "Failed to get hostConn for aerospike cluster nodes")

		return reconcile.Result{}, e
	}

	if err = deployment.InfoQuiesceUndo(
		r.Log,
		r.getClientPolicy(), allHostConns,
	); err != nil {
		r.Log.Error(err, "Failed to check for Quiesced nodes")

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Setup access control.
	// Assuming all pods must be security enabled or disabled.
	if err = r.validateAndReconcileAccessControl(nil, ignorablePodNames); err != nil {
		r.Log.Error(err, "Failed to Reconcile access control")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "ACLUpdateFailed",
			"Failed to setup Access Control %s/%s", r.aeroCluster.Namespace,
			r.aeroCluster.Name,
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Use policy from spec after setting up access control
	policy := r.getClientPolicy()

	// Revert migrate-fill-delay to original value if it was set to 0 during scale down.
	// Passing the first rack from the list as all the racks will have the same migrate-fill-delay
	// Redundant safe check to revert migrate-fill-delay if previous revert operation missed/skipped somehow
	if res := r.setMigrateFillDelay(
		policy, &r.aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig,
		false, ignorablePodNames,
	); !res.IsSuccess {
		r.Log.Error(res.Err, "Failed to revert migrate-fill-delay")

		recErr = res.Err

		return reconcile.Result{}, recErr
	}

	// Doing recluster before setting up roster to get the latest observed node list from server.
	if r.IsReclusterNeeded() {
		if err = deployment.InfoRecluster(
			r.Log,
			policy, allHostConns,
		); err != nil {
			r.Log.Error(err, "Failed to do recluster")
			return reconcile.Result{}, err
		}
	}

	if asdbv1.IsClusterSCEnabled(r.aeroCluster) {
		if !r.IsStatusEmpty() {
			if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
				recErr = res.Err

				return res.Result, recErr
			}
		}

		// Setup roster
		if err = r.getAndSetRoster(policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
			r.Log.Error(err, "Failed to set roster for cluster")
			recErr = err

			return reconcile.Result{}, recErr
		}
	}

	// Update the AerospikeCluster status.
	if err = r.updateStatus(); err != nil {
		r.Log.Error(err, "Failed to update AerospikeCluster status")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "StatusUpdateFailed",
			"Failed to update AerospikeCluster status %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		return reconcile.Result{}, err
	}

	// Try to recover pods only when MaxIgnorablePods is set
	if r.aeroCluster.Spec.RackConfig.MaxIgnorablePods != nil {
		if res := r.recoverIgnorablePods(); !res.IsSuccess {
			return res.GetResult()
		}
	}

	r.Log.Info("Reconcile completed successfully")

	return reconcile.Result{}, nil
}

func (r *SingleClusterReconciler) recoverIgnorablePods() common.ReconcileResult {
	podList, gErr := r.getClusterPodList()
	if gErr != nil {
		r.Log.Error(gErr, "Failed to get cluster pod list")
		return common.ReconcileError(gErr)
	}

	r.Log.Info("Try to recover failed/pending pods if any")

	var anyPodFailed bool
	// Try to recover failed/pending pods by deleting them
	for idx := range podList.Items {
		if cErr := utils.CheckPodFailed(&podList.Items[idx]); cErr != nil {
			anyPodFailed = true

			if err := r.createOrUpdatePodServiceIfNeeded([]string{podList.Items[idx].Name}); err != nil {
				return common.ReconcileError(err)
			}

			if err := r.Client.Delete(context.TODO(), &podList.Items[idx]); err != nil {
				r.Log.Error(err, "Failed to delete pod", "pod", podList.Items[idx].Name)
				return common.ReconcileError(err)
			}

			r.Log.Info("Deleted pod", "pod", podList.Items[idx].Name)
		}
	}

	if anyPodFailed {
		r.Log.Info("Found failed/pending pod(s), requeuing")
		return common.ReconcileRequeueAfter(0)
	}

	return common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) validateAndReconcileAccessControl(
	selectedPods []corev1.Pod,
	ignorablePodNames sets.Set[string],
) error {
	enabled, err := asdbv1.IsSecurityEnabled(r.aeroCluster.Spec.AerospikeConfig)
	if err != nil {
		return fmt.Errorf("failed to get cluster security status: %v", err)
	}

	if !enabled {
		r.Log.Info("Cluster is not security enabled, please enable security for this cluster.")
		return nil
	}

	var conns []*deployment.HostConn

	// Create client
	if selectedPods == nil {
		conns, err = r.newAllHostConnWithOption(ignorablePodNames)
		if err != nil {
			return fmt.Errorf("failed to get host info: %v", err)
		}
	} else {
		conns, err = r.newPodsHostConnWithOption(selectedPods, ignorablePodNames)
		if err != nil {
			return fmt.Errorf("failed to get host info: %v", err)
		}
	}

	hosts := make([]*as.Host, 0, len(conns))

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

	err = r.reconcileAccessControl(
		aeroClient, pp,
	)

	if err != nil {
		return fmt.Errorf("failed to reconcile access control: %v", err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "ACLUpdated",
		"Updated Access Control %s/%s", r.aeroCluster.Namespace,
		r.aeroCluster.Name,
	)

	// Update the AerospikeCluster status.
	if err := r.updateAccessControlStatus(); err != nil {
		r.Log.Error(err, "Failed to update AerospikeCluster access control status")
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "StatusUpdateFailed",
			"Failed to update AerospikeCluster access control status %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		return err
	}

	return nil
}

func (r *SingleClusterReconciler) updateStatus() error {
	r.Log.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return err
	}

	// TODO: FIXME: Copy only required fields, StatusSpec may not have all the fields in Spec.
	// DeepCopy at that location may create problem
	// Deep copy merges so blank out the spec part of status before copying over.
	// newAeroCluster.Status.AerospikeClusterStatusSpec = asdbv1.AerospikeClusterStatusSpec{}
	// if err := lib.DeepCopy(&newAeroCluster.Status.AerospikeClusterStatusSpec, &aeroCluster.Spec); err != nil {
	// 	return err
	// }

	specToStatus, err := asdbv1.CopySpecToStatus(&r.aeroCluster.Spec)
	if err != nil {
		return err
	}

	newAeroCluster.Status.AerospikeClusterStatusSpec = *specToStatus
	newAeroCluster.Status.Phase = asdbv1.AerospikeClusterCompleted

	// If IsReadinessProbeEnabled is not enabled, then only check for cluster readiness.
	// This is to avoid checking cluster readiness for every reconcile as once it is enabled, it will not be disabled.
	if !newAeroCluster.Status.IsReadinessProbeEnabled {
		clusterReadinessEnable, gErr := r.getClusterReadinessStatus()
		if gErr != nil {
			return fmt.Errorf("failed to get cluster readiness status: %v", gErr)
		}

		newAeroCluster.Status.IsReadinessProbeEnabled = clusterReadinessEnable
	}

	selector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(newAeroCluster.Name))
	newAeroCluster.Status.Selector = selector.String()

	err = r.patchStatus(newAeroCluster)
	if err != nil {
		return fmt.Errorf("error updating status: %w", err)
	}

	r.aeroCluster = newAeroCluster

	// Add the cluster phase metric
	r.addClusterPhaseMetric()

	r.Log.Info("Updated status", "status", newAeroCluster.Status)

	return nil
}

func (r *SingleClusterReconciler) setStatusPhase(phase asdbv1.AerospikeClusterPhase) error {
	if r.aeroCluster.Status.Phase != phase {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Client.Get(context.TODO(), utils.GetNamespacedName(r.aeroCluster), r.aeroCluster); err != nil {
				return err
			}

			r.aeroCluster.Status.Phase = phase

			return r.Client.Status().Update(context.Background(), r.aeroCluster)
		}); err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to set cluster status to %s", phase))
			return err
		}

		r.addClusterPhaseMetric()
	}

	return nil
}

func (r *SingleClusterReconciler) getClusterReadinessStatus() (bool, error) {
	podList, err := r.getClusterPodList()
	if err != nil {
		return false, err
	}

	for podIdx := range podList.Items {
		pod := &podList.Items[podIdx]

		for containerIdx := range pod.Spec.Containers {
			if pod.Spec.Containers[containerIdx].Name != asdbv1.AerospikeServerContainerName {
				continue
			}

			if pod.Spec.Containers[containerIdx].ReadinessProbe == nil {
				return false, nil
			}
		}
	}

	return true, nil
}

func (r *SingleClusterReconciler) updateAccessControlStatus() error {
	if r.aeroCluster.Spec.AerospikeAccessControl == nil {
		return nil
	}

	r.Log.Info("Update access control status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return err
	}

	var statusAerospikeAccessControl *asdbv1.AerospikeAccessControlSpec
	if r.aeroCluster.Spec.AerospikeAccessControl != nil {
		// AerospikeAccessControl
		statusAerospikeAccessControl = lib.DeepCopy(
			r.aeroCluster.Spec.AerospikeAccessControl,
		).(*asdbv1.AerospikeAccessControlSpec)
	}

	newAeroCluster.Status.AerospikeClusterStatusSpec.AerospikeAccessControl = statusAerospikeAccessControl

	if err := r.patchStatus(newAeroCluster); err != nil {
		return fmt.Errorf("error updating status: %w", err)
	}

	r.Log.Info("Updated access control status", "status", newAeroCluster.Status)

	return nil
}

func (r *SingleClusterReconciler) createStatus() error {
	r.Log.Info("Creating status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return err
	}

	if newAeroCluster.Status.Pods == nil {
		newAeroCluster.Status.Pods = map[string]asdbv1.AerospikePodStatus{}
	}

	if err := r.Client.Status().Update(
		context.TODO(), newAeroCluster,
	); err != nil {
		return fmt.Errorf("error creating status: %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) isNewCluster() (bool, error) {
	if !r.IsStatusEmpty() {
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

	if isNew {
		// New clusters should not be considered failed.
		return false, nil
	}

	// Check if there are any pods running
	pods, err := r.getClusterPodList()
	if err != nil {
		return false, err
	}

	for idx := range pods.Items {
		pod := &pods.Items[idx]
		if err := utils.CheckPodFailed(pod); err == nil {
			// There is at least one pod that has not yet failed.
			// It's possible that the containers are stuck doing a long disk
			// initialization.
			// Don't consider this cluster as failed and needing recovery
			// as long as there is at least one running pod.
			return false, nil
		}
	}

	return r.IsStatusEmpty(), nil
}

func (r *SingleClusterReconciler) patchStatus(newAeroCluster *asdbv1.AerospikeCluster) error {
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
	var filteredPatch []jsonpatch.PatchOperation

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

	// FIXME: Json unmarshal used by above client.Status(),
	//  Patch()  does not convert empty lists in the new Json to empty lists in the target.
	//  Seems like a bug in encoding/json/Unmarshall.
	//
	// Workaround by force copying new object's status to old object's status.
	aeroclusterStatus := lib.DeepCopy(&newAeroCluster.Status).(*asdbv1.AerospikeClusterStatus)
	oldAeroCluster.Status = *aeroclusterStatus

	return nil
}

// recoverFailedCreate deletes the stateful sets for every rack and retries creating the cluster again when the first
// cluster create has failed.
//
// The cluster is not new but maybe unreachable or down. There could be an Aerospike configuration
// error that passed the operator validation but is invalid on the server. This will happen for
// example where deeper parameter or value of combination of parameter values need validation which
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

	for idx := range statefulSetList.Items {
		statefulset := &statefulSetList.Items[idx]
		if err := r.deleteSTS(statefulset); err != nil {
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
	for rackIdx := range rackStateList {
		state := rackStateList[rackIdx]

		pods, err := r.getRackPodList(state.Rack.ID)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %v", err)
		}

		newPodNames := make([]string, 0)
		for podIdx := 0; podIdx < len(pods.Items); podIdx++ {
			newPodNames = append(newPodNames, pods.Items[podIdx].Name)
		}

		if err := r.cleanupPods(newPodNames, &state); err != nil {
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
	for idx := range r.aeroCluster.Spec.RackConfig.Racks {
		rack := &r.aeroCluster.Spec.RackConfig.Racks[idx]

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

	// removePVCs should be passed only filtered pvc otherwise rack pvc may be removed using global storage
	// cascadeDelete
	var filteredPVCItems []corev1.PersistentVolumeClaim

	for pvcIdx := range pvcItems {
		pvc := &pvcItems[pvcIdx]

		var found bool

		for rackIdx := range r.aeroCluster.Spec.RackConfig.Racks {
			rack := &r.aeroCluster.Spec.RackConfig.Racks[rackIdx]
			rackLabels := utils.LabelsForAerospikeClusterRack(
				r.aeroCluster.Name, rack.ID,
			)

			if reflect.DeepEqual(pvc.Labels, rackLabels) {
				found = true
				break
			}
		}

		if !found {
			filteredPVCItems = append(filteredPVCItems, *pvc)
		}
	}

	// Delete pvc for common storage.
	if _, err := r.removePVCsAsync(
		&r.aeroCluster.Spec.Storage, filteredPVCItems,
	); err != nil {
		return fmt.Errorf("failed to remove cluster PVCs: %v", err)
	}

	return nil
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

func (r *SingleClusterReconciler) checkPreviouslyFailedCluster() (bool, error) {
	isNew, err := r.isNewCluster()
	if err != nil {
		return false, fmt.Errorf("error determining if cluster is new: %v", err)
	}

	if isNew {
		r.Log.V(1).Info("It's a new cluster, create empty status object")

		if err := r.createStatus(); err != nil {
			return false, err
		}
	} else {
		r.Log.V(1).Info(
			"It's not a new cluster, " +
				"checking if it is failed and needs recovery",
		)

		hasFailed, err := r.hasClusterFailed()
		if err != nil {
			return hasFailed, fmt.Errorf(
				"error determining if cluster has failed: %v", err,
			)
		}

		if hasFailed {
			return hasFailed, r.recoverFailedCreate()
		}
	}

	return false, nil
}

func (r *SingleClusterReconciler) removedNamespaces(nodesNamespaces map[string][]string) []string {
	statusNamespaces := sets.NewString()
	for _, namespaces := range nodesNamespaces {
		statusNamespaces.Insert(namespaces...)
	}

	specNamespaces := sets.NewString()

	racks := r.aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		for _, namespace := range racks[idx].AerospikeConfig.Value["namespaces"].([]interface{}) {
			specNamespaces.Insert(namespace.(map[string]interface{})["name"].(string))
		}
	}

	removedNamespaces := statusNamespaces.Difference(specNamespaces)

	return removedNamespaces.List()
}

func (r *SingleClusterReconciler) IsStatusEmpty() bool {
	return r.aeroCluster.Status.AerospikeConfig == nil
}

func (r *SingleClusterReconciler) migrateAerospikeCluster(ctx context.Context, hasFailed bool) error {
	if !hasFailed {
		if int(r.aeroCluster.Spec.Size) > len(r.aeroCluster.Status.Pods) {
			return fmt.Errorf("cluster is not ready for migration, pod status is not populated")
		}

		if err := r.migrateInitialisedVolumeNames(ctx); err != nil {
			r.Log.Error(err, "Problem patching Initialised volumes")
			return err
		}
	}

	if err := r.AddAPIVersionLabel(ctx); err != nil {
		r.Log.Error(err, "Problem patching label")
		return err
	}

	return nil
}

func (r *SingleClusterReconciler) migrateInitialisedVolumeNames(ctx context.Context) error {
	r.Log.Info("Migrating Initialised Volumes name to new format")

	podList, err := r.getClusterPodList()
	if err != nil {
		if errors.IsNotFound(err) {
			// Request objects not found.
			return nil
		}
		// Error reading the object.
		return err
	}

	var patches []jsonpatch.PatchOperation

	for podIdx := range podList.Items {
		pod := &podList.Items[podIdx]

		if _, ok := r.aeroCluster.Status.Pods[pod.Name]; !ok {
			return fmt.Errorf("empty status found in CR for pod %s", pod.Name)
		}

		initializedVolumes := r.aeroCluster.Status.Pods[pod.Name].InitializedVolumes
		newFormatInitVolNames := sets.Set[string]{}
		oldFormatInitVolNames := make([]string, 0, len(initializedVolumes))

		for volIdx := range initializedVolumes {
			initVolInfo := strings.Split(initializedVolumes[volIdx], "@")
			if len(initVolInfo) < 2 {
				oldFormatInitVolNames = append(oldFormatInitVolNames, initializedVolumes[volIdx])
			} else {
				newFormatInitVolNames.Insert(initVolInfo[0])
			}
		}

		for oldVolIdx := range oldFormatInitVolNames {
			if !newFormatInitVolNames.Has(oldFormatInitVolNames[oldVolIdx]) {
				pvcUID, pvcErr := r.getPVCUid(ctx, pod, oldFormatInitVolNames[oldVolIdx])
				if pvcErr != nil {
					return pvcErr
				}

				if pvcUID == "" {
					return fmt.Errorf("found empty pvcUID for the volume %s", oldFormatInitVolNames[oldVolIdx])
				}

				// Appending volume name as <vol_name>@<pvcUID> in initializedVolumes list
				initializedVolumes = append(
					initializedVolumes, fmt.Sprintf("%s@%s", oldFormatInitVolNames[oldVolIdx], pvcUID),
				)
			}
		}

		if len(initializedVolumes) > len(r.aeroCluster.Status.Pods[pod.Name].InitializedVolumes) {
			r.Log.Info("Got updated initialised volumes list", "initVolumes", initializedVolumes, "podName", pod.Name)

			patch1 := jsonpatch.PatchOperation{
				Operation: "replace",
				Path:      "/status/pods/" + pod.Name + "/initializedVolumes",
				Value:     initializedVolumes,
			}

			patches = append(patches, patch1)
		}
	}

	r.Log.Info("Patching status with updated initialised volumes")

	return r.patchPodStatus(ctx, patches)
}

func (r *SingleClusterReconciler) getPVCUid(ctx context.Context, pod *corev1.Pod, volName string) (string, error) {
	for idx := range pod.Spec.Volumes {
		if pod.Spec.Volumes[idx].Name == volName {
			pvc := &corev1.PersistentVolumeClaim{}
			pvcNamespacedName := types.NamespacedName{
				Name:      pod.Spec.Volumes[idx].PersistentVolumeClaim.ClaimName,
				Namespace: pod.Namespace,
			}

			if err := r.Client.Get(ctx, pvcNamespacedName, pvc); err != nil {
				return "", err
			}

			return string(pvc.UID), nil
		}
	}

	return "", nil
}

func (r *SingleClusterReconciler) AddAPIVersionLabel(ctx context.Context) error {
	aeroCluster := r.aeroCluster
	if aeroCluster.Labels == nil {
		aeroCluster.Labels = make(map[string]string)
	}

	aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel] = asdbv1.AerospikeAPIVersion

	return r.Client.Update(ctx, aeroCluster, common.UpdateOption)
}

func (r *SingleClusterReconciler) IsReclusterNeeded() bool {
	// Return false if dynamic configuration updates are disabled
	if !asdbv1.GetBool(r.aeroCluster.Spec.EnableDynamicConfigUpdate) {
		return false
	}

	// Check for any active-rack addition/update across all the namespaces.
	// If there is any active-rack change, recluster is required.
	for specIdx := range r.aeroCluster.Spec.RackConfig.Racks {
		for statusIdx := range r.aeroCluster.Status.RackConfig.Racks {
			if r.aeroCluster.Spec.RackConfig.Racks[specIdx].ID == r.aeroCluster.Status.RackConfig.Racks[statusIdx].ID &&
				r.IsReclusterNeededForRack(&r.aeroCluster.Spec.RackConfig.Racks[specIdx],
					&r.aeroCluster.Status.RackConfig.Racks[statusIdx]) {
				return true
			}
		}
	}

	return false
}

func (r *SingleClusterReconciler) IsReclusterNeededForRack(specRack, statusRack *asdbv1.Rack) bool {
	specNamespaces, ok := specRack.AerospikeConfig.Value["namespaces"].([]interface{})
	if !ok {
		return false
	}

	statusNamespaces, ok := statusRack.AerospikeConfig.Value["namespaces"].([]interface{})
	if !ok {
		return false
	}

	for _, specNamespace := range specNamespaces {
		for _, statusNamespace := range statusNamespaces {
			if specNamespace.(map[string]interface{})["name"] != statusNamespace.(map[string]interface{})["name"] {
				continue
			}

			if specNamespace.(map[string]interface{})["active-rack"] != statusNamespace.(map[string]interface{})["active-rack"] {
				return true
			}
		}
	}

	return false
}
