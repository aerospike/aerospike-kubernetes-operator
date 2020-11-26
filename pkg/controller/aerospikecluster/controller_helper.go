package aerospikecluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	as "github.com/ashishshinde/aerospike-client-go"
	"github.com/ashishshinde/aerospike-client-go/pkg/ripemd160"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	accessControl "github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/asconfig"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/configmap"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/controller/utils"
	log "github.com/inconshreveable/log15"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	aeroClusterServiceAccountName string = "aerospike-cluster"
	configMaPodListKey            string = "podList.json"
)

// The default cpu request for the aerospike-server container
const (
	aerospikeServerContainerDefaultCPURequest = 1
	// The default memory request for the aerospike-server container
	// matches the default value of namespace.memory-size
	// https://www.aerospike.com/docs/reference/configuration#memory-size
	aerospikeServerContainerDefaultMemoryRequestGi         = 4
	aerospikeServerNamespaceDefaultFilesizeMemoryRequestGi = 1

	// This storage path annotation is added in pvc to make reverse association with storage.volume.path
	// while deleting pvc
	storagePathAnnotationKey = "storage-path"

	confDirName     = "confdir"
	initConfDirName = "initconfigs"
)

//------------------------------------------------------------------------------------
// controller helper
//------------------------------------------------------------------------------------

func (r *ReconcileAerospikeCluster) createStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rackState RackState) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	replicas := int32(rackState.Size)

	logger.Info("Create statefulset for AerospikeCluster", log.Ctx{"size": replicas})

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for all statefulset pods
		for i := 0; i < rackState.Size; i++ {
			// Statefulset name created from cr name
			name := fmt.Sprintf("%s-%d", namespacedName.Name, i)
			if err := r.createServiceForPod(aeroCluster, name, aeroCluster.Namespace); err != nil {
				return nil, err
			}
		}
	}

	ports := getAeroClusterContainerPort(aeroCluster.Spec.MultiPodPerHost)

	ls := utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackState.Rack.ID)

	envVarList := []corev1.EnvVar{
		newEnvVar("MY_POD_NAME", "metadata.name"),
		newEnvVar("MY_POD_NAMESPACE", "metadata.namespace"),
		newEnvVar("MY_POD_IP", "status.podIP"),
		newEnvVar("MY_HOST_IP", "status.hostIP"),
		newEnvVarStatic("MY_POD_TLS_NAME", getServiceTLSName(aeroCluster)),
		newEnvVarStatic("MY_POD_CLUSTER_NAME", aeroCluster.Name),
	}

	if rackState.Rack.ID != utils.DefaultRackID {
		envVarList = append(envVarList, newEnvVarStatic("MY_POD_RACK_ID", strconv.Itoa(rackState.Rack.ID)))
	}

	if name := getServiceTLSName(aeroCluster); name != "" {
		envVarList = append(envVarList, newEnvVarStatic("MY_POD_TLS_ENABLED", "true"))
	}

	st := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			Labels:    ls,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			ServiceName: aeroCluster.Name,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: aeroClusterServiceAccountName,
					//TerminationGracePeriodSeconds: &int64(30),
					InitContainers: []corev1.Container{{
						Name:  "aerospike-init",
						Image: "aerospike/aerospike-kubernetes-init:0.0.12",
						// Change to PullAlways for image testing.
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      confDirName,
								MountPath: "/etc/aerospike",
							},
							{
								Name:      initConfDirName,
								MountPath: "/configs",
							},
						},
						Env: append(envVarList, []corev1.EnvVar{
							{
								// Headless service has same name as AerospikeCluster
								Name:  "SERVICE",
								Value: getHeadLessSvcName(aeroCluster),
							},
							// TODO: Do we need this var?
							{
								Name:  "CONFIG_MAP_NAME",
								Value: getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID).Name,
							},
						}...),
					}},

					Containers: []corev1.Container{{
						Name:            "aerospike-server",
						Image:           aeroCluster.Spec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports:           ports,
						Env:             envVarList,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      confDirName,
								MountPath: "/etc/aerospike",
							},
						},
						// Resources to be updated later
					}},

					Volumes: []corev1.Volume{
						{
							Name: confDirName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: initConfDirName,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: getNamespacedNameForConfigMap(aeroCluster, rackState.Rack.ID).Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	updateStatefulSetPodSpec(aeroCluster, st)

	updateStatefulSetAerospikeServerContainerResources(aeroCluster, st)
	// TODO: Add validation. device, file, both should not exist in same storage class
	if err := updateStatefulSetStorage(aeroCluster, st, rackState); err != nil {
		return nil, err
	}

	updateStatefulSetSecretInfo(aeroCluster, st)

	updateStatefulSetConfigMapVolumes(aeroCluster, st)

	updateStatefulSetAffinity(aeroCluster, st, ls, rackState)
	// Set AerospikeCluster instance as the owner and controller
	controllerutil.SetControllerReference(aeroCluster, st, r.scheme)

	if err := r.client.Create(context.TODO(), st, createOption); err != nil {
		return nil, fmt.Errorf("Failed to create new StatefulSet: %v", err)
	}
	logger.Info("Created new StatefulSet", log.Ctx{"StatefulSet.Namespace": st.Namespace, "StatefulSet.Name": st.Name})

	if err := r.waitForStatefulSetToBeReady(st); err != nil {
		return st, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
	}

	return r.getStatefulSet(aeroCluster, rackState)
}

// TODO: Cascade delete storage as well.
func (r *ReconcileAerospikeCluster) deleteStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Delete statefulset")
	// No need to do cleanup pods after deleting sts
	// It is only deleted while its creation is failed
	// While doing rackRemove, we call scaleDown to 0 so that will do cleanup
	return r.client.Delete(context.TODO(), st)
}

