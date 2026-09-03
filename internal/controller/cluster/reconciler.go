package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
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

// reconcileComputedState holds the state a single Reconcile call computes as it goes, as opposed
// to the durable collaborators on SingleClusterReconciler. Every field is written mid-pass by one
// stage and read by a later one, and none of it is meaningful outside the pass that produced it.
type reconcileComputedState struct {
	// pendingOpReset holds the operation conditions finishReconcile is allowed to clear on exit.
	// A rack function that sets its condition True claims it, removing it from the set, so an
	// operation still in flight keeps reporting. See mergePatchStatus.
	// nil means clear nothing. It is armed immediately before the rack loop — the only code that
	// claims — so cluster deletion, spec.paused, and every stage ahead of the rack loop leave the
	// conditions frozen at their last known state.
	pendingOpReset sets.Set[string]
	// revisionChangedRackIDs holds the IDs of racks undergoing a revision migration this pass, as
	// categoriseRacks computed them from live StatefulSets. It is the single answer to "is this
	// rack migrating". Keyed by rack ID rather than by revision.
	// Set once in reconcileRacks before any rack function runs; nil on passes that never reach the
	// rack loop, which reads as "nothing migrating" and is correct for those paths.
	revisionChangedRackIDs sets.Set[int]
	// failureReason names the reconcile stage Reconcile bailed out at, surfaced as the Ready
	// condition's Reason by writeTerminalStatus. Read only when Reconcile returns an error, so
	// a value left here by a requeue path is inert. Empty means no stage was recorded.
	failureReason string
}

// SingleClusterReconciler reconciles a single AerospikeCluster.
// The controller builds a fresh one per Reconcile call, so computedState carries nothing between passes.
type SingleClusterReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	aeroCluster *asdbv1.AerospikeCluster
	KubeClient  *kubernetes.Clientset
	KubeConfig  *rest.Config
	Scheme      *k8sRuntime.Scheme
	Log         logr.Logger
	// computedState holds everything this Reconcile call computes as it goes.
	computedState reconcileComputedState
}

func (r *SingleClusterReconciler) asConfigLog() logr.Logger {
	return r.Log.WithName("lib.asconfig")
}

