package controllers

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
)

const (
	aeroClusterServiceAccountName string = "aerospike-cluster"
)

// The default cpu request for the aerospike-server container
const (
	// This storage path annotation is added in pvc to make reverse association with storage.volume.path
	// while deleting pvc
	storagePathAnnotationKey = "storage-path"

	confDirName                = "confdir"
	initConfDirName            = "initconfigs"
	podServiceAccountMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"
)

type PortInfo struct {
	connectionType string
	configParam    string
	defaultPort    int
	exposedOnHost  bool
}

var defaultContainerPorts = map[string]PortInfo{
	asdbv1beta1.ServicePortName: {
		connectionType: "service",
		configParam:    "port",
		defaultPort:    asdbv1beta1.ServicePort,
		exposedOnHost:  true,
	},
	asdbv1beta1.ServiceTLSPortName: {
		connectionType: "service",
		configParam:    "tls-port",
		defaultPort:    asdbv1beta1.ServiceTLSPort,
		exposedOnHost:  true,
	},
	asdbv1beta1.FabricPortName: {
		connectionType: "fabric",
		configParam:    "port",
		defaultPort:    asdbv1beta1.FabricPort,
	},
	asdbv1beta1.FabricTLSPortName: {
		connectionType: "fabric",
		configParam:    "tls-port",
		defaultPort:    asdbv1beta1.FabricTLSPort,
	},
	asdbv1beta1.HeartbeatPortName: {
		connectionType: "heartbeat",
		configParam:    "port",
		defaultPort:    asdbv1beta1.HeartbeatPort,
	},
	asdbv1beta1.HeartbeatTLSPortName: {
		connectionType: "heartbeat",
		configParam:    "tls-port",
		defaultPort:    asdbv1beta1.HeartbeatTLSPort,
	},
	asdbv1beta1.InfoPortName: {
		connectionType: "info",
		configParam:    "port",
		defaultPort:    asdbv1beta1.InfoPort,
		exposedOnHost:  true,
	},
}

func (r *AerospikeClusterReconciler) createSTS(aeroCluster *asdbv1beta1.AerospikeCluster, namespacedName types.NamespacedName, rackState RackState) (*appsv1.StatefulSet, error) {
	replicas := int32(rackState.Size)

	r.Log.Info("Create statefulset for AerospikeCluster", "size", replicas)

	if aeroCluster.Spec.PodSpec.MultiPodPerHost {
		// Create services for all statefulset pods
		for i := 0; i < rackState.Size; i++ {
			// Statefulset name created from cr name
			name := fmt.Sprintf("%s-%d", namespacedName.Name, i)
			if err := r.createPodService(aeroCluster, name, aeroCluster.Namespace); err != nil {
				return nil, err
			}
		}
	}

	ports := getSTSContainerPort(aeroCluster.Spec.PodSpec.MultiPodPerHost, aeroCluster.Spec.AerospikeConfig)

	ls := utils.LabelsForAerospikeClusterRack(aeroCluster.Name, rackState.Rack.ID)

	tlsName, _ := asdbv1beta1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig)
	envVarList := []corev1.EnvVar{
		newSTSEnvVar("MY_POD_NAME", "metadata.name"),
		newSTSEnvVar("MY_POD_NAMESPACE", "metadata.namespace"),
		newSTSEnvVar("MY_POD_IP", "status.podIP"),
		newSTSEnvVar("MY_HOST_IP", "status.hostIP"),
		newSTSEnvVarStatic("MY_POD_TLS_NAME", tlsName),
		newSTSEnvVarStatic("MY_POD_CLUSTER_NAME", aeroCluster.Name),
	}

	if tlsName != "" {
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
						Image: asdbv1beta1.AerospikeServerInitContainerImage,
						// Change to PullAlways for image testing.
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts:    getDefaultAerospikeInitContainerVolumeMounts(),
						Env: append(envVarList, []corev1.EnvVar{
							{
								// Headless service has the same name as AerospikeCluster
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
	if err := controllerutil.SetControllerReference(aeroCluster, st, r.Scheme); err != nil {
		return nil, err
	}

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

	// Check for statefulset at the end,
	// if we check before pods then we would not know status of individual pods
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
		r.Log.V(1).Info("StatefulSet spec.replica not matching status.replica", "status", st.Status.Replicas, "spec", *st.Spec.Replicas)
	}
	if !updated {
		return fmt.Errorf("statefulset status is not updated")
	}

	r.Log.Info("StatefulSet is ready")

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
			err = controllerutil.SetControllerReference(aeroCluster, confMap, r.Scheme)
			if err != nil {
				return err
			}

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

	serviceName := getSTSHeadLessSvcName(aeroCluster)
	service := &corev1.Service{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: aeroCluster.Namespace}, service)
	if err != nil {
		if errors.IsNotFound(err) {
			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					// Headless service has the same name as AerospikeCluster
					Name:      serviceName,
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
							Port: int32(asdbv1beta1.GetServicePort(aeroCluster.Spec.AerospikeConfig)),
							Name: "info",
						},
					},
				},
			}
			// Set AerospikeCluster instance as the owner and controller
			err := controllerutil.SetControllerReference(aeroCluster, service, r.Scheme)
			if err != nil {
				return err
			}

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