func (r *ReconcileAerospikeCluster) waitForStatefulSetToBeReady(st *appsv1.StatefulSet) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster statefulset": types.NamespacedName{Name: st.Name, Namespace: st.Namespace}})

	const podStatusMaxRetry = 18
	const podStatusRetryInterval = time.Second * 10

	logger.Info("Waiting for statefulset to be ready", log.Ctx{"WaitTimePerPod": podStatusRetryInterval * time.Duration(podStatusMaxRetry)})

	var podIndex int32
	for podIndex = 0; podIndex < *st.Spec.Replicas; podIndex++ {
		podName := getStatefulSetPodName(st.Name, podIndex)

		var isReady bool
		pod := &corev1.Pod{}

		// Wait for 10 sec to pod to get started
		for i := 0; i < 5; i++ {
			time.Sleep(time.Second * 2)
			if err := r.client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: st.Namespace}, pod); err == nil {
				break
			}
		}
		// Wait for pod to get ready
		for i := 0; i < podStatusMaxRetry; i++ {
			time.Sleep(podStatusRetryInterval)

			logger.Debug("Check statefulSet pod running and ready", log.Ctx{"pod": podName})

			pod := &corev1.Pod{}
			if err := r.client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: st.Namespace}, pod); err != nil {
				return fmt.Errorf("Failed to get statefulSet pod %s: %v", podName, err)
			}
			if err := utils.CheckPodFailed(pod); err != nil {
				return fmt.Errorf("StatefulSet pod %s failed: %v", podName, err)
			}
			if utils.IsPodRunningAndReady(pod) {
				isReady = true
				logger.Info("Pod is running and ready", log.Ctx{"pod": podName})
				break
			}
		}
		if !isReady {
			statusErr := fmt.Errorf("StatefulSet pod is not ready. Status: %v", pod.Status.Conditions)
			logger.Error("Statefulset Not ready", log.Ctx{"err": statusErr})
			return statusErr
		}
	}

	// Check for statfulset at the end,
	// if we check if before pods then we would not know status of individual pods
	const stsStatusMaxRetry = 10
	const stsStatusRetryInterval = time.Second * 2

	var updated bool
	for i := 0; i < stsStatusMaxRetry; i++ {
		time.Sleep(stsStatusRetryInterval)

		logger.Debug("Check statefulSet status is updated or not")

		err := r.client.Get(context.TODO(), types.NamespacedName{Name: st.Name, Namespace: st.Namespace}, st)
		if err != nil {
			return err
		}
		if *st.Spec.Replicas == st.Status.Replicas {
			updated = true
			break
		}
		logger.Debug("Statefulset spec.replica not matching status.replica", log.Ctx{"staus": st.Status.Replicas, "spec": *st.Spec.Replicas})
	}
	if !updated {
		return fmt.Errorf("Statefulset status is not updated")
	}

	logger.Info("Statefulset is ready")

	return nil
}

func (r *ReconcileAerospikeCluster) getStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackState RackState) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	err := r.client.Get(context.TODO(), getNamespacedNameForStatefulSet(aeroCluster, rackState.Rack.ID), found)
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (r *ReconcileAerospikeCluster) buildConfigMap(aeroCluster *aerospikev1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Creating a new ConfigMap for statefulSet")

	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: namespacedName.Name, Namespace: namespacedName.Namespace}, confMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// build the aerospike config file based on the current spec
			configMapData, err := configmap.CreateConfigMapData(aeroCluster, rack)
			if err != nil {
				return fmt.Errorf("Failed to build dotConfig from map: %v", err)
			}
			ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)

			// return a configmap object containing aerospikeConfig
			confMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Labels:    ls,
					Namespace: namespacedName.Namespace,
				},
				Data: configMapData,
			}
			// Set AerospikeCluster instance as the owner and controller
			controllerutil.SetControllerReference(aeroCluster, confMap, r.scheme)

			if err := r.client.Create(context.TODO(), confMap, createOption); err != nil {
				return fmt.Errorf("Failed to create new confMap for StatefulSet: %v", err)
			}
			logger.Info("Created new ConfigMap", log.Ctx{"ConfigMap.Namespace": confMap.Namespace, "ConfigMap.Name": confMap.Name})

			return nil
		}
		return err
	}

	logger.Info("Configmap already exists for statefulSet - using existing configmap", log.Ctx{"name": utils.NamespacedName(confMap.Namespace, confMap.Name)})

	// Update existing configmap as it might not be current.
	configMapData, err := configmap.CreateConfigMapData(aeroCluster, rack)
	if err != nil {
		return fmt.Errorf("Failed to build config map data: %v", err)
	}

	// Replace config map data since we are supposed to create a new config map.
	confMap.Data = configMapData

	if err := r.client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("Failed to update ConfigMap for StatefulSet: %v", err)
	}
	return nil
}

