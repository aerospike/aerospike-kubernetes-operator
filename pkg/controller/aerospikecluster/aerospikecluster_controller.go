package aerospikecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	aero "github.com/aerospike/aerospike-client-go"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/jsonpatch"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	lib "github.com/aerospike/aerospike-management-lib"
	log "github.com/inconshreveable/log15"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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
	c, err := controller.New("aerospikecluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	var pred predicate.Predicate

	pred = predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.MetaOld.GetGeneration() != e.MetaNew.GetGeneration()
		},
	}
	// Watch for changes to primary resource AerospikeCluster
	err = c.Watch(&source.Kind{Type: &aerospikev1alpha1.AerospikeCluster{}}, &handler.EnqueueRequestForObject{}, pred)
	if err != nil {
		return err
	}

	// TODO: Do we need to monitor this? Statefulset is updated many times in reconcile and this add new entry in
	// update queue. If user will change only cr then we may not need to monitor statefulset.
	// Think all possible situation

	// Watch for changes to secondary resource StatefulSet and requeue the owner AerospikeCluster
	err = c.Watch(&source.Kind{Type: &appsv1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &aerospikev1alpha1.AerospikeCluster{},
	}, predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			//return e.MetaOld.GetGeneration() != e.MetaNew.GetGeneration()
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
	scheme *runtime.Scheme
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
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{Requeue: true}, err
	}

	logger.Debug("AerospikeCluster", log.Ctx{"Spec": aeroCluster.Spec, "Status": aeroCluster.Status})

	isNew, err := r.isNewCluster(aeroCluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("Error determining if cluster is new: %v", err)
	}

	if isNew {
		logger.Debug("It's new cluster, create empty status object")
		if err := r.createStatus(aeroCluster); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		logger.Debug("It's not a new cluster, check if it is failed and needs recovery")
		hasFailed, err := r.hasClusterFailed(aeroCluster)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("Error determining if cluster has failed: %v", err)
		}

		if hasFailed {
			return r.recoverFailedCreate(aeroCluster)
		}
	}

	if err := r.ReconcileRacks(aeroCluster); err != nil {
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

// ReconcileRacks reconcile all racks
func (r *ReconcileAerospikeCluster) ReconcileRacks(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Reconciling rack for AerospikeCluster")

	var scaledDownRackSTSList []appsv1.StatefulSet
	var scaledDownRackList []RackState

	rackStateList := getNewRackStateList(aeroCluster)
	for _, state := range rackStateList {
		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, state.Rack.ID)
		if err := r.client.Get(context.TODO(), stsName, found); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
			// Create statefulset for rack
			if err := r.createSTSForCluster(aeroCluster, state); err != nil {
				return err
			}
			continue
		}

		// Get list of scaled down racks
		if *found.Spec.Replicas > int32(state.Size) {
			scaledDownRackSTSList = append(scaledDownRackSTSList, *found)
			scaledDownRackList = append(scaledDownRackList, state)
		} else {
			// Reconcile other statefulset
			if err := r.reconcileClusterSTS(aeroCluster, found, state); err != nil {
				return err
			}
		}
	}

	// Reconcile scaledDownRacks after all other racks are reconciled
	for idx, state := range scaledDownRackList {
		if err := r.reconcileClusterSTS(aeroCluster, &scaledDownRackSTSList[idx], state); err != nil {
			return err
		}
	}

	if len(aeroCluster.Status.RackConfig.Racks) != 0 {
		// remove removed racks
		if err := r.deleteSTSForRemovedRacks(aeroCluster, rackStateList); err != nil {
			logger.Error("Failed to remove statefulset for removed racks", log.Ctx{"err": err})
			return err
		}
	}

	// Pod termination in above call will take some time. Should we wait here for sometime or
	// Just check if Pod isTerminating in below calls?
	// TODO: Replace the sleep.
	time.Sleep(time.Second * 2)

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

