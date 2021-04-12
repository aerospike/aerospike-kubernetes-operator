package aerospikecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	as "github.com/ashishshinde/aerospike-client-go"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/configmap"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/jsonpatch"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"
	log "github.com/inconshreveable/log15"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const defaultUser = "admin"
const defaultPass = "admin"
const patchFieldOwner = "aerospike-kuberneter-operator"

// Number of reconcile threads to run reconcile operations
var maxConcurrentReconciles = runtime.NumCPU() * 2

var (
	updateOption = &client.UpdateOptions{
		FieldManager: "aerospike-operator",
	}
	createOption = &client.CreateOptions{
		FieldManager: "aerospike-operator",
	}
)

// Add creates a new AerospikeCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileAerospikeCluster{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("aerospikecluster-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: maxConcurrentReconciles})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource AerospikeCluster
	err = c.Watch(
		&source.Kind{Type: &aerospikev1alpha1.AerospikeCluster{}},
		&handler.EnqueueRequestForObject{},
		// Skip where cluster object generation is not changed
		predicate.GenerationChangedPredicate{})
	if err != nil {
		return err
	}

	// TODO: Do we need to monitor this? Statefulset is updated many times in reconcile and this add new entry in
	// update queue. If user will change only cr then we may not need to monitor statefulset.
	// Think all possible situation

	// Watch for changes to secondary resource StatefulSet and requeue the owner AerospikeCluster
	err = c.Watch(
		&source.Kind{Type: &appsv1.StatefulSet{}},
		&handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &aerospikev1alpha1.AerospikeCluster{},
		}, predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
		})
	if err != nil {
		return err
	}

	return nil
}

var pkglog = log.New(log.Ctx{"module": "controller_aerospikecluster"})

// blank assignment to verify that ReconcileAerospikeCluster implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileAerospikeCluster{}

// ReconcileAerospikeCluster reconciles a AerospikeCluster object
type ReconcileAerospikeCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *k8sRuntime.Scheme
}

// RackState contains the rack configuration and rack size.
type RackState struct {
	Rack aerospikev1alpha1.Rack
	Size int
}