func (r *ReconcileAerospikeCluster) updateConfigMap(aeroCluster *aerospikev1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rack aerospikev1alpha1.Rack) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Updating ConfigMap", log.Ctx{"ConfigMap": namespacedName})

	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), namespacedName, confMap)
	if err != nil {
		return err
	}

	// build the aerospike config file based on the current spec
	configMapData, err := configmap.CreateConfigMapData(aeroCluster, rack)
	if err != nil {
		return fmt.Errorf("Failed to build dotConfig from map: %v", err)
	}

	// Overwrite only spec based keys. Do not touch other keys like pod metadata.
	for k, v := range configMapData {
		confMap.Data[k] = v
	}

	if err := r.client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("Failed to update confMap for StatefulSet: %v", err)
	}
	return nil
}

func (r *ReconcileAerospikeCluster) updateConfigMapPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster, podNames []string) error {
	name := utils.ConfigMapName(aeroCluster)

	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Updating ConfigMap pod names", log.Ctx{"ConfigMap.Namespace": aeroCluster.Namespace, "ConfigMap.Name": name})

	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: aeroCluster.Namespace}, confMap)
	if err != nil {
		return err
	}

	podNamesJSON, _ := json.Marshal(podNames)
	confMap.Data[configMaPodListKey] = string(podNamesJSON)

	if err := r.client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("Failed to update confMap for pod names: %v", err)
	}
	return nil
}

func (r *ReconcileAerospikeCluster) createHeadlessSvc(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Create headless service for statefulSet")

	ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)

	service := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, service)
	if err != nil {
		if errors.IsNotFound(err) {
			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					// Headless service has same name as AerospikeCluster
					Name:      getHeadLessSvcName(aeroCluster),
					Namespace: aeroCluster.Namespace,
					// deprecation in 1.10, supported until at least 1.13,  breaks peer-finder/kube-dns if not used
					Annotations: map[string]string{
						"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
					},
					Labels: ls,
				},
				Spec: corev1.ServiceSpec{
					// deprecates service.alpha.kubernetes.io/tolerate-unready-endpoints as of 1.10? see: kubernetes/kubernetes#49239 Fixed in 1.11 as of #63742
					PublishNotReadyAddresses: true,
					ClusterIP:                "None",
					Selector:                 ls,
					Ports: []corev1.ServicePort{
						{
							Port: 3000,
							Name: "info",
						},
					},
				},
			}
			// Set AerospikeCluster instance as the owner and controller
			controllerutil.SetControllerReference(aeroCluster, service, r.scheme)

			if err := r.client.Create(context.TODO(), service, createOption); err != nil {
				return fmt.Errorf("Failed to create headless service for statefulset: %v", err)
			}
			logger.Info("Created new headless service")

			return nil
		}
		return err
	}
	logger.Info("Service already exist for statefulSet. Using existing service", log.Ctx{"name": utils.NamespacedName(service.Namespace, service.Name)})
	return nil
}

func (r *ReconcileAerospikeCluster) createServiceForPod(aeroCluster *aerospikev1alpha1.AerospikeCluster, pName, pNamespace string) error {
	service := &corev1.Service{}
	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: pName, Namespace: pNamespace}, service); err == nil {
		return nil
	}

	// NodePort will be allocated automatically
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pName,
			Namespace: pNamespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeNodePort,
			Selector: map[string]string{
				"statefulset.kubernetes.io/pod-name": pName,
			},
			Ports: []corev1.ServicePort{
				{
					Name: "info",
					Port: utils.ServicePort,
				},
			},
			ExternalTrafficPolicy: "Local",
		},
	}
	if name := getServiceTLSName(aeroCluster); name != "" {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name: "tls",
			Port: utils.ServiceTLSPort,
		})
	}
	// Set AerospikeCluster instance as the owner and controller.
	// It is created before Pod, so Pod cannot be the owner
	controllerutil.SetControllerReference(aeroCluster, service, r.scheme)

	if err := r.client.Create(context.TODO(), service, createOption); err != nil {
		return fmt.Errorf("Failed to create new service for pod %s: %v", pName, err)
	}
	return nil
}

func (r *ReconcileAerospikeCluster) deleteServiceForPod(pName, pNamespace string) error {
	service := &corev1.Service{}

	if err := r.client.Get(context.TODO(), types.NamespacedName{Name: pName, Namespace: pNamespace}, service); err != nil {
		return fmt.Errorf("Failed to get service for pod %s: %v", pName, err)
	}
	if err := r.client.Delete(context.TODO(), service); err != nil {
		return fmt.Errorf("Failed to delete service for pod %s: %v", pName, err)
	}
	return nil
}