func (r *ReconcileAerospikeCluster) createSTSForCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Create new Aerospike cluster if needed")

	// NoOp if already exist
	logger.Info("AerospikeCluster", log.Ctx{"Spec": aeroCluster.Spec})
	if err := r.createHeadlessSvc(aeroCluster); err != nil {
		logger.Error("Failed to create headless service", log.Ctx{"err": err})
		return err
	}
	// Bad config should not come here. It should be validated in validation hook
	cmName := getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID)
	if err := r.buildConfigMap(aeroCluster, cmName, rackState.Rack); err != nil {
		logger.Error("Failed to create configMap from AerospikeConfig", log.Ctx{"err": err})
		return err
	}

	// _, err := r.createClusterSTS(aeroCluster, rackStateList)
	// if err != nil {
	// 	return err
	// }

	found := &appsv1.StatefulSet{}
	stsName := getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)
	found, err := r.createStatefulSetForAerospikeCluster(aeroCluster, stsName, rackState)
	if err != nil {
		logger.Error("Statefulset setup failed. Deleting statefulset", log.Ctx{"name": stsName, "err": err})
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		r.deleteStatefulSet(aeroCluster, found)
		return err
	}
	return nil
}

func (r *ReconcileAerospikeCluster) deleteSTSForRemovedRacks(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackStateList []RackState) error {
	oldRackIDList := getOldRackIDList(aeroCluster)

	for _, rackID := range oldRackIDList {
		var rackFound bool
		for _, newRack := range rackStateList {
			if rackID == newRack.Rack.ID {
				rackFound = true
				break
			}
		}

		if rackFound {
			continue
		}

		found := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForStatefulSet(aeroCluster, rackID)
		err := r.client.Get(context.TODO(), stsName, found)
		if err != nil {
			// If not found then go to next
			if errors.IsNotFound(err) {
				continue
			}
			return err
		}
		// TODO: Add option for quick delete of rack. DefaultRackID should always be removed gracefully
		found, err = r.scaleDown(aeroCluster, found, RackState{Size: 0, Rack: aerospikev1alpha1.Rack{ID: rackID}})
		//removedPods, err := r.getRackPodList(aeroCluster, rackID)
		if err != nil {
			return err
		}

		// Delete sts
		if err := r.deleteStatefulSet(aeroCluster, found); err != nil {
			return err
		}
	}
	return nil
}

// TODO: rename to reflect this reconciles a rack.
func (r *ReconcileAerospikeCluster) reconcileClusterSTS(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) error {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})
	logger.Info("Reconcile existing Aerospike cluster statefulset")

	logger.Info("Ensure the StatefulSet size is the same as the spec")

	//desiredSize := rackState.Size

	var err error
	// nil aerospikeConfig in status indicate that AerospikeCluster object is created but status is not successfully updated even once
	var needRollingRestart bool
	// AerospikeConfig nil means status not updated yet
	if aeroCluster.Status.AerospikeConfig != nil {
		needRollingRestart = !reflect.DeepEqual(aeroCluster.Spec.AerospikeConfig, aeroCluster.Status.AerospikeConfig)
		if !needRollingRestart {
			// Check if rack aerospikeConfig has changed
			for _, statusRack := range aeroCluster.Status.RackConfig.Racks {
				if rackState.Rack.ID == statusRack.ID && !reflect.DeepEqual(rackState.Rack.AerospikeConfig, statusRack.AerospikeConfig) {
					needRollingRestart = true
					logger.Info("Rack AerospikeConfig changed. Need rolling restart", log.Ctx{"oldRackConfig": statusRack, "newRackConfig": rackState.Rack})
					break
				}
			}
		} else {
			logger.Info("AerospikeConfig changed. Need rolling restart")
		}

		if needRollingRestart {
			if err := r.updateConfigMap(aeroCluster, getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID), rackState.Rack); err != nil {
				logger.Error("Failed to update configMap from AerospikeConfig", log.Ctx{"err": err})
				return err
			}
		}

		if !needRollingRestart {
			// Secret (having config info like tls, feature-key-file) is updated, need rolling restart
			needRollingRestart = !reflect.DeepEqual(aeroCluster.Spec.AerospikeConfigSecret, aeroCluster.Status.AerospikeConfigSecret)
		}
		if aeroCluster.Spec.Resources != nil || aeroCluster.Status.Resources != nil {
			if !needRollingRestart && isClusterResourceUpdated(aeroCluster) {
				logger.Info("Resources changed. Need rolling restart")

				needRollingRestart = true
			}
		}
	}

	// TODO: For which case it is needed? This cannot be considered rolling restart. It can be scale up/down also
	// } else {
	// 	if aeroCluster.Generation != 1 {
	// 		// TODO: check if its correct
	// 		// If status is nil but generation is greater than 1,
	// 		// it means user has updated it and wants a rolling restart with new conf
	// 		needRollingRestart = true
	// 	}
	// }
	desiredSize := int32(rackState.Size)
	// Scale down
	if *found.Spec.Replicas > desiredSize {
		found, err = r.scaleDown(aeroCluster, found, rackState)
		if err != nil {
			logger.Error("Failed to scaleDown StatefulSet pods", log.Ctx{"err": err})
			return err
		}
	}

	// Upgrade
	upgradeNeeded, err := r.isAeroClusterUpgradeNeeded(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return err
	}

	if upgradeNeeded {
		found, err = r.upgrade(aeroCluster, found, aeroCluster.Spec.Build, rackState)
		if err != nil {
			logger.Error("Failed to update StatefulSet image", log.Ctx{"err": err})
			return err
		}
	} else if needRollingRestart {
		found, err = r.rollingRestart(aeroCluster, found, rackState)
		if err != nil {
			logger.Error("Failed to do rolling restart", log.Ctx{"err": err})
			return err
		}
	}

	// Scale up after upgrading, so that new pods comeup with new image
	if *found.Spec.Replicas < desiredSize {
		found, err = r.scaleUp(aeroCluster, found, rackState)
		if err != nil {
			logger.Error("Failed to scaleUp StatefulSet pods", log.Ctx{"err": err})
			return err
		}
	}

	return nil
}

