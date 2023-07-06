package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
)

const (
	// aerospike-operator- is the prefix set in config/default/kustomization.yaml file.
	// Need to modify this name if prefix is changed in yaml file
	aeroClusterServiceAccountName string = "aerospike-operator-controller-manager"

	// This storage path annotation is added in pvc to make reverse association with storage.volume.path
	// while deleting pvc
	storageVolumeAnnotationKey       = "storage-volume"
	storageVolumeLegacyAnnotationKey = "storage-path"

	confDirName                = "confdir"
	initConfDirName            = "initconfigs"
	podServiceAccountMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
)

type PortInfo struct {
	connectionType string
	configParam    string
	exposedOnHost  bool
}

var defaultContainerPorts = map[string]PortInfo{
	asdbv1.ServicePortName: {
		connectionType: "service",
		configParam:    "port",
		exposedOnHost:  true,
	},
	asdbv1.ServiceTLSPortName: {
		connectionType: "service",
		configParam:    "tls-port",
		exposedOnHost:  true,
	},
	asdbv1.FabricPortName: {
		connectionType: "fabric",
		configParam:    "port",
	},
	asdbv1.FabricTLSPortName: {
		connectionType: "fabric",
		configParam:    "tls-port",
	},
	asdbv1.HeartbeatPortName: {
		connectionType: "heartbeat",
		configParam:    "port",
	},
	asdbv1.HeartbeatTLSPortName: {
		connectionType: "heartbeat",
		configParam:    "tls-port",
	},
	asdbv1.InfoPortName: {
		connectionType: "info",
		configParam:    "port",
		exposedOnHost:  true,
	},
}

func (r *SingleClusterReconciler) createSTS(
	namespacedName types.NamespacedName, rackState *RackState,
) (*appsv1.StatefulSet, error) {
	replicas := int32(rackState.Size)

	r.Log.Info("Create statefulset for AerospikeCluster", "size", replicas)

	ports := getSTSContainerPort(
		r.aeroCluster.Spec.PodSpec.MultiPodPerHost,
		r.aeroCluster.Spec.AerospikeConfig,
	)

	operatorDefinedLabels := utils.LabelsForAerospikeClusterRack(
		r.aeroCluster.Name, rackState.Rack.ID,
	)

	tlsName, _ := asdbv1.GetServiceTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	envVarList := []corev1.EnvVar{
		newSTSEnvVar("MY_POD_NAME", "metadata.name"),
		newSTSEnvVar("MY_POD_NAMESPACE", "metadata.namespace"),
		newSTSEnvVar("MY_POD_IP", "status.podIP"),
		newSTSEnvVar("MY_HOST_IP", "status.hostIP"),
		newSTSEnvVarStatic("MY_POD_TLS_NAME", tlsName),
		newSTSEnvVarStatic("MY_POD_CLUSTER_NAME", r.aeroCluster.Name),
	}

	if tlsName != "" {
		envVarList = append(
			envVarList, newSTSEnvVarStatic("MY_POD_TLS_ENABLED", "true"),
		)
	}

	st := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			Labels:    operatorDefinedLabels,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: operatorDefinedLabels,
			},
			ServiceName: r.aeroCluster.Name,
			Template: corev1.PodTemplateSpec{

				ObjectMeta: metav1.ObjectMeta{
					Labels: operatorDefinedLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: aeroClusterServiceAccountName,
					// TerminationGracePeriodSeconds: &int64(30),
					InitContainers: []corev1.Container{
						{
							Name:            asdbv1.AerospikeInitContainerName,
							Image:           asdbv1.GetAerospikeInitContainerImage(r.aeroCluster),
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    getDefaultAerospikeInitContainerVolumeMounts(),
							Env: append(
								envVarList, []corev1.EnvVar{
									{
										// Headless service has the same name as AerospikeCluster
										Name:  "SERVICE",
										Value: getSTSHeadLessSvcName(r.aeroCluster),
									},
									// TODO: Do we need this var?
									{
										Name: "CONFIG_MAP_NAME",
										Value: getNamespacedNameForSTSConfigMap(
											r.aeroCluster, rackState.Rack.ID,
										).Name,
									},
								}...,
							),
						},
					},

					Containers: []corev1.Container{
						{
							Name:            asdbv1.AerospikeServerContainerName,
							Image:           r.aeroCluster.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Ports:           ports,
							Env:             envVarList,
							VolumeMounts:    getDefaultAerospikeContainerVolumeMounts(),
						},
					},

					Volumes: getDefaultSTSVolumes(r.aeroCluster, rackState),
				},
			},
		},
	}

	r.updateSTSFromPodSpec(st, rackState)

	// TODO: Add validation. device, file, both should not exist in same storage class
	r.updateSTSStorage(st, rackState)

	// Set AerospikeCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(
		r.aeroCluster, st, r.Scheme,
	); err != nil {
		return nil, err
	}

	if err := r.Client.Create(context.TODO(), st, createOption); err != nil {
		return nil, fmt.Errorf("failed to create new StatefulSet: %v", err)
	}

	r.Log.Info(
		"Created new StatefulSet", "StatefulSet.Namespace", st.Namespace,
		"StatefulSet.Name", st.Name,
	)

	if err := r.waitForSTSToBeReady(st); err != nil {
		return st, fmt.Errorf(
			"failed to wait for statefulset to be ready: %v", err,
		)
	}

	return r.getSTS(rackState)
}

func (r *SingleClusterReconciler) deleteSTS(st *appsv1.StatefulSet) error {
	r.Log.Info("Delete statefulset")
	// No need to do cleanup pods after deleting sts
	// It is only deleted while its creation is failed
	// While doing rackRemove, we call scaleDown to 0 so that will do cleanup
	return r.Client.Delete(context.TODO(), st)
}

