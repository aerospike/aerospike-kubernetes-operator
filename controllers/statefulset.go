package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	// log "github.com/inconshreveable/log15"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
)

const (
	aeroClusterServiceAccountName string = "aerospike-cluster"
	configMaPodListKey            string = "podList.json"
)

// The default cpu request for the aerospike-server container
const (
	// This storage path annotation is added in pvc to make reverse association with storage.volume.path
	// while deleting pvc
	storagePathAnnotationKey = "storage-path"

	confDirName     = "confdir"
	initConfDirName = "initconfigs"
)

func (r *AerospikeClusterReconciler) createSTS(aeroCluster *asdbv1beta1.AerospikeCluster, namespacedName types.NamespacedName, rackState RackState) (*appsv1.StatefulSet, error) {
	replicas := int32(rackState.Size)

	r.Log.Info("Create statefulset for AerospikeCluster", "size", replicas)

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for all statefulset pods
		for i := 0; i < rackState.Size; i++ {
			// Statefulset name created from cr name
			name := fmt.Sprintf("%s-%d", namespacedName.Name, i)
			if err := r.createPodService(aeroCluster, name, aeroCluster.Namespace); err != nil {
				return nil, err
			}
		}
	}

	ports := getSTSContainerPort(aeroCluster.Spec.MultiPodPerHost)

	ls := utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackState.Rack.ID)

	envVarList := []corev1.EnvVar{
		newSTSEnvVar("MY_POD_NAME", "metadata.name"),
		newSTSEnvVar("MY_POD_NAMESPACE", "metadata.namespace"),
		newSTSEnvVar("MY_POD_IP", "status.podIP"),
		newSTSEnvVar("MY_HOST_IP", "status.hostIP"),
		newSTSEnvVarStatic("MY_POD_TLS_NAME", getServiceTLSName(aeroCluster)),
		newSTSEnvVarStatic("MY_POD_CLUSTER_NAME", aeroCluster.Name),
	}

	if name := getServiceTLSName(aeroCluster); name != "" {
		envVarList = append(envVarList, newSTSEnvVarStatic("MY_POD_TLS_ENABLED", "true"))
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
					HostNetwork:        aeroCluster.Spec.PodSpec.HostNetwork,
					DNSPolicy:          aeroCluster.Spec.PodSpec.DNSPolicy,
					//TerminationGracePeriodSeconds: &int64(30),
					InitContainers: []corev1.Container{{
						Name:  asdbv1beta1.AerospikeServerInitContainerName,
						Image: "aerospike/aerospike-kubernetes-init:0.0.14",
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
								Value: getSTSHeadLessSvcName(aeroCluster),
							},
							// TODO: Do we need this var?
							{
								Name:  "CONFIG_MAP_NAME",
								Value: getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID).Name,
							},
						}...),
					}},

					Containers: []corev1.Container{{
						Name:            asdbv1beta1.AerospikeServerContainerName,
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
										Name: getNamespacedNameForSTSConfigMap(aeroCluster, rackState.Rack.ID).Name,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	r.updateSTSPodSpec(aeroCluster, st)

	r.updateSTSContainerResources(aeroCluster, st)
	// TODO: Add validation. device, file, both should not exist in same storage class
	if err := r.updateSTSStorage(aeroCluster, st, rackState); err != nil {
		return nil, err
	}

	r.updateSTSSecretInfo(aeroCluster, st)

	r.updateSTSConfigMapVolumes(aeroCluster, st, rackState)

	r.updateSTSAffinity(aeroCluster, st, ls, rackState)
	// Set AerospikeCluster instance as the owner and controller
	controllerutil.SetControllerReference(aeroCluster, st, r.Scheme)

	if err := r.Client.Create(context.TODO(), st, createOption); err != nil {
		return nil, fmt.Errorf("failed to create new StatefulSet: %v", err)
	}
	r.Log.Info("Created new StatefulSet", "StatefulSet.Namespace", st.Namespace, "StatefulSet.Name", st.Name)

	if err := r.waitForSTSToBeReady(st); err != nil {
		return st, fmt.Errorf("failed to wait for statefulset to be ready: %v", err)
	}

	return r.getSTS(aeroCluster, rackState)
}

func (r *AerospikeClusterReconciler) deleteSTS(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet) error {
	r.Log.Info("Delete statefulset")
	// No need to do cleanup pods after deleting sts
	// It is only deleted while its creation is failed
	// While doing rackRemove, we call scaleDown to 0 so that will do cleanup
	return r.Client.Delete(context.TODO(), st)
}

func (r *AerospikeClusterReconciler) waitForSTSToBeReady(st *appsv1.StatefulSet) error {

	const podStatusMaxRetry = 18
	const podStatusRetryInterval = time.Second * 10

	r.Log.Info("Waiting for statefulset to be ready", "WaitTimePerPod", podStatusRetryInterval*time.Duration(podStatusMaxRetry))

	var podIndex int32
	for podIndex = 0; podIndex < *st.Spec.Replicas; podIndex++ {
		podName := getSTSPodName(st.Name, podIndex)

		var isReady bool
		pod := &corev1.Pod{}

		// Wait for 10 sec to pod to get started
		for i := 0; i < 5; i++ {
			if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: st.Namespace}, pod); err == nil {
				break
			}
			time.Sleep(time.Second * 2)
		}

		// Wait for pod to get ready
		for i := 0; i < podStatusMaxRetry; i++ {
			r.Log.V(1).Info("Check statefulSet pod running and ready", "pod", podName)

			pod := &corev1.Pod{}
			if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: st.Namespace}, pod); err != nil {
				return fmt.Errorf("failed to get statefulSet pod %s: %v", podName, err)
			}
			if err := utils.CheckPodFailed(pod); err != nil {
				return fmt.Errorf("StatefulSet pod %s failed: %v", podName, err)
			}
			if utils.IsPodRunningAndReady(pod) {
				isReady = true
				r.Log.Info("Pod is running and ready", "pod", podName)
				break
			}

			time.Sleep(podStatusRetryInterval)
		}
		if !isReady {
			statusErr := fmt.Errorf("StatefulSet pod is not ready. Status: %v", pod.Status.Conditions)
			r.Log.Error(statusErr, "Statefulset Not ready")
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

		r.Log.V(1).Info("Check statefulSet status is updated or not")

		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: st.Name, Namespace: st.Namespace}, st)
		if err != nil {
			return err
		}
		if *st.Spec.Replicas == st.Status.Replicas {
			updated = true
			break
		}
		r.Log.V(1).Info("Statefulset spec.replica not matching status.replica", "staus", st.Status.Replicas, "spec", *st.Spec.Replicas)
	}
	if !updated {
		return fmt.Errorf("statefulset status is not updated")
	}

	r.Log.Info("Statefulset is ready")

	return nil
}