func (r *ReconcileAerospikeCluster) scaleUp(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	desiredSize := int32(rackState.Size)

	oldSz := *found.Spec.Replicas
	found.Spec.Replicas = &desiredSize

	logger.Info("Scaling up pods", log.Ctx{"currentSz": oldSz, "desiredSz": desiredSize})

	// No need for this? But if image is bad then new pod will also comeup with bad node.
	podList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}
	if r.isAnyPodInFailedState(aeroCluster, podList.Items) {
		return found, fmt.Errorf("Cannot scale up AerospikeCluster. A pod is already in failed state")
	}

	newPodNames := []string{}
	for i := oldSz; i < desiredSize; i++ {
		newPodNames = append(newPodNames, getStatefulSetPodName(found.Name, i))
	}

	// Ensure none of the to be launched pods are active.
	for _, newPodName := range newPodNames {
		for _, pod := range podList.Items {
			if pod.Name == newPodName {
				return found, fmt.Errorf("Pod %s yet to be launched is still present", newPodName)
			}
		}
	}

	// Clean up any dangling resources associated with the new pods.
	// This implements a safety net to protect scale up against failed cleanup operations when cluster
	// is scaled down.
	cleanupPods := []string{}
	cleanupPods = append(cleanupPods, newPodNames...)

	// Find dangling pods in podstatus
	if aeroCluster.Status.PodStatus != nil {
		for podName := range aeroCluster.Status.PodStatus {
			ordinal, err := getStatefulSetPodOrdinal(podName)
			if err != nil {
				return found, fmt.Errorf("Invalid pod name: %s", podName)
			}

			if *ordinal >= desiredSize {
				cleanupPods = append(cleanupPods, podName)
			}
		}
	}

	err = r.cleanupPods(aeroCluster, cleanupPods)
	if err != nil {
		return found, fmt.Errorf("Failed scale up pre-check: %v", err)
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for each pod
		for _, podName := range newPodNames {
			if err := r.createServiceForPod(aeroCluster, podName, aeroCluster.Namespace); err != nil {
				return found, err
			}
		}
	}

	// Scale up the statefulset
	if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
		return found, fmt.Errorf("Failed to update StatefulSet pods: %v", err)
	}

	if err := r.waitForStatefulSetToBeReady(found); err != nil {
		return found, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
	}

	// return a fresh copy
	return r.getStatefulSet(aeroCluster, rackState)
}