func (r *SingleClusterReconciler) waitForSTSToBeReady(st *appsv1.StatefulSet) error {
	const (
		podStatusMaxRetry      = 18
		podStatusRetryInterval = time.Second * 10
	)

	r.Log.Info(
		"Waiting for statefulset to be ready", "WaitTimePerPod",
		podStatusRetryInterval*time.Duration(podStatusMaxRetry),
	)

	var podIndex int32
	for podIndex = 0; podIndex < *st.Spec.Replicas; podIndex++ {
		podName := getSTSPodName(st.Name, podIndex)

		var isReady bool

		pod := &corev1.Pod{}

		// Wait for 10 sec to pod to get started
		for i := 0; i < 5; i++ {
			if err := r.Client.Get(
				context.TODO(),
				types.NamespacedName{Name: podName, Namespace: st.Namespace},
				pod,
			); err == nil {
				break
			}

			time.Sleep(time.Second * 2)
		}

		// Wait for pod to get ready
		for i := 0; i < podStatusMaxRetry; i++ {
			r.Log.V(1).Info(
				"Check statefulSet pod running and ready", "pod", podName,
			)

			if err := r.Client.Get(
				context.TODO(),
				types.NamespacedName{Name: podName, Namespace: st.Namespace},
				pod,
			); err != nil {
				return fmt.Errorf(
					"failed to get statefulSet pod %s: %v", podName, err,
				)
			}

			if err := utils.CheckPodFailed(pod); err != nil {
				return fmt.Errorf("statefulSet pod %s failed: %v", podName, err)
			}

			if utils.IsPodRunningAndReady(pod) {
				isReady = true

				r.Log.Info("Pod is running and ready", "pod", podName)

				break
			}

			time.Sleep(podStatusRetryInterval)
		}

		if !isReady {
			statusErr := fmt.Errorf(
				"statefulSet pod is not ready. Status: %v",
				pod.Status.Conditions,
			)
			r.Log.Error(statusErr, "Statefulset Not ready")

			return statusErr
		}
	}

	// Check for statefulset at the end,
	// if we check before pods then we would not know status of individual pods
	const (
		stsStatusMaxRetry      = 10
		stsStatusRetryInterval = time.Second * 2
	)

	var updated bool

	for i := 0; i < stsStatusMaxRetry; i++ {
		time.Sleep(stsStatusRetryInterval)

		r.Log.V(1).Info("Check statefulSet status is updated or not")

		if err := r.Client.Get(
			context.TODO(),
			types.NamespacedName{Name: st.Name, Namespace: st.Namespace}, st,
		); err != nil {
			return err
		}

		if *st.Spec.Replicas == st.Status.Replicas {
			updated = true
			break
		}

		r.Log.V(1).Info(
			"StatefulSet spec.replica not matching status.replica", "status",
			st.Status.Replicas, "spec", *st.Spec.Replicas,
		)
	}

	if !updated {
		return fmt.Errorf("statefulset status is not updated")
	}

	r.Log.Info("StatefulSet is ready")

	return nil
}

func (r *SingleClusterReconciler) getSTS(rackState *RackState) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	if err := r.Client.Get(
		context.TODO(),
		getNamespacedNameForSTS(r.aeroCluster, rackState.Rack.ID),
		found,
	); err != nil {
		return nil, err
	}

	return found, nil
}

func (r *SingleClusterReconciler) buildSTSConfigMap(
	namespacedName types.NamespacedName, rack *asdbv1.Rack,
) error {
	r.Log.Info("Creating a new ConfigMap for statefulSet")

	confMap := &corev1.ConfigMap{}
	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: namespacedName.Name, Namespace: namespacedName.Namespace,
		}, confMap,
	)

	if err != nil {
		if errors.IsNotFound(err) {
			// build the aerospike config file based on the current spec
			var configMapData map[string]string

			configMapData, err = r.createConfigMapData(rack)
			if err != nil {
				return fmt.Errorf("failed to build dotConfig from map: %v", err)
			}

			ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)

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
			err = controllerutil.SetControllerReference(
				r.aeroCluster, confMap, r.Scheme,
			)
			if err != nil {
				return err
			}

			if err = r.Client.Create(
				context.TODO(), confMap, createOption,
			); err != nil {
				return fmt.Errorf(
					"failed to create new confMap for StatefulSet: %v", err,
				)
			}

			r.Log.Info(
				"Created new ConfigMap", "ConfigMap.Namespace",
				confMap.Namespace, "ConfigMap.Name", confMap.Name,
			)

			return nil
		}

		return err
	}

	r.Log.Info(
		"Configmap already exists for statefulSet - using existing configmap",
		"name", utils.NamespacedName(confMap.Namespace, confMap.Name),
	)

	// Update existing configmap as it might not be current.
	configMapData, err := r.createConfigMapData(rack)
	if err != nil {
		return fmt.Errorf("failed to build config map data: %v", err)
	}

	// Replace config map data if differs since we are supposed to create a new config map.
	if reflect.DeepEqual(confMap.Data, configMapData) {
		return nil
	}

	r.Log.Info(
		"Updating existed configmap",
		"name", utils.NamespacedName(confMap.Namespace, confMap.Name),
	)

	confMap.Data = configMapData

	if err := r.Client.Update(
		context.TODO(), confMap, updateOption,
	); err != nil {
		return fmt.Errorf("failed to update ConfigMap for StatefulSet: %v", err)
	}

	return nil
}