// Reconcile AerospikeCluster object
func (r *ReconcileAerospikeCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": request.NamespacedName})
	logger.Info("Reconciling AerospikeCluster")

	// Fetch the AerospikeCluster instance
	aeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, aeroCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	logger.Debug("AerospikeCluster", log.Ctx{"Spec": utils.PrettyPrint(aeroCluster.Spec), "Status": utils.PrettyPrint(aeroCluster.Status)})

	finalizerName := "storage.finalizer"

	// Check DeletionTimestamp to see if cluster is being deleted
	if !aeroCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		// TODO: LOG FOR CLUSTER DELETION
		// The cluster is being deleted
		if err := r.handleClusterDeletion(aeroCluster, finalizerName); err != nil {
			return reconcile.Result{}, err
		}
		// Stop reconciliation as the cluster is being deleted
		return reconcile.Result{}, nil
	}

	// The cluster is not being deleted, add finalizer in not added already
	if err := r.addFinalizer(aeroCluster, finalizerName); err != nil {
		logger.Error("Failed to add finalizer", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// Handle previously failed cluster
	if err := r.handlePreviouslyFailedCluster(aeroCluster); err != nil {
		return reconcile.Result{}, err
	}

	// Reconcile all racks
	if res := r.reconcileRacks(aeroCluster); !res.isSuccess {
		return res.result, res.err
	}

	// Check if there is any node with quiesce status. We need to undo that
	// It may have been left from previous steps
	allHostConns, err := r.newAllHostConn(aeroCluster)
	if err != nil {
		e := fmt.Errorf("Failed to get hostConn for aerospike cluster nodes: %v", err)
		logger.Error("Failed to get hostConn for aerospike cluster nodes", log.Ctx{"err": err})
		return reconcile.Result{}, e
	}
	if err := deployment.InfoQuiesceUndo(r.getClientPolicy(aeroCluster), allHostConns); err != nil {
		logger.Error("Failed to check for Quiesced nodes", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// Setup access control.
	if err := r.reconcileAccessControl(aeroCluster); err != nil {
		logger.Error("Failed to reconcile access control", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// Update the AerospikeCluster status.
	if err := r.updateStatus(aeroCluster); err != nil {
		logger.Error("Failed to update AerospikeCluster status", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAerospikeCluster) handleClusterDeletion(aeroCluster *aerospikev1alpha1.AerospikeCluster, finalizerName string) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Handle cluster deletion")

	// The cluster is being deleted
	if err := r.cleanUpAndremoveFinalizer(aeroCluster, finalizerName); err != nil {
		logger.Error("Failed to remove finalizer", log.Ctx{"err": err})
		return err
	}
	return nil
}

func (r *ReconcileAerospikeCluster) handlePreviouslyFailedCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Handle previously failed cluster")

	isNew, err := r.isNewCluster(aeroCluster)
	if err != nil {
		return fmt.Errorf("Error determining if cluster is new: %v", err)
	}

	if isNew {
		logger.Debug("It's new cluster, create empty status object")
		if err := r.createStatus(aeroCluster); err != nil {
			return err
		}
	} else {
		logger.Debug("It's not a new cluster, check if it is failed and needs recovery")
		hasFailed, err := r.hasClusterFailed(aeroCluster)
		if err != nil {
			return fmt.Errorf("Error determining if cluster has failed: %v", err)
		}

		if hasFailed {
			return r.recoverFailedCreate(aeroCluster)
		}
	}
	return nil
}

func (r *ReconcileAerospikeCluster) getResourceVersion(stsName types.NamespacedName) string {
	found := &appsv1.StatefulSet{}
	r.client.Get(context.TODO(), stsName, found)
	return found.ObjectMeta.ResourceVersion
}

func (r *ReconcileAerospikeCluster) reconcileRacks(aeroCluster *aerospikev1alpha1.AerospikeCluster) reconcileResult {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Reconciling rack for AerospikeCluster")

	var scaledDownRackSTSList []appsv1.StatefulSet
	var scaledDownRackList []RackState
	var res reconcileResult

	rackStateList := getNewRackStateList(aeroCluster)
	racksToDelete, err := r.getRacksToDelete(aeroCluster, rackStateList)
	if err != nil {
		return reconcileError(err)
	}

	ignorablePods, err := r.getIgnorablePods(aeroCluster, racksToDelete)
	if err != nil {
		return reconcileError(err)
	}

	ignorablePodNames := []string{}
	for _, pod := range ignorablePods {
		ignorablePodNames = append(ignorablePodNames, pod.Name)
	}

	logger.Info("Rack changes", log.Ctx{"racksToDelete": racksToDelete, "ignorablePods": ignorablePodNames})

	for _, state := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, state.Rack.ID)
		if err := r.client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return reconcileError(err)
			}

			// Create statefulset with 0 size rack and then scaleUp later in reconcile
			zeroSizedRack := RackState{Rack: state.Rack, Size: 0}
			found, res = r.createRack(aeroCluster, zeroSizedRack)
			if !res.isSuccess {
				return res
			}
		}

		// Get list of scaled down racks
		if *found.Spec.Replicas > int32(state.Size) {
			scaledDownRackSTSList = append(scaledDownRackSTSList, *found)
			scaledDownRackList = append(scaledDownRackList, state)
		} else {
			// Reconcile other statefulset
			if res := r.reconcileRack(aeroCluster, found, state, ignorablePods); !res.isSuccess {
				return res
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx, state := range scaledDownRackList {
		if res := r.reconcileRack(aeroCluster, &scaledDownRackSTSList[idx], state, ignorablePods); !res.isSuccess {
			return res
		}
	}

	if len(aeroCluster.Status.RackConfig.Racks) != 0 {
		// Remove removed racks
		if res := r.deleteRacks(aeroCluster, racksToDelete, ignorablePods); !res.isSuccess {
			if res.err != nil {
				logger.Error("Failed to remove statefulset for removed racks", log.Ctx{"err": res.err})
			}
			return res
		}
	}

	return reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) createRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Create new Aerospike cluster if needed")

	// NoOp if already exist
	logger.Info("AerospikeCluster", log.Ctx{"Spec": aeroCluster.Spec})
	if err := r.createHeadlessSvc(aeroCluster); err != nil {
		logger.Error("Failed to create headless service", log.Ctx{"err": err})
		return nil, reconcileError(err)
	}

	// Bad config should not come here. It should be validated in validation hook
	cmName := getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID)
	if err := r.buildConfigMap(aeroCluster, cmName, rackState.Rack); err != nil {
		logger.Error("Failed to create configMap from AerospikeConfig", log.Ctx{"err": err})
		return nil, reconcileError(err)
	}

	stsName := getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)
	found, err := r.createStatefulSet(aeroCluster, stsName, rackState)
	if err != nil {
		logger.Error("Statefulset setup failed. Deleting statefulset", log.Ctx{"name": stsName, "err": err})
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		r.deleteStatefulSet(aeroCluster, found)
		return nil, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) getRacksToDelete(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackStateList []RackState) ([]int, error) {
	oldRackIDs, err := r.getOldRackList(aeroCluster)

	if err != nil {
		return nil, err
	}

	toDelete := []int{}
	for _, rackID := range oldRackIDs {
		var rackFound bool
		for _, newRack := range rackStateList {
			if rackID == newRack.Rack.ID {
				rackFound = true
				break
			}
		}

		if !rackFound {
			toDelete = append(toDelete, rackID)
		}
	}

	return toDelete, nil
}

// getIgnorablePods returns pods from racksToDelete that are currently not running and can be ignored in stability checks.
func (r *ReconcileAerospikeCluster) getIgnorablePods(aeroCluster *aerospikev1alpha1.AerospikeCluster, racksToDelete []int) ([]corev1.Pod, error) {
	ignorablePods := []corev1.Pod{}
	for _, rackID := range racksToDelete {
		rackPods, err := r.getRackPodList(aeroCluster, rackID)

		if err != nil {
			return nil, err
		}

		for _, pod := range rackPods.Items {
			if !utils.IsPodRunningAndReady(&pod) {
				ignorablePods = append(ignorablePods, pod)
			}
		}
	}
	return ignorablePods, nil
}

func (r *ReconcileAerospikeCluster) deleteRacks(aeroCluster *aerospikev1alpha1.AerospikeCluster, racksToDelete []int, ignorablePods []corev1.Pod) reconcileResult {
	for _, rackID := range racksToDelete {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, rackID)
		err := r.client.Get(context.TODO(), stsName, found)
		if err != nil {
			// If not found then go to next
			if errors.IsNotFound(err) {
				continue
			}
			return reconcileError(err)
		}
		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		found, res := r.scaleDownRack(aeroCluster, found, RackState{Size: 0, Rack: aerospikev1alpha1.Rack{ID: rackID}}, ignorablePods)
		if !res.isSuccess {
			return res
		}

		// Delete sts
		if err := r.deleteStatefulSet(aeroCluster, found); err != nil {
			return reconcileError(err)
		}
	}
	return reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) reconcileRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod) reconcileResult {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})
	logger.Info("Reconcile existing Aerospike cluster statefulset")

	var err error
	var res reconcileResult

	logger.Info("Ensure rack StatefulSet size is the same as the spec")
	desiredSize := int32(rackState.Size)
	// Scale down
	if *found.Spec.Replicas > desiredSize {
		found, res = r.scaleDownRack(aeroCluster, found, rackState, ignorablePods)
		if !res.isSuccess {
			if res.err != nil {
				logger.Error("Failed to scaleDown StatefulSet pods", log.Ctx{"err": res.err})
			}
			return res
		}
	}

	// Always update configMap. We won't be able to find if a rack's config and it's pod config is in sync or not
	// Checking rack.spec, rack.status will not work.
	// We may change config, let some pods restart with new config and then change config back to original value.
	// Now rack.spec, rack.status will be same but few pods will have changed config.
	// So a check based on spec and status will skip configMap update.
	// Hence a rolling restart of pod will never bring pod to desired config
	if err := r.updateConfigMap(aeroCluster, getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID), rackState.Rack); err != nil {
		logger.Error("Failed to update configMap from AerospikeConfig", log.Ctx{"err": err})
		return reconcileError(err)
	}

	// Upgrade
	upgradeNeeded, err := r.isAeroClusterUpgradeNeeded(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return reconcileError(err)
	}

	if upgradeNeeded {
		found, res = r.upgradeRack(aeroCluster, found, aeroCluster.Spec.Image, rackState)
		if !res.isSuccess {
			if res.err != nil {
				logger.Error("Failed to update StatefulSet image", log.Ctx{"err": res.err})
			}
			return res
		}
	} else {
		needRollingRestartRack, err := r.needRollingRestartRack(aeroCluster, rackState)
		if err != nil {
			return reconcileError(err)
		}
		if needRollingRestartRack {
			found, res = r.rollingRestartRack(aeroCluster, found, rackState)
			if !res.isSuccess {
				if res.err != nil {
					logger.Error("Failed to do rolling restart", log.Ctx{"err": res.err})
				}
				return res
			}
		}
	}

	// Scale up after upgrading, so that new pods comeup with new image
	if *found.Spec.Replicas < desiredSize {
		found, res = r.scaleUpRack(aeroCluster, found, rackState)
		if !res.isSuccess {
			logger.Error("Failed to scaleUp StatefulSet pods", log.Ctx{"err": res.err})
			return res
		}
	}

	// TODO: check if all the pods are up or not
	return reconcileSuccess()
}