func (r *ReconcileAerospikeCluster) upgrade(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredImage string, rackState RackState) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	currentBuild := found.Spec.Template.Spec.Containers[0].Image
	logger.Info("Upgrading/Downgrading AerospikeCluster", log.Ctx{"desiredImage": aeroCluster.Spec.Build, "currentImage": currentBuild})

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}

	// Update strategy for statefulSet is OnDelete, so client.Update will not start update.
	// Update will happen only when a pod is deleted.
	// So first update build and then delete a pod. Pod will come up with new image.
	// Repeat the above process.
	if found.Spec.Template.Spec.Containers[0].Image != desiredImage {
		found.Spec.Template.Spec.Containers[0].Image = desiredImage
		logger.Info("Updating image in statefulset spec", log.Ctx{"desiredImage": aeroCluster.Spec.Build, "currentImage": currentBuild})
		err = r.client.Update(context.TODO(), found, updateOption)
		if err != nil {
			return found, fmt.Errorf("Failed to update image for StatefulSet %s: %v", found.Name, err)
		}
	}

	for _, p := range podList {
		// Also check if statefulSet is in stable condition
		// Check for all containers. Status.ContainerStatuses doesn't include init container
		for _, ps := range p.Status.ContainerStatuses {
			logger.Info("Upgrading/downgrading pod", log.Ctx{"podName": p.Name, "currentImage": ps.Image, "desiredImage": desiredImage})

			// Looks like bad image
			if ps.Image == desiredImage {
				if err := utils.CheckPodFailed(&p); err != nil {
					return found, err
				}
			}
			if ps.Image != desiredImage {
				logger.Debug("Delete the Pod", log.Ctx{"podName": p.Name})

				// If already dead node, so no need to check node safety, migration
				if err := utils.CheckPodFailed(&p); err == nil {
					if err := r.waitForNodeSafeStopReady(aeroCluster, &p); err != nil {
						return found, err
					}
				}

				// Delete pod
				if err := r.client.Delete(context.TODO(), &p); err != nil {
					return found, err
				}
				logger.Debug("Pod deleted", log.Ctx{"podName": p.Name})

				// Wait for pod to come up
				for {
					logger.Debug("Waiting for pod to be ready after delete", log.Ctx{"podName": p.Name})

					pFound := &corev1.Pod{}
					err = r.client.Get(context.TODO(), types.NamespacedName{Name: p.Name, Namespace: p.Namespace}, pFound)
					if err != nil {
						logger.Error("Failed to get pod", log.Ctx{"podName": p.Name, "err": err})

						if found, err = r.getStatefulSet(aeroCluster, rackState); err != nil {
							// Stateful set has been deleted.
							// TODO Ashish to rememeber which scenario this can happen.
							logger.Error("Statefulset has been deleted for pod", log.Ctx{"podName": p.Name, "err": err})
							return nil, err
						}

						time.Sleep(time.Second * 5)
						continue
					}
					if err := utils.CheckPodFailed(pFound); err != nil {
						return found, err
					}
					if !utils.IsPodUpgraded(pFound, desiredImage) {
						logger.Debug("Waiting for pod to come up with new image", log.Ctx{"podName": p.Name})
						time.Sleep(time.Second * 5)
						continue
					}

					logger.Info("Pod is upgraded/downgraded", log.Ctx{"podName": p.Name})
					break
				}
			}
		}
	}
	// return a fresh copy
	return r.getStatefulSet(aeroCluster, rackState)
}

func (r *ReconcileAerospikeCluster) rollingRestart(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	logger.Info("Rolling restart AerospikeCluster statefulset nodes with new config")

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}
	if r.isAnyPodInFailedState(aeroCluster, podList) {
		return found, fmt.Errorf("Cannot Rolling restart AerospikeCluster. A pod is already in failed state")
	}

	// Can we optimize this? Update stateful set only if there is any update for it.
	//updateStatefulSetStorage(aeroCluster, found)

	updateStatefulSetAerospikeServerContainerResources(aeroCluster, found)

	updateStatefulSetSecretInfo(aeroCluster, found)

	logger.Info("Updating statefulset spec")
	if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
		return found, fmt.Errorf("Failed to update StatefulSet %s: %v", found.Name, err)
	}
	logger.Info("Statefulset spec updated. Doing rolling restart with new config")

	for _, pod := range podList {
		// Also check if statefulSet is in stable condition
		// Check for all containers. Status.ContainerStatuses doesn't include init container
		if pod.Status.ContainerStatuses == nil {
			return found, fmt.Errorf("Pod %s containerStatus is nil, pod may be in unscheduled state", pod.Name)
		}

		logger.Info("Rolling restart pod", log.Ctx{"podName": pod.Name})
		var pFound *corev1.Pod

		for i := 0; i < 5; i++ {
			logger.Debug("Waiting for pod to be ready", log.Ctx{"podName": pod.Name})

			pFound = &corev1.Pod{}
			err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, pFound)
			if err != nil {
				logger.Error("Failed to get pod, try retry after 5 sec", log.Ctx{"err": err})
				time.Sleep(time.Second * 5)
				continue
			}
			ps := pFound.Status.ContainerStatuses[0]
			if ps.Ready {
				break
			}

			if utils.IsCrashed(pFound) {
				logger.Error("Pod has crashed", log.Ctx{"podName": pFound.Name})
				break
			}

			logger.Error("Pod containerStatus is not ready, try after 5 sec")
			time.Sleep(time.Second * 5)
		}

		err = utils.CheckPodFailed(pFound)
		if err == nil {
			// Check for migration
			if err := r.waitForNodeSafeStopReady(aeroCluster, pFound); err != nil {
				return found, err
			}
		} else {
			// TODO: Check a user flag to restart failed pods.
			logger.Info("Restarting failed pod", log.Ctx{"podName": pFound.Name, "error": err})
		}

		// Delete pod
		if err := r.client.Delete(context.TODO(), pFound); err != nil {
			logger.Error("Failed to delete pod", log.Ctx{"err": err})
			continue
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
				return found, err
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

		if !started {
			logger.Error("Pos is not running or ready. Pod might also be terminating", log.Ctx{"podName": pod.Name, "status": pod.Status.Phase, "DeletionTimestamp": pod.DeletionTimestamp})
		}

	}
	// return a fresh copy
	return r.getStatefulSet(aeroCluster, rackState)
}