func (r *AerospikeClusterReconciler) getSTS(aeroCluster *asdbv1beta1.AerospikeCluster, rackState RackState) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	err := r.Client.Get(context.TODO(), getNamespacedNameForSTS(aeroCluster, rackState.Rack.ID), found)
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (r *AerospikeClusterReconciler) buildSTSConfigMap(aeroCluster *asdbv1beta1.AerospikeCluster, namespacedName types.NamespacedName, rack asdbv1beta1.Rack) error {

	r.Log.Info("Creating a new ConfigMap for statefulSet")

	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: namespacedName.Name, Namespace: namespacedName.Namespace}, confMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// build the aerospike config file based on the current spec
			configMapData, err := r.CreateConfigMapData(aeroCluster, rack)
			if err != nil {
				return fmt.Errorf("failed to build dotConfig from map: %v", err)
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
			controllerutil.SetControllerReference(aeroCluster, confMap, r.Scheme)

			if err := r.Client.Create(context.TODO(), confMap, createOption); err != nil {
				return fmt.Errorf("failed to create new confMap for StatefulSet: %v", err)
			}
			r.Log.Info("Created new ConfigMap", "ConfigMap.Namespace", confMap.Namespace, "ConfigMap.Name", confMap.Name)

			return nil
		}
		return err
	}

	r.Log.Info("Configmap already exists for statefulSet - using existing configmap", "name", utils.NamespacedName(confMap.Namespace, confMap.Name))

	// Update existing configmap as it might not be current.
	configMapData, err := r.CreateConfigMapData(aeroCluster, rack)
	if err != nil {
		return fmt.Errorf("failed to build config map data: %v", err)
	}

	// Replace config map data since we are supposed to create a new config map.
	confMap.Data = configMapData

	if err := r.Client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("failed to update ConfigMap for StatefulSet: %v", err)
	}
	return nil
}