func isClusterAerospikeConfigSecretUpdated(aeroCluster *aerospikev1alpha1.AerospikeCluster) bool {
	return !reflect.DeepEqual(aeroCluster.Spec.AerospikeConfigSecret, aeroCluster.Status.AerospikeConfigSecret)
}

func (r *ReconcileAerospikeCluster) needRollingRestartRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState) (bool, error) {
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return false, fmt.Errorf("Failed to list pods: %v", err)
	}
	for _, pod := range podList {
		// Check if this pod need restart
		needRollingRestart, err := r.needRollingRestartPod(aeroCluster, rackState, pod)
		if err != nil {
			return false, err
		}
		if needRollingRestart {
			return true, nil
		}
	}
	return false, nil
}

func (r *ReconcileAerospikeCluster) isAerospikeConfigUpdatedForRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState) bool {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	// AerospikeConfig nil means status not updated yet
	if aeroCluster.Status.AerospikeConfig == nil {
		return false
	}
	// TODO: Should we use some other check? Reconcile may requeue request multiple times before updating status and
	// then this func will return true until status is updated.
	// No need to check global AerospikeConfig. Racks will always have
	// Only Check if rack AerospikeConfig has changed
	for _, statusRack := range aeroCluster.Status.RackConfig.Racks {
		if rackState.Rack.ID == statusRack.ID {
			if !reflect.DeepEqual(rackState.Rack.AerospikeConfig, statusRack.AerospikeConfig) {
				logger.Info("Rack AerospikeConfig changed. Need rolling restart", log.Ctx{"oldRackConfig": statusRack, "newRackConfig": rackState.Rack})
				return true
			}

			break
		}
	}

	return false
}