func (r *ReconcileAerospikeCluster) scaleDown(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, rackState RackState) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeClusterSTS": getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID)})

	desiredSize := int32(rackState.Size)

	logger.Info("ScaleDown AerospikeCluster statefulset", log.Ctx{"desiredSz": desiredSize, "currentSz": *found.Spec.Replicas})

	oldPodList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}

	if r.isAnyPodInFailedState(aeroCluster, oldPodList.Items) {
		return found, fmt.Errorf("Cannot scale down AerospikeCluster. A pod is already in failed state")
	}

	var removedPods []*corev1.Pod

	for *found.Spec.Replicas > desiredSize {

		// maintain list of removed pods. It will be used for alumni-reset and tip-clear
		podName := getStatefulSetPodName(found.Name, *found.Spec.Replicas-1)

		pod := utils.GetPod(podName, oldPodList.Items)

		removedPods = append(removedPods, pod)

		// Ignore safe stop check on pod not in running state.
		if utils.IsPodRunningAndReady(pod) {
			if err := r.waitForNodeSafeStopReady(aeroCluster, pod); err != nil {
				// The pod is running and is unsafe to terminate.
				return found, err
			}
		}

		// Update new object with new size
		newSize := *found.Spec.Replicas - 1
		found.Spec.Replicas = &newSize
		if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
			return found, fmt.Errorf("Failed to update pod size %d StatefulSet pods: %v", newSize, err)
		}

		// Wait for pods to get terminated
		if err := r.waitForStatefulSetToBeReady(found); err != nil {
			return found, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
		}

		// Fetch new object
		nFound, err := r.getStatefulSet(aeroCluster, rackState)
		if err != nil {
			return found, fmt.Errorf("Failed to get StatefulSet pods: %v", err)
		}
		found = nFound

		err = r.cleanupPods(aeroCluster, []string{podName})
		if err != nil {
			return nFound, fmt.Errorf("Failed to cleanup pod %s: %v", podName, err)
		}

		logger.Info("Pod Removed", log.Ctx{"podName": podName})
	}

	newPodList, err := r.getRackPodList(aeroCluster, rackState.Rack.ID)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}

	// Do post-remove-node info calls
	for _, np := range newPodList.Items {
		// TODO: We remove node from the end. Nodes will not have seed of successive nodes
		// So this will be no op.
		// We should tip in all nodes the same seed list,
		// then only this will have any impact. Is it really necessary?

		// TODO: tip after scaleup and create
		// All nodes from other rack
		for _, rp := range removedPods {
			r.tipClearHostname(aeroCluster, &np, rp)
		}
		r.alumniReset(aeroCluster, &np)
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Remove service for pod
		for _, rp := range removedPods {
			// TODO: make it more roboust, what if it fails
			if err := r.deleteServiceForPod(rp.Name, aeroCluster.Namespace); err != nil {
				return found, err
			}
		}
	}

	// This is alredy new copy, no need to fetch again
	return found, nil
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
	var hosts []*aero.Host
	for _, conn := range conns {
		hosts = append(hosts, &aero.Host{
			Name:    conn.ASConn.AerospikeHostName,
			TLSName: conn.ASConn.AerospikeTLSName,
			Port:    conn.ASConn.AerospikePort,
		})
	}
	// Create policy using status, status has current connection info
	aeroClient, err := aero.NewClientWithPolicyAndHost(r.getClientPolicy(aeroCluster), hosts...)
	if err != nil {
		return fmt.Errorf("Failed to create aerospike cluster client: %v", err)
	}

	defer aeroClient.Close()

	pp := r.getPasswordProvider(aeroCluster)
	err = accessControl.ReconcileAccessControl(&aeroCluster.Spec, &aeroCluster.Status.AerospikeClusterSpec, aeroClient, pp, logger)
	return err
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

	if newAeroCluster.Status.Nodes == nil {
		newAeroCluster.Status.Nodes = []aerospikev1alpha1.AerospikeNodeSummary{}
	}

	if newAeroCluster.Status.PodStatus == nil {
		newAeroCluster.Status.PodStatus = map[string]aerospikev1alpha1.AerospikePodStatus{}
	}

	if err = r.client.Status().Update(context.TODO(), newAeroCluster); err != nil {
		return fmt.Errorf("Error creating status: %v", err)
	}

	return nil
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

	// Commit important access control information since getting node summary could take time.
	err = r.patchStatus(aeroCluster, newAeroCluster)
	if err != nil {
		return fmt.Errorf("Error patching status: %v", err)
	}

	summary, err := r.getAerospikeClusterNodeSummary(newAeroCluster)
	if err != nil {
		// This should not be error. Before status update, we already try reconcileAccessControl
		// This error may be because of few nodes, That should not be cosidered as full cluster error
		logger.Error("Failed to get nodes summary", log.Ctx{"err": err})
	}

	newAeroCluster.Status.Nodes = summary
	logger.Info("Updated status", log.Ctx{"status": newAeroCluster.Status})

	return r.patchStatus(aeroCluster, newAeroCluster)
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
		// podStatus should never be updated here
		// podStatus is updated only from 2 places
		// 1: While pod init, it will add pod in podStatus
		// 2: While pod cleanup, it will remove pod from podStatus
		if strings.HasPrefix(operation.Path, "/status") && !strings.HasPrefix(operation.Path, "/status/podStatus") {
			filteredPatch = append(filteredPatch, operation)
		}
	}

	if len(filteredPatch) == 0 {
		logger.Info("No status change required")
		return nil
	}

	jsonpatchJSON, err := json.Marshal(filteredPatch)

	if err != nil {
		return fmt.Errorf("Error marshalling json patch: %v", err)
	}

	patch := client.ConstantPatch(types.JSONPatchType, jsonpatchJSON)

	if err = r.client.Status().Patch(context.TODO(), newAeroCluster, patch, client.FieldOwner(patchFieldOwner)); err != nil {
		return fmt.Errorf("Error updating status: %v", err)
	}
	return nil
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
			Path:      "/status/podStatus/" + podName,
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