func (r *ReconcileAerospikeCluster) getClusterPVCList(aeroCluster *aerospikev1alpha1.AerospikeCluster) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func (r *ReconcileAerospikeCluster) getRackPVCList(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) ([]corev1.PersistentVolumeClaim, error) {
	// List the pvc for this aeroCluster's statefulset
	pvcList := &corev1.PersistentVolumeClaimList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackID))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.client.List(context.TODO(), pvcList, listOps); err != nil {
		return nil, err
	}
	return pvcList.Items, nil
}

func (r *ReconcileAerospikeCluster) getPodsPVCList(aeroCluster *aerospikev1alpha1.AerospikeCluster, podNames []string, rackID int) ([]corev1.PersistentVolumeClaim, error) {
	pvcListItems, err := r.getRackPVCList(aeroCluster, rackID)
	if err != nil {
		return nil, err
	}
	// https://github.com/kubernetes/kubernetes/issues/72196
	// No regex support in field-selector
	// Can not get pvc having matching podName. Need to check more.
	var newPVCItems []corev1.PersistentVolumeClaim
	for _, pvc := range pvcListItems {
		for _, podName := range podNames {
			// Get PVC belonging to pod only
			if strings.HasSuffix(pvc.Name, podName) {
				newPVCItems = append(newPVCItems, pvc)
			}
		}
	}
	return newPVCItems, nil
}

func (r *ReconcileAerospikeCluster) getRackPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackID))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}
	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart

	if err := r.client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *ReconcileAerospikeCluster) getOrderedRackPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) ([]corev1.Pod, error) {
	podList, err := r.getRackPodList(aeroCluster, rackID)
	if err != nil {
		return nil, err
	}
	sortedList := make([]corev1.Pod, len(podList.Items))
	for _, p := range podList.Items {
		indexStr := strings.Split(p.Name, "-")
		// Index is last, [1] can be rackID
		indexInt, _ := strconv.Atoi(indexStr[len(indexStr)-1])
		if indexInt >= len(podList.Items) {
			// Happens if we do not get full list of pods due to a crash,
			return nil, fmt.Errorf("Error get pod list for rack:%v", rackID)
		}
		sortedList[(len(podList.Items)-1)-indexInt] = p
	}
	return sortedList, nil
}

func (r *ReconcileAerospikeCluster) getClusterPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	// TODO: Should we add check to get only non-terminating pod? What if it is rolling restart
	if err := r.client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *ReconcileAerospikeCluster) getOrderedClusterPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster) ([]corev1.Pod, error) {
	podList, err := r.getClusterPodList(aeroCluster)
	if err != nil {
		return nil, err
	}
	sortedList := make([]corev1.Pod, len(podList.Items))
	for _, p := range podList.Items {
		indexStr := strings.Split(p.Name, "-")
		indexInt, _ := strconv.Atoi(indexStr[1])
		if indexInt >= len(podList.Items) {
			// Happens if we do not get full list of pods due to a crash,
			return nil, fmt.Errorf("Error get pod list for cluster: %v", aeroCluster.Name)
		}
		sortedList[(len(podList.Items)-1)-indexInt] = p
	}
	return sortedList, nil
}

func (r *ReconcileAerospikeCluster) getClusterStatefulSets(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*appsv1.StatefulSetList, error) {
	// List the pods for this aeroCluster's statefulset
	statefulSetList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.client.List(context.TODO(), statefulSetList, listOps); err != nil {
		return nil, err
	}
	return statefulSetList, nil
}