func (r *AerospikeClusterReconciler) updateSTSConfigMap(aeroCluster *asdbv1beta1.AerospikeCluster, namespacedName types.NamespacedName, rack asdbv1beta1.Rack) error {

	r.Log.Info("Updating ConfigMap", "ConfigMap", namespacedName)

	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), namespacedName, confMap)
	if err != nil {
		return err
	}

	// build the aerospike config file based on the current spec
	configMapData, err := r.CreateConfigMapData(aeroCluster, rack)
	if err != nil {
		return fmt.Errorf("failed to build dotConfig from map: %v", err)
	}

	// Overwrite only spec based keys. Do not touch other keys like pod metadata.
	for k, v := range configMapData {
		confMap.Data[k] = v
	}

	if err := r.Client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("failed to update confMap for StatefulSet: %v", err)
	}
	return nil
}

func (r *AerospikeClusterReconciler) createSTSHeadlessSvc(aeroCluster *asdbv1beta1.AerospikeCluster) error {

	r.Log.Info("Create headless service for statefulSet")

	ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)

	service := &corev1.Service{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, service)
	if err != nil {
		if errors.IsNotFound(err) {
			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					// Headless service has same name as AerospikeCluster
					Name:      getSTSHeadLessSvcName(aeroCluster),
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
			controllerutil.SetControllerReference(aeroCluster, service, r.Scheme)

			if err := r.Client.Create(context.TODO(), service, createOption); err != nil {
				return fmt.Errorf("failed to create headless service for statefulset: %v", err)
			}
			r.Log.Info("Created new headless service")

			return nil
		}
		return err
	}
	r.Log.Info("Service already exist for statefulSet. Using existing service", "name", utils.NamespacedName(service.Namespace, service.Name))
	return nil
}

func (r *AerospikeClusterReconciler) createPodService(aeroCluster *asdbv1beta1.AerospikeCluster, pName, pNamespace string) error {
	service := &corev1.Service{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pName, Namespace: pNamespace}, service); err == nil {
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
					Port: asdbv1beta1.ServicePort,
				},
			},
			ExternalTrafficPolicy: "Local",
		},
	}
	if name := getServiceTLSName(aeroCluster); name != "" {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name: "tls",
			Port: asdbv1beta1.ServiceTLSPort,
		})
	}
	// Set AerospikeCluster instance as the owner and controller.
	// It is created before Pod, so Pod cannot be the owner
	controllerutil.SetControllerReference(aeroCluster, service, r.Scheme)

	if err := r.Client.Create(context.TODO(), service, createOption); err != nil {
		return fmt.Errorf("failed to create new service for pod %s: %v", pName, err)
	}
	return nil
}

func (r *AerospikeClusterReconciler) deletePodService(pName, pNamespace string) error {
	service := &corev1.Service{}

	if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: pName, Namespace: pNamespace}, service); err != nil {
		return fmt.Errorf("failed to get service for pod %s: %v", pName, err)
	}
	if err := r.Client.Delete(context.TODO(), service); err != nil {
		return fmt.Errorf("failed to delete service for pod %s: %v", pName, err)
	}
	return nil
}