func (r *ReconcileAerospikeCluster) scaleUpRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	desiredSize := int32(rackState.Size)

	oldSz := *found.Spec.Replicas
	found.Spec.Replicas = &desiredSize

	logger.Info("Scaling up pods", log.Ctx{"currentSz": oldSz, "desiredSz": desiredSize})

	// No need for this? But if image is bad then new pod will also comeup with bad node.
	podList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(aeroCluster, podList.Items) {
		return found, reconcileError(fmt.Errorf("Cannot scale up AerospikeCluster. A pod is already in failed state"))
	}

	newPodNames := []string{}
	for i := oldSz; i < desiredSize; i++ {
		newPodNames = append(newPodNames, getStatefulSetPodName(found.Name, i))
	}

	// Ensure none of the to be launched pods are active.
	for _, newPodName := range newPodNames {
		for _, pod := range podList.Items {
			if pod.Name == newPodName {
				return found, reconcileError(fmt.Errorf("Pod %s yet to be launched is still present", newPodName))
			}
		}
	}

	// Clean up any dangling resources associated with the new pods.
	// This implements a safety net to protect scale up against failed cleanup operations when cluster
	// is scaled down.
	cleanupPods := []string{}
	cleanupPods = append(cleanupPods, newPodNames...)

	// Find dangling pods in pods
	if aeroCluster.Status.Pods != nil {
		for podName := range aeroCluster.Status.Pods {
			ordinal, err := getStatefulSetPodOrdinal(podName)
			if err != nil {
				return found, reconcileError(fmt.Errorf("Invalid pod name: %s", podName))
			}

			if *ordinal >= desiredSize {
				cleanupPods = append(cleanupPods, podName)
			}
		}
	}

	err = r.cleanupPods(aeroCluster, cleanupPods, rackState)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed scale up pre-check: %v", err))
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for each pod
		for _, podName := range newPodNames {
			if err := r.createServiceForPod(aeroCluster, podName, aeroCluster.Namespace); err != nil {
				return found, reconcileError(err)
			}
		}
	}

	// Scale up the statefulset
	if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
		return found, reconcileError(fmt.Errorf("Failed to update StatefulSet pods: %v", err))
	}

	if err := r.waitForStatefulSetToBeReady(found); err != nil {
		return found, reconcileError(fmt.Errorf("Failed to wait for statefulset to be ready: %v", err))
	}

	// return a fresh copy
	found, err = r.getStatefulSet(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) upgradeRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredImage string, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed to list pods: %v", err))
	}

	// Update strategy for statefulSet is OnDelete, so client.Update will not start update.
	// Update will happen only when a pod is deleted.
	// So first update image and then delete a pod. Pod will come up with new image.
	// Repeat the above process.
	needsUpdate := false
	for i, container := range found.Spec.Template.Spec.Containers {
		desiredImage, err := utils.GetDesiredImage(aeroCluster, container.Name)

		if err != nil {
			// Maybe a deleted sidecar.
			continue
		}

		if !utils.IsImageEqual(container.Image, desiredImage) {
			logger.Info("Updating image in statefulset spec", log.Ctx{"container": container.Name, "desiredImage": desiredImage, "currentImage": container.Image})
			found.Spec.Template.Spec.Containers[i].Image = desiredImage
			needsUpdate = true
		}
	}

	if needsUpdate {
		err = r.client.Update(context.TODO(), found, updateOption)
		if err != nil {
			return found, reconcileError(fmt.Errorf("Failed to update image for StatefulSet %s: %v", found.Name, err))
		}
	}

	for _, p := range podList {
		logger.Info("Check if pod needs upgrade or not")
		var needPodUpgrade bool
		for _, ps := range p.Spec.Containers {
			desiredImage, err := utils.GetDesiredImage(aeroCluster, ps.Name)
			if err != nil {
				continue
			}

			if !utils.IsImageEqual(ps.Image, desiredImage) {
				logger.Info("Upgrading/downgrading pod", log.Ctx{"podName": p.Name, "currentImage": ps.Image, "desiredImage": desiredImage})
				needPodUpgrade = true
				break
			}
		}

		if !needPodUpgrade {
			logger.Info("Pod doesn't need upgrade")
			continue
		}

		// Also check if statefulSet is in stable condition
		// Check for all containers. Status.ContainerStatuses doesn't include init container
		res := r.ensurePodImageUpdated(aeroCluster, desiredImage, rackState, p)
		if !res.isSuccess {
			return found, res
		}

		// Handle the next pod in subsequent reconcile.
		return found, reconcileRequeueAfter(0)
	}

	// return a fresh copy
	found, err = r.getStatefulSet(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) ensurePodImageUpdated(aeroCluster *aerospikev1alpha1.AerospikeCluster, desiredImage string, rackState RackState, p corev1.Pod) reconcileResult {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	needsDeletion := false
	// Also check if statefulSet is in stable condition
	// Check for all containers. Spec.Containers doesn't include init container
	for _, ps := range p.Spec.Containers {
		desiredImage, err := utils.GetDesiredImage(aeroCluster, ps.Name)

		if err != nil {
			// Maybe a deleted sidecar.
			continue
		}

		if utils.IsImageEqual(ps.Image, desiredImage) {
			if err := utils.CheckPodFailed(&p); err != nil {
				// Looks like bad image
				return reconcileError(err)
			}
		}

		if !utils.IsImageEqual(ps.Image, desiredImage) {
			logger.Info("Upgrading/downgrading pod", log.Ctx{"podName": p.Name, "currentImage": ps.Image, "desiredImage": desiredImage})
			needsDeletion = true
			break
		}
	}

	if needsDeletion {
		logger.Debug("Delete the Pod", log.Ctx{"podName": p.Name})

		// If already dead node, so no need to check node safety, migration
		if err := utils.CheckPodFailed(&p); err == nil {
			if res := r.waitForNodeSafeStopReady(aeroCluster, &p, nil); !res.isSuccess {
				return res
			}
		}

		// Delete pod
		if err := r.client.Delete(context.TODO(), &p); err != nil {
			return reconcileError(err)
		}
		logger.Debug("Pod deleted", log.Ctx{"podName": p.Name})

		// Wait for pod to come up
		const maxRetries = 6
		const retryInterval = time.Second * 10
		var isUpgraded bool
		for i := 0; i < maxRetries; i++ {
			logger.Debug("Waiting for pod to be ready after delete", log.Ctx{"podName": p.Name})

			pFound := &corev1.Pod{}
			err := r.client.Get(context.TODO(), types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, pFound)
			if err != nil {
				logger.Error("Failed to get pod", log.Ctx{"podName": p.Name, "err": err})

				if _, err = r.getStatefulSet(aeroCluster, rackState); err != nil {
					// Stateful set has been deleted.
					// TODO Ashish to rememeber which scenario this can happen.
					logger.Error("Statefulset has been deleted for pod", log.Ctx{"podName": p.Name, "err": err})
					return reconcileError(err)
				}

				time.Sleep(retryInterval)
				continue
			}
			if err := utils.CheckPodFailed(pFound); err != nil {
				return reconcileError(err)
			}

			if utils.IsPodUpgraded(pFound, aeroCluster) {
				isUpgraded = true
				logger.Info("Pod is upgraded/downgraded", log.Ctx{"podName": p.Name})
				break
			}

			logger.Debug("Waiting for pod to come up with new image", log.Ctx{"podName": p.Name})
			time.Sleep(retryInterval)
		}

		if !isUpgraded {
			logger.Info("Timed out waiting for pod to come up with new image", log.Ctx{"podName": p.Name})
			return reconcileRequeueAfter(10)
		}
	}

	return reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) rollingRestartRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, reconcileResult) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	logger.Info("Rolling restart AerospikeCluster statefulset nodes with new config")

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed to list pods: %v", err))
	}
	if r.isAnyPodInFailedState(aeroCluster, podList) {
		return found, reconcileError(fmt.Errorf("Cannot Rolling restart AerospikeCluster. A pod is already in failed state"))
	}

	// Can we optimize this? Update stateful set only if there is any update for it.
	updateStatefulSetPodSpec(aeroCluster, found)

	updateStatefulSetAerospikeServerContainerResources(aeroCluster, found)

	updateStatefulSetSecretInfo(aeroCluster, found)

	updateStatefulSetConfigMapVolumes(aeroCluster, found, rackState)

	logger.Info("Updating statefulset spec")

	if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
		return found, reconcileError(fmt.Errorf("Failed to update StatefulSet %s: %v", found.Name, err))
	}
	logger.Info("Statefulset spec updated. Doing rolling restart with new config")

	for _, pod := range podList {
		// Check if this pod need restart
		needRollingRestart, err := r.needRollingRestartPod(aeroCluster, rackState, pod)
		if err != nil {
			return found, reconcileError(err)
		}
		if !needRollingRestart {
			logger.Info("This Pod doesn't need rolling restart, Skip this", log.Ctx{"pod": pod.Name})
			continue
		}

		res := r.rollingRestartPod(aeroCluster, rackState, pod)
		if !res.isSuccess {
			return found, res
		}

		// Handle next pod in subsequent reconcile.
		return found, reconcileRequeueAfter(0)
	}

	// return a fresh copy
	found, err = r.getStatefulSet(aeroCluster, rackState)
	if err != nil {
		return found, reconcileError(err)
	}
	return found, reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) needRollingRestartPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) (bool, error) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID), "Pod": pod.Name})

	needRollingRestartPod := false

	// AerospikeConfig nil means status not updated yet
	if aeroCluster.Status.AerospikeConfig == nil {
		return needRollingRestartPod, nil
	}

	cmName := getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID)
	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), cmName, confMap)
	if err != nil {
		return false, err
	}
	requiredConfHash := confMap.Data[configmap.AerospikeConfHashFileName]
	requiredNetworkPolicyHash := confMap.Data[configmap.NetworkPolicyHashFileName]
	requiredPodSpecHash := confMap.Data[configmap.PodSpecHashFileName]

	podStatus := aeroCluster.Status.Pods[pod.Name]

	// Check if aerospikeConfig is updated
	if podStatus.AerospikeConfigHash != requiredConfHash {
		needRollingRestartPod = true
		logger.Info("AerospikeConfig changed. Need rolling restart", log.Ctx{
			"requiredHash": requiredConfHash,
			"currentHash":  podStatus.AerospikeConfigHash})
	}

	// Check if networkPolicy is updated
	if podStatus.NetworkPolicyHash != requiredNetworkPolicyHash {
		needRollingRestartPod = true
		logger.Info("Aerospike network policy changed. Need rolling restart", log.Ctx{
			"requiredHash": requiredNetworkPolicyHash,
			"currentHash":  podStatus.NetworkPolicyHash})
	}

	// Check if podSpec is updated
	if podStatus.PodSpecHash != requiredPodSpecHash {
		needRollingRestartPod = true
		logger.Info("Aerospike pod spec changed. Need rolling restart", log.Ctx{
			"requiredHash": requiredPodSpecHash,
			"currentHash":  podStatus.PodSpecHash})
	}

	// Check if secret is updated
	if isAerospikeConfigSecretUpdatedInAeroCluster(aeroCluster, pod) {
		needRollingRestartPod = true
		logger.Info("AerospikeConfigSecret changed. Need rolling restart")
	}

	// Check if resources are updated
	if isResourceUpdatedInAeroCluster(aeroCluster, pod) {
		needRollingRestartPod = true
		logger.Info("Aerospike resources changed. Need rolling restart")
	}

	// Check if RACKSTORAGE/CONFIGMAP is updated
	if isRackConfigMapsUpdatedInAeroCluster(aeroCluster, rackState, pod) {
		needRollingRestartPod = true
		logger.Info("Aerospike rack storage configMaps changed. Need rolling restart")
	}

	return needRollingRestartPod, nil
}

func isRackConfigMapsUpdatedInAeroCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) bool {
	configMaps, _ := rackState.Rack.Storage.GetConfigMaps()

	var volumesToBeMatched []corev1.Volume
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap == nil ||
			volume.Name == confDirName ||
			volume.Name == initConfDirName {
			continue
		}
		volumesToBeMatched = append(volumesToBeMatched, volume)
	}

	if len(configMaps) != len(volumesToBeMatched) {
		return true
	}

	for _, configMapVolume := range configMaps {
		// Ignore error since its not possible.
		pvcName, _ := getPVCName(configMapVolume.Path)
		found := false
		for _, volume := range volumesToBeMatched {
			if volume.Name == pvcName {
				found = true
				break
			}
		}

		if !found {
			return true
		}

	}
	return false
}

func isResourceUpdatedInAeroCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod corev1.Pod) bool {
	res := aeroCluster.Spec.Resources
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}

	if !isResourceListEqual(pod.Spec.Containers[0].Resources.Requests, res.Requests) ||
		!isResourceListEqual(pod.Spec.Containers[0].Resources.Limits, res.Limits) {
		return true
	}
	return false
}

func isAerospikeConfigSecretUpdatedInAeroCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster, pod corev1.Pod) bool {
	// TODO: Should we restart when secret is removed completely
	// Can secret be even empty for enterprise cluster (feature-key is alsways required)?
	if aeroCluster.Spec.AerospikeConfigSecret.SecretName != "" {
		const secretVolumeName = "secretinfo"
		var volFound bool
		for _, vol := range pod.Spec.Volumes {
			if vol.Name == secretVolumeName {

				if vol.VolumeSource.Secret.SecretName != aeroCluster.Spec.AerospikeConfigSecret.SecretName {
					// Pod volume secret doesn't match. It needs restart
					return true
				}
				volFound = true
				break
			}
		}
		if !volFound {
			// Pod volume secret not found. It needs restart
			return true
		}

		var volmFound bool
		for _, volMount := range pod.Spec.Containers[0].VolumeMounts {
			if volMount.Name == secretVolumeName {
				if volMount.MountPath != aeroCluster.Spec.AerospikeConfigSecret.MountPath {
					// Pod volume mount doesn't match. It needs restart
					return true
				}
				volmFound = true
				break
			}
		}
		if !volmFound {
			// Pod volume mount not found. It needs restart
			return true
		}
	}
	return false
}

func (r *ReconcileAerospikeCluster) rollingRestartPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState, pod corev1.Pod) reconcileResult {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	// Also check if statefulSet is in stable condition
	// Check for all containers. Status.ContainerStatuses doesn't include init container
	if pod.Status.ContainerStatuses == nil {
		return reconcileError(fmt.Errorf("Pod %s containerStatus is nil, pod may be in unscheduled state", pod.Name))
	}

	logger.Info("Rolling restart pod", log.Ctx{"podName": pod.Name})
	var pFound *corev1.Pod

	for i := 0; i < 5; i++ {
		logger.Debug("Waiting for pod to be ready", log.Ctx{"podName": pod.Name})

		pFound = &corev1.Pod{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pFound)
		if err != nil {
			logger.Error("Failed to get pod, try retry after 5 sec", log.Ctx{"err": err})
			time.Sleep(time.Second * 5)
			continue
		}

		if utils.IsPodRunningAndReady(pFound) {
			break
		}

		if utils.IsCrashed(pFound) {
			logger.Error("Pod has crashed", log.Ctx{"podName": pFound.Name})
			break
		}

		logger.Error("Pod containerStatus is not ready, try after 5 sec")
		time.Sleep(time.Second * 5)
	}

	// TODO: What if pod is still not found

	err := utils.CheckPodFailed(pFound)
	if err == nil {
		// Check for migration
		if res := r.waitForNodeSafeStopReady(aeroCluster, pFound, nil); !res.isSuccess {
			return res
		}
	} else {
		// TODO: Check a user flag to restart failed pods.
		logger.Info("Restarting failed pod", log.Ctx{"podName": pFound.Name, "error": err})
	}

	// Delete pod
	if err := r.client.Delete(context.TODO(), pFound); err != nil {
		logger.Error("Failed to delete pod", log.Ctx{"err": err})
		return reconcileError(err)
	}
	logger.Debug("Pod deleted", log.Ctx{"podName": pFound.Name})

	// Wait for pod to come up
	var started bool
	for i := 0; i < 20; i++ {
		logger.Debug("Waiting for pod to be ready after delete", log.Ctx{"podName": pFound.Name, "status": pFound.Status.Phase, "DeletionTimestamp": pFound.DeletionTimestamp})

		pod := &corev1.Pod{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: pFound.Name, Namespace: pFound.Namespace}, pod)
		if err != nil {
			logger.Error("Failed to get pod", log.Ctx{"err": err})
			time.Sleep(time.Second * 5)
			continue
		}

		if err := utils.CheckPodFailed(pod); err != nil {
			return reconcileError(err)
		}

		if !utils.IsPodRunningAndReady(pod) {
			logger.Debug("Waiting for pod to be ready", log.Ctx{"podName": pod.Name, "status": pod.Status.Phase, "DeletionTimestamp": pod.DeletionTimestamp})
			time.Sleep(time.Second * 5)
			continue
		}

		logger.Info("Pod is restarted", log.Ctx{"podName": pod.Name})
		started = true
		break
	}

	// TODO: In what situation this can happen?
	if !started {
		logger.Error("Pos is not running or ready. Pod might also be terminating", log.Ctx{"podName": pod.Name, "status": pod.Status.Phase, "DeletionTimestamp": pod.DeletionTimestamp})
	}

	return reconcileSuccess()
}