func (r *SingleClusterReconciler) updateSTSConfigMap(
	namespacedName types.NamespacedName, rack *asdbv1.Rack,
) error {
	r.Log.Info("Updating ConfigMap", "ConfigMap", namespacedName)

	confMap := &corev1.ConfigMap{}
	if err := r.Client.Get(context.TODO(), namespacedName, confMap); err != nil {
		return err
	}

	// build the aerospike config file based on the current spec
	configMapData, err := r.createConfigMapData(rack)
	if err != nil {
		return fmt.Errorf("failed to build dotConfig from map: %v", err)
	}

	// Overwrite only spec based keys. Do not touch other keys like pod metadata.
	for k, v := range configMapData {
		confMap.Data[k] = v
	}

	if err := r.Client.Update(
		context.TODO(), confMap, updateOption,
	); err != nil {
		return fmt.Errorf("failed to update confMap for StatefulSet: %v", err)
	}

	return nil
}

// isPodUpgraded checks if all containers in the pod have images from the
// cluster spec.
func (r *SingleClusterReconciler) isPodUpgraded(pod *corev1.Pod) bool {
	if !utils.IsPodRunningAndReady(pod) {
		return false
	}

	return r.isPodOnDesiredImage(pod, false)
}

// isPodOnDesiredImage indicates if the pod is ready and on desired images for
// all containers.
func (r *SingleClusterReconciler) isPodOnDesiredImage(
	pod *corev1.Pod, logChanges bool,
) bool {
	return r.allContainersAreOnDesiredImages(
		r.aeroCluster,
		pod.Name, pod.Spec.Containers, logChanges,
	) &&
		r.allContainersAreOnDesiredImages(
			r.aeroCluster,
			pod.Name, pod.Spec.InitContainers,
			logChanges,
		)
}

func (r *SingleClusterReconciler) allContainersAreOnDesiredImages(
	aeroCluster *asdbv1.AerospikeCluster, podName string,
	containers []corev1.Container, logChanges bool,
) bool {
	for idx := range containers {
		container := &containers[idx]

		desiredImage, err := utils.GetDesiredImage(aeroCluster, container.Name)
		if err != nil {
			// Maybe a deleted sidecar. Ignore.
			continue
		}

		// TODO: Should we check status here?
		// status may not be ready due to any bad config (e.g. bad podSpec).
		// Due to this check, flow will be stuck at this place (upgradeImage)
		// status := getPodContainerStatus(pod, ps.Name)
		// if status == nil || !status.Ready || !IsImageEqual(ps.Image, desiredImage) {
		// 	return false
		// }
		if !utils.IsImageEqual(container.Image, desiredImage) {
			if container.Name == asdbv1.AerospikeInitContainerName {
				if logChanges {
					r.Log.Info(
						"Ignoring change to Aerospike Init Image", "pod",
						podName, "container", container.Name, "currentImage",
						container.Image, "desiredImage", desiredImage,
					)
				}

				continue
			}

			if logChanges {
				r.Log.Info(
					"Found container for upgrading/downgrading in pod", "pod",
					podName, "container", container.Name, "currentImage",
					container.Image, "desiredImage", desiredImage,
				)
			}

			return false
		}
	}

	return true
}

// Called while creating new cluster and also during rolling restart.
// Note: STS podSpec must be updated before updating storage
func (r *SingleClusterReconciler) updateSTSStorage(
	st *appsv1.StatefulSet, rackState *RackState,
) {
	r.updateSTSPVStorage(st, rackState)
	r.updateSTSNonPVStorage(st, rackState)

	// Sort volume attachments so that overlapping paths do not shadow each other.
	// For e.g. mount for /etc/ should be listed before mount for /etc/aerospike
	// otherwise /etc/aerospike will get shadowed.
	sortContainerVolumeAttachments(st.Spec.Template.Spec.InitContainers)
	sortContainerVolumeAttachments(st.Spec.Template.Spec.Containers)
}

func sortContainerVolumeAttachments(containers []corev1.Container) {
	for idx := range containers {
		sort.Slice(
			containers[idx].VolumeMounts, func(p, q int) bool {
				return containers[idx].VolumeMounts[p].MountPath < containers[idx].VolumeMounts[q].MountPath
			},
		)

		sort.Slice(
			containers[idx].VolumeDevices, func(p, q int) bool {
				return containers[idx].VolumeDevices[p].DevicePath < containers[idx].VolumeDevices[q].DevicePath
			},
		)
	}
}

// updateSTS updates the stateful set to match the spec. It is idempotent.
func (r *SingleClusterReconciler) updateSTS(
	statefulSet *appsv1.StatefulSet, rackState *RackState,
) error {
	// Update settings from pod spec.
	r.updateSTSFromPodSpec(statefulSet, rackState)

	// Update the images for all containers from the spec.
	// Our Pod Spec does not contain image for the Aerospike Server
	// Container.
	r.updateContainerImages(statefulSet)

	// This should be called before updating storage
	r.initializeSTSStorage(statefulSet, rackState)

	// TODO: Add validation. device, file, both should not exist in same storage class
	r.updateSTSStorage(statefulSet, rackState)

	// Save the updated stateful set.
	// Can we optimize this? Update stateful set only if there is any change
	// in it.
	err := r.Client.Update(context.TODO(), statefulSet, updateOption)
	if err != nil {
		return fmt.Errorf(
			"failed to update StatefulSet %s: %v",
			statefulSet.Name,
			err,
		)
	}

	r.Log.V(1).Info(
		"Saved StatefulSet", "statefulSet", *statefulSet,
	)

	return nil
}

