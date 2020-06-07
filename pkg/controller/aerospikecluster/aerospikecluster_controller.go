package aerospikecluster

import (
	"context"
	"fmt"
	"reflect"
	"time"

	aero "github.com/aerospike/aerospike-client-go"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
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

const aerospikeConfConfigMapName = "aerospike-conf"

const defaultUser = "admin"
const defaultPass = "admin"

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
	logger.Info("AerospikeCluster", log.Ctx{"Spec": aeroCluster.Spec})

	// Get/Create statefulset
	logger.Info("Check if the StatefulSet already exists, if not create a new one")
	found := &appsv1.StatefulSet{}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		return r.createCluster(aeroCluster)
	}

	return r.reconcileCluster(aeroCluster, found)
}

func (r *ReconcileAerospikeCluster) createCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) (reconcile.Result, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Create new Aerospike cluster")

	if err := r.createHeadlessSvc(aeroCluster); err != nil {
		logger.Error("Failed to create headless service", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// Bad config should not come here. It should be validated in validation hook
	if err := r.buildConfigMap(aeroCluster, aerospikeConfConfigMapName); err != nil {
		logger.Error("Failed to create configMap from AerospikeConfig", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// IMP: Currently created by user before creating cluster.
	// User may not be comfortable in giving permission of creating serviceAccount by operator

	// if err := r.createServiceAccountWithPermission(aeroCluster); err != nil {
	// 	logger.Error("Failed to create rbac", log.Ctx{"err": err})
	// 	return reconcile.Result{}, err
	// }

	if found, err := r.createStatefulSetForAerospikeCluster(aeroCluster); err != nil {
		logger.Error("Failed to create statefulset", log.Ctx{"err": err})
		// return reconcile.Result{}, nil
		// Delete statefulset and everything related so that it can be properly created and updated in next run
		r.deleteStatefulSet(aeroCluster, found)
		return reconcile.Result{}, err
	}

	// Setup access control.
	if err := r.reconcileAccessControl(aeroCluster); err != nil {
		logger.Error("Failed to reconcile access control", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	// Update the AerospikeCluster status with the pod names
	if err := r.updateStatus(aeroCluster); err != nil {
		logger.Error("Failed to update AerospikeCluster status", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAerospikeCluster) reconcileCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet) (reconcile.Result, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Reconcile existing Aerospike cluster")

	logger.Info("Ensure the StatefulSet size is the same as the spec")

	desiredSize := aeroCluster.Spec.Size

	var err error
	// nil aerospikeConfig in status indicate that AerospikeCluster object is created but status is not successfully updated even once
	var needRollingRestart bool
	// AerospikeConfig nil means status not updated yet
	if aeroCluster.Status.AerospikeConfig != nil {
		needRollingRestart = !reflect.DeepEqual(aeroCluster.Spec.AerospikeConfig, aeroCluster.Status.AerospikeConfig)
		if needRollingRestart {
			logger.Info("AerospikeConfig changed. Need rolling restart")
			if err := r.updateConfigMap(aeroCluster, aerospikeConfConfigMapName); err != nil {
				logger.Error("Failed to update configMap from AerospikeConfig", log.Ctx{"err": err})
				return reconcile.Result{}, err
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

	// Scale down
	if *found.Spec.Replicas > desiredSize {
		found, err = r.scaleDown(aeroCluster, found, desiredSize)
		if err != nil {
			logger.Error("Failed to scaleDown StatefulSet pods", log.Ctx{"err": err})
			return reconcile.Result{}, err
		}
	}

	// Upgrade
	upgradeNeeded, err := r.isAeroClusterUpgradeNeeded(aeroCluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	if upgradeNeeded {
		found, err = r.upgrade(aeroCluster, found, aeroCluster.Spec.Build)
		if err != nil {
			logger.Error("Failed to update StatefulSet image", log.Ctx{"err": err})
			return reconcile.Result{}, err
		}
	} else if needRollingRestart {
		found, err = r.rollingRestart(aeroCluster, found)
		if err != nil {
			logger.Error("Failed to do rolling restart", log.Ctx{"err": err})
			return reconcile.Result{}, err
		}
	}

	// Scale up after upgrading, so that new pods comeup with new image
	if *found.Spec.Replicas < desiredSize {
		found, err = r.scaleUp(aeroCluster, found, desiredSize)
		if err != nil {
			logger.Error("Failed to scaleUp StatefulSet pods", log.Ctx{"err": err})
			return reconcile.Result{}, err
		}
	}

	if !reflect.DeepEqual(aeroCluster.Spec.AerospikeAccessControl, aeroCluster.Status.AerospikeAccessControl) {
		// Set access control at the end, so that previous info calls can use auth info stored in aeroCluster.status
		if err := r.reconcileAccessControl(aeroCluster); err != nil {
			logger.Error("Failed reconcile access control", log.Ctx{"err": err})
			return reconcile.Result{}, err
		}
	}

	// Update the AerospikeCluster status with the pod names
	// UpdateStatus will always use auth info stored in aeroCluster.Spec,
	// as at this point cluster should be running according to its spec
	if err := r.updateStatus(aeroCluster); err != nil {
		logger.Error("Failed to update AerospikeCluster status", log.Ctx{"err": err})
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileAerospikeCluster) scaleUp(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredSize int32) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	oldSz := *found.Spec.Replicas
	found.Spec.Replicas = &desiredSize

	logger.Info("Scaling up pods", log.Ctx{"currentSz": oldSz, "desiredSz": desiredSize})

	// No need for this? But if image is bad then new pod will also comeup with bad node.
	podList, err := r.getPodList(aeroCluster)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}
	if r.isAnyPodInFailedState(aeroCluster, podList.Items) {
		return found, fmt.Errorf("Cannot scale up AerospikeCluster. A pod is already in failed state")
	}

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for each pod
		for i := oldSz; i < desiredSize; i++ {
			name := getStatefulSetPodName(found.Name, i)
			if err := r.createServiceForPod(aeroCluster, name, aeroCluster.Namespace); err != nil {
				return found, err
			}
		}
	}

	// Scale up the statefulset
	if err := r.client.Update(context.TODO(), found, updateOption); err != nil {
		return found, fmt.Errorf("Failed to update StatefulSet pods: %v", err)
	}

	if err := r.waitForStatefulSetToBeReady(aeroCluster, found); err != nil {
		return found, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
	}

	// return a fresh copy
	return r.getStatefulSet(aeroCluster)
}

func (r *ReconcileAerospikeCluster) upgrade(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredImage string) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	currentBuild := found.Spec.Template.Spec.Containers[0].Image
	logger.Info("Upgrading/Downgrading AerospikeCluster", log.Ctx{"desiredImage": aeroCluster.Spec.Build, "currentImage": currentBuild})

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedPodList(aeroCluster)
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
				if err := utils.PodFailedStatus(&p); err != nil {
					return found, err
				}
			}
			if ps.Image != desiredImage {
				logger.Debug("Delete the Pod", log.Ctx{"podName": p.Name})

				// If already dead node, so no need to check node safety, migration
				if err := utils.PodFailedStatus(&p); err == nil {
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
						time.Sleep(time.Second * 5)
						continue
					}
					if err := utils.PodFailedStatus(pFound); err != nil {
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
	return r.getStatefulSet(aeroCluster)
}

func (r *ReconcileAerospikeCluster) rollingRestart(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Rolling restart AerospikeCluster nodes with new config")

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getOrderedPodList(aeroCluster)
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
			logger.Error("Pod containerStatus is not ready, try after 5 sec")
			time.Sleep(time.Second * 5)
		}

		// Check for migration
		if err := r.waitForNodeSafeStopReady(aeroCluster, pFound); err != nil {
			return found, err
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
			if err := utils.PodFailedStatus(pod); err != nil {
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
	return r.getStatefulSet(aeroCluster)
}

func (r *ReconcileAerospikeCluster) scaleDown(aeroCluster *aerospikev1alpha1.AerospikeCluster, found *appsv1.StatefulSet, desiredSize int32) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("ScaleDown AerospikeCluster", log.Ctx{"desiredSz": desiredSize, "currentSz": *found.Spec.Replicas})

	oldPodList, err := r.getPodList(aeroCluster)
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
		if err := r.waitForStatefulSetToBeReady(aeroCluster, found); err != nil {
			return found, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
		}

		// Fetch new object
		nFound, err := r.getStatefulSet(aeroCluster)
		if err != nil {
			return found, fmt.Errorf("Failed to get StatefulSet pods: %v", err)
		}
		found = nFound

		logger.Info("Pod Removed", log.Ctx{"podName": podName})
	}

	newPodList, err := r.getPodList(aeroCluster)
	if err != nil {
		return found, fmt.Errorf("Failed to list pods: %v", err)
	}

	// Do post-remove-node info calls
	for _, np := range newPodList.Items {
		// TODO: We remove node from the end. Nodes will not have seed of successive nodes
		// So this will be no op.
		// We should tip in all nodes the same seed list,
		// then only this will have any impact. Is it really necessary?
		// for _, rp := range removedPods {
		// 	r.tipClearHostname(aeroCluster, &np, rp)
		// }
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

func (r *ReconcileAerospikeCluster) updateStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Update status for AerospikeCluster")

	// Get the old object, it may have been updated in between.
	newAeroCluster := &aerospikev1alpha1.AerospikeCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newAeroCluster)
	if err != nil {
		return err
	}

	if err := lib.DeepCopy(&newAeroCluster.Status.AerospikeClusterSpec, &aeroCluster.Spec); err != nil {
		return err
	}

	// Commit important access control information since getting node summary could take time.
	newAeroCluster.Status.Nodes = []aerospikev1alpha1.AerospikeNodeSummary{}
	if err = r.client.Status().Update(context.Background(), newAeroCluster); err != nil {
		return err
	}

	summary, err := r.getAerospikeClusterNodeSummary(newAeroCluster)
	if err != nil {
		logger.Error("Failed to get nodes summary", log.Ctx{"err": err})
	}
	newAeroCluster.Status.Nodes = summary
	logger.Info("AerospikeCluster", log.Ctx{"status": newAeroCluster.Status})

	if err = r.client.Status().Update(context.Background(), newAeroCluster); err != nil {
		return err
	}
	return nil
}