func (r *SingleClusterReconciler) Reconcile(ctx context.Context) (result ctrl.Result, recErr error) {
	r.Log.V(1).Info(
		"AerospikeCluster", "spec", r.aeroCluster.Spec, "status",
		r.aeroCluster.Status,
	)

	defer func() {
		// finishReconcile returns the error to assign here so we avoid *error params; recErr is Reconcile's named return.
		recErr = r.finishReconcile(ctx, result, recErr)
	}()

	// Check DeletionTimestamp to see if the cluster is being deleted
	if !r.aeroCluster.DeletionTimestamp.IsZero() {
		return reconcile.Result{}, r.handleTerminatingCluster(ctx)
	}

	// Pre-seed conditions on first reconcile so kubectl wait doesn't hang.
	if err := r.initializeConditionsIfNeeded(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("initialize conditions: %w", err)
	}

	// Pause the reconciliation for the AerospikeCluster if the paused field is set to true.
	// Deletion of the AerospikeCluster will not be paused.
	if asdbv1.GetBool(r.aeroCluster.Spec.Paused) {
		r.Log.Info("Reconciliation is paused for this AerospikeCluster")

		// Pause should keep all old conditions as is
		return reconcile.Result{}, r.setConditions(
			ctx, metav1.Condition{
				Type:    string(asdbv1.AerospikeClusterConditionPaused),
				Status:  metav1.ConditionTrue,
				Reason:  asdbv1.AerospikeClusterReasonPausedByUser,
				Message: "Reconciliation is paused via spec.paused=true",
			})
	}

	// Reset the Paused condition to False irrespective of CR phase if code flow reached this stage
	if err := r.mergePatchStatus(
		ctx, nil,
		metav1.Condition{
			Type:   string(asdbv1.AerospikeClusterConditionPaused),
			Status: metav1.ConditionFalse,
			Reason: asdbv1.AerospikeClusterReasonNotPaused,
		},
	); err != nil {
		return reconcile.Result{}, fmt.Errorf("reset reconcile paused state: %w", err)
	}

	// Mark Ready=False and phase=InProgress at the start of every reconcile
	// but only if the cluster is not already in an error state.
	if r.aeroCluster.Status.Phase != asdbv1.AerospikeClusterError {
		inProgress := asdbv1.AerospikeClusterInProgress

		if err := r.mergePatchStatus(
			ctx, &inProgress,
			metav1.Condition{
				Type:    string(asdbv1.AerospikeClusterConditionReady),
				Status:  metav1.ConditionFalse,
				Reason:  asdbv1.AerospikeClusterReasonReconciling,
				Message: "Reconcile in progress",
			},
		); err != nil {
			return reconcile.Result{}, fmt.Errorf("mark reconcile in progress: %w", err)
		}
	}

	// The cluster is not being deleted, add finalizer if not added already
	if err := r.addFinalizer(ctx, finalizerName); err != nil {
		r.computedState.failureReason = asdbv1.AerospikeClusterReasonClusterSetupFailed

		return reconcile.Result{}, fmt.Errorf("add finalizer: %w", err)
	}

	// Handle previously failed cluster
	hasFailed, res := r.checkPreviouslyFailedCluster(ctx)
	if !res.IsSuccess {
		if res.Err != nil {
			r.computedState.failureReason = asdbv1.AerospikeClusterReasonClusterSetupFailed
		}

		return res.Result, res.Err
	}

	if r.aeroCluster.Labels[asdbv1.AerospikeAPIVersionLabel] == asdbv1.AerospikeAPIVersion {
		r.Log.Info("Cluster migration is not needed")
	} else {
		if err := r.migrateAerospikeCluster(ctx, hasFailed); err != nil {
			return reconcile.Result{}, err
		}
	}

	if err := r.createOrUpdateSTSHeadlessSvc(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, ReasonServiceCreateFailed,
			"Failed to create headless Service %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		r.computedState.failureReason = asdbv1.AerospikeClusterReasonServiceReconcileFailed

		return reconcile.Result{}, fmt.Errorf("create or update headless Service: %w", err)
	}

	// From here on this pass owns the operation conditions: finishReconcile clears whichever
	// ones no rack function claims, per the policy set after the rack loop.
	r.initPendingOpConditionReset()

	// Reconcile all racks
	if res := r.reconcileRacks(ctx); !res.IsSuccess {
		if res.Err != nil {
			r.Recorder.Eventf(
				r.aeroCluster, corev1.EventTypeWarning, "UpdateFailed",
				"Failed to reconcile racks",
			)

			r.computedState.failureReason = asdbv1.AerospikeClusterReasonRackReconcileFailed
		}

		return res.Result, res.Err
	}

	if err := r.reconcilePDB(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "PodDisruptionBudgetReconcileFailed",
			"Failed to reconcile PodDisruptionBudget %s",
			utils.GetNamespacedNameString(r.aeroCluster),
		)

		r.computedState.failureReason = asdbv1.AerospikeClusterReasonPDBReconcileFailed

		return reconcile.Result{}, fmt.Errorf("reconcile PodDisruptionBudget: %w", err)
	}

	if err := r.reconcileSTSLoadBalancerSvc(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, ReasonServiceCreateFailed,
			"Failed to create LoadBalancer Service %s",
			utils.NamespacedName(r.aeroCluster.Namespace, r.aeroCluster.Name+"-lb"),
		)

		r.computedState.failureReason = asdbv1.AerospikeClusterReasonServiceReconcileFailed

		return reconcile.Result{}, fmt.Errorf("reconcile LoadBalancer Service: %w", err)
	}

	ignorablePodNames, err := r.getIgnorablePods(ctx, nil, getConfiguredRackStateList(r.aeroCluster))
	if err != nil {
		r.computedState.failureReason = asdbv1.AerospikeClusterReasonPodStateFetchFailed

		return reconcile.Result{}, fmt.Errorf("determine ignorable Pods: %w", err)
	}

	// Check if there is any node with quiesce status. We need to undo that
	// It may have been left from previous steps
	allHostConns, err := r.newAllHostConnWithOption(ctx, ignorablePodNames)
	if err != nil {
		r.computedState.failureReason = asdbv1.AerospikeClusterReasonPodStateFetchFailed

		return reconcile.Result{}, fmt.Errorf("get host connections for cluster nodes: %w", err)
	}

	if err = deployment.InfoQuiesceUndo(
		r.Log,
		r.getClientPolicy(ctx), allHostConns,
	); err != nil {
		r.computedState.failureReason = asdbv1.AerospikeClusterReasonQuiesceUndoFailed

		return reconcile.Result{}, fmt.Errorf("undo quiesce state: %w", err)
	}

	// Setup access control.
	// Assuming all pods must be security enabled or disabled.
	if err = r.validateAndReconcileAccessControl(ctx, nil, ignorablePodNames); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, ReasonACLUpdateFailed,
			"Failed to set up access control",
		)

		r.computedState.failureReason = asdbv1.AerospikeClusterReasonACLReconcileFailed

		return reconcile.Result{}, fmt.Errorf("reconcile access control: %w", err)
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
		if res.Err != nil {
			r.computedState.failureReason = asdbv1.AerospikeClusterReasonMFDSetFailed

			return reconcile.Result{}, fmt.Errorf("revert migrate-fill-delay: %w", res.Err)
		}

		return reconcile.Result{}, nil
	}

	// Doing recluster before setting up roster to get the latest observed node list from server.
	if r.IsReclusterNeeded() {
		if err = deployment.InfoRecluster(
			r.Log,
			policy, allHostConns,
		); err != nil {
			r.computedState.failureReason = asdbv1.AerospikeClusterReasonReclusterFailed

			return reconcile.Result{}, fmt.Errorf("run recluster: %w", err)
		}
	}

	if res := r.ensureSCRoster(ctx, policy, allHostConns, ignorablePodNames); !res.IsSuccess {
		if res.Err != nil {
			r.computedState.failureReason = asdbv1.AerospikeClusterReasonRosterSetFailed
		}

		return res.Result, res.Err
	}

	// Update the AerospikeCluster status.
	if err = r.updateStatus(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, ReasonStatusUpdateFailed,
			"Failed to update status",
		)

		r.computedState.failureReason = asdbv1.AerospikeClusterReasonStatusUpdateFailed

		return reconcile.Result{}, fmt.Errorf("update AerospikeCluster status: %w", err)
	}

	// Try to recover pods only if there are any ignorable pods, which may be failed or pending.
	if ignorablePodNames.Len() > 0 {
		if res := r.recoverIgnorablePods(ctx, ignorablePodNames); !res.IsSuccess {
			return res.GetResult()
		}
	}

	return reconcile.Result{}, nil
}