func (r *ReconcileAerospikeCluster) scaleDownRack(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState, ignorablePods []corev1.Pod) (*appsv1.StatefulSet, reconcileResult) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	desiredSize := int32(rackState.Size)

	logger.Info("ScaleDown AerospikeCluster statefulset", log.Ctx{"desiredSz": desiredSize, "currentSz": *found.Spec.Replicas})

	// Continue if scaleDown is not needed
	if *found.Spec.Replicas <= desiredSize {
		return found, reconcileSuccess()
	}

	oldPodList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed to list pods: %v", err))
	}

	if r.isAnyPodInFailedState(aeroCluster, oldPodList.Items) {
		return found, reconcileError(fmt.Errorf("Cannot scale down AerospikeCluster. A pod is already in failed state"))
	}

	var pod *corev1.Pod

	if *found.Spec.Replicas > desiredSize {

		// maintain list of removed pods. It will be used for alumni-reset and tip-clear
		podName := getStatefulSetPodName(found.Name, *found.Spec.Replicas-1)

		pod = utils.GetPod(podName, oldPodList.Items)

		// Ignore safe stop check on pod not in running state.
		if utils.IsPodRunningAndReady(pod) {
			if res := r.waitForNodeSafeStopReady(aeroCluster, pod, ignorablePods); !res.isSuccess {
				// The pod is running and is unsafe to terminate.
				return found, res
			}
		}

		// Update new object with new size
		newSize := *found.Spec.Replicas - 1
		found.Spec.Replicas = &newSize
		if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
			return found, reconcileError(fmt.Errorf("Failed to update pod size %d StatefulSet pods: %v", newSize, err))
		}

		// Wait for pods to get terminated
		if err := r.waitForStatefulSetToBeReady(found); err != nil {
			return found, reconcileError(fmt.Errorf("Failed to wait for statefulset to be ready: %v", err))
		}

		// Fetch new object
		nFound, err := r.getStatefulSet(aeroCluster, rackState)
		if err != nil {
			return found, reconcileError(fmt.Errorf("Failed to get StatefulSet pods: %v", err))
		}
		found = nFound

		err = r.cleanupPods(aeroCluster, []string{podName}, rackState)
		if err != nil {
			return nFound, reconcileError(fmt.Errorf("Failed to cleanup pod %s: %v", podName, err))
		}

		logger.Info("Pod Removed", log.Ctx{"podName": podName})
	}

	clusterPodList, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return found, reconcileError(fmt.Errorf("Failed to list pods: %v", err))
	}

	// Do post-remove-node info calls
	for _, np := range clusterPodList.Items {
		// TODO: We remove node from the end. Nodes will not have seed of successive nodes
		// So this will be no op.
		// We should tip in all nodes the same seed list,
		// then only this will have any impact. Is it really necessary?

		// TODO: tip after scaleup and create
		// All nodes from other rack
		r.tipClearHostname(aeroCluster, &np, pod)

		r.alumniReset(aeroCluster, &np)
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Remove service for pod
		// TODO: make it more roboust, what if it fails
		if err := r.deleteServiceForPod(pod.Name, aeroCluster.Namespace); err != nil {
			return found, reconcileError(err)
		}
	}

	return found, reconcileRequeueAfter(0)
}

func (r *ReconcileAerospikeCluster) reconcileAccessControl(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	enabled, err := utils.IsSecurityEnabled(aeroCluster.Spec.AerospikeConfig)
	if err != nil {
		return fmt.Errorf("Failed to get cluster security status: %v", err)
	}
	if !enabled {
		logger.Info("Cluster is not security enabled, please enable security for this cluster.")
		return nil
	}

	// Create client
	conns, err := r.newAllHostConn(aeroCluster)
	if err != nil {
		return fmt.Errorf("Failed to get host info: %v", err)
	}
	var hosts []*as.Host
	for _, conn := range conns {
		hosts = append(hosts, &as.Host{
			Name:    conn.ASConn.AerospikeHostName,
			TLSName: conn.ASConn.AerospikeTLSName,
			Port:    conn.ASConn.AerospikePort,
		})
	}
	// Create policy using status, status has current connection info
	clientPolicy := r.getClientPolicy(aeroCluster)
	aeroClient, err := as.NewClientWithPolicyAndHost(clientPolicy, hosts...)

	if err != nil {
		return fmt.Errorf("Failed to create aerospike cluster client: %v", err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider(aeroCluster)
	err = accessControl.ReconcileAccessControl(&aeroCluster.Spec, &aeroCluster.Status.AerospikeClusterSpec, aeroClient, pp, logger)
	return err
}

func (r *ReconcileAerospikeCluster) updateStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newAeroCluster)
	if err != nil {
		return err
	}

	// Deep copy merges so blank out the spec part of status before copying over.
	newAeroCluster.Status.AerospikeClusterSpec = aerospikev1alpha1.AerospikeClusterSpec{}
	if err := lib.DeepCopy(&newAeroCluster.Status.AerospikeClusterSpec, &aeroCluster.Spec); err != nil {
		return err
	}

	err = r.patchStatus(aeroCluster, newAeroCluster)
	if err != nil {
		return fmt.Errorf("Error updating status: %v", err)
	}
	logger.Info("Updated status", log.Ctx{"status": newAeroCluster.Status})
	return nil
}

func (r *ReconcileAerospikeCluster) createStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Creating status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newAeroCluster)
	if err != nil {
		return err
	}

	if newAeroCluster.Status.Pods == nil {
		newAeroCluster.Status.Pods = map[string]aerospikev1alpha1.AerospikePodStatus{}
	}

	if err = r.client.Status().Update(context.TODO(), newAeroCluster); err != nil {
		return fmt.Errorf("Error creating status: %v", err)
	}

	return nil
}

