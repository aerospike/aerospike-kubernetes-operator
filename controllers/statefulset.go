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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
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

	confDirName                = "confdir"
	initConfDirName            = "initconfigs"
	secretVolumeName           = "secretinfo"
	podServiceAccountMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
)

func (r *AerospikeClusterReconciler) createSTS(aeroCluster *asdbv1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rackState RackState) (*appsv1.StatefulSet, error) {
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
						Name:  asdbv1alpha1.AerospikeServerInitContainerName,
						Image: "aerospike/aerospike-kubernetes-init:0.0.14",
						// Change to PullAlways for image testing.
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts:    getDefaultAerospikeInitContainerVolumeMounts(),
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
						Name:            asdbv1alpha1.AerospikeServerContainerName,
						Image:           aeroCluster.Spec.Image,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports:           ports,
						Env:             envVarList,
						VolumeMounts:    getDefaultAerospikeContainerVolumeMounts(),
						// Resources to be updated later
					}},

					Volumes: getDefaultSTSVolumes(aeroCluster, rackState),
				},
			},
		},
	}

	r.updateSTSPodSpec(aeroCluster, st, ls, rackState)

	r.updateSTSContainerResources(aeroCluster, st)

	// TODO: Add validation. device, file, both should not exist in same storage class
	r.updateSTSStorage(aeroCluster, st, rackState)

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

func (r *AerospikeClusterReconciler) deleteSTS(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet) error {
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

func (r *AerospikeClusterReconciler) getSTS(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	err := r.Client.Get(context.TODO(), getNamespacedNameForSTS(aeroCluster, rackState.Rack.ID), found)
	if err != nil {
		return nil, err
	}
	return found, nil
}

func (r *AerospikeClusterReconciler) buildSTSConfigMap(aeroCluster *asdbv1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rack asdbv1alpha1.Rack) error {

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

func (r *AerospikeClusterReconciler) updateSTSConfigMap(aeroCluster *asdbv1alpha1.AerospikeCluster, namespacedName types.NamespacedName, rack asdbv1alpha1.Rack) error {

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

func (r *AerospikeClusterReconciler) createSTSHeadlessSvc(aeroCluster *asdbv1alpha1.AerospikeCluster) error {

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

func (r *AerospikeClusterReconciler) createPodService(aeroCluster *asdbv1alpha1.AerospikeCluster, pName, pNamespace string) error {
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
					Port: asdbv1alpha1.ServicePort,
				},
			},
			ExternalTrafficPolicy: "Local",
		},
	}
	if name := getServiceTLSName(aeroCluster); name != "" {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name: "tls",
			Port: asdbv1alpha1.ServiceTLSPort,
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

// Called while creating new cluster and also during rolling restart.
// Note: STS podSpec must be updated before updating storage
func (r *AerospikeClusterReconciler) updateSTSStorage(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	r.updateSTSPVStorage(aeroCluster, st, rackState)
	r.updateSTSNonPVStorage(aeroCluster, st, rackState)
	r.updateSTSAerospikeSecretInfo(aeroCluster, st)
}

func (r *AerospikeClusterReconciler) updateSTSPVStorage(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {

	volumes := rackState.Rack.Storage.GetPVs()

	for _, volume := range volumes {

		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
			initContainerVolumePathPrefix := "/block-volumes"

			r.Log.Info("Add volume device for volume", "volume", volume)

			addVolumeDeviceInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, initContainerVolumePathPrefix)
			addVolumeDeviceInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")

		} else if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeFilesystem {
			initContainerVolumePathPrefix := "/filesystem-volumes"

			r.Log.Info("Add volume mount for volume", "volume", volume)

			addVolumeMountInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, initContainerVolumePathPrefix)
			addVolumeMountInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")
		} else {
			// Should never come here
			continue
		}

		r.Log.Info("Add PVC for volume", "volume", volume)

		pvc := createPVCForVolumeAttachment(aeroCluster, volume)
		st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)
	}
}

func (r *AerospikeClusterReconciler) updateSTSNonPVStorage(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	volumes := rackState.Rack.Storage.GetNonPVs()

	for _, volume := range volumes {

		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		r.Log.Info("Add volumeMount in statefulset pod containers for volume", "volume", volume)

		// Add volumeMount in statefulset pod containers for volume
		addVolumeMountInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, "")
		addVolumeMountInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")

		// Add volume in statefulset template
		k8sVolume := createVolumeForVolumeAttachment(volume)
		st.Spec.Template.Spec.Volumes = append(st.Spec.Template.Spec.Volumes, k8sVolume)
	}
}