func (r *AerospikeClusterReconciler) createSTSLoadBalancerSvc(aeroCluster *asdbv1beta1.AerospikeCluster) error {
	loadBalancer := aeroCluster.Spec.SeedsFinderServices.LoadBalancer
	if loadBalancer == nil {
		r.Log.Info("LoadBalancer is not configured. Skipping...")
		return nil
	}

	serviceName := aeroCluster.Name + "-lb"
	service := &corev1.Service{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: aeroCluster.Namespace}, service)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating LoadBalancer service for cluster")
			ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)
			var targetPort int32
			if loadBalancer.TargetPort >= 1024 {
				// if target port is specified in CR.
				targetPort = loadBalancer.TargetPort
			} else if tlsName, tlsPort := asdbv1beta1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig); tlsName != "" {
				targetPort = int32(tlsPort)
			} else {
				targetPort = int32(asdbv1beta1.GetServicePort(aeroCluster.Spec.AerospikeConfig))
			}
			var port int32
			if loadBalancer.Port >= 1024 {
				// if port is specified in CR.
				port = loadBalancer.Port
			} else {
				port = targetPort
			}

			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        serviceName,
					Namespace:   aeroCluster.Namespace,
					Annotations: loadBalancer.Annotations,
					Labels:      ls,
				},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeLoadBalancer,
					Selector: ls,
					Ports: []corev1.ServicePort{
						{
							Port:       port,
							TargetPort: intstr.FromInt(int(targetPort)),
						},
					},
				},
			}
			if len(loadBalancer.LoadBalancerSourceRanges) > 0 {
				service.Spec.LoadBalancerSourceRanges = loadBalancer.LoadBalancerSourceRanges
			}
			if len(loadBalancer.ExternalTrafficPolicy) > 0 {
				service.Spec.ExternalTrafficPolicy = loadBalancer.ExternalTrafficPolicy
			}

			// Set AerospikeCluster instance as the owner and controller
			if err := controllerutil.SetControllerReference(aeroCluster, service, r.Scheme); err != nil {
				return err
			}

			if err := r.Client.Create(context.TODO(), service, createOption); err != nil {
				return err
			}
			r.Log.Info("Created new LoadBalancer service.", "serviceName", service.GetName())

			return nil
		}
		return err
	}
	r.Log.Info("LoadBalancer Service already exist for cluster. Using existing service", "name", utils.NamespacedName(service.Namespace, service.Name))
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
					Port: int32(asdbv1beta1.GetServicePort(aeroCluster.Spec.AerospikeConfig)),
				},
			},
			ExternalTrafficPolicy: "Local",
		},
	}
	if tlsName, tlsPort := asdbv1beta1.GetServiceTLSNameAndPort(aeroCluster.Spec.AerospikeConfig); tlsName != "" {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name: "tls",
			Port: int32(tlsPort),
		})
	}
	// Set AerospikeCluster instance as the owner and controller.
	// It is created before Pod, so Pod cannot be the owner
	err := controllerutil.SetControllerReference(aeroCluster, service, r.Scheme)
	if err != nil {
		return err
	}

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
func (r *AerospikeClusterReconciler) updateSTSStorage(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	r.updateSTSPVStorage(aeroCluster, st, rackState)
	r.updateSTSNonPVStorage(aeroCluster, st, rackState)

	// Sort volume attachments so that overlapping paths do not shadow each other.
	// For e.g. mount for /etc should be listed before mount for /etc/aerospike
	// otherwise /etc/aerospike will get shadowed.
	sortContainerVolumeAttachments(st.Spec.Template.Spec.InitContainers)
	sortContainerVolumeAttachments(st.Spec.Template.Spec.Containers)
}

