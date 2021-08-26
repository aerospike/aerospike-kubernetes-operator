package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"

	as "github.com/ashishshinde/aerospike-client-go/v5"
	"github.com/go-logr/logr"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/jsonpatch"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	"github.com/aerospike/aerospike-management-lib/deployment"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const patchFieldOwner = "aerospike-kuberneter-operator"
const finalizerName = "asdb.aerospike.com/storage-finalizer"

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

func ignoreSecondaryResource() predicate.Predicate {
	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore updates to CR status in which case metadata.Generation does not change
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	return p
}

// SetupWithManager sets up the controller with the Manager
func (r *AerospikeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&asdbv1beta1.AerospikeCluster{}).
		Owns(&appsv1.StatefulSet{}, builder.WithPredicates(
			predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
			},
		)).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: maxConcurrentReconciles,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

// AerospikeClusterReconciler reconciles a AerospikeCluster object
type AerospikeClusterReconciler struct {
	client.Client
	KubeClient *kubernetes.Clientset
	KubeConfig *rest.Config
	Log        logr.Logger
	Scheme     *k8sRuntime.Scheme
}

// RackState contains the rack configuration and rack size.
type RackState struct {
	Rack asdbv1beta1.Rack
	Size int
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/finalizers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=asdb.aerospike.com,resources=aerospikeclusters/finalizers,verbs=update

// Reconcile AerospikeCluster object
func (r *AerospikeClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("aerospikecluster", request.NamespacedName)

	r.Log.Info("Reconciling AerospikeCluster")

	// Fetch the AerospikeCluster instance
	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, aeroCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	r.Log.V(1).Info("AerospikeCluster", "Spec", aeroCluster.Spec, "Status", aeroCluster.Status)

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
		r.Log.Error(err, "Failed to add finalizer")
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
		e := fmt.Errorf("failed to get hostConn for aerospike cluster nodes: %v", err)
		r.Log.Error(err, "Failed to get hostConn for aerospike cluster nodes")
		return reconcile.Result{}, e
	}
	if err := deployment.InfoQuiesceUndo(r.getClientPolicy(aeroCluster), allHostConns); err != nil {
		r.Log.Error(err, "Failed to check for Quiesced nodes")
		return reconcile.Result{}, err
	}

	// Setup access control.
	if err := r.reconcileAccessControl(aeroCluster); err != nil {
		r.Log.Error(err, "Failed to reconcile access control")
		return reconcile.Result{}, err
	}

	// Update the AerospikeCluster status.
	if err := r.updateStatus(aeroCluster); err != nil {
		r.Log.Error(err, "Failed to update AerospikeCluster status")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *AerospikeClusterReconciler) handleClusterDeletion(aeroCluster *asdbv1beta1.AerospikeCluster, finalizerName string) error {

	r.Log.Info("Handle cluster deletion")

	// The cluster is being deleted
	if err := r.cleanUpAndremoveFinalizer(aeroCluster, finalizerName); err != nil {
		r.Log.Error(err, "Failed to remove finalizer")
		return err
	}
	return nil
}

func (r *AerospikeClusterReconciler) handlePreviouslyFailedCluster(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	r.Log.Info("Handle previously failed cluster")

	isNew, err := r.isNewCluster(aeroCluster)
	if err != nil {
		return fmt.Errorf("error determining if cluster is new: %v", err)
	}

	if isNew {
		r.Log.V(1).Info("It's new cluster, create empty status object")
		if err := r.createStatus(aeroCluster); err != nil {
			return err
		}
	} else {
		r.Log.V(1).Info("It's not a new cluster, check if it is failed and needs recovery")
		hasFailed, err := r.hasClusterFailed(aeroCluster)
		if err != nil {
			return fmt.Errorf("error determining if cluster has failed: %v", err)
		}

		if hasFailed {
			return r.recoverFailedCreate(aeroCluster)
		}
	}
	return nil
}

func (r *AerospikeClusterReconciler) reconcileAccessControl(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	enabled, err := asdbv1beta1.IsSecurityEnabled(aeroCluster.Spec.AerospikeConfig)
	if err != nil {
		return fmt.Errorf("failed to get cluster security status: %v", err)
	}
	if !enabled {
		r.Log.Info("Cluster is not security enabled, please enable security for this cluster.")
		return nil
	}

	// Create client
	conns, err := r.newAllHostConn(aeroCluster)
	if err != nil {
		return fmt.Errorf("failed to get host info: %v", err)
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
		return fmt.Errorf("failed to create aerospike cluster client: %v", err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider(aeroCluster)

	// TODO: FIXME: We are creating a spec object here so that it can be passed to reconcileAccessControl
	// reconcileAccessControl uses many helper func over spec object. So statusSpec to spec conversion
	// help in reusing those functions over statusSpec.
	// See if this can be done in better manner
	// statusSpec := asdbv1beta1.AerospikeClusterSpec{}
	// if err := lib.DeepCopy(&statusSpec, &aeroCluster.Status.AerospikeClusterStatusSpec); err != nil {
	// 	return err
	// }
	statusToSpec, err := asdbv1beta1.CopyStatusToSpec(aeroCluster.Status.AerospikeClusterStatusSpec)
	if err != nil {
		return err
	}

	// // TODO: FIXME: REMOVE LOGGER
	// logger := pkgLog.New("AerospikeCluster", utils.ClusterNamespacedName(aeroCluster))

	err = ReconcileAccessControl(&aeroCluster.Spec, statusToSpec, aeroClient, pp, r.Log)
	return err
}

func (r *AerospikeClusterReconciler) updateStatus(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	r.Log.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newAeroCluster)
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

	specToStatus, err := asdbv1beta1.CopySpecToStatus(aeroCluster.Spec)
	if err != nil {
		return err
	}
	newAeroCluster.Status.AerospikeClusterStatusSpec = *specToStatus

	err = r.patchStatus(aeroCluster, newAeroCluster)
	if err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}
	r.Log.Info("Updated status", "status", newAeroCluster.Status)
	return nil
}

func (r *AerospikeClusterReconciler) createStatus(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	r.Log.Info("Creating status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &asdbv1beta1.AerospikeCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newAeroCluster)
	if err != nil {
		return err
	}

	if newAeroCluster.Status.Pods == nil {
		newAeroCluster.Status.Pods = map[string]asdbv1beta1.AerospikePodStatus{}
	}

	if err = r.Client.Status().Update(context.TODO(), newAeroCluster); err != nil {
		return fmt.Errorf("error creating status: %v", err)
	}

	return nil
}

func (r *AerospikeClusterReconciler) isNewCluster(aeroCluster *asdbv1beta1.AerospikeCluster) (bool, error) {
	if aeroCluster.Status.AerospikeConfig != nil {
		// We have valid status, cluster cannot be new.
		return false, nil
	}

	statefulSetList, err := r.getClusterSTSList(aeroCluster)

	if err != nil {
		return false, err
	}

	// Cluster can have status nil and still have pods on failures.
	// For cluster to be new there should be no pods in the cluster.
	return len(statefulSetList.Items) == 0, nil
}

func (r *AerospikeClusterReconciler) hasClusterFailed(aeroCluster *asdbv1beta1.AerospikeCluster) (bool, error) {
	isNew, err := r.isNewCluster(aeroCluster)

	if err != nil {
		// Checking cluster status failed.
		return false, err
	}

	return !isNew && aeroCluster.Status.AerospikeConfig == nil, nil
}

func (r *AerospikeClusterReconciler) patchStatus(oldAeroCluster, newAeroCluster *asdbv1beta1.AerospikeCluster) error {

	oldJSON, err := json.Marshal(oldAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling old status: %v", err)
	}

	newJSON, err := json.Marshal(newAeroCluster)
	if err != nil {
		return fmt.Errorf("error marshalling new status: %v", err)
	}

	jsonpatchPatch, err := jsonpatch.CreatePatch(oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("error creating json patch: %v", err)
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
		r.Log.Info("No status change required")
		return nil
	}
	r.Log.V(1).Info("Filtered status patch ", "patch", filteredPatch, "oldObj.status", oldAeroCluster.Status, "newObj.status", newAeroCluster.Status)

	jsonpatchJSON, err := json.Marshal(filteredPatch)

	if err != nil {
		return fmt.Errorf("error marshalling json patch: %v", err)
	}

	patch := client.RawPatch(types.JSONPatchType, jsonpatchJSON)

	if err = r.Client.Status().Patch(context.TODO(), oldAeroCluster, patch, client.FieldOwner(patchFieldOwner)); err != nil {
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
func (r *AerospikeClusterReconciler) recoverFailedCreate(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	r.Log.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration.")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterSTSList(aeroCluster)
	if err != nil {
		return fmt.Errorf("error getting statefulsets while forcing recreate of the cluster as status is nil: %v", err)
	}

	r.Log.V(1).Info("Found statefulset for cluster. Need to delete them", "nSTS", len(statefulSetList.Items))
	for _, statefulset := range statefulSetList.Items {
		if err := r.deleteSTS(aeroCluster, &statefulset); err != nil {
			return fmt.Errorf("error deleting statefulset while forcing recreate of the cluster as status is nil: %v", err)
		}
	}

	// Clear pod status as well in status since we want to be re-initializing or cascade deleting devices if any.
	// This is not necessary since scale-up would cleanup danglin pod status. However done here for general
	// cleanliness.
	rackStateList := getConfiguredRackStateList(aeroCluster)
	for _, state := range rackStateList {
		pods, err := r.getRackPodList(aeroCluster, state.Rack.ID)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %v", err)
		}

		newPodNames := []string{}
		for i := 0; i < len(pods.Items); i++ {
			newPodNames = append(newPodNames, pods.Items[i].Name)
		}

		err = r.cleanupPods(aeroCluster, newPodNames, state)
		if err != nil {
			return fmt.Errorf("failed recover failed cluster: %v", err)
		}
	}

	return fmt.Errorf("forcing recreate of the cluster as status is nil")
}

func (r *AerospikeClusterReconciler) addFinalizer(aeroCluster *asdbv1beta1.AerospikeCluster, finalizerName string) error {
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !utils.ContainsString(aeroCluster.ObjectMeta.Finalizers, finalizerName) {
		aeroCluster.ObjectMeta.Finalizers = append(aeroCluster.ObjectMeta.Finalizers, finalizerName)
		if err := r.Client.Update(context.TODO(), aeroCluster); err != nil {
			return err
		}
	}
	return nil
}

func (r *AerospikeClusterReconciler) cleanUpAndremoveFinalizer(aeroCluster *asdbv1beta1.AerospikeCluster, finalizerName string) error {
	// The object is being deleted
	if utils.ContainsString(aeroCluster.ObjectMeta.Finalizers, finalizerName) {
		// Handle any external dependency
		if err := r.deleteExternalResources(aeroCluster); err != nil {
			// If fail to delete the external dependency here, return with error
			// so that it can be retried
			return err
		}

		// Remove finalizer from the list
		aeroCluster.ObjectMeta.Finalizers = utils.RemoveString(aeroCluster.ObjectMeta.Finalizers, finalizerName)
		if err := r.Client.Update(context.TODO(), aeroCluster); err != nil {
			return err
		}
	}

	// Stop reconciliation as the item is being deleted
	return nil
}

func (r *AerospikeClusterReconciler) deleteExternalResources(aeroCluster *asdbv1beta1.AerospikeCluster) error {
	// Delete should be idempotent

	r.Log.Info("Removing pvc for removed cluster")

	// Delete pvc for all rack storage
	for _, rack := range aeroCluster.Spec.RackConfig.Racks {
		rackPVCItems, err := r.getRackPVCList(aeroCluster, rack.ID)
		if err != nil {
			return fmt.Errorf("could not find pvc for rack: %v", err)
		}
		storage := rack.Storage

		if _, err := r.removePVCsAsync(aeroCluster, &storage, rackPVCItems); err != nil {
			return fmt.Errorf("failed to remove cluster PVCs: %v", err)
		}
	}

	// Delete PVCs for any remaining old removed racks
	pvcItems, err := r.getClusterPVCList(aeroCluster)
	if err != nil {
		return fmt.Errorf("could not find pvc for cluster: %v", err)
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
		return fmt.Errorf("failed to remove cluster PVCs: %v", err)
	}

	return nil
}

func (r *AerospikeClusterReconciler) isResourceUpdatedInAeroCluster(aeroCluster *asdbv1beta1.AerospikeCluster, pod corev1.Pod) bool {
	res := aeroCluster.Spec.Resources
	if res == nil {
		res = &corev1.ResourceRequirements{}
	}

	if !isClusterResourceListEqual(pod.Spec.Containers[0].Resources.Requests, res.Requests) ||
		!isClusterResourceListEqual(pod.Spec.Containers[0].Resources.Limits, res.Limits) {
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