func (r *AerospikeClusterReconciler) updateSTSSchedulingPolicy(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {

	var affinity *corev1.Affinity

	// Use rack affinity, if given
	if rackState.Rack.Affinity != nil {
		lib.DeepCopy(affinity, rackState.Rack.Affinity)
	} else {
		lib.DeepCopy(affinity, aeroCluster.Spec.PodSpec.Affinity)
	}

	// Set our rules in PodAntiAffinity
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
		affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
			affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
	}

	// Set our rules in NodeAffinity
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

		selector := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution

		if selector == nil || len(selector.NodeSelectorTerms) == 0 {
			ns := &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{
						MatchExpressions: matchExpressions,
					},
				},
			}
			affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = ns
		} else {
			for i := range selector.NodeSelectorTerms {
				selector.NodeSelectorTerms[i].MatchExpressions = append(selector.NodeSelectorTerms[i].MatchExpressions, matchExpressions...)
			}
		}
	}

	st.Spec.Template.Spec.Affinity = affinity

	// Use rack nodeSelector, if given
	if len(rackState.Rack.NodeSelector) != 0 {
		st.Spec.Template.Spec.NodeSelector = rackState.Rack.NodeSelector
	} else {
		st.Spec.Template.Spec.NodeSelector = aeroCluster.Spec.PodSpec.NodeSelector
	}

	// Use rack tolerations, if given
	if len(rackState.Rack.Tolerations) != 0 {
		st.Spec.Template.Spec.Tolerations = rackState.Rack.Tolerations
	} else {
		st.Spec.Template.Spec.Tolerations = aeroCluster.Spec.PodSpec.Tolerations
	}
}

// TODO: How to remove if user has removed this field? Should we find and remove volume
// Called while creating new cluster and also during rolling restart
func (r *AerospikeClusterReconciler) updateSTSAerospikeSecretInfo(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {

	if aeroCluster.Spec.AerospikeConfigSecret.SecretName != "" {
		r.Log.Info("Add secret volume in statefulset pods")

		// Add volume in template
		secretVolume := corev1.Volume{
			Name: secretVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: aeroCluster.Spec.AerospikeConfigSecret.SecretName,
				},
			},
		}
		var volFound bool
		for i, vol := range st.Spec.Template.Spec.Volumes {
			if vol.Name == secretVolumeName {
				volFound = true
				st.Spec.Template.Spec.Volumes[i] = secretVolume
				break
			}
		}
		if !volFound {
			st.Spec.Template.Spec.Volumes = append(st.Spec.Template.Spec.Volumes, secretVolume)
		}

		// Add volume mount in aerospike container
		secretVolumeMount := corev1.VolumeMount{
			Name:      secretVolumeName,
			MountPath: aeroCluster.Spec.AerospikeConfigSecret.MountPath,
		}
		var volmFound bool
		for i, vol := range st.Spec.Template.Spec.Containers[0].VolumeMounts {
			if vol.Name == secretVolumeName {
				volmFound = true
				st.Spec.Template.Spec.Containers[0].VolumeMounts[i] = secretVolumeMount
				break
			}
		}
		if !volmFound {
			st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, secretVolumeMount)
		}
	}
}

// Called while creating new cluster and also during rolling restart.
func (r *AerospikeClusterReconciler) updateSTSPodSpec(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {
	r.updateSTSSchedulingPolicy(aeroCluster, st, labels, rackState)

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

	// Add additional init containers
	for _, newAddIC := range aeroCluster.Spec.PodSpec.InitContainers {
		found := false

		// Create a copy because updating stateful sets sets defaults
		// on the init container object which mutates original aeroCluster object.
		addICCopy := corev1.Container{}
		lib.DeepCopy(&addICCopy, &newAddIC)
		for i, container := range st.Spec.Template.Spec.InitContainers {
			if newAddIC.Name == container.Name {
				// Update the init container in case something has changed.
				st.Spec.Template.Spec.InitContainers[i] = newAddIC
				found = true
				break
			}
		}

		if !found {
			// Add to stateful set init containers.
			st.Spec.Template.Spec.InitContainers = append(st.Spec.Template.Spec.InitContainers, addICCopy)
		}
	}

	// Remove deleted additional init containers.
	j = 0
	for i, container := range st.Spec.Template.Spec.InitContainers {
		found := i == 0
		for _, newAddIC := range aeroCluster.Spec.PodSpec.InitContainers {
			if newAddIC.Name == container.Name {
				found = true
				break
			}
		}

		if found {
			// Retain main aerospike-init container or a matched sidecar.
			st.Spec.Template.Spec.InitContainers[j] = container
			j++
		}
	}
	st.Spec.Template.Spec.InitContainers = st.Spec.Template.Spec.InitContainers[:j]
}

func (r *AerospikeClusterReconciler) waitForAllSTSToBeReady(aeroCluster *asdbv1alpha1.AerospikeCluster) error {
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

func (r *AerospikeClusterReconciler) getClusterSTSList(aeroCluster *asdbv1alpha1.AerospikeCluster) (*appsv1.StatefulSetList, error) {
	// List the pods for this aeroCluster's statefulset
	statefulSetList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.Client.List(context.TODO(), statefulSetList, listOps); err != nil {
		return nil, err
	}
	return statefulSetList, nil
}

func (r *AerospikeClusterReconciler) updateSTSContainerResources(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	// These resource is for main aerospike container. Other sidecar can mention their own resource
	st.Spec.Template.Spec.Containers[0].Resources = *aeroCluster.Spec.Resources
}

func getDefaultAerospikeInitContainerVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      confDirName,
			MountPath: "/etc/aerospike",
		},
		{
			Name:      initConfDirName,
			MountPath: "/configs",
		},
	}
}

