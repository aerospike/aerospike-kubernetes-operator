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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	as "github.com/aerospike/aerospike-client-go/v8"
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

func (r *SingleClusterReconciler) Reconcile(ctx context.Context) (result ctrl.Result, recErr error) {
	r.Log.V(1).Info(
		"AerospikeCluster", "Spec", r.aeroCluster.Spec, "Status",
		r.aeroCluster.Status,
	)

	// Set the status phase to Error if the recErr is not nil
	// recErr is only set when reconcile failure should result in Error phase of the cluster
	defer func() {
		logValues := reconcileExitLogValues(result, recErr)
		if recErr != nil {
			if err := r.setStatusPhase(ctx, asdbv1.AerospikeClusterError); err != nil {
				recErr = fmt.Errorf(
					"%w; setting error phase for cluster %s: %w",
					recErr, utils.ClusterNamespacedName(r.aeroCluster), err,
				)
			}

			r.Log.Error(recErr, "Reconcile failed", logValues...)

			return
		}

		r.Log.Info("Reconcile completed", logValues...)
	}()

	// Check DeletionTimestamp to see if the cluster is being deleted
	if !r.aeroCluster.DeletionTimestamp.IsZero() {
		r.Log.V(1).Info("Deleting AerospikeCluster")
		// The cluster is being deleted
		if err := r.handleClusterDeletion(ctx, finalizerName); err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, EventReasonDeleteFailed,
				"Failed to delete AerospikeCluster %s",
				utils.GetNamespacedNameString(r.aeroCluster),
			)

			recErr = err

			return reconcile.Result{}, recErr
		}

		r.removeClusterPhaseMetric()

		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeNormal, EventReasonDeleted,
			"Deleted AerospikeCluster %s", utils.GetNamespacedNameString(r.aeroCluster),
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
	if err := r.setStatusPhase(ctx, asdbv1.AerospikeClusterInProgress); err != nil {
		recErr = err

		return reconcile.Result{}, recErr
	}

	// The cluster is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(ctx, finalizerName); err != nil {
		recErr = err

		return reconcile.Result{}, recErr
	}

	// Handle previously failed cluster
	hasFailed, res := r.checkPreviouslyFailedCluster(ctx)
	if !res.IsSuccess {
		recErr = res.Err

		return res.Result, recErr
	}

	if r.aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel] == asdbv1.AerospikeAPIVersion {
		r.Log.Info("cluster migration is not needed")
	} else {
		if err := r.migrateAerospikeCluster(ctx, hasFailed); err != nil {
			recErr = err

			return reconcile.Result{}, recErr
		}
	}

	if err := r.createOrUpdateSTSHeadlessSvc(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonServiceCreateFailed,
			"Failed to create headless Service for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Reconcile all racks
	if res := r.reconcileRacks(ctx); !res.IsSuccess {
		if res.Err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, EventReasonRackReconcileFailed,
				"Failed to reconcile racks for AerospikeCluster %s",
				utils.GetNamespacedNameString(r.aeroCluster),
			)

			recErr = res.Err
		}

		return res.Result, recErr
	}

	if err := r.reconcilePDB(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonPodDisruptionBudgetReconcileFailed,
			"Failed to reconcile PodDisruptionBudget for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	if err := r.reconcileSTSLoadBalancerSvc(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonServiceCreateFailed,
			"Failed to create LoadBalancer Service for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	ignorablePodNames, err := r.getIgnorablePods(ctx, nil, getConfiguredRackStateList(r.aeroCluster), nil)
	if err != nil {
		recErr = err

		return reconcile.Result{}, recErr
	}

	// Check if there is any node with quiesce status. We need to undo that
	// It may have been left from previous steps
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		e := fmt.Errorf(
			"getting host connections for cluster %s nodes: %w", utils.ClusterNamespacedName(r.aeroCluster), err,
		)

		recErr = e

		return reconcile.Result{}, recErr
	}

	if err = deployment.InfoQuiesceUndo(
		r.Log,
		r.getClientPolicy(ctx), allHostConns,
	); err != nil {
		recErr = err

		return reconcile.Result{}, recErr
	}

	// Setup access control.
	// Assuming all pods must be security enabled or disabled.
	if err = r.validateAndReconcileAccessControl(ctx, nil, ignorablePodNames); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonAccessControlUpdateFailed,
			"Failed to set up access control for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Use policy from spec after setting up access control
	policy := r.getClientPolicy(ctx)

	// Revert migrate-fill-delay to the original value if it was set to a different value while processing racks.
	// Passing the first rack from the list as all the racks will have the same migrate-fill-delay
	// Redundant safe check to revert migrate-fill-delay if the previous revert operation missed/skipped somehow
	if res := r.setMigrateFillDelay(
		ctx, policy, &r.aeroCluster.Spec.RackConfig.Racks[0].AerospikeConfig,
		false, ignorablePodNames,
	); !res.IsSuccess {
		recErr = res.Err

		return reconcile.Result{}, recErr
	}

	// Doing recluster before setting up roster to get the latest observed node list from server.
	if r.IsReclusterNeeded() {
		if err = deployment.InfoRecluster(
			r.Log,
			policy, allHostConns,
		); err != nil {
			recErr = err

			return reconcile.Result{}, recErr
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
		if err = r.getAndSetRoster(ctx, policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
			recErr = err

			return reconcile.Result{}, recErr
		}
	}

	// Update the AerospikeCluster status.
	if err = r.updateStatus(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonStatusUpdateFailed,
			"Failed to update status for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		recErr = err

		return reconcile.Result{}, recErr
	}

	// Try to recover pods only if there are any ignorable pods, which may be failed or pending.
	if len(ignorablePodNames) > 0 {
		if res := r.recoverIgnorablePods(ctx, ignorablePodNames); !res.IsSuccess {
			recErr = res.Err

			return res.Result, recErr
		}
	}

	return reconcile.Result{}, nil
}