// Called only when new cluster is created
func (r *AerospikeClusterReconciler) updateSTSStorage(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) error {

	storage := rackState.Rack.Storage
	// TODO: Add validation. device, file, both should not exist in same storage class
	for _, volume := range storage.Volumes {
		r.Log.Info("Add PVC for volume", "volume", volume)
		var volumeMode corev1.PersistentVolumeMode
		var initContainerVolumePathPrefix string

		pvcName, err := getPVCName(volume.Path)
		if err != nil {
			return fmt.Errorf("failed to create ripemd hash for pvc name from volume.path %s", volume.Path)
		}

		if volume.VolumeMode == asdbv1beta1.AerospikeVolumeModeBlock {
			volumeMode = corev1.PersistentVolumeBlock
			initContainerVolumePathPrefix = "/block-volumes"

			r.Log.Info("Add volume device for volume", "volume", volume)
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
		} else if volume.VolumeMode == asdbv1beta1.AerospikeVolumeModeFilesystem {
			volumeMode = corev1.PersistentVolumeFilesystem
			initContainerVolumePathPrefix = "/filesystem-volumes"

			r.Log.Info("Add volume mount for volume", "volume", volume)
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

func (r *AerospikeClusterReconciler) updateSTSAffinity(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {

	affinity := &corev1.Affinity{}

	// only enable in production, so it can be used in 1 node clusters while debugging (minikube)
	if !aeroCluster.Spec.MultiPodPerHost {
		r.Log.Info("Adding pod affinity rules for statefulset pod")
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
func (r *AerospikeClusterReconciler) updateSTSSecretInfo(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet) {

	if aeroCluster.Spec.AerospikeConfigSecret.SecretName != "" {
		const secretVolumeName = "secretinfo"
		r.Log.Info("Add secret volume in statefulset pods")
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
func (r *AerospikeClusterReconciler) updateSTSPodSpec(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet) {
	// Update pod spec.
	st.Spec.Template.Spec.HostNetwork = aeroCluster.Spec.PodSpec.HostNetwork

	st.Spec.Template.Spec.DNSPolicy = aeroCluster.Spec.PodSpec.DNSPolicy

	// Add new sidecars.
	for _, newSidecar := range aeroCluster.Spec.PodSpec.Sidecars {
		found := false

		// Create a copy because updating stateful sets sets defaults
		// on the sidecar container object which mutates original aeroCluster object.
		sideCarCopy := corev1.Container{}
		lib.DeepCopy(&sideCarCopy, &newSidecar)
		for i, container := range st.Spec.Template.Spec.Containers {
			if newSidecar.Name == container.Name {
				// Update the sidecar in case something has changed.
				st.Spec.Template.Spec.Containers[i] = sideCarCopy
				found = true
				break
			}
		}

		if !found {
			// Add to stateful set containers.
			st.Spec.Template.Spec.Containers = append(st.Spec.Template.Spec.Containers, sideCarCopy)
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
func (r *AerospikeClusterReconciler) updateSTSConfigMapVolumes(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	configMaps, _ := rackState.Rack.Storage.GetConfigMaps()

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

		addConfigMapVolumeMountInSTS(st.Spec.Template.Spec.InitContainers, configMapVolume)
		addConfigMapVolumeMountInSTS(st.Spec.Template.Spec.Containers, configMapVolume)
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
			removeConfigMapVolumeMountFromSTS(st.Spec.Template.Spec.InitContainers, volume)
			removeConfigMapVolumeMountFromSTS(st.Spec.Template.Spec.Containers, volume)
		}
	}
	st.Spec.Template.Spec.Volumes = st.Spec.Template.Spec.Volumes[:j]
}

func (r *AerospikeClusterReconciler) waitForAllSTSToBeReady(aeroCluster *asdbv1beta1.AerospikeCluster) error {
	// User aeroCluster.Status to get all existing sts.
	// Can status be empty here
	r.Log.Info("Waiting for cluster to be ready")

	for _, rack := range aeroCluster.Status.RackConfig.Racks {
		st := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(aeroCluster, rack.ID)
		if err := r.Client.Get(context.TODO(), stsName, st); err != nil {
			if !errors.IsNotFound(err) {
				return err
			}
			// Skip if a sts not found. It may have be deleted and status may not have been updated yet
			continue
		}
		if err := r.waitForSTSToBeReady(st); err != nil {
			return err
		}
	}
	return nil
}

func (r *AerospikeClusterReconciler) getClusterSTSList(aeroCluster *asdbv1beta1.AerospikeCluster) (*appsv1.StatefulSetList, error) {
	// List the pods for this aeroCluster's statefulset
	statefulSetList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.Client.List(context.TODO(), statefulSetList, listOps); err != nil {
		return nil, err
	}
	return statefulSetList, nil
}
func addConfigMapVolumeMountInSTS(containers []corev1.Container, configMapVolume asdbv1beta1.AerospikePersistentVolumeSpec) {
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

func removeConfigMapVolumeMountFromSTS(containers []corev1.Container, volume corev1.Volume) {
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

func (r *AerospikeClusterReconciler) updateSTSContainerResources(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet) {
	// These resource is for main aerospike container. Other sidecar can mention their own resource
	st.Spec.Template.Spec.Containers[0].Resources = *aeroCluster.Spec.Resources
}

func getSTSContainerPort(multiPodPerHost bool) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	if multiPodPerHost {
		// Create ports without hostPort setting
		ports = []corev1.ContainerPort{
			{
				Name:          asdbv1beta1.ServicePortName,
				ContainerPort: asdbv1beta1.ServicePort,
			},
			{
				Name:          asdbv1beta1.ServiceTLSPortName,
				ContainerPort: asdbv1beta1.ServiceTLSPort,
			},
			{
				Name:          asdbv1beta1.HeartbeatPortName,
				ContainerPort: asdbv1beta1.HeartbeatPort,
			},
			{
				Name:          asdbv1beta1.HeartbeatTLSPortName,
				ContainerPort: asdbv1beta1.HeartbeatTLSPort,
			},
			{
				Name:          asdbv1beta1.FabricPortName,
				ContainerPort: asdbv1beta1.FabricPort,
			},
			{
				Name:          asdbv1beta1.FabricTLSPortName,
				ContainerPort: asdbv1beta1.FabricTLSPort,
			},
			{
				Name:          asdbv1beta1.InfoPortName,
				ContainerPort: asdbv1beta1.InfoPort,
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
				Name:          asdbv1beta1.ServicePortName,
				ContainerPort: asdbv1beta1.ServicePort,
				HostPort:      asdbv1beta1.ServicePort,
			},
			{
				Name:          asdbv1beta1.ServiceTLSPortName,
				ContainerPort: asdbv1beta1.ServiceTLSPort,
				HostPort:      asdbv1beta1.ServiceTLSPort,
			},
			{
				Name:          asdbv1beta1.HeartbeatPortName,
				ContainerPort: asdbv1beta1.HeartbeatPort,
			},
			{
				Name:          asdbv1beta1.HeartbeatTLSPortName,
				ContainerPort: asdbv1beta1.HeartbeatTLSPort,
			},
			{
				Name:          asdbv1beta1.FabricPortName,
				ContainerPort: asdbv1beta1.FabricPort,
			},
			{
				Name:          asdbv1beta1.FabricTLSPortName,
				ContainerPort: asdbv1beta1.FabricTLSPort,
			},
			{
				Name:          asdbv1beta1.InfoPortName,
				ContainerPort: asdbv1beta1.InfoPort,
				HostPort:      asdbv1beta1.InfoPort,
			},
		}
	}
	return ports
}

func getNamespacedNameForSTS(aeroCluster *asdbv1beta1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getNamespacedNameForSTSConfigMap(aeroCluster *asdbv1beta1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getSTSPodOrdinal(podName string) (*int32, error) {
	parts := strings.Split(podName, "-")
	ordinalStr := parts[len(parts)-1]
	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil {
		return nil, err
	}
	result := int32(ordinal)
	return &result, nil
}

func getSTSPodName(statefulSetName string, index int32) string {
	return fmt.Sprintf("%s-%d", statefulSetName, index)
}
func getSTSHeadLessSvcName(aeroCluster *asdbv1beta1.AerospikeCluster) string {
	return aeroCluster.Name
}

func newSTSEnvVar(name, fieldPath string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{
				FieldPath: fieldPath,
			},
		},
	}
}

func newSTSEnvVarStatic(name, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}