// handleTerminatingCluster sets Ready=False/Terminating and drives cluster deletion.
// Returns the result to be returned directly from Reconcile.
func (r *SingleClusterReconciler) handleTerminatingCluster(ctx context.Context) error {
	r.Log.V(1).Info("Deleting AerospikeCluster")

	// Signal to observers that the cluster is no longer ready — it is being torn down.
	if err := r.setConditions(ctx, metav1.Condition{
		Type:    string(asdbv1.AerospikeClusterConditionReady),
		Status:  metav1.ConditionFalse,
		Reason:  asdbv1.AerospikeClusterReasonTerminating,
		Message: "Cluster is being deleted",
	}); err != nil {
		// Log error and continue with the cluster deletion
		r.Log.Error(err, "Failed to set Ready condition for terminating cluster")
	}

	if err := r.handleClusterDeletion(ctx, finalizerName); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, "DeleteFailed",
			"Unable to handle AerospikeCluster delete operations %s/%s",
			r.aeroCluster.Namespace, r.aeroCluster.Name,
		)

		return err
	}

	r.removeClusterPhaseMetric()

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "Deleted",
		"Deleted AerospikeCluster %s/%s", r.aeroCluster.Namespace,
		r.aeroCluster.Name,
	)

	// Stop reconciliation as the cluster is being deleted
	return nil
}

// A failed reconcile writes Ready=False naming the stage that failed, plus phase=Error. It doesn't
// reset conditions in a failure path to keep the old conditions state intact.
// A successful or requeueing pass resets whichever operation conditions no rack function claimed.
// Doing it on requeue matters: a large upgrade or restart requeues once per batch for hours, and
// waiting for the whole rack loop to succeed would leave a finished operation reporting True for
// that entire time.
func (r *SingleClusterReconciler) writeTerminalStatus(ctx context.Context, recErr error) error {
	if recErr == nil {
		return r.mergePatchStatus(ctx, nil, r.opConditionsToClear()...)
	}

	errorPhase := asdbv1.AerospikeClusterError

	// Prefer the stage recorded by Reconcile so consumers can distinguish a rack problem
	// from an access-control or roster problem without parsing the message.
	reason := asdbv1.AerospikeClusterReasonReconcileFailed
	if r.computedState.failureReason != "" {
		reason = r.computedState.failureReason
	}

	return r.mergePatchStatus(ctx, &errorPhase, metav1.Condition{
		Type:    string(asdbv1.AerospikeClusterConditionReady),
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: truncateConditionMessage(recErr.Error()),
	})
}

// ensureSCRoster handles Strong Consistency roster management.
// For non-SC clusters it is a no-op. For SC clusters it waits for cluster
// stability (when status is already populated) and then applies the roster.
func (r *SingleClusterReconciler) ensureSCRoster(
	ctx context.Context,
	policy *as.ClientPolicy,
	allHostConns []*deployment.HostConn,
	ignorablePodNames sets.Set[string],
) common.ReconcileResult {
	if !asdbv1.IsClusterSCEnabled(r.aeroCluster) {
		return common.ReconcileSuccess()
	}

	if !r.IsStatusEmpty() {
		if res := r.waitForClusterStability(policy, allHostConns); !res.IsSuccess {
			return res
		}
	}

	if err := r.getAndSetRoster(ctx, policy, r.aeroCluster.Spec.RosterNodeBlockList, ignorablePodNames); err != nil {
		return common.ReconcileError(fmt.Errorf("set roster: %w", err))
	}

	return common.ReconcileSuccess()
}

// finishReconcile runs at end of Reconcile; return value is assigned to Reconcile's named recErr in defer.
func (r *SingleClusterReconciler) finishReconcile(ctx context.Context, result ctrl.Result, recErr error) error {
	logValues := common.ReconcileExitLogValues(result, recErr)

	statusErr := r.writeTerminalStatus(ctx, recErr)

	if recErr != nil {
		if statusErr != nil {
			recErr = errors.Join(
				recErr,
				fmt.Errorf("set AerospikeCluster error status: %w", statusErr),
			)
		}

		r.Log.Error(recErr, "Reconcile failed", logValues...)

		return recErr
	}

	if statusErr != nil {
		return fmt.Errorf("reset operation conditions: %w", statusErr)
	}

	r.Log.Info("Reconcile completed", logValues...)

	return nil
}