func getDefaultSTSVolumes(aeroCluster *asdbv1alpha1.AerospikeCluster, rackState RackState) []corev1.Volume {
	return []corev1.Volume{
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
	}
}

func getDefaultAerospikeContainerVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      confDirName,
			MountPath: "/etc/aerospike",
		},
	}
}

func initializeSTSStorage(aeroCluster *asdbv1alpha1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	// Initialize sts storage
	for i := range st.Spec.Template.Spec.InitContainers {
		if i == 0 {
			st.Spec.Template.Spec.InitContainers[i].VolumeMounts = getDefaultAerospikeInitContainerVolumeMounts()
		} else {
			st.Spec.Template.Spec.InitContainers[i].VolumeMounts = []corev1.VolumeMount{}
		}

		st.Spec.Template.Spec.InitContainers[i].VolumeDevices = []corev1.VolumeDevice{}
	}

	for i := range st.Spec.Template.Spec.Containers {
		if i == 0 {
			st.Spec.Template.Spec.Containers[i].VolumeMounts = getDefaultAerospikeContainerVolumeMounts()
		} else {
			st.Spec.Template.Spec.Containers[i].VolumeMounts = []corev1.VolumeMount{}
		}

		st.Spec.Template.Spec.Containers[i].VolumeDevices = []corev1.VolumeDevice{}
	}

	st.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{}

	st.Spec.Template.Spec.Volumes = getDefaultSTSVolumes(aeroCluster, rackState)
}

func createPVCForVolumeAttachment(aeroCluster *asdbv1alpha1.AerospikeCluster, volume asdbv1alpha1.VolumeSpec) corev1.PersistentVolumeClaim {

	pv := volume.Source.PersistentVolume

	var accessModes []corev1.PersistentVolumeAccessMode
	accessModes = append(accessModes, pv.AccessModes...)
	if len(accessModes) == 0 {
		accessModes = append(accessModes, "ReadWriteOnce")
	}

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volume.Name,
			Namespace: aeroCluster.Namespace,
			// Use this path annotation while matching pvc with storage volume
			Annotations: map[string]string{
				storagePathAnnotationKey: volume.Name,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeMode:  &pv.VolumeMode,
			AccessModes: accessModes,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: pv.Size,
				},
			},
			StorageClassName: &pv.StorageClass,
			Selector:         pv.Selector,
		},
	}
}

func createVolumeForVolumeAttachment(volume asdbv1alpha1.VolumeSpec) corev1.Volume {
	return corev1.Volume{
		Name: volume.Name,
		// Add all type of source,
		// we have already validated in webhook that only one of the source is present, rest are nil.
		VolumeSource: corev1.VolumeSource{
			ConfigMap: volume.Source.ConfigMap,
			Secret:    volume.Source.Secret,
			EmptyDir:  volume.Source.EmptyDir,
		},
	}
}