// Returns external storage devices and the corresponding source
// that have been attached to the containers directly.
// Devices that are attached to containers and not present in aerospike config spec
// as well status are considered as external devices.
func (r *SingleClusterReconciler) getExternalStorageDevices(
	container *corev1.Container, rackState *RackState, stSpecVolumes []corev1.Volume,
) ([]corev1.VolumeDevice, []corev1.Volume) {
	var (
		externalDevices  []corev1.VolumeDevice
		volumesForDevice []corev1.Volume
	)

	rackStatusVolumes := r.getRackStatusVolumes(rackState)

	for _, volumeDevice := range container.VolumeDevices {
		volumeInSpec := getStorageVolume(rackState.Rack.Storage.Volumes, volumeDevice.Name)
		volumeInStatus := getStorageVolume(rackStatusVolumes, volumeDevice.Name)

		if volumeInSpec == nil && volumeInStatus == nil {
			externalDevices = append(externalDevices, volumeDevice)

			for idx := range stSpecVolumes {
				volume := stSpecVolumes[idx]
				if volume.Name == volumeDevice.Name {
					volumesForDevice = append(volumesForDevice, volume)
				}
			}
		}
	}

	return externalDevices, volumesForDevice
}

// Returns external volume mounts and the corresponding source
// that have been mounted to the containers directly.
// Volumes that are mounted to containers and not present in aerospike config spec
// as well status and are not one of the default volume mounts
// are considered as external Volumes.
func (r *SingleClusterReconciler) getExternalStorageMounts(
	container *corev1.Container, rackState *RackState, stSpecVolumes []corev1.Volume,
) ([]corev1.VolumeMount, []corev1.Volume) {
	var (
		externalMounts   []corev1.VolumeMount
		volumesForMounts []corev1.Volume
	)

	rackStatusVolumes := r.getRackStatusVolumes(rackState)

	for _, volumeMount := range container.VolumeMounts {
		volumeInSpec := getStorageVolume(rackState.Rack.Storage.Volumes, volumeMount.Name)
		volumeInStatus := getStorageVolume(rackStatusVolumes, volumeMount.Name)
		volumeInDefault := getContainerVolumeMounts(getDefaultAerospikeInitContainerVolumeMounts(), volumeMount.Name)

		if volumeInSpec == nil && volumeInStatus == nil && volumeInDefault == nil {
			externalMounts = append(externalMounts, volumeMount)

			for idx := range stSpecVolumes {
				volume := stSpecVolumes[idx]
				if volume.Name == volumeMount.Name {
					volumesForMounts = append(volumesForMounts, volume)
				}
			}
		}
	}

	return externalMounts, volumesForMounts
}

func (r *SingleClusterReconciler) updateSTSPVStorage(
	st *appsv1.StatefulSet, rackState *RackState,
) {
	volumes := rackState.Rack.Storage.GetPVs()

	for idx := range volumes {
		volume := &volumes[idx]
		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		switch {
		case volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock:
			initContainerVolumePathPrefix := "/workdir/block-volumes"

			r.Log.V(1).Info("Added volume device for volume", "volume", volume)

			addVolumeDeviceInContainer(
				volume.Name, initContainerAttachments,
				st.Spec.Template.Spec.InitContainers,
				initContainerVolumePathPrefix,
			)
			addVolumeDeviceInContainer(
				volume.Name, containerAttachments,
				st.Spec.Template.Spec.Containers, "",
			)
		case volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeFilesystem:
			initContainerVolumePathPrefix := "/workdir/filesystem-volumes"

			r.Log.V(1).Info("Added volume mount for volume", "volume", volume)

			addVolumeMountInContainer(
				volume.Name, initContainerAttachments,
				st.Spec.Template.Spec.InitContainers,
				initContainerVolumePathPrefix,
			)
			addVolumeMountInContainer(
				volume.Name, containerAttachments,
				st.Spec.Template.Spec.Containers, "",
			)
		default:
			// Should never come here
			continue
		}

		pvc := createPVCForVolumeAttachment(r.aeroCluster, volume)

		if !containsElement(st.Spec.VolumeClaimTemplates, &pvc) {
			st.Spec.VolumeClaimTemplates = append(
				st.Spec.VolumeClaimTemplates, pvc,
			)

			r.Log.V(1).Info("Added PVC for volume", "volume", volume)
		}
	}
}

func containsElement(claims []corev1.PersistentVolumeClaim, query *corev1.PersistentVolumeClaim) bool {
	for idx := range claims {
		if claims[idx].Name == query.Name {
			return true
		}
	}

	return false
}

func (r *SingleClusterReconciler) updateSTSNonPVStorage(
	st *appsv1.StatefulSet, rackState *RackState,
) {
	volumes := rackState.Rack.Storage.GetNonPVs()

	for idx := range volumes {
		volume := &volumes[idx]
		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		r.Log.V(1).Info(
			"Added volume mount in statefulSet pod containers for volume",
			"volume", volume,
		)

		// Add volumeMount in statefulSet pod containers for volume
		addVolumeMountInContainer(
			volume.Name, initContainerAttachments,
			st.Spec.Template.Spec.InitContainers, "",
		)
		addVolumeMountInContainer(
			volume.Name, containerAttachments, st.Spec.Template.Spec.Containers,
			"",
		)

		// Add volume in statefulSet template
		k8sVolume := createVolumeForVolumeAttachment(volume)
		st.Spec.Template.Spec.Volumes = append(
			st.Spec.Template.Spec.Volumes, k8sVolume,
		)
	}
}