func (r *ReconcileAerospikeCluster) getClusterServerPool(aeroCluster *aerospikev1alpha1.AerospikeCluster) *x509.CertPool {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// Try to load system CA certs, otherwise just make an empty pool
	serverPool, err := x509.SystemCertPool()
	if err != nil {
		logger.Warn("Failed to add system certificates to the pool", log.Ctx{"err": err})
		serverPool = x509.NewCertPool()
	}

	// get the tls info from secret
	found := &v1.Secret{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("Failed to get secret certificates to the pool, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("Failed to get tlsName from aerospikeConfig, returning empty certPool", log.Ctx{"err": err})
		return serverPool
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"].([]interface{})
	for _, tlsConfInt := range tlsConfList {
		tlsConf := tlsConfInt.(map[string]interface{})
		if tlsConf["name"].(string) == tlsName {
			if cafile, ok := tlsConf["ca-file"]; ok {
				logger.Debug("Adding cert in tls serverpool", log.Ctx{"tlsConf": tlsConf})
				caFileName := filepath.Base(cafile.(string))
				serverPool.AppendCertsFromPEM(found.Data[caFileName])
			}
		}
	}
	return serverPool
}

func (r *ReconcileAerospikeCluster) getClientCertificate(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*tls.Certificate, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// get the tls info from secret
	found := &v1.Secret{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeConfigSecret.SecretName, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		logger.Warn("Failed to get secret certificates to the pool", log.Ctx{"err": err})
		return nil, err
	}

	tlsName := getServiceTLSName(aeroCluster)
	if tlsName == "" {
		logger.Warn("Failed to get tlsName from aerospikeConfig", log.Ctx{"err": err})
		return nil, err
	}
	// get ca-file and use as cacert
	tlsConfList := aeroCluster.Spec.AerospikeConfig["network"].(map[string]interface{})["tls"].([]interface{})
	for _, tlsConfInt := range tlsConfList {
		tlsConf := tlsConfInt.(map[string]interface{})
		if tlsConf["name"].(string) == tlsName {
			certFileName := filepath.Base(tlsConf["cert-file"].(string))
			keyFileName := filepath.Base(tlsConf["key-file"].(string))

			cert, err := tls.X509KeyPair(found.Data[certFileName], found.Data[keyFileName])
			if err != nil {
				return nil, fmt.Errorf("failed to load X509 key pair for cluster: %v", err)
			}
			return &cert, nil
		}
	}
	return nil, fmt.Errorf("Failed to get tls config for creating client certificate")
}

func (r *ReconcileAerospikeCluster) isAeroClusterUpgradeNeeded(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) (bool, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	podList, err := r.getRackPodList(aeroCluster, rackID)
	if err != nil {
		return true, fmt.Errorf("Failed to list pods: %v", err)
	}
	for _, p := range podList.Items {
		if !utils.IsPodOnDesiredImage(&p, aeroCluster) {
			logger.Info("Pod need upgrade/downgrade")
			return true, nil
		}

	}
	return false, nil
}

func (r *ReconcileAerospikeCluster) isAnyPodInFailedState(aeroCluster *aerospikev1alpha1.AerospikeCluster, podList []corev1.Pod) bool {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	for _, p := range podList {
		for _, ps := range p.Status.ContainerStatuses {
			// TODO: Should we use checkPodFailed or CheckPodImageFailed?
			// scaleDown, rollingRestart should work even if node is crashed
			// If node was crashed due to wrong config then only rollingRestart can bring it back.
			if err := utils.CheckPodImageFailed(&p); err != nil {
				logger.Info("AerospikeCluster Pod is in failed state", log.Ctx{"currentImage": ps.Image, "podName": p.Name, "err": err})
				return true
			}
		}
	}
	return false
}

// FromSecretPasswordProvider provides user password from the secret provided in AerospikeUserSpec.
type FromSecretPasswordProvider struct {
	// Client to read secrets.
	client *client.Client

	// The secret namespace.
	namespace string
}

// Get returns the password for the username using userSpec.
func (pp FromSecretPasswordProvider) Get(username string, userSpec *aerospikev1alpha1.AerospikeUserSpec) (string, error) {
	secret := &corev1.Secret{}
	secretName := userSpec.SecretName
	// Assuming secret is in same namespace
	err := (*pp.client).Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: pp.namespace}, secret)
	if err != nil {
		return "", fmt.Errorf("Failed to get secret %s: %v", secretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return "", fmt.Errorf("Failed to get password from secret. Please check your secret %s", secretName)
	}
	return string(passbyte), nil
}

func (r *ReconcileAerospikeCluster) getPasswordProvider(aeroCluster *aerospikev1alpha1.AerospikeCluster) FromSecretPasswordProvider {
	return FromSecretPasswordProvider{client: &r.client, namespace: aeroCluster.Namespace}
}

func (r *ReconcileAerospikeCluster) getClientPolicy(aeroCluster *aerospikev1alpha1.AerospikeCluster) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := as.NewClientPolicy()

	policy.SeedOnlyCluster = true

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// tls config
	if tlsName := getServiceTLSName(aeroCluster); tlsName != "" {
		logger.Debug("Set tls config in aeospike client policy")
		tlsConf := tls.Config{
			RootCAs:                  r.getClusterServerPool(aeroCluster),
			Certificates:             []tls.Certificate{},
			PreferServerCipherSuites: true,
			// used only in testing
			// InsecureSkipVerify: true,
		}

		cert, err := r.getClientCertificate(aeroCluster)
		if err != nil {
			logger.Error("Failed to get client certificate. Using basic clientPolicy", log.Ctx{"err": err})
			return policy
		}
		tlsConf.Certificates = append(tlsConf.Certificates, *cert)

		tlsConf.BuildNameToCertificate()
		policy.TlsConfig = &tlsConf
	}

	user, pass, err := accessControl.AerospikeAdminCredentials(&aeroCluster.Spec, &aeroCluster.Status.AerospikeClusterSpec, r.getPasswordProvider(aeroCluster))
	if err != nil {
		logger.Error("Failed to get cluster auth info", log.Ctx{"err": err})
	}

	policy.User = user
	policy.Password = pass
	return policy
}