func reconcileExitLogValues(result ctrl.Result, recErr error) []interface{} {
	if recErr != nil {
		return []interface{}{"result", "error"}
	}

	if result.RequeueAfter > 0 {
		values := []interface{}{"result", "requeue"}
		values = append(values, "requeueAfter", result.RequeueAfter.String())

		return values
	}

	return []interface{}{"result", "success"}
}

func (r *SingleClusterReconciler) recoverIgnorablePods(
	ctx context.Context, ignorablePodNames sets.Set[string]) common.ReconcileResult {
	podList, gErr := r.getClusterPodList(ctx)
	if gErr != nil {
		return common.ReconcileError(fmt.Errorf("list pods for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), gErr))
	}

	r.Log.Info("Try to recover failed/pending pods if any")

	var (
		anyPodFailed    bool
		requeueInterval int
	)

	// Try to recover failed/pending pods by deleting them if grace period is over.
	for idx := range podList.Items {
		if ignorablePodNames.Has(podList.Items[idx].Name) {
			podState := utils.CheckPodFailedWithGrace(&podList.Items[idx], true)

			if podState.State != utils.PodHealthy {
				anyPodFailed = true

				if podState.State == utils.PodFailedInGrace {
					r.Log.Info(
						"Pod is in failed state but within grace period, will not delete",
						"pod", podList.Items[idx].Name,
					)

					requeueInterval = asdbv1.RequeueIntervalSeconds10

					continue
				}

				// Pod has failed and grace period is over
				if err := r.createOrUpdatePodServiceIfNeeded(ctx, []string{podList.Items[idx].Name}); err != nil {
					return common.ReconcileError(err)
				}

				if err := r.Delete(ctx, &podList.Items[idx]); err != nil {
					return common.ReconcileError(fmt.Errorf("delete pod %s: %w", utils.GetNamespacedNameString(&podList.Items[idx]), err))
				}

				r.Log.Info("Deleted pod", "pod", podList.Items[idx].Name)
			}
		}
	}

	if anyPodFailed {
		r.Log.Info("Found failed/pending pod(s), requeuing")
	} else {
		r.Log.Info("Found ignorable pod(s), requeuing")
	}

	return common.ReconcileRequeueAfter(requeueInterval)
}

func (r *SingleClusterReconciler) validateAndReconcileAccessControl(
	ctx context.Context,
	selectedPods []corev1.Pod,
	ignorablePodNames sets.Set[string],
) error {
	enabled, err := asdbv1.IsSecurityEnabled(r.aeroCluster.Spec.AerospikeConfig.Value)
	if err != nil {
		return fmt.Errorf("getting cluster security status: %w", err)
	}

	if !enabled {
		r.Log.Info("Cluster is not security enabled, please enable security for this cluster.")
		return nil
	}

	var conns []*deployment.HostConn

	// Create client
	if selectedPods == nil {
		conns, err = r.newAllHostConnWithOption(ctx, ignorablePodNames)
		if err != nil {
			return fmt.Errorf("getting host connections for cluster %s nodes: %w",
				utils.ClusterNamespacedName(r.aeroCluster), err)
		}
	} else {
		conns, err = r.newPodsHostConnWithOption(selectedPods, ignorablePodNames)
		if err != nil {
			return fmt.Errorf("getting host connections for selected pods of cluster %s: %w",
				utils.ClusterNamespacedName(r.aeroCluster), err)
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
	clientPolicy := r.getClientPolicy(ctx)

	aeroClient, err := as.NewClientWithPolicyAndHost(clientPolicy, hosts...)
	if err != nil {
		return fmt.Errorf("creating aerospike client for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider()

	err = r.reconcileAccessControl(
		ctx, aeroClient, pp,
	)
	if err != nil {
		return fmt.Errorf("reconciling access control for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), err)
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, EventReasonAccessControlUpdated,
		"Updated access control for AerospikeCluster %s",
		utils.GetNamespacedNameString(r.aeroCluster),
	)

	// Update the AerospikeCluster status.
	if err := r.updateAccessControlStatus(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, EventReasonStatusUpdateFailed,
			"Failed to update access control status for AerospikeCluster %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		return err
	}

	return nil
}

func (r *SingleClusterReconciler) updateStatus(ctx context.Context) error {
	r.Log.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Get(
		ctx, types.NamespacedName{
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
		clusterReadinessEnable, gErr := r.getClusterReadinessStatus(ctx)
		if gErr != nil {
			return fmt.Errorf("getting cluster readiness status: %w", gErr)
		}

		newAeroCluster.Status.IsReadinessProbeEnabled = clusterReadinessEnable
	}

	selector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(newAeroCluster.Name))
	newAeroCluster.Status.Selector = selector.String()

	err = r.patchStatus(ctx, newAeroCluster)
	if err != nil {
		return fmt.Errorf("error updating status: %w", err)
	}

	r.aeroCluster = newAeroCluster

	// Add the cluster phase metric
	r.addClusterPhaseMetric()

	r.Log.Info("Updated status", "status", newAeroCluster.Status)

	return nil
}

func (r *SingleClusterReconciler) setStatusPhase(ctx context.Context, phase asdbv1.AerospikeClusterPhase) error {
	if r.aeroCluster.Status.Phase != phase {
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Get(ctx, utils.GetNamespacedName(r.aeroCluster), r.aeroCluster); err != nil {
				return err
			}

			r.aeroCluster.Status.Phase = phase

			// detached: status update must complete even if the reconcile context is cancelled
			return r.Client.Status().Update(context.Background(), r.aeroCluster)
		}); err != nil {
			return fmt.Errorf("set status to %s for cluster %s: %w", phase, utils.ClusterNamespacedName(r.aeroCluster), err)
		}

		r.addClusterPhaseMetric()
	}

	return nil
}

func (r *SingleClusterReconciler) getClusterReadinessStatus(ctx context.Context) (bool, error) {
	podList, err := r.getClusterPodList(ctx)
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

func (r *SingleClusterReconciler) updateAccessControlStatus(ctx context.Context) error {
	if r.aeroCluster.Spec.AerospikeAccessControl == nil {
		return nil
	}

	r.Log.Info("Update access control status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Get(
		ctx, types.NamespacedName{
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

	newAeroCluster.Status.AerospikeAccessControl = statusAerospikeAccessControl

	if err := r.patchStatus(ctx, newAeroCluster); err != nil {
		return fmt.Errorf("error updating status: %w", err)
	}

	r.Log.Info("Updated access control status", "status", newAeroCluster.Status)

	return nil
}

func (r *SingleClusterReconciler) createStatus(ctx context.Context) error {
	r.Log.Info("Creating status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Get(
		ctx, types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return err
	}

	if newAeroCluster.Status.Pods == nil {
		newAeroCluster.Status.Pods = map[string]asdbv1.AerospikePodStatus{}
	}

	if err := r.Client.Status().Update(
		ctx, newAeroCluster,
	); err != nil {
		return fmt.Errorf("error creating status: %w", err)
	}

	return nil
}

func (r *SingleClusterReconciler) isNewCluster(ctx context.Context) (bool, error) {
	if !r.IsStatusEmpty() {
		// We have valid status, cluster cannot be new.
		return false, nil
	}

	statefulSetList, err := r.getClusterSTSList(ctx)
	if err != nil {
		return false, err
	}

	// Cluster can have status nil and still have pods on failures.
	// For cluster to be new there should be no pods in the cluster.
	return len(statefulSetList.Items) == 0, nil
}

// hasClusterFailed returns (failed, inGracePeriod, error)
// failed: true if cluster has truly failed and needs recovery
// inGracePeriod: true if cluster has failed pods but within grace period
// error: any error during the check
func (r *SingleClusterReconciler) hasClusterFailed(ctx context.Context) (failed, inGracePeriod bool, err error) {
	isNew, err := r.isNewCluster(ctx)
	if err != nil {
		// Checking cluster status failed.
		return failed, inGracePeriod, err
	}

	if isNew {
		// New clusters should not be considered failed.
		return failed, inGracePeriod, nil
	}

	// Check if there are any pods running
	pods, err := r.getClusterPodList(ctx)
	if err != nil {
		return failed, inGracePeriod, err
	}

	for idx := range pods.Items {
		pod := &pods.Items[idx]

		podState := utils.CheckPodFailedWithGrace(pod, true)
		if podState.State == utils.PodHealthy {
			// There is at least one pod that has not yet failed.
			// It's possible that the containers are stuck doing a long disk
			// initialization.
			// Don't consider this cluster as failed and needing recovery
			// as long as there is at least one running pod.
			return false, false, nil
		}

		// Pod is failed, check if it's in grace period
		if podState.State == utils.PodFailedInGrace {
			inGracePeriod = true
		}
	}

	// If we reach here, all pods are failed
	if inGracePeriod {
		// Return grace period state
		return failed, inGracePeriod, nil
	}

	if r.IsStatusEmpty() {
		return true, inGracePeriod, nil
	}

	return failed, inGracePeriod, nil
}

func (r *SingleClusterReconciler) patchStatus(ctx context.Context, newAeroCluster *asdbv1.AerospikeCluster) error {
	oldAeroCluster := r.aeroCluster

	oldJSON, err := json.Marshal(oldAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling old status: %w", err)
	}

	newJSON, err := json.Marshal(newAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling new status: %w", err)
	}

	jsonPatchPatch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("error creating json patch: %w", err)
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
		return fmt.Errorf("error marshalling json patch: %w", err)
	}

	patch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

	if err = r.Client.Status().Patch(
		ctx, &asdbv1.AerospikeCluster{ObjectMeta: metav1.ObjectMeta{
			Name:      oldAeroCluster.Name,
			Namespace: oldAeroCluster.Namespace,
		}}, patch,
		client.FieldOwner(patchFieldOwner),
	); err != nil {
		return fmt.Errorf("error patching status: %w", err)
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
func (r *SingleClusterReconciler) recoverFailedCreate(ctx context.Context) error {
	r.Log.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration.")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterSTSList(ctx)
	if err != nil {
		return fmt.Errorf(
			"error getting statefulsets while forcing recreate of the cluster as status is nil: %w",
			err,
		)
	}

	r.Log.V(1).Info(
		"Found statefulset for cluster. Need to delete them", "nSTS",
		len(statefulSetList.Items),
	)

	for idx := range statefulSetList.Items {
		statefulset := &statefulSetList.Items[idx]
		if err := r.deleteSTS(ctx, statefulset); err != nil {
			return fmt.Errorf(
				"error deleting statefulset while forcing recreate of the cluster as status is nil: %w",
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

		pods, err := r.getRackPodList(ctx, state.Rack.ID, state.Rack.Revision)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %w", err)
		}

		newPodNames := make([]string, 0)
		for podIdx := 0; podIdx < len(pods.Items); podIdx++ {
			newPodNames = append(newPodNames, pods.Items[podIdx].Name)
		}

		if err := r.cleanupPods(ctx, newPodNames, &state); err != nil {
			return fmt.Errorf("failed recover failed cluster: %w", err)
		}
	}

	return fmt.Errorf("forcing recreate of the cluster as status is nil")
}

func (r *SingleClusterReconciler) addFinalizer(ctx context.Context, finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(
		r.aeroCluster.Finalizers, finalizerName,
	) {
		r.aeroCluster.Finalizers = append(
			r.aeroCluster.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroCluster); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) cleanUpAndRemoveFinalizer(ctx context.Context, finalizerName string) error {
	// The object is being deleted
	if utils.ContainsString(
		r.aeroCluster.Finalizers, finalizerName,
	) {
		// Handle any external dependency
		if err := r.deleteExternalResources(ctx); err != nil {
			// If fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// Remove finalizer from the list
		r.aeroCluster.Finalizers = utils.RemoveString(
			r.aeroCluster.Finalizers, finalizerName,
		)

		if err := r.Update(ctx, r.aeroCluster); err != nil {
			return err
		}
	}

	// Stop reconciliation as the item is being deleted
	return nil
}

func (r *SingleClusterReconciler) deleteExternalResources(ctx context.Context) error {
	// Delete should be idempotent
	r.Log.Info("Removing pvc for removed cluster")

	// Delete pvc for all rack storage
	for idx := range r.aeroCluster.Spec.RackConfig.Racks {
		rack := &r.aeroCluster.Spec.RackConfig.Racks[idx]

		rackPVCItems, err := r.getRackPVCList(ctx, rack.ID, rack.Revision)
		if err != nil {
			return fmt.Errorf("could not find pvc for rack %d in cluster %s: %w", rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err)
		}

		storage := rack.Storage
		if _, err := r.removePVCsAsync(ctx, &storage, rackPVCItems); err != nil {
			return fmt.Errorf("removing pvcs for rack %d in cluster %s: %w", rack.ID, utils.ClusterNamespacedName(r.aeroCluster), err)
		}
	}

	// Delete PVCs for any remaining old removed racks
	pvcItems, err := r.getClusterPVCList(ctx)
	if err != nil {
		return fmt.Errorf("could not find pvc for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), err)
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
				r.aeroCluster.Name, rack.ID, rack.Revision,
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
		ctx, &r.aeroCluster.Spec.Storage, filteredPVCItems,
	); err != nil {
		return fmt.Errorf("removing pvcs for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), err)
	}

	return nil
}

func (r *SingleClusterReconciler) handleClusterDeletion(ctx context.Context, finalizerName string) error {
	r.Log.Info("Handle cluster deletion")

	// The cluster is being deleted
	if err := r.cleanUpAndRemoveFinalizer(ctx, finalizerName); err != nil {
		return fmt.Errorf("clean up and remove finalizer for cluster %s: %w", utils.ClusterNamespacedName(r.aeroCluster), err)
	}

	return nil
}

func (r *SingleClusterReconciler) checkPreviouslyFailedCluster(ctx context.Context) (bool, common.ReconcileResult) {
	isNew, err := r.isNewCluster(ctx)
	if err != nil {
		return false, common.ReconcileError(fmt.Errorf("error determining if cluster is new: %w", err))
	}

	if isNew {
		r.Log.V(1).Info("It's a new cluster, create empty status object")

		if err := r.createStatus(ctx); err != nil {
			return false, common.ReconcileError(err)
		}
	} else {
		r.Log.V(1).Info(
			"It's not a new cluster, " +
				"checking if it is failed and needs recovery",
		)

		hasFailed, inGracePeriod, err := r.hasClusterFailed(ctx)
		if err != nil {
			return false, common.ReconcileError(fmt.Errorf("error determining if cluster has failed: %w", err))
		}

		if hasFailed {
			if err = r.recoverFailedCreate(ctx); err != nil {
				return hasFailed, common.ReconcileError(err)
			}

			return hasFailed, common.ReconcileSuccess()
		}

		if inGracePeriod {
			r.Log.Info("Pods are failed but within grace period, requeueing...")
			return false, common.ReconcileRequeueAfter(asdbv1.RequeueIntervalSeconds10)
		}
	}

	return false, common.ReconcileSuccess()
}

func (r *SingleClusterReconciler) removedNamespaces(nodesNamespaces map[string][]string) []string {
	statusNamespaces := sets.NewString()
	for _, namespaces := range nodesNamespaces {
		statusNamespaces.Insert(namespaces...)
	}

	specNamespaces := sets.NewString()

	racks := r.aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		for _, namespace := range racks[idx].AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{}) {
			specNamespaces.Insert(namespace.(map[string]interface{})[asdbv1.ConfKeyName].(string))
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
			return err
		}
	}

	if err := r.AddAPIVersionLabel(ctx); err != nil {
		return err
	}

	return nil
}

func (r *SingleClusterReconciler) migrateInitialisedVolumeNames(ctx context.Context) error {
	r.Log.Info("Migrating Initialised Volumes name to new format")

	podList, err := r.getClusterPodList(ctx)
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

			if err := r.Get(ctx, pvcNamespacedName, pvc); err != nil {
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

	return r.Update(ctx, aeroCluster, common.UpdateOption)
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
	specNamespaces, ok := specRack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
	if !ok {
		return false
	}

	statusNamespaces, ok := statusRack.AerospikeConfig.Value[asdbv1.ConfKeyNamespace].([]interface{})
	if !ok {
		return false
	}

	for _, specNamespace := range specNamespaces {
		for _, statusNamespace := range statusNamespaces {
			if specNamespace.(map[string]interface{})[asdbv1.ConfKeyName] !=
				statusNamespace.(map[string]interface{})[asdbv1.ConfKeyName] {
				continue
			}

			if specNamespace.(map[string]interface{})["active-rack"] != statusNamespace.(map[string]interface{})["active-rack"] {
				return true
			}

			if specNamespace.(map[string]interface{})[asdbv1.ConfKeyReplicationFactor] !=
				statusNamespace.(map[string]interface{})[asdbv1.ConfKeyReplicationFactor] {
				return true
			}
		}
	}

	return false
}