func (r *SingleClusterReconciler) updateSTSSchedulingPolicy(
	st *appsv1.StatefulSet, rackState *RackState,
) {
	affinity := &corev1.Affinity{}

	// Use rack affinity, if given
	if rackState.Rack.PodSpec.Affinity != nil {
		lib.DeepCopy(affinity, rackState.Rack.PodSpec.Affinity)
	} else if r.aeroCluster.Spec.PodSpec.Affinity != nil {
		lib.DeepCopy(affinity, r.aeroCluster.Spec.PodSpec.Affinity)
	}

	// Set our rules in PodAntiAffinity
	// only enable in production, so it can be used in 1 node clusters while debugging (minikube)
	if !r.aeroCluster.Spec.PodSpec.MultiPodPerHost {
		if affinity.PodAntiAffinity == nil {
			affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		antiAffinityLabels := utils.LabelsForPodAntiAffinity(r.aeroCluster.Name)

		r.Log.Info("Adding pod affinity rules for statefulSet pod")

		antiAffinity := &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: antiAffinityLabels,
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		}

		affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(
			affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution,
			antiAffinity.RequiredDuringSchedulingIgnoredDuringExecution...,
		)
	}

	// Set our rules in NodeAffinity
	var matchExpressions []corev1.NodeSelectorRequirement

	if rackState.Rack.Zone != "" {
		matchExpressions = append(
			matchExpressions, corev1.NodeSelectorRequirement{
				Key:      "topology.kubernetes.io/zone",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{rackState.Rack.Zone},
			},
		)
	}

	if rackState.Rack.Region != "" {
		matchExpressions = append(
			matchExpressions, corev1.NodeSelectorRequirement{
				Key:      "topology.kubernetes.io/region",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{rackState.Rack.Region},
			},
		)
	}

	if rackState.Rack.RackLabel != "" {
		matchExpressions = append(
			matchExpressions, corev1.NodeSelectorRequirement{
				Key:      "aerospike.com/rack-label",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{rackState.Rack.RackLabel},
			},
		)
	}

	if rackState.Rack.NodeName != "" {
		matchExpressions = append(
			matchExpressions, corev1.NodeSelectorRequirement{
				Key:      "kubernetes.io/hostname",
				Operator: corev1.NodeSelectorOpIn,
				Values:   []string{rackState.Rack.NodeName},
			},
		)
	}

	if len(matchExpressions) != 0 {
		if affinity.NodeAffinity == nil {
			affinity.NodeAffinity = &corev1.NodeAffinity{}
		}

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
			for idx := range selector.NodeSelectorTerms {
				selector.NodeSelectorTerms[idx].MatchExpressions = append(
					selector.NodeSelectorTerms[idx].MatchExpressions,
					matchExpressions...,
				)
			}
		}
	}

	st.Spec.Template.Spec.Affinity = affinity

	// Use rack nodeSelector, if given
	if len(rackState.Rack.PodSpec.NodeSelector) != 0 {
		st.Spec.Template.Spec.NodeSelector = rackState.Rack.PodSpec.NodeSelector
	} else {
		st.Spec.Template.Spec.NodeSelector = r.aeroCluster.Spec.PodSpec.NodeSelector
	}

	// Use rack tolerations, if given
	if len(rackState.Rack.PodSpec.Tolerations) != 0 {
		st.Spec.Template.Spec.Tolerations = rackState.Rack.PodSpec.Tolerations
	} else {
		st.Spec.Template.Spec.Tolerations = r.aeroCluster.Spec.PodSpec.Tolerations
	}
}

// Called while creating new cluster and also during rolling restart.
func (r *SingleClusterReconciler) updateSTSFromPodSpec(
	st *appsv1.StatefulSet, rackState *RackState,
) {
	defaultLabels := utils.LabelsForAerospikeClusterRack(
		r.aeroCluster.Name, rackState.Rack.ID,
	)

	r.updateSTSSchedulingPolicy(st, rackState)

	userDefinedLabels := r.aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Labels
	mergedLabels := utils.MergeLabels(defaultLabels, userDefinedLabels)

	st.Spec.Template.Spec.HostNetwork = r.aeroCluster.Spec.PodSpec.HostNetwork
	st.Spec.Template.ObjectMeta.Labels = mergedLabels
	st.Spec.Template.ObjectMeta.Annotations = r.aeroCluster.Spec.PodSpec.AerospikeObjectMeta.Annotations

	st.Spec.Template.Spec.DNSPolicy = r.aeroCluster.Spec.PodSpec.DNSPolicy
	st.Spec.Template.Spec.DNSConfig = r.aeroCluster.Spec.PodSpec.DNSConfig

	st.Spec.Template.Spec.SecurityContext = r.aeroCluster.Spec.PodSpec.SecurityContext
	st.Spec.Template.Spec.ImagePullSecrets = r.aeroCluster.Spec.PodSpec.ImagePullSecrets

	st.Spec.Template.Spec.Containers =
		updateSTSContainers(
			st.Spec.Template.Spec.Containers,
			r.aeroCluster.Spec.PodSpec.Sidecars,
		)

	st.Spec.Template.Spec.InitContainers =
		updateSTSContainers(
			st.Spec.Template.Spec.InitContainers,
			r.aeroCluster.Spec.PodSpec.InitContainers,
		)

	r.updateReservedContainers(st)
}