// Called only when new cluster is created
func updateStatefulSetStorage(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	storage := rackState.Rack.Storage
	// TODO: Add validation. device, file, both should not exist in same storage class
	for _, volume := range storage.Volumes {
		logger.Info("Add PVC for volume", log.Ctx{"volume": volume})
		var volumeMode corev1.PersistentVolumeMode
		var initContainerVolumePathPrefix string

		pvcName, err := getPVCName(volume.Path)
		if err != nil {
			return fmt.Errorf("Failed to create ripemd hash for pvc name from volume.path %s", volume.Path)
		}

		if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeBlock {
			volumeMode = corev1.PersistentVolumeBlock
			initContainerVolumePathPrefix = "/block-volumes"

			logger.Info("Add volume device for volume", log.Ctx{"volume": volume})
			volumeDevice := corev1.VolumeDevice{
				Name:       pvcName,
				DevicePath: volume.Path,
			}
			st.Spec.Template.Spec.Containers[0].VolumeDevices = append(st.Spec.Template.Spec.Containers[0].VolumeDevices, volumeDevice)

			initVolumeDevice := corev1.VolumeDevice{
				Name:       pvcName,
				DevicePath: initContainerVolumePathPrefix + volume.Path,
			}
			st.Spec.Template.Spec.InitContainers[0].VolumeDevices = append(st.Spec.Template.Spec.InitContainers[0].VolumeDevices, initVolumeDevice)
		} else if volume.VolumeMode == aerospikev1alpha1.AerospikeVolumeModeFilesystem {
			volumeMode = corev1.PersistentVolumeFilesystem
			initContainerVolumePathPrefix = "/filesystem-volumes"

			logger.Info("Add volume mount for volume", log.Ctx{"volume": volume})
			volumeMount := corev1.VolumeMount{
				Name:      pvcName,
				MountPath: volume.Path,
			}
			st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, volumeMount)

			initVolumeMount := corev1.VolumeMount{
				Name:      pvcName,
				MountPath: initContainerVolumePathPrefix + volume.Path,
			}
			st.Spec.Template.Spec.InitContainers[0].VolumeMounts = append(st.Spec.Template.Spec.InitContainers[0].VolumeMounts, initVolumeMount)
		} else {
			// No volume claims for config maps.
			continue
		}

		storageClass := volume.StorageClass
		pvc := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pvcName,
				Namespace: aeroCluster.Namespace,
				// Use this path annotation while matching pvc with storage volume
				Annotations: map[string]string{
					storagePathAnnotationKey: volume.Path,
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				VolumeMode:  &volumeMode,
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", volume.SizeInGB)),
					},
				},
				StorageClassName: &storageClass,
			},
		}
		st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)
	}
	return nil
}

func updateStatefulSetAffinity(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	affinity := &corev1.Affinity{}

	// only enable in production, so it can be used in 1 node clusters while debugging (minikube)
	if !aeroCluster.Spec.MultiPodPerHost {
		logger.Info("Adding pod affinity rules for statefulset pod")
		antiAffinity := &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: labels,
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		}
		affinity.PodAntiAffinity = antiAffinity
	}

	var matchExpressions []corev1.NodeSelectorRequirement

	if rackState.Rack.Zone != "" {
		matchExpressions = append(matchExpressions, corev1.NodeSelectorRequirement{
			Key:      "failure-domain.beta.kubernetes.io/zone",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Zone},
		})
	}
	if rackState.Rack.Region != "" {
		matchExpressions = append(matchExpressions, corev1.NodeSelectorRequirement{
			Key:      "failure-domain.beta.kubernetes.io/region",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.Region},
		})
	}
	if rackState.Rack.RackLabel != "" {
		matchExpressions = append(matchExpressions, corev1.NodeSelectorRequirement{
			Key:      "aerospike.com/rack-label",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.RackLabel},
		})
	}

	if rackState.Rack.NodeName != "" {
		matchExpressions = append(matchExpressions, corev1.NodeSelectorRequirement{
			Key:      "kubernetes.io/hostname",
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{rackState.Rack.NodeName},
		})
	}

	if len(matchExpressions) != 0 {
		nodeAffinity := &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: matchExpressions,
					},
				},
			},
		}
		affinity.NodeAffinity = nodeAffinity
	}

	st.Spec.Template.Spec.Affinity = affinity
}

// TODO: How to remove if user has removed this field? Should we find and remove volume
// Called while creating new cluster and also during rolling restart
func updateStatefulSetSecretInfo(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	if aeroCluster.Spec.AerospikeConfigSecret.SecretName != "" {
		const secretVolumeName = "secretinfo"
		logger.Info("Add secret volume in statefulset pods")
		var volFound bool
		for _, vol := range st.Spec.Template.Spec.Volumes {
			if vol.Name == secretVolumeName {
				vol.VolumeSource.Secret.SecretName = aeroCluster.Spec.AerospikeConfigSecret.SecretName
				volFound = true
				break
			}
		}
		if !volFound {
			secretVolume := corev1.Volume{
				Name: secretVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: aeroCluster.Spec.AerospikeConfigSecret.SecretName,
					},
				},
			}
			st.Spec.Template.Spec.Volumes = append(st.Spec.Template.Spec.Volumes, secretVolume)
		}

		var volmFound bool
		for _, vol := range st.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vol.Name == secretVolumeName {
				volmFound = true
				break
			}
		}
		if !volmFound {
			secretVolumeMount := corev1.VolumeMount{
				Name:      secretVolumeName,
				MountPath: aeroCluster.Spec.AerospikeConfigSecret.MountPath,
			}
			st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, secretVolumeMount)
		}
	}
}