// cleanupPods checks pods and status before scaleup to detect and fix any status anomalies.
func (r *ReconcileAerospikeCluster) cleanupPods(aeroCluster *aerospikev1alpha1.AerospikeCluster, podNames []string) error {
	needStatusCleanup := []string{}
	for _, podName := range podNames {
		_, ok := aeroCluster.Status.PodStatus[podName]
		if ok {
			needStatusCleanup = append(needStatusCleanup, podName)
		}
	}

	if len(needStatusCleanup) > 0 {
		err := r.removePodStatus(aeroCluster, needStatusCleanup)
		if err != nil {
			return fmt.Errorf("Could not cleanup pod status: %v", err)
		}
	}

	// TODO: Remove dangling volumes that should have been cascade deleted on pod termination.

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
func (r *ReconcileAerospikeCluster) recoverFailedCreate(aeroCluster *aerospikev1alpha1.AerospikeCluster) (reconcile.Result, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Forcing a cluster recreate as status is nil. The cluster could be unreachable due to bad configuration.")

	// Delete all statefulsets and everything related so that it can be properly created and updated in next run.
	statefulSetList, err := r.getClusterStatefulSets(aeroCluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("Error getting statefulsets while forcing recreate of the cluster as status is nil: %v", err)
	}

	logger.Debug("Found statefulset for cluster. Need to delete them", log.Ctx{"nSTS": len(statefulSetList.Items)})
	for _, statefulset := range statefulSetList.Items {
		if err := r.deleteStatefulSet(aeroCluster, &statefulset); err != nil {
			return reconcile.Result{}, fmt.Errorf("Error deleting statefulset while forcing recreate of the cluster as status is nil: %v", err)
		}
	}

	return reconcile.Result{}, fmt.Errorf("Forcing recreate of the cluster as status is nil")
}