func (r *SingleClusterReconciler) recoverIgnorablePods(
	ctx context.Context, ignorablePodNames sets.Set[string]) common.ReconcileResult {
	podList, gErr := r.getClusterPodList(ctx)
	if gErr != nil {
		return common.ReconcileError(fmt.Errorf("list Pods: %w", gErr))
	}

	r.Log.V(1).Info("Try to recover failed/pending Pods if any")

	var deletedPodNames []string

	// Delete all failed ignorable pods in one pass; collect their names for
	// a single shared recovery wait after the loop.
	for idx := range podList.Items {
		if !ignorablePodNames.Has(podList.Items[idx].Name) {
			continue
		}

		if podState := utils.CheckServerFailedWithGrace(&podList.Items[idx], false); podState.State != utils.PodFailed {
			continue
		}

		if err := r.createOrUpdatePodServiceIfNeeded(ctx, []string{podList.Items[idx].Name}); err != nil {
			return common.ReconcileError(err)
		}

		if err := r.Delete(ctx, &podList.Items[idx]); err != nil {
			return common.ReconcileError(fmt.Errorf(
				"delete Pod %s: %w",
				utils.GetNamespacedNameString(&podList.Items[idx]), err,
			))
		}

		r.Log.Info("Deleted Pod", "pod", utils.GetNamespacedName(&podList.Items[idx]))

		deletedPodNames = append(deletedPodNames, podList.Items[idx].Name)
	}

	// No pods were failed — all ignorable pods are healthy.
	if len(deletedPodNames) == 0 {
		r.Log.Info("All ignorable pods are healthy; re-entering normal reconcile")

		return common.ReconcileRequeueAfter(10)
	}

	// Wait for all deleted pods together within a single 3-minute window.
	allReady, waitErr := r.waitForIgnorablePodsRecovery(ctx, deletedPodNames)

	if !allReady && waitErr == nil {
		// Timeout — some pods still not ready after 3 minutes.
		r.Log.Info("Some ignorable pods not ready after 3 minutes, will requeue")

		return common.ReconcileRequeueAfter(asdbv1.RequeueIntervalSeconds10)
	}

	if waitErr != nil {
		// Re-failed — don't requeue; next reconcile will be triggered by a CR change.
		r.Log.Error(waitErr, "One or more ignorable pods failed during recovery, won't requeue")

		return common.ReconcileSuccess()
	}

	r.Log.Info("Ignorable pods recovered, requeuing")

	return common.ReconcileRequeueAfter(1)
}

// waitForIgnorablePodsRecovery polls all named pods together within a single
// 3-minute window (18 × 10 s), resolving each pod as soon as its outcome is
// known. Returns:
//   - (true, nil)  — every pod's server container became ready
//   - (false, nil) — timeout elapsed with at least one pod still not ready
//   - (false, err) — at least one pod's server container entered a hard-failed
//     state; remaining pods may still be pending or ready
func (r *SingleClusterReconciler) waitForIgnorablePodsRecovery(ctx context.Context, podNames []string) (bool, error) {
	const (
		maxRetries    = 18 // 18 * 10s = 3 minutes
		retryInterval = 10 * time.Second
	)

	awaitingRecovery := sets.New(podNames...)

	var failureReasons []string

	for i := 0; i < maxRetries; i++ {
		for podName := range awaitingRecovery {
			pod := &corev1.Pod{}

			if err := r.Get(
				ctx,
				types.NamespacedName{Name: podName, Namespace: r.aeroCluster.Namespace},
				pod,
			); err != nil {
				if apierrors.IsNotFound(err) {
					// Still terminating / not yet recreated — keep waiting.
					continue
				}

				return false, fmt.Errorf("get Pod %s during recovery wait: %w", utils.GetNamespacedNameString(pod), err)
			}

			if podState := utils.CheckServerFailedWithGrace(pod, false); podState.State == utils.PodFailed {
				r.Log.V(1).Info("Ignorable pod server container failed during recovery",
					"pod", utils.GetNamespacedName(pod), "reason", podState.Reason)

				failureReasons = append(failureReasons,
					fmt.Sprintf("%s: %s", podName, podState.Reason))
				awaitingRecovery.Delete(podName)

				continue
			}

			if utils.IsAerospikeServerReady(pod) {
				r.Log.V(1).Info("Ignorable pod server container is ready", "pod", utils.GetNamespacedName(pod))
				awaitingRecovery.Delete(podName)
			}
		}

		if len(awaitingRecovery) == 0 {
			if len(failureReasons) > 0 {
				return false, fmt.Errorf("pods failed during recovery: %s",
					strings.Join(failureReasons, "; "))
			}

			return true, nil
		}

		r.Log.V(1).Info("Waiting for ignorable pods to recover",
			"pods", awaitingRecovery.UnsortedList(), "attempt", i+1)

		time.Sleep(retryInterval)
	}

	if len(failureReasons) > 0 {
		return false, fmt.Errorf("pods failed during recovery: %s",
			strings.Join(failureReasons, "; "))
	}

	return false, nil
}