func sortContainerVolumeAttachments(containers []corev1.Container) {
	for i := range containers {
		sort.Slice(containers[i].VolumeMounts, func(p, q int) bool {
			return containers[i].VolumeMounts[p].MountPath < containers[i].VolumeMounts[q].MountPath
		})

		sort.Slice(containers[i].VolumeDevices, func(p, q int) bool {
			return containers[i].VolumeDevices[p].DevicePath < containers[i].VolumeDevices[q].DevicePath
		})
	}
}

func (r *AerospikeClusterReconciler) updateSTSPVStorage(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	volumes := rackState.Rack.Storage.GetPVs()

	for _, volume := range volumes {
		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeBlock {
			initContainerVolumePathPrefix := "/block-volumes"

			r.Log.V(1).Info("added volume device for volume", "volume", volume)

			addVolumeDeviceInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, initContainerVolumePathPrefix)
			addVolumeDeviceInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")
		} else if volume.Source.PersistentVolume.VolumeMode == corev1.PersistentVolumeFilesystem {
			initContainerVolumePathPrefix := "/filesystem-volumes"

			r.Log.V(1).Info("added volume mount for volume", "volume", volume)

			addVolumeMountInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, initContainerVolumePathPrefix)
			addVolumeMountInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")
		} else {
			// Should never come here
			continue
		}

		r.Log.V(1).Info("added PVC for volume", "volume", volume)

		pvc := createPVCForVolumeAttachment(aeroCluster, volume)
		st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)
	}
}

func (r *AerospikeClusterReconciler) updateSTSNonPVStorage(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
	volumes := rackState.Rack.Storage.GetNonPVs()

	for _, volume := range volumes {
		initContainerAttachments, containerAttachments := getFinalVolumeAttachmentsForVolume(volume)

		r.Log.V(1).Info("added volume mount in statefulSet pod containers for volume", "volume", volume)

		// Add volumeMount in statefulSet pod containers for volume
		addVolumeMountInContainer(volume.Name, initContainerAttachments, st.Spec.Template.Spec.InitContainers, "")
		addVolumeMountInContainer(volume.Name, containerAttachments, st.Spec.Template.Spec.Containers, "")

		// Add volume in statefulSet template
		k8sVolume := createVolumeForVolumeAttachment(volume)
		st.Spec.Template.Spec.Volumes = append(st.Spec.Template.Spec.Volumes, k8sVolume)
	}
}