func (r *ReconcileAerospikeCluster) isNewCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) (bool, error) {
	if aeroCluster.Status.AerospikeConfig != nil {
		// We have valid status, cluster cannot be new.
		return false, nil
	}

	statefulSetList, err := r.getClusterStatefulSets(aeroCluster)

	if err != nil {
		return false, err
	}

	// Cluster can have status nil and still have pods on failures.
	// For cluster to be new there should be no pods in the cluster.
	return len(statefulSetList.Items) == 0, nil
}

func (r *ReconcileAerospikeCluster) hasClusterFailed(aeroCluster *aerospikev1alpha1.AerospikeCluster) (bool, error) {
	isNew, err := r.isNewCluster(aeroCluster)

	if err != nil {
		// Checking cluster status failed.
		return false, err
	}

	return !isNew && aeroCluster.Status.AerospikeConfig == nil, nil
}

func (r *ReconcileAerospikeCluster) patchStatus(oldAeroCluster, newAeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(oldAeroCluster)})

	oldJSON, err := json.Marshal(oldAeroCluster)
	if err != nil {
		return fmt.Errorf("Error marshalling old status: %v", err)
	}

	newJSON, err := json.Marshal(newAeroCluster)
	if err != nil {
		return fmt.Errorf("Error marshalling new status: %v", err)
	}

	jsonpatchPatch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("Error creating json patch: %v", err)
	}

	// Pick changes to the status object only.
	filteredPatch := []jsonpatch.JsonPatchOperation{}
	for _, operation := range jsonpatchPatch {
		// pods should never be updated here
		// pods is updated only from 2 places
		// 1: While pod init, it will add pod in pods
		// 2: While pod cleanup, it will remove pod from pods
		if strings.HasPrefix(operation.Path, "/status") && !strings.HasPrefix(operation.Path, "/status/pods") {
			filteredPatch = append(filteredPatch, operation)
		}
	}

	if len(filteredPatch) == 0 {
		logger.Info("No status change required")
		return nil
	}
	logger.Debug("Filtered status patch ", log.Ctx{"patch": filteredPatch, "oldObj.status": oldAeroCluster.Status, "newObj.status": newAeroCluster.Status})

	jsonpatchJSON, err := json.Marshal(filteredPatch)

	if err != nil {
		return fmt.Errorf("Error marshalling json patch: %v", err)
	}

	patch := client.ConstantPatch(types.JSONPatchType, jsonpatchJSON)

	if err = r.client.Status().Patch(context.TODO(), oldAeroCluster, patch, client.FieldOwner(patchFieldOwner)); err != nil {
		return fmt.Errorf("Error patching status: %v", err)
	}

	// FIXME: Json unmarshal used by above client.Status(),Patch()  does not convert empty lists in the new JSON to empty lists in the target. Seems like a bug in encoding/json/Unmarshall.
	//
	// Workaround by force copying new object's status to old object's status.
	return lib.DeepCopy(&oldAeroCluster.Status, &newAeroCluster.Status)
}

// removePodStatus removes podNames from the cluster's pod status.
// Assumes the pods are not running so that the no concurrent update to this pod status is possbile.
func (r *ReconcileAerospikeCluster) removePodStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster, podNames []string) error {
	if len(podNames) == 0 {
		return nil
	}

	patches := []jsonpatch.JsonPatchOperation{}

	for _, podName := range podNames {
		patch := jsonpatch.JsonPatchOperation{
			Operation: "remove",
			Path:      "/status/pods/" + podName,
		}
		patches = append(patches, patch)
	}

	jsonpatchJSON, err := json.Marshal(patches)
	constantPatch := client.ConstantPatch(types.JSONPatchType, jsonpatchJSON)

	// Since the pod status is updated from pod init container, set the fieldowner to "pod" for pod status updates.
	if err = r.client.Status().Patch(context.TODO(), aeroCluster, constantPatch, client.FieldOwner("pod")); err != nil {
		return fmt.Errorf("Error updating status: %v", err)
	}

	return nil
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
func (r *ReconcileAerospikeCluster) recoverFailedCreate(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration.")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterStatefulSets(aeroCluster)
	if err != nil {
		return fmt.Errorf("Error getting statefulsets while forcing recreate of the cluster as status is nil: %v", err)
	}

	logger.Debug("Found statefulset for cluster. Need to delete them", log.Ctx{"nSTS": len(statefulSetList.Items)})
	for _, statefulset := range statefulSetList.Items {
		if err := r.deleteStatefulSet(aeroCluster, &statefulset); err != nil {
			return fmt.Errorf("Error deleting statefulset while forcing recreate of the cluster as status is nil: %v", err)
		}
	}

	// Clear pod status as well in status since we want to be re-initializing or cascade deleting devices if any.
	// This is not necessary since scale-up would cleanup danglin pod status. However done here for general
	// cleanliness.
	rackStateList := getNewRackStateList(aeroCluster)
	for _, state := range rackStateList {
		pods, err := r.getRackPodList(aeroCluster, state.Rack.ID)
		if err != nil {
			return fmt.Errorf("Failed recover failed cluster: %v", err)
		}

		newPodNames := []string{}
		for i := 0; i < len(pods.Items); i++ {
			newPodNames = append(newPodNames, pods.Items[i].Name)
		}

		err = r.cleanupPods(aeroCluster, newPodNames, state)
		if err != nil {
			return fmt.Errorf("Failed recover failed cluster: %v", err)
		}
	}

	return fmt.Errorf("Forcing recreate of the cluster as status is nil")
}

// cleanupPods checks pods and status before scaleup to detect and fix any status anomalies.
func (r *ReconcileAerospikeCluster) cleanupPods(aeroCluster *aerospikev1alpha1.AerospikeCluster, podNames []string, rackState RackState) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	needStatusCleanup := []string{}
	for _, podName := range podNames {
		_, ok := aeroCluster.Status.Pods[podName]
		if ok {
			needStatusCleanup = append(needStatusCleanup, podName)
		}
	}

	if len(needStatusCleanup) > 0 {
		logger.Info("Removing pod status for dangling pods", log.Ctx{"pods": podNames})

		if err := r.removePodStatus(aeroCluster, needStatusCleanup); err != nil {
			return fmt.Errorf("Could not cleanup pod status: %v", err)
		}
	}

	logger.Info("Removing pvc for removed pods", log.Ctx{"pods": podNames})

	// Delete PVCs if cascadeDelete
	pvcItems, err := r.getPodsPVCList(aeroCluster, podNames, rackState.Rack.ID)
	if err != nil {
		return fmt.Errorf("Could not find pvc for pods %v: %v", podNames, err)
	}
	storage := rackState.Rack.Storage
	if err := r.removePVCs(aeroCluster, &storage, pvcItems); err != nil {
		return fmt.Errorf("Could not cleanup pod PVCs: %v", err)
	}

	return nil
}