func (r *SingleClusterReconciler) validateAndReconcileAccessControl(
	ctx context.Context,
	selectedPods []corev1.Pod,
	ignorablePodNames sets.Set[string],
) error {
	enabled, err := asdbv1.IsSecurityEnabled(r.aeroCluster.Spec.AerospikeConfig.Value)
	if err != nil {
		return fmt.Errorf("get cluster security status: %w", err)
	}

	if !enabled {
		r.Log.Info("Cluster is not security enabled, please enable security for this cluster")
		return nil
	}

	var conns []*deployment.HostConn

	// Create client
	if selectedPods == nil {
		conns, err = r.newAllHostConnWithOption(ctx, ignorablePodNames)
		if err != nil {
			return fmt.Errorf("get host connections for cluster nodes: %w", err)
		}
	} else {
		conns, err = r.newPodsHostConnWithOption(selectedPods, ignorablePodNames)
		if err != nil {
			return fmt.Errorf("get host connections for selected Pods: %w", err)
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
		return fmt.Errorf("create Aerospike client: %w", err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider()

	err = r.reconcileAccessControl(
		ctx, aeroClient, pp,
	)
	if err != nil {
		return err
	}

	r.Recorder.Eventf(
		r.aeroCluster, corev1.EventTypeNormal, "ACLUpdated",
		"Updated access control",
	)

	// Update the AerospikeCluster status.
	if err := r.updateAccessControlStatus(ctx); err != nil {
		r.Recorder.Eventf(
			r.aeroCluster, corev1.EventTypeWarning, ReasonStatusUpdateFailed,
			"Failed to update access control status",
		)

		return err
	}

	return nil
}

// operationConditions lists the "operation in progress" conditions
// paired with the reason used when each is in its resting (False) state.
// Paused is intentionally excluded: it reflects a user action (spec.paused=true),
// not an operator-driven operation.
var operationConditions = []struct {
	condType    string
	falseReason string
}{
	{string(asdbv1.AerospikeClusterConditionScalingUp), asdbv1.AerospikeClusterReasonNotScalingUp},
	{string(asdbv1.AerospikeClusterConditionScalingDown), asdbv1.AerospikeClusterReasonNotScalingDown},
	{string(asdbv1.AerospikeClusterConditionUpgrading), asdbv1.AerospikeClusterReasonNotUpgrading},
	{string(asdbv1.AerospikeClusterConditionRollingRestart), asdbv1.AerospikeClusterReasonNotRollingRestart},
	{
		string(asdbv1.AerospikeClusterConditionRackRevisionRollingOut),
		asdbv1.AerospikeClusterReasonNotRackRevisionRollingOut,
	},
}

// initPendingOpConditionReset takes ownership of the operation conditions for this reconcile: every
// one of them becomes eligible for reset unless a rack function claims it.
// Called immediately before the rack loop, deliberately as late as possible: every stage that runs
// earlier doesn't claim any operation condition.
func (r *SingleClusterReconciler) initPendingOpConditionReset() {
	r.computedState.pendingOpReset = sets.New[string]()

	for _, opCond := range operationConditions {
		r.computedState.pendingOpReset.Insert(opCond.condType)
	}
}

// opConditionAtRest returns the resting (False) form of an operation condition. Shared so the
// first-reconcile seed and the exit-path clear cannot drift on status or reason.
func opConditionAtRest(condType, falseReason string) metav1.Condition {
	return metav1.Condition{
		Type:   condType,
		Status: metav1.ConditionFalse,
		Reason: falseReason,
	}
}

// opConditionsToClear returns the resting form of every operation condition this pass is
// still allowed to clear. A condition claimed by a rack function is omitted, so an operation
// spanning a requeue keeps reporting.
//
// A claimed condition is omitted even when the operation completed: on the success path
// updateStatus clears it, atomically with Ready=True.
func (r *SingleClusterReconciler) opConditionsToClear() []metav1.Condition {
	conditions := make([]metav1.Condition, 0, len(operationConditions))

	for _, opCond := range operationConditions {
		if !r.computedState.pendingOpReset.Has(opCond.condType) {
			continue
		}

		conditions = append(conditions, opConditionAtRest(opCond.condType, opCond.falseReason))
	}

	return conditions
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

	// Carry forward conditions from r.aeroCluster (which is kept in sync by setConditions calls).
	// We must deep-copy the slice: patchStatus diffs r.aeroCluster (old) vs newAeroCluster (new).
	// A plain slice assignment shares the backing array, so in-place mutations by
	// SetStatusCondition would modify both old and new, producing no diff.
	newAeroCluster.Status.Conditions = slices.Clone(r.aeroCluster.Status.Conditions)

	// Set Ready=True on the success path. ObservedGeneration is always stamped — see
	// mergePatchStatus for why. patchStatus diffs the result, so an unchanged condition is free.
	apimeta.SetStatusCondition(&newAeroCluster.Status.Conditions, metav1.Condition{
		Type:               string(asdbv1.AerospikeClusterConditionReady),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: r.aeroCluster.Generation,
		Reason:             asdbv1.AerospikeClusterReasonReconcileComplete,
		Message:            "Cluster reconcile completed successfully",
	})

	// All operations are done at this point; set each operation condition to False.
	//
	// On the success path this is the only place a condition claimed by a completed operation is cleared.
	// opConditionsToClear omits claimed conditions by design, so an operation interrupted by an error keeps reporting.
	// Clearing here also keeps it atomic with Ready=True, so the two can never be observed disagreeing.
	for _, opCond := range operationConditions {
		atRest := opConditionAtRest(opCond.condType, opCond.falseReason)
		atRest.ObservedGeneration = r.aeroCluster.Generation

		apimeta.SetStatusCondition(&newAeroCluster.Status.Conditions, atRest)
	}

	// If IsReadinessProbeEnabled is not enabled, then only check for cluster readiness.
	// This is to avoid checking cluster readiness for every reconcile as once it is enabled, it will not be disabled.
	if !newAeroCluster.Status.IsReadinessProbeEnabled {
		clusterReadinessEnable, gErr := r.getClusterReadinessStatus(ctx)
		if gErr != nil {
			return fmt.Errorf("get cluster readiness status: %w", gErr)
		}

		newAeroCluster.Status.IsReadinessProbeEnabled = clusterReadinessEnable
	}

	selector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(newAeroCluster.Name))
	newAeroCluster.Status.Selector = selector.String()

	err = r.patchStatus(ctx, newAeroCluster)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	r.aeroCluster = newAeroCluster

	// Add the cluster phase metric
	r.addClusterPhaseMetric()

	r.Log.V(1).Info("Updated status", "status", newAeroCluster.Status)

	return nil
}

// mergePatchStatus applies an optional phase change and any number of conditions to the
// AerospikeCluster status in a single guarded merge patch. It is the shared primitive behind
// setConditions and writeTerminalStatus, and is called directly wherever a phase change and a
// condition change must land together.
//
// Whether anything changed is decided by SetStatusCondition's own return value, applied to a
// clone of just the conditions slice.
// Phase is applied only when it differs. Only status fields and resourceVersion are copied back
// onto r.aeroCluster, so the spec is never overwritten.
func (r *SingleClusterReconciler) mergePatchStatus(
	ctx context.Context, phase *asdbv1.AerospikeClusterPhase, conditions ...metav1.Condition,
) error {
	candidate := slices.Clone(r.aeroCluster.Status.Conditions)
	condChanged := false

	// Never return early from this loop: every True condition must be claimed even when it
	// needs no patch.
	for i := range conditions {
		if conditions[i].Status == metav1.ConditionTrue && r.computedState.pendingOpReset != nil {
			r.computedState.pendingOpReset.Delete(conditions[i].Type)
		}

		// Copy so the caller's input slice is not mutated.
		cond := conditions[i]
		cond.ObservedGeneration = r.aeroCluster.Generation

		if apimeta.SetStatusCondition(&candidate, cond) {
			condChanged = true
		}
	}

	phaseChanged := phase != nil && r.aeroCluster.Status.Phase != *phase

	if !condChanged && !phaseChanged {
		r.addClusterPhaseMetric()
		return nil
	}

	patchTarget := r.aeroCluster.DeepCopy()
	patch := client.MergeFrom(r.aeroCluster.DeepCopy())

	patchTarget.Status.Conditions = candidate

	if phaseChanged {
		patchTarget.Status.Phase = *phase
	}

	if err := r.Client.Status().Patch(ctx, patchTarget, patch); err != nil {
		return err
	}

	// Copy back only status and resourceVersion so r.aeroCluster.Spec is never overwritten.
	r.aeroCluster.Status.Conditions = patchTarget.Status.Conditions
	r.aeroCluster.Status.Phase = patchTarget.Status.Phase
	r.aeroCluster.ResourceVersion = patchTarget.ResourceVersion

	if phaseChanged {
		r.addClusterPhaseMetric()
	}

	return nil
}

// maxConditionMessageLength bounds what we write into a condition's message. The CRD caps
// the field at 32768; an AerospikeCluster error chain can embed config diffs and pod lists,
// and an overlong message makes the API server reject the whole patch — which would lose the
// phase=Error write riding along with it.
const maxConditionMessageLength = 2048

// truncateConditionMessage bounds msg, backing off to a rune boundary so the result stays
// valid UTF-8 (invalid UTF-8 in a JSON string is itself rejected).
func truncateConditionMessage(msg string) string {
	if len(msg) <= maxConditionMessageLength {
		return msg
	}

	const suffix = "... (truncated)"

	cut := maxConditionMessageLength - len(suffix)
	for cut > 0 && !utf8.RuneStart(msg[cut]) {
		cut--
	}

	return msg[:cut] + suffix
}

// setConditions updates one or more conditions on the AerospikeCluster status using a
// merge patch.
// ObservedGeneration is stamped only when a condition's Status, Reason, or Message actually
// changes — consistent with how LastTransitionTime behaves. Conditions already in the
// desired state are left untouched (no ObservedGeneration bump, no API call).
func (r *SingleClusterReconciler) setConditions(ctx context.Context, conditions ...metav1.Condition) error {
	return r.mergePatchStatus(ctx, nil, conditions...)
}

// initializeConditionsIfNeeded pre-seeds all conditions on the very first reconcile
// so that `kubectl wait --for=condition=Ready` and similar commands do not hang,
// and the status shape is consistent from the start.
func (r *SingleClusterReconciler) initializeConditionsIfNeeded(ctx context.Context) error {
	if len(r.aeroCluster.Status.Conditions) > 0 {
		return nil
	}

	seedConditions := []metav1.Condition{
		{
			Type:    string(asdbv1.AerospikeClusterConditionReady),
			Status:  metav1.ConditionUnknown,
			Reason:  asdbv1.AerospikeClusterReasonInitializing,
			Message: "Cluster conditions not yet evaluated",
		},
		// Paused is not an operation condition but still needs an initial resting state.
		{
			Type:   string(asdbv1.AerospikeClusterConditionPaused),
			Status: metav1.ConditionFalse,
			Reason: asdbv1.AerospikeClusterReasonNotPaused,
		},
	}

	// Every operation condition seeds in the same resting state the exit path clears to.
	for _, opCond := range operationConditions {
		seedConditions = append(seedConditions, opConditionAtRest(opCond.condType, opCond.falseReason))
	}

	return r.setConditions(ctx, seedConditions...)
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
		return fmt.Errorf("update access control status: %w", err)
	}

	r.Log.V(1).Info("Updated access control status", "status", newAeroCluster.Status)

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
		return fmt.Errorf("create status: %w", err)
	}

	return nil
}