// Called while creating new cluster and also during rolling restart.
func updateStatefulSetPodSpec(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	// Add new sidecars.
	for i, newSidecar := range aeroCluster.Spec.PodSpec.Sidecars {
		found := false
		for _, container := range st.Spec.Template.Spec.Containers {
			if newSidecar.Name == container.Name {
				// Update the sidecar in case something has changed.
				st.Spec.Template.Spec.Containers[i] = newSidecar
				found = true
				break
			}
		}

		if !found {
			// Add to stateful set containers.
			st.Spec.Template.Spec.Containers = append(st.Spec.Template.Spec.Containers, newSidecar)
		}
	}

	// Remove deleted sidecars.
	j := 0
	for i, container := range st.Spec.Template.Spec.Containers {
		found := i == 0
		for _, newSidecar := range aeroCluster.Spec.PodSpec.Sidecars {
			if newSidecar.Name == container.Name {
				found = true
				break
			}
		}

		if found {
			// Retain main aerospike container or a matched sidecar.
			st.Spec.Template.Spec.Containers[j] = container
			j++
		}
	}
	st.Spec.Template.Spec.Containers = st.Spec.Template.Spec.Containers[:j]
}

// Called while creating new cluster and also during rolling restart.
func updateStatefulSetConfigMapVolumes(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	configMaps, _ := aeroCluster.Spec.Storage.GetConfigMaps()

	// Add to stateful set volumes.
	for _, configMapVolume := range configMaps {
		// Ignore error since its not possible.
		pvcName, _ := getPVCName(configMapVolume.Path)
		found := false
		for _, volume := range st.Spec.Template.Spec.Volumes {
			if volume.Name == pvcName {
				found = true
				break
			}
		}

		if !found {
			k8sVolume := corev1.Volume{
				Name: pvcName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: configMapVolume.ConfigMapName,
						},
					},
				},
			}
			st.Spec.Template.Spec.Volumes = append(st.Spec.Template.Spec.Volumes, k8sVolume)
		}

		addConfigMapVolumeMount(st.Spec.Template.Spec.InitContainers, configMapVolume)
		addConfigMapVolumeMount(st.Spec.Template.Spec.Containers, configMapVolume)
	}

	// Remove to stateful set volumes.
	j := 0
	for _, volume := range st.Spec.Template.Spec.Volumes {
		found := volume.ConfigMap == nil || volume.Name == confDirName || volume.Name == initConfDirName
		for _, configMapVolume := range configMaps {
			// Ignore error since its not possible.
			pvcName, _ := getPVCName(configMapVolume.Path)
			if volume.Name == pvcName {
				// Do not touch non confimap volumes or reserved config map volumes.
				found = true
				break
			}
		}

		if found {
			// Retain internal volumen or a matched volume
			st.Spec.Template.Spec.Volumes[j] = volume
			j++
		} else {
			// Remove volume mounts from the containers.
			removeConfigMapVolumeMount(st.Spec.Template.Spec.InitContainers, volume)
			removeConfigMapVolumeMount(st.Spec.Template.Spec.Containers, volume)
		}
	}
	st.Spec.Template.Spec.Volumes = st.Spec.Template.Spec.Volumes[:j]
}

func addConfigMapVolumeMount(containers []corev1.Container, configMapVolume aerospikev1alpha1.AerospikePersistentVolumeSpec) {
	// Update volume mounts.
	for i := range containers {
		container := &containers[i]
		// Ignore error since its not possible.
		pvcName, _ := getPVCName(configMapVolume.Path)
		mountFound := false
		for _, mount := range container.VolumeMounts {
			if mount.Name == pvcName {
				mountFound = true
				break
			}
		}

		if !mountFound {
			volumeMount := corev1.VolumeMount{
				Name:      pvcName,
				MountPath: configMapVolume.Path,
			}
			container.VolumeMounts = append(container.VolumeMounts, volumeMount)
		}
	}
}

func removeConfigMapVolumeMount(containers []corev1.Container, volume corev1.Volume) {
	// Update volume mounts.
	for i := range containers {
		container := &containers[i]
		j := 0
		for _, mount := range container.VolumeMounts {
			if mount.Name != volume.Name {
				// This mount point must be retained.
				container.VolumeMounts[j] = mount
				j++
			}
		}

		container.VolumeMounts = container.VolumeMounts[:j]
	}
}

func updateStatefulSetAerospikeServerContainerResources(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	st.Spec.Template.Spec.Containers[0].Resources = *aeroCluster.Spec.Resources
	// st.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{
	// 	Requests: corev1.ResourceList{
	// 		corev1.ResourceCPU:    computeCPURequest(aeroCluster),
	// 		corev1.ResourceMemory: computeMemoryRequest(aeroCluster),
	// 	},
	// 	Limits: computeResourceLimits(aeroCluster),
	// }
}