// Add dummy volumeAttachment for aerospike, init container
func getFinalVolumeAttachmentsForVolume(volume asdbv1alpha1.VolumeSpec) (initContainerAttachments, containerAttachments []v1alpha1.VolumeAttachment) {
	// Create dummy attachment for initcontainer
	initVolumePath := "/" + volume.Name // Using volume name for initContainer
	if volume.Aerospike != nil {
		initVolumePath = volume.Aerospike.Path
	}
	// All volumes should be mounted in init container to allow initialization
	initContainerAttachments = append(initContainerAttachments, volume.InitContainers...)
	initContainerAttachments = append(initContainerAttachments, v1alpha1.VolumeAttachment{
		ContainerName: v1alpha1.AerospikeServerInitContainerName,
		Path:          initVolumePath,
	})

	// Create dummy attachment for aerospike server conatiner
	containerAttachments = append(containerAttachments, volume.Sidecars...)
	if volume.Aerospike != nil {
		containerAttachments = append(containerAttachments, v1alpha1.VolumeAttachment{
			ContainerName: v1alpha1.AerospikeServerContainerName,
			Path:          volume.Aerospike.Path,
		})
	}
	return initContainerAttachments, containerAttachments
}

func addVolumeMountInContainer(volumeName string, volumeAttachments []asdbv1alpha1.VolumeAttachment, containers []corev1.Container, pathPrefix string) {
	for _, volumeAttachment := range volumeAttachments {
		for i := range containers {
			container := &containers[i]

			if container.Name == volumeAttachment.ContainerName {
				volumeMount := corev1.VolumeMount{
					Name:      volumeName,
					MountPath: pathPrefix + volumeAttachment.Path,
				}
				container.VolumeMounts = append(container.VolumeMounts, volumeMount)
				break
			}
		}
	}
}

func addVolumeDeviceInContainer(volumeName string, volumeAttachments []asdbv1alpha1.VolumeAttachment, containers []corev1.Container, pathPrefix string) {
	for _, volumeAttachment := range volumeAttachments {
		for i := range containers {
			container := &containers[i]

			if container.Name == volumeAttachment.ContainerName {
				volumeDevice := corev1.VolumeDevice{
					Name:       volumeName,
					DevicePath: pathPrefix + volumeAttachment.Path,
				}
				container.VolumeDevices = append(container.VolumeDevices, volumeDevice)
				break
			}
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

func getSTSContainerPort(multiPodPerHost bool) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	if multiPodPerHost {
		// Create ports without hostPort setting
		ports = []corev1.ContainerPort{
			{
				Name:          asdbv1alpha1.ServicePortName,
				ContainerPort: asdbv1alpha1.ServicePort,
			},
			{
				Name:          asdbv1alpha1.ServiceTLSPortName,
				ContainerPort: asdbv1alpha1.ServiceTLSPort,
			},
			{
				Name:          asdbv1alpha1.HeartbeatPortName,
				ContainerPort: asdbv1alpha1.HeartbeatPort,
			},
			{
				Name:          asdbv1alpha1.HeartbeatTLSPortName,
				ContainerPort: asdbv1alpha1.HeartbeatTLSPort,
			},
			{
				Name:          asdbv1alpha1.FabricPortName,
				ContainerPort: asdbv1alpha1.FabricPort,
			},
			{
				Name:          asdbv1alpha1.FabricTLSPortName,
				ContainerPort: asdbv1alpha1.FabricTLSPort,
			},
			{
				Name:          asdbv1alpha1.InfoPortName,
				ContainerPort: asdbv1alpha1.InfoPort,
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
				Name:          asdbv1alpha1.ServicePortName,
				ContainerPort: asdbv1alpha1.ServicePort,
				HostPort:      asdbv1alpha1.ServicePort,
			},
			{
				Name:          asdbv1alpha1.ServiceTLSPortName,
				ContainerPort: asdbv1alpha1.ServiceTLSPort,
				HostPort:      asdbv1alpha1.ServiceTLSPort,
			},
			{
				Name:          asdbv1alpha1.HeartbeatPortName,
				ContainerPort: asdbv1alpha1.HeartbeatPort,
			},
			{
				Name:          asdbv1alpha1.HeartbeatTLSPortName,
				ContainerPort: asdbv1alpha1.HeartbeatTLSPort,
			},
			{
				Name:          asdbv1alpha1.FabricPortName,
				ContainerPort: asdbv1alpha1.FabricPort,
			},
			{
				Name:          asdbv1alpha1.FabricTLSPortName,
				ContainerPort: asdbv1alpha1.FabricTLSPort,
			},
			{
				Name:          asdbv1alpha1.InfoPortName,
				ContainerPort: asdbv1alpha1.InfoPort,
				HostPort:      asdbv1alpha1.InfoPort,
			},
		}
	}
	return ports
}

func getNamespacedNameForSTS(aeroCluster *asdbv1alpha1.AerospikeCluster, rackID int) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getNamespacedNameForSTSConfigMap(aeroCluster *asdbv1alpha1.AerospikeCluster, rackID int) types.NamespacedName {
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
func getSTSHeadLessSvcName(aeroCluster *asdbv1alpha1.AerospikeCluster) string {
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