func (r *SingleClusterReconciler) patchStatus(ctx context.Context, newAeroCluster *asdbv1.AerospikeCluster) error {
	oldAeroCluster := r.aeroCluster

	oldJSON, err := json.Marshal(oldAeroCluster)
	if err != nil {
		return fmt.Errorf("marshal old status: %w", err)
	}

	newJSON, err := json.Marshal(newAeroCluster)
	if err != nil {
		return fmt.Errorf("marshal new status: %w", err)
	}

	jsonPatchPatch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("create JSON patch: %w", err)
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
		"Filtered status patch ", "patch", filteredPatch, "oldStatus",
		oldAeroCluster.Status, "newStatus", newAeroCluster.Status,
	)

	jsonPatchJSON, err := json.Marshal(filteredPatch)
	if err != nil {
		return fmt.Errorf("marshal JSON patch: %w", err)
	}

	patch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)

	if err = r.Client.Status().Patch(
		ctx, &asdbv1.AerospikeCluster{ObjectMeta: metav1.ObjectMeta{
			Name:      oldAeroCluster.Name,
			Namespace: oldAeroCluster.Namespace,
		}}, patch,
		client.FieldOwner(patchFieldOwner),
	); err != nil {
		return fmt.Errorf("patch status: %w", err)
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
	r.Log.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterSTSList(ctx)
	if err != nil {
		return fmt.Errorf("get StatefulSets while forcing recreate of cluster (status is nil): %w", err)
	}

	r.Log.V(1).Info(
		"Found StatefulSet for cluster. Need to delete them", "nSTS",
		len(statefulSetList.Items),
	)

	for idx := range statefulSetList.Items {
		statefulset := &statefulSetList.Items[idx]
		if err := r.deleteSTS(ctx, statefulset); err != nil {
			return fmt.Errorf(
				"delete StatefulSet %s while forcing recreate of cluster (status is nil): %w",
				utils.GetNamespacedNameString(statefulset), err,
			)
		}
	}

	// Delete all PVCs for the cluster unconditionally, regardless of the cascadeDelete flag on
	// individual volumes. During a failed-create recovery the cluster must start completely fresh.
	if err := r.deleteAllClusterPVCsForce(ctx); err != nil {
		return fmt.Errorf("delete cluster PVCs while forcing recreate: %w", err)
	}

	// Clear pod status, mesh references, and per-pod services.
	// This is not necessary since scale-up would clean dangling pod status. However, done here for
	// general cleanliness.
	rackStateList := getConfiguredRackStateList(r.aeroCluster)
	for rackIdx := range rackStateList {
		state := rackStateList[rackIdx]

		pods, err := r.getRackPodList(ctx, state.Rack.ID, state.Rack.Revision)
		if err != nil {
			return fmt.Errorf(
				"list Pods for rack %d during failed cluster recovery: %w",
				state.Rack.ID, err,
			)
		}

		newPodNames := make([]string, 0)
		for podIdx := 0; podIdx < len(pods.Items); podIdx++ {
			newPodNames = append(newPodNames, pods.Items[podIdx].Name)
		}

		if err := r.cleanupPodMeshAndStatus(ctx, newPodNames); err != nil {
			return fmt.Errorf(
				"clean up Pod mesh and status for rack %d during failed cluster recovery: %w",
				state.Rack.ID, err,
			)
		}
	}

	// Clear ACL status so that the next reconcile applies credentials fresh
	// against the default admin account. When STSes and PVCs are deleted above,
	// all user/credential data on the Aerospike nodes is wiped. If the stale
	// ACL status is left intact, the operator would attempt to authenticate with
	// those old credentials on the newly recreated nodes, causing info commands
	// to fail.
	if err := r.clearAerospikeAccessControlStatus(ctx); err != nil {
		return fmt.Errorf("clear access control status during cluster recovery: %w", err)
	}

	return fmt.Errorf("forcing recreate of cluster: status is nil")
}