func (r *AerospikeClusterReconciler) updateSTSSchedulingPolicy(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {
	affinity := &corev1.Affinity{}

	// Use rack affinity, if given
	if rackState.Rack.PodSpec.Affinity != nil {
		lib.DeepCopy(affinity, rackState.Rack.PodSpec.Affinity)
	} else if aeroCluster.Spec.PodSpec.Affinity != nil {
		lib.DeepCopy(affinity, aeroCluster.Spec.PodSpec.Affinity)
	}

	// Set our rules in PodAntiAffinity
	// only enable in production, so it can be used in 1 node clusters while debugging (minikube)
	if !aeroCluster.Spec.PodSpec.MultiPodPerHost {
		if affinity.PodAntiAffinity == nil {
			affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		r.Log.Info("Adding pod affinity rules for statefulSet pod")
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
			for i := range selector.NodeSelectorTerms {
				selector.NodeSelectorTerms[i].MatchExpressions = append(selector.NodeSelectorTerms[i].MatchExpressions, matchExpressions...)
			}
		}
	}

	st.Spec.Template.Spec.Affinity = affinity

	// Use rack nodeSelector, if given
	if len(rackState.Rack.PodSpec.NodeSelector) != 0 {
		st.Spec.Template.Spec.NodeSelector = rackState.Rack.PodSpec.NodeSelector
	} else {
		st.Spec.Template.Spec.NodeSelector = aeroCluster.Spec.PodSpec.NodeSelector
	}

	// Use rack tolerations, if given
	if len(rackState.Rack.PodSpec.Tolerations) != 0 {
		st.Spec.Template.Spec.Tolerations = rackState.Rack.PodSpec.Tolerations
	} else {
		st.Spec.Template.Spec.Tolerations = aeroCluster.Spec.PodSpec.Tolerations
	}
}

// Called while creating new cluster and also during rolling restart.
func (r *AerospikeClusterReconciler) updateSTSPodSpec(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string, rackState RackState) {
	r.updateSTSSchedulingPolicy(aeroCluster, st, labels, rackState)

	st.Spec.Template.Spec.HostNetwork = aeroCluster.Spec.PodSpec.HostNetwork

	st.Spec.Template.Spec.DNSPolicy = aeroCluster.Spec.PodSpec.DNSPolicy

	st.Spec.Template.Spec.Containers =
		updateStatefulSetContainers(st.Spec.Template.Spec.Containers, aeroCluster.Spec.PodSpec.Sidecars)

	st.Spec.Template.Spec.InitContainers =
		updateStatefulSetContainers(st.Spec.Template.Spec.InitContainers, aeroCluster.Spec.PodSpec.InitContainers)
}

func updateStatefulSetContainers(stsContainers []corev1.Container, specContainers []corev1.Container) []corev1.Container {
	// Add new sidecars.
	for _, specContainer := range specContainers {
		found := false

		// Create a copy because updating stateful sets defaults
		// on the sidecar container object which mutates original aeroCluster object.
		specContainerCopy := corev1.Container{}
		lib.DeepCopy(&specContainerCopy, &specContainer)
		for i, stsContainer := range stsContainers {
			if specContainer.Name == stsContainer.Name {
				// Update the sidecar in case something has changed.
				stsContainers[i] = specContainerCopy
				found = true
				break
			}
		}

		if !found {
			// Add to stateful set containers.
			stsContainers = append(stsContainers, specContainerCopy)
		}
	}

	// Remove deleted sidecars.
	j := 0
	for i, stsContainer := range stsContainers {
		found := i == 0
		for _, specContainer := range specContainers {
			if specContainer.Name == stsContainer.Name {
				found = true
				break
			}
		}

		if found {
			// Retain main aerospike container or a matched sidecar.
			stsContainers[j] = stsContainer
			j++
		}
	}
	return stsContainers[:j]
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

func (r *AerospikeClusterReconciler) updateSTSContainerResources(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet) {
	// These resources are for main aerospike container. Other sidecar can mention their own resources.
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

func getDefaultSTSVolumes(aeroCluster *asdbv1beta1.AerospikeCluster, rackState RackState) []corev1.Volume {
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

func initializeSTSStorage(aeroCluster *asdbv1beta1.AerospikeCluster, st *appsv1.StatefulSet, rackState RackState) {
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

func createPVCForVolumeAttachment(aeroCluster *asdbv1beta1.AerospikeCluster, volume asdbv1beta1.VolumeSpec) corev1.PersistentVolumeClaim {

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

func createVolumeForVolumeAttachment(volume asdbv1beta1.VolumeSpec) corev1.Volume {
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
func getFinalVolumeAttachmentsForVolume(volume asdbv1beta1.VolumeSpec) (initContainerAttachments, containerAttachments []asdbv1beta1.VolumeAttachment) {
	// Create dummy attachment for initContainer
	initVolumePath := "/" + volume.Name // Using volume name for initContainer

	// All volumes should be mounted in init container to allow initialization
	initContainerAttachments = append(initContainerAttachments, volume.InitContainers...)
	initContainerAttachments = append(initContainerAttachments, asdbv1beta1.VolumeAttachment{
		ContainerName: asdbv1beta1.AerospikeServerInitContainerName,
		Path:          initVolumePath,
	})

	// Create dummy attachment for aerospike server container
	containerAttachments = append(containerAttachments, volume.Sidecars...)
	if volume.Aerospike != nil {
		containerAttachments = append(containerAttachments, asdbv1beta1.VolumeAttachment{
			ContainerName: asdbv1beta1.AerospikeServerContainerName,
			Path:          volume.Aerospike.Path,
		})
	}
	return initContainerAttachments, containerAttachments
}

func addVolumeMountInContainer(volumeName string, volumeAttachments []asdbv1beta1.VolumeAttachment, containers []corev1.Container, pathPrefix string) {
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

func addVolumeDeviceInContainer(volumeName string, volumeAttachments []asdbv1beta1.VolumeAttachment, containers []corev1.Container, pathPrefix string) {
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

func getSTSContainerPort(multiPodPerHost bool, aeroConf *asdbv1beta1.AerospikeConfigSpec) []corev1.ContainerPort {
	var ports []corev1.ContainerPort
	for portName, portInfo := range defaultContainerPorts {
		containerPort := corev1.ContainerPort{
			Name:          portName,
			ContainerPort: int32(asdbv1beta1.GetPortFromConfig(aeroConf, portInfo.connectionType, portInfo.configParam, portInfo.defaultPort)),
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