func getAeroClusterContainerPort(multiPodPerHost bool) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	if multiPodPerHost {
		// Create ports without hostPort setting
		ports = []corev1.ContainerPort{
			{
				Name:          utils.ServicePortName,
				ContainerPort: utils.ServicePort,
			},
			{
				Name:          utils.ServiceTLSPortName,
				ContainerPort: utils.ServiceTLSPort,
			},
			{
				Name:          utils.HeartbeatPortName,
				ContainerPort: utils.HeartbeatPort,
			},
			{
				Name:          utils.HeartbeatTLSPortName,
				ContainerPort: utils.HeartbeatTLSPort,
			},
			{
				Name:          utils.FabricPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.FabricTLSPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.InfoPortName,
				ContainerPort: utils.InfoPort,
			},
		}
	} else {
		// Single pod per host. Enable hostPort setting
		// The hostPort setting applies to the Kubernetes containers.
		// The container port will be exposed to the external network at <hostIP>:<hostPort>,
		// where the hostIP is the IP address of the Kubernetes node where
		// the container is running and the hostPort is the port requested by the user
		ports = []corev1.ContainerPort{
			{
				Name:          utils.ServicePortName,
				ContainerPort: utils.ServicePort,
				HostPort:      utils.ServicePort,
			},
			{
				Name:          utils.ServiceTLSPortName,
				ContainerPort: utils.ServiceTLSPort,
				HostPort:      utils.ServiceTLSPort,
			},
			{
				Name:          utils.HeartbeatPortName,
				ContainerPort: utils.HeartbeatPort,
			},
			{
				Name:          utils.HeartbeatTLSPortName,
				ContainerPort: utils.HeartbeatTLSPort,
			},
			{
				Name:          utils.FabricPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.FabricTLSPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.InfoPortName,
				ContainerPort: utils.InfoPort,
				HostPort:      utils.InfoPort,
			},
		}
	}
	return ports
}

func newEnvVar(name, fieldPath string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: fieldPath,
			},
		},
	}
}

func newEnvVarStatic(name, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}

func isClusterResourceUpdated(aeroCluster *aerospikev1alpha1.AerospikeCluster) bool {
	if aeroCluster.Spec.Resources == nil && aeroCluster.Status.Resources == nil {
		return false
	}
	// TODO: What should be the convention, should we allow removing these things once added in spec?
	// Should removing be the no op or change the cluster also for these changes? Check for the other also, like auth, secret
	if (aeroCluster.Spec.Resources == nil && aeroCluster.Status.Resources != nil) ||
		(aeroCluster.Spec.Resources != nil && aeroCluster.Status.Resources == nil) ||
		!isResourceListEqual(aeroCluster.Spec.Resources.Requests, aeroCluster.Status.Resources.Requests) ||
		!isResourceListEqual(aeroCluster.Spec.Resources.Limits, aeroCluster.Status.Resources.Limits) {

		return true
	}

	return false
}

func isResourceListEqual(res1, res2 corev1.ResourceList) bool {
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

func getStatefulSetPodName(statefulSetName string, index int32) string {
	return fmt.Sprintf("%s-%d", statefulSetName, index)
}

func getStatefulSetPodOrdinal(podName string) (*int32, error) {
	parts := strings.Split(podName, "-")
	ordinalStr := parts[len(parts)-1]
	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil {
		return nil, err
	}
	result := int32(ordinal)
	return &result, nil
}

func truncateString(str string, num int) string {
	if len(str) > num {
		return str[0:num]
	}
	return str
}

func getPVCName(path string) (string, error) {
	path = strings.Trim(path, "/")

	hashPath, err := getHash(path)
	if err != nil {
		return "", err
	}

	reg, err := regexp.Compile("[^-a-z0-9]+")
	if err != nil {
		return "", err
	}
	newPath := reg.ReplaceAllString(path, "-")
	return truncateString(hashPath, 30) + "-" + truncateString(newPath, 20), nil
}

func getHeadLessSvcName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	return aeroCluster.Name
}

func getNamespacedNameForCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name,
		Namespace: aeroCluster.Namespace,
	}
}

func getNamespacedNameForStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getNamespacedNameForConfigMap(aeroCluster *aerospikev1alpha1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func splitRacks(nodes, racks int) []int {
	nodesPerRack, extraNodes := nodes/racks, nodes%racks

	// Distributing nodes in given racks
	var topology []int

	for rackIdx := 0; rackIdx < racks; rackIdx++ {
		nodesForThisRack := nodesPerRack
		if rackIdx < extraNodes {
			nodesForThisRack++
		}
		topology = append(topology, nodesForThisRack)
	}

	return topology
}

func getNewRackStateList(aeroCluster *aerospikev1alpha1.AerospikeCluster) []RackState {
	topology := splitRacks(int(aeroCluster.Spec.Size), len(aeroCluster.Spec.RackConfig.Racks))
	var rackStateList []RackState
	for idx, rack := range aeroCluster.Spec.RackConfig.Racks {
		rackStateList = append(rackStateList, RackState{
			Rack: rack,
			Size: topology[idx],
		})
	}
	return rackStateList
}

func getOldRackList(aeroCluster *aerospikev1alpha1.AerospikeCluster) []aerospikev1alpha1.Rack {
	var rackList []aerospikev1alpha1.Rack
	for _, rack := range aeroCluster.Status.RackConfig.Racks {
		rackList = append(rackList, rack)
	}
	return rackList
}

func getHash(str string) (string, error) {
	var digest []byte
	hash := ripemd160.New()
	hash.Reset()
	if _, err := hash.Write([]byte(str)); err != nil {
		return "", err
	}
	res := hash.Sum(digest)
	return hex.EncodeToString(res), nil
}