// clearAerospikeAccessControlStatus sets AerospikeAccessControl to nil in the
// CR status. This is called during recoverFailedCreate so that the operator
// does not attempt to authenticate with stale credentials against freshly
// recreated Aerospike nodes that have no user data.
func (r *SingleClusterReconciler) clearAerospikeAccessControlStatus(ctx context.Context) error {
	if r.aeroCluster.Status.AerospikeAccessControl == nil {
		return nil
	}

	newAeroCluster := &asdbv1.AerospikeCluster{}
	if err := r.Get(
		ctx, types.NamespacedName{
			Name: r.aeroCluster.Name, Namespace: r.aeroCluster.Namespace,
		}, newAeroCluster,
	); err != nil {
		return fmt.Errorf("get AerospikeCluster for access control status clear: %w", err)
	}

	newAeroCluster.Status.AerospikeAccessControl = nil

	if err := r.patchStatus(ctx, newAeroCluster); err != nil {
		return fmt.Errorf("clear access control status: %w", err)
	}

	r.Log.Info("Cleared access control status for cluster recovery")

	return nil
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
	r.Log.Info("Removing PVC for removed cluster")

	// Delete pvc for all rack storage
	for idx := range r.aeroCluster.Spec.RackConfig.Racks {
		rack := &r.aeroCluster.Spec.RackConfig.Racks[idx]

		rackPVCItems, err := r.getRackPVCList(ctx, rack.ID, rack.Revision)
		if err != nil {
			return fmt.Errorf("find PVCs for rack %d: %w", rack.ID, err)
		}

		storage := rack.Storage
		if _, err := r.removePVCsAsync(ctx, &storage, rackPVCItems); err != nil {
			return fmt.Errorf("remove PVCs for rack %d: %w", rack.ID, err)
		}
	}

	// Delete PVCs for any remaining old removed racks
	pvcItems, err := r.getClusterPVCList(ctx)
	if err != nil {
		return fmt.Errorf("find PVC for cluster: %w", err)
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
		return fmt.Errorf("remove cluster PVCs: %w", err)
	}

	return nil
}