func updateSTSContainers(
	stsContainers []corev1.Container, specContainers []corev1.Container,
) []corev1.Container {
	// Add new sidecars.
	for specIdx := range specContainers {
		specContainer := &specContainers[specIdx]
		found := false

		// Create a copy because updating stateful sets defaults
		// on the sidecar container object which mutates original aeroCluster object.
		specContainerCopy := &corev1.Container{}
		lib.DeepCopy(specContainerCopy, specContainer)

		for stsIdx := range stsContainers {
			if specContainer.Name != stsContainers[stsIdx].Name {
				continue
			}
			// Update the sidecar in case something has changed.
			// Retain volume mounts and devices to make sure external storage will not lose.
			specContainerCopy.VolumeMounts = stsContainers[stsIdx].VolumeMounts
			specContainerCopy.VolumeDevices = stsContainers[stsIdx].VolumeDevices
			stsContainers[stsIdx] = *specContainerCopy
			found = true

			break
		}

		if !found {
			// Add to stateful set containers.
			stsContainers = append(stsContainers, *specContainerCopy)
		}
	}

	// Remove deleted sidecars.
	idx := 0

	for stsIdx := range stsContainers {
		stsContainer := stsContainers[stsIdx]
		found := stsIdx == 0

		for specIdx := range specContainers {
			if specContainers[specIdx].Name == stsContainer.Name {
				found = true
				break
			}
		}

		if found {
			// Retain main aerospike container or a matched sidecar.
			stsContainers[idx] = stsContainer
			idx++
		}
	}

	return stsContainers[:idx]
}