func (r *ReconcileAerospikeCluster) addFinalizer(aeroCluster *aerospikev1alpha1.AerospikeCluster, finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !containsString(aeroCluster.ObjectMeta.Finalizers, finalizerName) {
		aeroCluster.ObjectMeta.Finalizers = append(aeroCluster.ObjectMeta.Finalizers, finalizerName)
		if err := r.client.Update(context.TODO(), aeroCluster); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcileAerospikeCluster) cleanUpAndremoveFinalizer(aeroCluster *aerospikev1alpha1.AerospikeCluster, finalizerName string) error {
	// The object is being deleted
	if containsString(aeroCluster.ObjectMeta.Finalizers, finalizerName) {
		// Handle any external dependency
		if err := r.deleteExternalResources(aeroCluster); err != nil {
			// If fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// Remove finalizer from the list
		aeroCluster.ObjectMeta.Finalizers = removeString(aeroCluster.ObjectMeta.Finalizers, finalizerName)
		if err := r.client.Update(context.TODO(), aeroCluster); err != nil {
			return err
		}
	}

	// Stop reconciliation as the item is being deleted
	return nil
}

func (r *ReconcileAerospikeCluster) deleteExternalResources(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	// Delete should be idempotent
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Removing pvc for removed cluster")

	// Delete pvc for all rack storage
	for _, rack := range aeroCluster.Spec.RackConfig.Racks {
		rackPVCItems, err := r.getRackPVCList(aeroCluster, rack.ID)
		if err != nil {
			return fmt.Errorf("Could not find pvc for rack: %v", err)
		}
		storage := rack.Storage
		if _, err := r.removePVCsAsync(aeroCluster, &storage, rackPVCItems); err != nil {
			return fmt.Errorf("Failed to remove cluster PVCs: %v", err)
		}
	}

	// Delete PVCs for any remaining old removed racks
	pvcItems, err := r.getClusterPVCList(aeroCluster)
	if err != nil {
		return fmt.Errorf("Could not find pvc for cluster: %v", err)
	}

	// removePVCs should be passed only filtered pvc otherwise rack pvc may be removed using global storage cascadeDelete
	var fileredPVCItems []corev1.PersistentVolumeClaim
	for _, pvc := range pvcItems {
		var found bool
		for _, rack := range aeroCluster.Spec.RackConfig.Racks {
			rackLables := utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rack.ID)
			if reflect.DeepEqual(pvc.Labels, rackLables) {
				found = true
				break
			}
		}
		if !found {
			fileredPVCItems = append(fileredPVCItems, pvc)
		}
	}

	// Delete pvc for commmon storage.
	if _, err := r.removePVCsAsync(aeroCluster, &aeroCluster.Spec.Storage, fileredPVCItems); err != nil {
		return fmt.Errorf("Failed to remove cluster PVCs: %v", err)
	}

	return nil
}

func (r *ReconcileAerospikeCluster) removePVCs(aeroCluster *aerospikev1alpha1.AerospikeCluster, storage *aerospikev1alpha1.AerospikeStorageSpec, pvcItems []corev1.PersistentVolumeClaim) error {
	deletedPVCs, err := r.removePVCsAsync(aeroCluster, storage, pvcItems)
	if err != nil {
		return err
	}

	return r.waitForPVCTermination(aeroCluster, deletedPVCs)
}

func (r *ReconcileAerospikeCluster) removePVCsAsync(aeroCluster *aerospikev1alpha1.AerospikeCluster, storage *aerospikev1alpha1.AerospikeStorageSpec, pvcItems []corev1.PersistentVolumeClaim) ([]corev1.PersistentVolumeClaim, error) {
	aeroClusterNamespacedName := getNamespacedNameForCluster(aeroCluster)

	logger := pkglog.New(log.Ctx{"AerospikeCluster": aeroClusterNamespacedName})

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
			logger.Error("PVC can not be removed, it does not have storage-path annotation", log.Ctx{"PVC": pvc.Name, "annotations": pvc.Annotations})
			continue
		}

		var cascadeDelete bool
		v := getVolumeConfigForPVC(aeroClusterNamespacedName.Name, storage, path)
		if v == nil {
			if *pvc.Spec.VolumeMode == corev1.PersistentVolumeBlock {
				cascadeDelete = storage.BlockVolumePolicy.CascadeDelete
			} else {
				cascadeDelete = storage.FileSystemVolumePolicy.CascadeDelete
			}
			logger.Info("PVC path not found in configured storage volumes. Use storage level cascadeDelete policy", log.Ctx{"PVC": pvc.Name, "path": path, "cascadeDelete": cascadeDelete})

		} else {
			cascadeDelete = v.CascadeDelete
		}

		if cascadeDelete {
			deletedPVCs = append(deletedPVCs, pvc)
			if err := r.client.Delete(context.TODO(), &pvc); err != nil {
				return nil, fmt.Errorf("Could not delete pvc %s: %v", pvc.Name, err)
			}
			logger.Info("PVC removed", log.Ctx{"PVC": pvc.Name, "PVCCascadeDelete": cascadeDelete})
		} else {
			logger.Info("PVC not removed", log.Ctx{"PVC": pvc.Name, "PVCCascadeDelete": cascadeDelete})
		}
	}

	return deletedPVCs, nil
}

func (r *ReconcileAerospikeCluster) waitForPVCTermination(aeroCluster *aerospikev1alpha1.AerospikeCluster, deletedPVCs []corev1.PersistentVolumeClaim) error {
	if len(deletedPVCs) == 0 {
		return nil
	}

	aeroClusterNamespacedName := getNamespacedNameForCluster(aeroCluster)

	logger := pkglog.New(log.Ctx{"AerospikeCluster": aeroClusterNamespacedName})

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
					logger.Info("Waiting for PVC termination", log.Ctx{"PVC": pvc.Name})
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

func getVolumeConfigForPVC(aeroClusterName string, storage *aerospikev1alpha1.AerospikeStorageSpec, pvcPathAnnotation string) *aerospikev1alpha1.AerospikePersistentVolumeSpec {
	volumes := storage.Volumes
	for _, v := range volumes {
		if pvcPathAnnotation == v.Path {
			return &v
		}
	}
	return nil
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