func (r *SingleClusterReconciler) handleClusterDeletion(ctx context.Context, finalizerName string) error {
	r.Log.Info("Handle cluster deletion")

	// The cluster is being deleted
	if err := r.cleanUpAndRemoveFinalizer(ctx, finalizerName); err != nil {
		return fmt.Errorf("remove finalizer: %w", err)
	}

	return nil
}

func (r *SingleClusterReconciler) checkPreviouslyFailedCluster(ctx context.Context) (bool, common.ReconcileResult) {
	// Fast path: non-empty status means the cluster was previously initialized.
	if !r.IsStatusEmpty() {
		return false, common.ReconcileSuccess()
	}

	// Status is empty — distinguish a new cluster from a failed create via STS presence.
	stsList, err := r.getClusterSTSList(ctx)
	if err != nil {
		return false, common.ReconcileError(err)
	}

	if len(stsList.Items) == 0 {
		r.Log.V(1).Info("New cluster, creating empty status object")

		if err = r.createStatus(ctx); err != nil {
			return false, common.ReconcileError(err)
		}

		return false, common.ReconcileSuccess()
	}

	// StatefulSets exist but status is empty: either the cluster is still being
	// created or it failed during its initial create. Inspect pod states to
	// decide whether to recover, requeue, or proceed normally.
	r.Log.V(1).Info("Cluster status is empty with existing StatefulSets, checking Pod states")

	pods, err := r.getClusterPodList(ctx)
	if err != nil {
		return false, common.ReconcileError(err)
	}

	inGracePeriod := false

	for idx := range pods.Items {
		podState := utils.CheckPodFailedWithGrace(&pods.Items[idx], true)

		switch podState.State {
		case utils.PodHealthy:
			// At least one pod has not yet failed — the cluster is still coming
			// up or recovering on its own. Don't trigger recreate.
			return false, common.ReconcileSuccess()
		case utils.PodFailedInGrace:
			inGracePeriod = true
		case utils.PodFailed:
			// Hard-failed — keep iterating; grace-period pods take precedence.
		}
	}

	if inGracePeriod {
		r.Log.Info("Pods are failed but within grace period, requeueing")
		return false, common.ReconcileRequeueAfter(asdbv1.RequeueIntervalSeconds10)
	}

	// All pods have hard-failed and status is empty — the cluster failed during
	// its initial create and needs to be recovered.
	if err := r.recoverFailedCreate(ctx); err != nil {
		return true, common.ReconcileError(err)
	}

	return true, common.ReconcileSuccess()
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
			return fmt.Errorf("cluster is not ready for migration, Pod status is not populated")
		}

		if err := r.migrateInitialisedVolumeNames(ctx); err != nil {
			return fmt.Errorf("patch initialised volumes: %w", err)
		}
	}

	if err := r.AddAPIVersionLabel(ctx); err != nil {
		return fmt.Errorf("patch API version label: %w", err)
	}

	return nil
}

func (r *SingleClusterReconciler) migrateInitialisedVolumeNames(ctx context.Context) error {
	r.Log.Info("Migrating Initialised Volumes name to new format")

	podList, err := r.getClusterPodList(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
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
			return fmt.Errorf("empty status for Pod %s in CR",
				utils.GetNamespacedNameString(pod))
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
					return fmt.Errorf("volume %s: empty pvcUID", oldFormatInitVolNames[oldVolIdx])
				}

				// Appending volume name as <vol_name>@<pvcUID> in initializedVolumes list
				initializedVolumes = append(
					initializedVolumes, fmt.Sprintf("%s@%s", oldFormatInitVolNames[oldVolIdx], pvcUID),
				)
			}
		}

		if len(initializedVolumes) > len(r.aeroCluster.Status.Pods[pod.Name].InitializedVolumes) {
			r.Log.Info("Got updated initialised volumes list",
				"initVolumes", initializedVolumes, "pod", utils.GetNamespacedName(pod))

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