func (r *SingleClusterReconciler) waitForAllSTSToBeReady() error {
	r.Log.Info("Waiting for cluster to be ready")

	allRackIDs := sets.NewInt()

	statusRacks := r.aeroCluster.Status.RackConfig.Racks
	for idx := range statusRacks {
		allRackIDs.Insert(statusRacks[idx].ID)
	}

	// Check for newly added racks also because we do not check for these racks just after they are added
	specRacks := r.aeroCluster.Spec.RackConfig.Racks
	for idx := range specRacks {
		allRackIDs.Insert(specRacks[idx].ID)
	}

	for rackID := range allRackIDs {
		st := &appsv1.StatefulSet{}
		stsName := getNamespacedNameForSTS(r.aeroCluster, rackID)

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

func (r *SingleClusterReconciler) getClusterSTSList() (
	*appsv1.StatefulSetList, error,
) {
	// List the pods for this aeroCluster's statefulset
	statefulSetList := &appsv1.StatefulSetList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(r.aeroCluster.Name))
	listOps := &client.ListOptions{
		Namespace: r.aeroCluster.Namespace, LabelSelector: labelSelector,
	}

	if err := r.Client.List(
		context.TODO(), statefulSetList, listOps,
	); err != nil {
		return nil, err
	}

	return statefulSetList, nil
}

// updateContainerImages updates all container images to match the Aerospike
// Cluster Spec.
func (r *SingleClusterReconciler) updateContainerImages(statefulset *appsv1.StatefulSet) {
	updateImage := func(containers []corev1.Container) {
		for idx := range containers {
			container := &containers[idx]
			desiredImage, err := utils.GetDesiredImage(
				r.aeroCluster, container.Name,
			)

			if err != nil {
				// Maybe a deleted container or an auto-injected container like Istio.
				continue
			}

			if !utils.IsImageEqual(container.Image, desiredImage) {
				r.Log.Info(
					"Updating image in statefulset spec", "container",
					container.Name, "desiredImage", desiredImage,
					"currentImage", container.Image,
				)

				containers[idx].Image = desiredImage
			}
		}
	}

	updateImage(statefulset.Spec.Template.Spec.Containers)
	updateImage(statefulset.Spec.Template.Spec.InitContainers)
}

func (r *SingleClusterReconciler) updateAerospikeInitContainerImage() error {
	stsList, err := r.getClusterSTSList()
	if err != nil {
		return err
	}

	for stsIdx := range stsList.Items {
		statefulSet := stsList.Items[stsIdx]
		for idx := range statefulSet.Spec.Template.Spec.InitContainers {
			container := &statefulSet.Spec.Template.Spec.InitContainers[idx]
			if container.Name != asdbv1.AerospikeInitContainerName {
				continue
			}

			desiredImage, err := utils.GetDesiredImage(
				r.aeroCluster, container.Name,
			)
			if err != nil {
				return err
			}

			if !utils.IsImageEqual(container.Image, desiredImage) {
				r.Log.Info(
					"Updating image in statefulset spec",
					"statefulset", statefulSet.Name,
					"container", container.Name,
					"desiredImage", desiredImage,
					"currentImage", container.Image,
				)

				statefulSet.Spec.Template.Spec.InitContainers[idx].Image = desiredImage

				if err := r.Client.Update(context.TODO(), &statefulSet, updateOption); err != nil {
					return fmt.Errorf(
						"failed to update StatefulSet %s: %v",
						statefulSet.Name,
						err,
					)
				}

				r.Log.V(1).Info(
					"Saved StatefulSet", "statefulSet", statefulSet,
				)
			}

			break
		}
	}

	return nil
}

func (r *SingleClusterReconciler) updateReservedContainers(st *appsv1.StatefulSet) {
	r.updateAerospikeContainer(st)
	r.updateAerospikeInitContainer(st)
}

func (r *SingleClusterReconciler) updateAerospikeContainer(st *appsv1.StatefulSet) {
	resources := r.aeroCluster.Spec.PodSpec.AerospikeContainerSpec.Resources
	if resources != nil {
		// These resources are for main aerospike container. Other sidecar can mention their own resources.
		st.Spec.Template.Spec.Containers[0].Resources = *resources
	} else {
		st.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
	}

	// This SecurityContext is for main aerospike container. Other sidecars can mention their own SecurityContext.
	st.Spec.Template.Spec.Containers[0].SecurityContext = r.aeroCluster.Spec.PodSpec.AerospikeContainerSpec.SecurityContext
}

func (r *SingleClusterReconciler) updateAerospikeInitContainer(st *appsv1.StatefulSet) {
	var resources *corev1.ResourceRequirements
	if r.aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		resources = r.aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec.Resources
	}

	if resources != nil {
		// These resources are for main aerospike-init container. Other init containers can mention their own resources.
		st.Spec.Template.Spec.InitContainers[0].Resources = *resources
	} else {
		st.Spec.Template.Spec.InitContainers[0].Resources = corev1.ResourceRequirements{}
	}

	// This SecurityContext is for main aerospike-init container.
	// Other init containers can mention their own SecurityContext.
	if r.aeroCluster.Spec.PodSpec.AerospikeInitContainerSpec != nil {
		st.Spec.Template.Spec.InitContainers[0].SecurityContext = r.aeroCluster.Spec.PodSpec.
			AerospikeInitContainerSpec.SecurityContext
	} else {
		st.Spec.Template.Spec.InitContainers[0].SecurityContext = nil
	}
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

func getDefaultSTSVolumes(
	aeroCluster *asdbv1.AerospikeCluster, rackState *RackState,
) []corev1.Volume {
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
						Name: getNamespacedNameForSTSConfigMap(
							aeroCluster, rackState.Rack.ID,
						).Name,
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

func (r *SingleClusterReconciler) initializeSTSStorage(
	st *appsv1.StatefulSet,
	rackState *RackState,
) {
	// Initialize sts storage
	var specVolumes []corev1.Volume

	//nolint:dupl // not duplicate
	for idx := range st.Spec.Template.Spec.InitContainers {
		externalMounts, volumesForMount := r.getExternalStorageMounts(
			&st.Spec.Template.Spec.InitContainers[idx], rackState, st.Spec.Template.Spec.Volumes,
		)

		specVolumes = append(specVolumes, volumesForMount...)

		if idx == 0 {
			st.Spec.Template.Spec.InitContainers[idx].VolumeMounts = getDefaultAerospikeInitContainerVolumeMounts()
		} else {
			st.Spec.Template.Spec.InitContainers[idx].VolumeMounts = []corev1.VolumeMount{}
		}

		// Appending external volume mounts back to the corresponding container.
		st.Spec.Template.Spec.InitContainers[idx].VolumeMounts = append(
			st.Spec.Template.Spec.InitContainers[idx].VolumeMounts, externalMounts...,
		)

		externalDevices, volumesForDevice := r.getExternalStorageDevices(
			&st.Spec.Template.Spec.InitContainers[idx], rackState, st.Spec.Template.Spec.Volumes,
		)

		specVolumes = append(specVolumes, volumesForDevice...)

		st.Spec.Template.Spec.InitContainers[idx].VolumeDevices = []corev1.VolumeDevice{}

		// Appending external devices back to the corresponding container.
		st.Spec.Template.Spec.InitContainers[idx].VolumeDevices = append(
			st.Spec.Template.Spec.InitContainers[idx].VolumeDevices, externalDevices...,
		)
	}

	//nolint:dupl // not duplicate
	for idx := range st.Spec.Template.Spec.Containers {
		externalMounts, volumesForMount := r.getExternalStorageMounts(
			&st.Spec.Template.Spec.Containers[idx], rackState, st.Spec.Template.Spec.Volumes,
		)

		specVolumes = append(specVolumes, volumesForMount...)

		if idx == 0 {
			st.Spec.Template.Spec.Containers[idx].VolumeMounts = getDefaultAerospikeContainerVolumeMounts()
		} else {
			st.Spec.Template.Spec.Containers[idx].VolumeMounts = []corev1.VolumeMount{}
		}

		// Appending external volume mounts back to the corresponding container.
		st.Spec.Template.Spec.Containers[idx].VolumeMounts = append(
			st.Spec.Template.Spec.Containers[idx].VolumeMounts, externalMounts...,
		)

		externalDevices, volumesForDevice := r.getExternalStorageDevices(
			&st.Spec.Template.Spec.Containers[idx], rackState, st.Spec.Template.Spec.Volumes,
		)

		specVolumes = append(specVolumes, volumesForDevice...)

		st.Spec.Template.Spec.Containers[idx].VolumeDevices = []corev1.VolumeDevice{}

		// Appending external devices back to the corresponding container.
		st.Spec.Template.Spec.Containers[idx].VolumeDevices = append(
			st.Spec.Template.Spec.Containers[idx].VolumeDevices, externalDevices...,
		)
	}

	st.Spec.Template.Spec.Volumes = getDefaultSTSVolumes(
		r.aeroCluster, rackState,
	)

	// Populating unique volume source to statefulSet spec storage.
	st.Spec.Template.Spec.Volumes = r.appendUniqueVolume(st.Spec.Template.Spec.Volumes, specVolumes)
}

func createPVCForVolumeAttachment(
	aeroCluster *asdbv1.AerospikeCluster, volume *asdbv1.VolumeSpec,
) corev1.PersistentVolumeClaim {
	pv := volume.Source.PersistentVolume

	var accessModes []corev1.PersistentVolumeAccessMode
	accessModes = append(accessModes, pv.AccessModes...)

	if len(accessModes) == 0 {
		accessModes = append(accessModes, "ReadWriteOnce")
	}

	// Use this path annotation while matching pvc with storage volume
	newAnnotations := map[string]string{storageVolumeAnnotationKey: volume.Name}
	for k, v := range pv.Annotations {
		newAnnotations[k] = v
	}

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        volume.Name,
			Namespace:   aeroCluster.Namespace,
			Annotations: newAnnotations,
			Labels:      pv.Labels,
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

func createVolumeForVolumeAttachment(volume *asdbv1.VolumeSpec) corev1.Volume {
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
func getFinalVolumeAttachmentsForVolume(volume *asdbv1.VolumeSpec) (
	initContainerAttachments,
	containerAttachments []asdbv1.VolumeAttachment,
) {
	// Create dummy attachment for initContainer
	initVolumePath := "/" + volume.Name // Using volume name for initContainer

	// All volumes should be mounted in init container to allow initialization
	initContainerAttachments = append(
		initContainerAttachments, volume.InitContainers...,
	)
	initContainerAttachments = append(
		initContainerAttachments, asdbv1.VolumeAttachment{
			ContainerName: asdbv1.AerospikeInitContainerName,
			Path:          initVolumePath,
		},
	)

	// Create dummy attachment for aerospike server container
	containerAttachments = append(containerAttachments, volume.Sidecars...)

	if volume.Aerospike != nil {
		containerAttachments = append(
			containerAttachments, asdbv1.VolumeAttachment{
				ContainerName: asdbv1.AerospikeServerContainerName,
				Path:          volume.Aerospike.Path,
			},
		)
	}

	return initContainerAttachments, containerAttachments
}

func addVolumeMountInContainer(
	volumeName string, volumeAttachments []asdbv1.VolumeAttachment,
	containers []corev1.Container, pathPrefix string,
) {
	var volumeMount corev1.VolumeMount

	for _, volumeAttachment := range volumeAttachments {
		for idx := range containers {
			container := &containers[idx]

			if container.Name == volumeAttachment.ContainerName {
				if container.Name == asdbv1.AerospikeInitContainerName {
					volumeMount = corev1.VolumeMount{
						Name:      volumeName,
						MountPath: pathPrefix + volumeAttachment.Path,
					}
				} else {
					volumeMount = corev1.VolumeMount{
						Name:      volumeName,
						MountPath: volumeAttachment.Path,
					}
				}

				container.VolumeMounts = append(
					container.VolumeMounts, volumeMount,
				)

				break
			}
		}
	}
}

func addVolumeDeviceInContainer(
	volumeName string, volumeAttachments []asdbv1.VolumeAttachment,
	containers []corev1.Container, pathPrefix string,
) {
	var volumeDevice corev1.VolumeDevice

	for _, volumeAttachment := range volumeAttachments {
		for idx := range containers {
			container := &containers[idx]

			if container.Name == volumeAttachment.ContainerName {
				if container.Name == asdbv1.AerospikeInitContainerName {
					volumeDevice = corev1.VolumeDevice{
						Name:       volumeName,
						DevicePath: pathPrefix + volumeAttachment.Path,
					}
				} else {
					volumeDevice = corev1.VolumeDevice{
						Name:       volumeName,
						DevicePath: volumeAttachment.Path,
					}
				}

				container.VolumeDevices = append(
					container.VolumeDevices, volumeDevice,
				)

				break
			}
		}
	}
}

func getSTSContainerPort(
	multiPodPerHost bool, aeroConf *asdbv1.AerospikeConfigSpec,
) []corev1.ContainerPort {
	ports := make([]corev1.ContainerPort, 0, len(defaultContainerPorts))

	for portName, portInfo := range defaultContainerPorts {
		configPort := asdbv1.GetPortFromConfig(
			aeroConf, portInfo.connectionType, portInfo.configParam,
		)

		if configPort == nil {
			continue
		}

		containerPort := corev1.ContainerPort{
			Name:          portName,
			ContainerPort: int32(*configPort),
		}
		// Single pod per host. Enable hostPort setting
		// The hostPort setting applies to the Kubernetes containers.
		// The container port will be exposed to the external network at <hostIP>:<hostPort>,
		// where the hostIP is the IP address of the Kubernetes node where
		// the container is running and the hostPort is the port requested by the user
		if (!multiPodPerHost) && portInfo.exposedOnHost {
			containerPort.HostPort = containerPort.ContainerPort
		}

		ports = append(ports, containerPort)
	}

	return ports
}

func getNamespacedNameForSTS(
	aeroCluster *asdbv1.AerospikeCluster, rackID int,
) types.NamespacedName {
	return types.NamespacedName{
		Name:      aeroCluster.Name + "-" + strconv.Itoa(rackID),
		Namespace: aeroCluster.Namespace,
	}
}

func getSTSPodOrdinal(podName string) (*int32, error) {
	parts := strings.Split(podName, "-")
	ordinalStr := parts[len(parts)-1]

	ordinal, err := strconv.ParseInt(ordinalStr, 10, 32)
	if err != nil {
		return nil, err
	}

	result := int32(ordinal)

	return &result, nil
}

func getSTSPodName(statefulSetName string, index int32) string {
	return fmt.Sprintf("%s-%d", statefulSetName, index)
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

// Return if volume with name given is present in volumes array else return nil.
func getVolume(
	volumes []corev1.Volume, name string,
) *corev1.Volume {
	for idx := range volumes {
		if volumes[idx].Name == name {
			return &volumes[idx]
		}
	}

	return nil
}

// Merge and return two core volume arrays with unique entry.
func (r *SingleClusterReconciler) appendUniqueVolume(
	stSpecVolumes []corev1.Volume, externalSpecVolumes []corev1.Volume,
) []corev1.Volume {
	for idx := range externalSpecVolumes {
		if getVolume(stSpecVolumes, externalSpecVolumes[idx].Name) == nil {
			stSpecVolumes = append(stSpecVolumes, externalSpecVolumes[idx])
		}
	}

	return stSpecVolumes
}
