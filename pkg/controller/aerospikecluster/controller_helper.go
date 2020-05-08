package aerospikecluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	as "github.com/aerospike/aerospike-client-go"

	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/configmap"
	"github.com/citrusleaf/aerospike-kubernetes-operator/pkg/controller/utils"
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
)

// the default cpu request for the aerospike-server container
const (
	aerospikeServerContainerDefaultCPURequest = 1
	// the default memory request for the aerospike-server container
	// matches the default value of namespace.memory-size
	// https://www.aerospike.com/docs/reference/configuration#memory-size
	aerospikeServerContainerDefaultMemoryRequestGi         = 4
	aerospikeServerNamespaceDefaultFilesizeMemoryRequestGi = 1
)

//------------------------------------------------------------------------------------
// controller helper
//------------------------------------------------------------------------------------

func (r *ReconcileAerospikeCluster) isAeroClusterUpgradeNeeded(aeroCluster *aerospikev1alpha1.AerospikeCluster) (bool, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	desiredImage := aeroCluster.Spec.Build
	podList, err := r.getPodList(aeroCluster)
	if err != nil {
		return true, fmt.Errorf("Failed to list pods: %v", err)
	}
	for _, p := range podList.Items {
		for _, ps := range p.Status.ContainerStatuses {
			actualImage := ps.Image
			if !utils.IsImageEqual(actualImage, desiredImage) {
				logger.Info("Pod image validation failed. Need upgrade/downgrade", log.Ctx{"currentImage": ps.Image, "desiredImage": desiredImage, "podName": p.Name})
				return true, nil
			}
		}
	}
	return false, nil
}

func (r *ReconcileAerospikeCluster) isAnyPodInFailedState(aeroCluster *aerospikev1alpha1.AerospikeCluster, podList []corev1.Pod) bool {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	for _, p := range podList {
		for _, ps := range p.Status.ContainerStatuses {
			if err := utils.PodFailedStatus(&p); err != nil {
				logger.Info("AerospikeCluster Pod is in failed state", log.Ctx{"currentImage": ps.Image, "podName": p.Name, "err": err})
				return true
			}
		}
	}
	return false
}

func (r *ReconcileAerospikeCluster) getPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*corev1.PodList, error) {
	// List the pods for this aeroCluster's statefulset
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeCluster(aeroCluster.Name))
	listOps := &client.ListOptions{Namespace: aeroCluster.Namespace, LabelSelector: labelSelector}

	if err := r.client.List(context.TODO(), podList, listOps); err != nil {
		return nil, err
	}
	return podList, nil
}

func (r *ReconcileAerospikeCluster) getOrderedPodList(aeroCluster *aerospikev1alpha1.AerospikeCluster) ([]corev1.Pod, error) {
	podList, err := r.getPodList(aeroCluster)
	if err != nil {
		return nil, err
	}
	sortedList := make([]corev1.Pod, len(podList.Items))
	for _, p := range podList.Items {
		indexStr := strings.Split(p.Name, "-")
		indexInt, _ := strconv.Atoi(indexStr[1])
		sortedList[(len(podList.Items)-1)-indexInt] = p
	}
	return sortedList, nil
}

func (r *ReconcileAerospikeCluster) createStatefulSetForAerospikeCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*appsv1.StatefulSet, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Create statefulset for AerospikeCluster")

	if aeroCluster.Spec.MultiPodPerHost {
		// Create services for all statefulset pods
		for i := 0; i < int(aeroCluster.Spec.Size); i++ {
			// Statefulset name created from cr name
			name := fmt.Sprintf("%s-%d", aeroCluster.Name, i)
			if err := r.createServiceForPod(aeroCluster, name, aeroCluster.Namespace); err != nil {
				return nil, err
			}
		}
	}

	ports := getAeroClusterContainerPort(aeroCluster.Spec.MultiPodPerHost)

	ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)

	replicas := aeroCluster.Spec.Size

	envVarList := []corev1.EnvVar{
		newEnvVar("MY_POD_NAME", "metadata.name"),
		newEnvVar("MY_POD_NAMESPACE", "metadata.namespace"),
		newEnvVar("MY_POD_IP", "status.podIP"),
		newEnvVar("MY_HOST_IP", "status.hostIP"),
	}

	const confDirName = "confdir"
	const initConfDirName = "initconfigs"

	st := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      aeroCluster.Name,
			Namespace: aeroCluster.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			//PodManagementPolicy: appsv1.ParallelPodManagement,
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
						Name:            "aerospike-init",
						Image:           "aerospike/aerospike-kubernetes-init:0.0.9",
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
							corev1.EnvVar{
								// Headless service has same name as AerospikeCluster
								Name:  "SERVICE",
								Value: getHeadLessSvcName(aeroCluster),
							},
							corev1.EnvVar{
								Name:  "MULTI_POD_PER_HOST",
								Value: strconv.FormatBool(aeroCluster.Spec.MultiPodPerHost),
							},
						}...),
					}},

					Containers: []corev1.Container{{
						Name:            "aerospike-server",
						Image:           aeroCluster.Spec.Build,
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
										Name: aerospikeConfConfigMapName,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	updateStatefulSetAerospikeServerContainerResources(aeroCluster, st)
	// TODO: Add validation. device, file, both should not exist in same storage class
	updateStatefulSetStorage(aeroCluster, st)

	updateStatefulSetSecretInfo(aeroCluster, st)

	updateStatefulSetAffinity(aeroCluster, st, ls)
	// Set AerospikeCluster instance as the owner and controller
	controllerutil.SetControllerReference(aeroCluster, st, r.scheme)

	if err := r.client.Create(context.TODO(), st, createOption); err != nil {
		return nil, fmt.Errorf("Failed to create new StatefulSet: %v", err)
	}
	logger.Info("Created new StatefulSet", log.Ctx{"StatefulSet.Namespace": st.Namespace, "StatefulSet.Name": st.Name})

	if err := r.waitForStatefulSetToBeReady(aeroCluster, st); err != nil {
		return st, fmt.Errorf("Failed to wait for statefulset to be ready: %v", err)
	}

	return st, nil
}

func (r *ReconcileAerospikeCluster) deleteStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Delete statefulset")

	return r.client.Delete(context.TODO(), st)
}

func (r *ReconcileAerospikeCluster) buildConfigMap(aeroCluster *aerospikev1alpha1.AerospikeCluster, name string) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	logger.Info("Creating a new ConfigMap for statefulSet")

	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: aeroCluster.Namespace}, confMap)
	if err != nil {
		if errors.IsNotFound(err) {
			// build the aerospike config file based on the current spec
			configMapData, err := configmap.CreateConfigMapData(aeroCluster)
			if err != nil {
				return fmt.Errorf("Failed to build dotConfig from map: %v", err)
			}
			ls := utils.LabelsForAerospikeCluster(aeroCluster.Name)

			// return a configmap object containing aerospikeConfig
			confMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Labels:    ls,
					Namespace: aeroCluster.Namespace,
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
	logger.Info("Configmap already exist for statefulSet. Using existing configmap", log.Ctx{"name": utils.NamespacedName(confMap.Namespace, confMap.Name)})
	return nil
}

func (r *ReconcileAerospikeCluster) updateConfigMap(aeroCluster *aerospikev1alpha1.AerospikeCluster, name string) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	logger.Info("Updating ConfigMap", log.Ctx{"ConfigMap.Namespace": aeroCluster.Namespace, "ConfigMap.Name": name})

	confMap := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: aeroCluster.Namespace}, confMap)
	if err != nil {
		return err
	}

	// build the aerospike config file based on the current spec
	configMapData, err := configmap.CreateConfigMapData(aeroCluster)
	if err != nil {
		return fmt.Errorf("Failed to build dotConfig from map: %v", err)
	}
	confMap.Data = configMapData

	if err := r.client.Update(context.TODO(), confMap, updateOption); err != nil {
		return fmt.Errorf("Failed to update confMap for StatefulSet: %v", err)
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
			Port: utils.ServiceTlsPort,
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

func (r *ReconcileAerospikeCluster) waitForStatefulSetToBeReady(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) error {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	const podStatusMaxRetry = 18
	const podStatusRetryInterval = time.Second * 10

	logger.Info("Waiting for statefulset to be ready", log.Ctx{"WaitTimePerPod": podStatusRetryInterval * time.Duration(podStatusMaxRetry)})

	var podIndex int32
	for podIndex = 0; podIndex < aeroCluster.Spec.Size; podIndex++ {
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
			if err := utils.PodFailedStatus(pod); err != nil {
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
	logger.Info("Statefulset is ready")

	return nil
}

func (r *ReconcileAerospikeCluster) isStatefulSetReady(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) (bool, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// Check if statefulset exist or not
	newSt := &appsv1.StatefulSet{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: st.Name, Namespace: st.Namespace}, newSt)
	if err != nil {
		return false, err
	}

	// List the pods for this aeroCluster's statefulset
	podList, err := r.getPodList(aeroCluster)
	if err != nil {
		return false, err
	}

	for _, p := range podList.Items {
		logger.Debug("Check pod running and ready", log.Ctx{"pod": p.Name})
		if err := utils.PodFailedStatus(&p); err != nil {
			return false, fmt.Errorf("Pod %s failed: %v", p.Name, err)
		}
		if !utils.IsPodRunningAndReady(&p) {
			return false, nil
		}
	}

	logger.Debug("StatefulSet status", log.Ctx{"spec.replica": *st.Spec.Replicas, "status.replica": newSt.Status.Replicas})

	if *st.Spec.Replicas != newSt.Status.Replicas {
		return false, nil
	}
	return true, nil
}

func (r *ReconcileAerospikeCluster) getStatefulSet(aeroCluster *aerospikev1alpha1.AerospikeCluster) (*appsv1.StatefulSet, error) {
	found := &appsv1.StatefulSet{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, found)
	if err != nil {
		return nil, err
	}
	return found, nil
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

func (r *ReconcileAerospikeCluster) getAuthInfoFromStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster) (string, string, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	if aeroCluster.Status.AerospikeConfig == nil {
		logger.Debug("Its new cluster, no status available yet. Check security configuration in spec")
		// Here status is not updated yet, so its cluster created for first time
		// Get if security is enabled or not from spec,
		// If enabled then return default user, pass. If not then return empty user, pass
		enabled, err := utils.IsSecurityEnabled(aeroCluster.Spec.AerospikeConfig)
		if err != nil {
			return "", "", fmt.Errorf("Failed to get cluster security status: %v", err)
		}
		if !enabled {
			logger.Debug("Security not enabled, Please secure your cluster")
			return "", "", nil
		}
		logger.Debug("Security is enabled, using default username and password for now")
		return defaultUser, defaultPass, nil
	}

	logger.Debug("Getting current username, password for aerospike cluster from status")
	// Check with aeroCluster.Status.AerospikeConfig, not by config of spec as current config should be used to connect
	// Return empty user, pass if security not enabled
	enabled, err := utils.IsSecurityEnabled(aeroCluster.Status.AerospikeConfig)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get cluster security status: %v", err)
	}
	if !enabled {
		return "", "", nil
	}
	// Default user, pass when security is enabled
	user := defaultUser
	pass := defaultPass

	if aeroCluster.Status.AerospikeAuthSecret.SecretName == "" {
		logger.Debug("Security enabled but password not changed for default user yet. Using default username, password. Please update auth info by providing AerospikeAuthSecret")
		return user, pass, nil
	}

	secret := &corev1.Secret{}
	// Assuming secret is in same namespace
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Status.AerospikeAuthSecret.SecretName, Namespace: aeroCluster.Namespace}, secret)
	if err != nil {
		return user, pass, fmt.Errorf("Failed to get AerospikeAuthSecret. secretName %s: %v", aeroCluster.Status.AerospikeAuthSecret.SecretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return user, pass, fmt.Errorf("Failed to get password in AerospikeAuthSecret. Please check your secret. secretName %s", aeroCluster.Status.AerospikeAuthSecret.SecretName)
	}
	return user, string(passbyte), nil
}

func (r *ReconcileAerospikeCluster) getAuthInfoFromSpec(aeroCluster *aerospikev1alpha1.AerospikeCluster) (string, string, error) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})
	// TODO: remove it
	logger.Debug("Getting username, password for aerospike cluster from spec")
	// Return empty user, pass if security not enabled
	enabled, err := utils.IsSecurityEnabled(aeroCluster.Spec.AerospikeConfig)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get cluster security status: %v", err)
	}
	if !enabled {
		return "", "", nil
	}

	// Default user, pass when security is enabled
	user := defaultUser
	pass := defaultPass
	if aeroCluster.Spec.AerospikeAuthSecret.SecretName == "" {
		logger.Debug("Security enabled but password not changed for default user yet. Using default username, password. Please update auth info by providing AerospikeAuthSecret")
		return user, pass, nil
	}

	secret := &corev1.Secret{}
	// Assuming secret is in same namespace
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: aeroCluster.Spec.AerospikeAuthSecret.SecretName, Namespace: aeroCluster.Namespace}, secret)
	if err != nil {
		return user, pass, fmt.Errorf("Failed to get AerospikeAuthSecret. secretName %s: %v", aeroCluster.Spec.AerospikeAuthSecret.SecretName, err)
	}

	passbyte, ok := secret.Data["password"]
	if !ok {
		return user, pass, fmt.Errorf("Failed to get password in AerospikeAuthSecret. Please check your secret. secretName %s", aeroCluster.Spec.AerospikeAuthSecret.SecretName)
	}
	return user, string(passbyte), nil
}

func (r *ReconcileAerospikeCluster) getClientPolicyFromStatus(aeroCluster *aerospikev1alpha1.AerospikeCluster) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := r.getClientPolicyWithoutAuthInfo(aeroCluster)

	user, pass, err := r.getAuthInfoFromStatus(aeroCluster)
	if err != nil {
		logger.Error("Failed to get cluster auth info", log.Ctx{"err": err})
	}

	policy.User = user
	policy.Password = pass
	return policy
}

func (r *ReconcileAerospikeCluster) getClientPolicyFromSpec(aeroCluster *aerospikev1alpha1.AerospikeCluster) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := r.getClientPolicyWithoutAuthInfo(aeroCluster)

	user, pass, err := r.getAuthInfoFromSpec(aeroCluster)
	if err != nil {
		logger.Error("Failed to get cluster auth info", log.Ctx{"err": err})
	}

	policy.User = user
	policy.Password = pass
	return policy
}

func (r *ReconcileAerospikeCluster) getClientPolicyWithoutAuthInfo(aeroCluster *aerospikev1alpha1.AerospikeCluster) *as.ClientPolicy {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	policy := as.NewClientPolicy()

	// cluster name
	policy.ClusterName = aeroCluster.Name

	// tls config
	if tlsName := getServiceTLSName(aeroCluster); tlsName == "" {
		return policy
	}

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

	return policy
}

// // Called only when new cluster is created
// func updateStatefulSetStorage(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
// 	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

// 	// TODO: Add validation. device, file, both should not exist in same storage class
// 	var pvcList []corev1.PersistentVolumeClaim
// 	for _, storage := range aeroCluster.Spec.BlockStorage {
// 		if storage.StorageClass != "" {
// 			for _, device := range storage.Devices {
// 				logger.Info("Add PVC for block device", log.Ctx{"device": device})
// 				volumeMode := corev1.PersistentVolumeBlock

// 				pvc := corev1.PersistentVolumeClaim{
// 					ObjectMeta: metav1.ObjectMeta{
// 						Name:      getPVCName(device.Name),
// 						Namespace: aeroCluster.Namespace,
// 					},
// 					Spec: corev1.PersistentVolumeClaimSpec{
// 						VolumeMode:  &volumeMode,
// 						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
// 						Resources: corev1.ResourceRequirements{
// 							Requests: corev1.ResourceList{
// 								corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", device.SizeInGB)),
// 							},
// 						},
// 						StorageClassName: &storage.StorageClass,
// 					},
// 				}
// 				pvcList = append(pvcList, pvc)
// 			}
// 			st.Spec.VolumeClaimTemplates = pvcList
// 		}
// 	}

// 	const filesystemPVCName = "filesystem-datadir"
// 	if aeroCluster.Spec.FileStorage.StorageClass != "" {
// 		sz := computeFilesizeRequest(aeroCluster)
// 		// sz may be zero for InMemory only namespace
// 		if !sz.IsZero() {
// 			logger.Info("Add PVC for filesystem size")
// 			pvc := corev1.PersistentVolumeClaim{
// 				ObjectMeta: metav1.ObjectMeta{
// 					Name:      filesystemPVCName,
// 					Namespace: aeroCluster.Namespace,
// 				},
// 				Spec: corev1.PersistentVolumeClaimSpec{
// 					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
// 					Resources: corev1.ResourceRequirements{
// 						Requests: corev1.ResourceList{
// 							corev1.ResourceStorage: sz,
// 						},
// 					},
// 					StorageClassName: &aeroCluster.Spec.FileStorage.StorageClass,
// 				},
// 			}
// 			st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)
// 		}
// 	}

// 	var volumeAdded bool
// 	nsList := aeroCluster.Spec.AerospikeConfig[utils.ConfKeyNamespace].([]interface{})
// 	for _, nsInterface := range nsList {
// 		ns, _ := nsInterface.(map[string]interface{})
// 		if _, ok := ns[utils.ConfKeyStorageEngine].(string); ok {
// 			// InMemory only namespace
// 			continue
// 		}
// 		storage := ns[utils.ConfKeyStorageEngine].(map[string]interface{})
// 		if filesInterface, ok := storage[utils.ConfKeyFile]; ok {
// 			files := filesInterface.([]interface{})
// 			if len(files) == 0 {
// 				// files Cannot be empty
// 				continue
// 			}
// 			if !volumeAdded {
// 				// TODO: Only /opt/aerospike/data/ dir path works. There are other files needed by aerospike so other mount point wont work
// 				filePath := files[0].(string)
// 				dirPath := filepath.Dir(filePath)

// 				logger.Info("Add VolumeMounts for file", log.Ctx{"file": filePath})
// 				volume := corev1.VolumeMount{
// 					Name:      filesystemPVCName,
// 					MountPath: dirPath,
// 				}
// 				st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, volume)
// 				volumeAdded = true
// 			}
// 		} else if devicesInterface, ok := storage[utils.ConfKeyDevice]; ok {
// 			var devices []string
// 			for _, dev := range devicesInterface.([]interface{}) {
// 				// Parsing for shadow device
// 				devices = append(devices, strings.Fields(dev.(string))...)
// 			}
// 			// mount device
// 			for _, device := range devices {
// 				logger.Info("Add VolumeDevices for device", log.Ctx{"device": device})

// 				volume := corev1.VolumeDevice{
// 					Name:       getPVCName(device),
// 					DevicePath: device,
// 				}
// 				st.Spec.Template.Spec.Containers[0].VolumeDevices = append(st.Spec.Template.Spec.Containers[0].VolumeDevices, volume)
// 			}
// 		}
// 	}
// }

// Called only when new cluster is created
func updateStatefulSetStorage(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// TODO: Add validation. device, file, both should not exist in same storage class
	for _, storage := range aeroCluster.Spec.BlockStorage {
		if storage.StorageClass != "" {
			for _, device := range storage.VolumeDevices {

				logger.Info("Add PVC for block device", log.Ctx{"device": device})
				volumeMode := corev1.PersistentVolumeBlock
				pvc := corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      getPVCName(device.DevicePath),
						Namespace: aeroCluster.Namespace,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						VolumeMode:  &volumeMode,
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", device.SizeInGB)),
							},
						},
						StorageClassName: &storage.StorageClass,
					},
				}
				st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)

				logger.Info("Add VolumeDevice for device", log.Ctx{"device": device})
				volume := corev1.VolumeDevice{
					Name:       getPVCName(device.DevicePath),
					DevicePath: device.DevicePath,
				}
				st.Spec.Template.Spec.Containers[0].VolumeDevices = append(st.Spec.Template.Spec.Containers[0].VolumeDevices, volume)
			}
		}
	}

	// TODO: Add validation. device, file, both should not exist in same storage class
	for _, storage := range aeroCluster.Spec.FileStorage {
		if storage.StorageClass != "" {
			for _, volumeMount := range storage.VolumeMounts {
				logger.Info("Add PVC for volumeMount", log.Ctx{"volumeMount": volumeMount})
				pvc := corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      getPVCName(volumeMount.MountPath),
						Namespace: aeroCluster.Namespace,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(fmt.Sprintf("%dGi", volumeMount.SizeInGB)),
							},
						},
						StorageClassName: &storage.StorageClass,
					},
				}
				st.Spec.VolumeClaimTemplates = append(st.Spec.VolumeClaimTemplates, pvc)

				logger.Info("Add VolumeMounts for file", log.Ctx{"file": volumeMount})
				volume := corev1.VolumeMount{
					Name:      getPVCName(volumeMount.MountPath),
					MountPath: volumeMount.MountPath,
				}
				st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, volume)
			}
		}
	}

	// // var volumeAdded bool
	// nsList := aeroCluster.Spec.AerospikeConfig[utils.ConfKeyNamespace].([]interface{})
	// for _, nsInterface := range nsList {
	// 	ns, _ := nsInterface.(map[string]interface{})
	// 	if _, ok := ns[utils.ConfKeyStorageEngine].(string); ok {
	// 		// InMemory only namespace
	// 		continue
	// 	}
	// 	storage := ns[utils.ConfKeyStorageEngine].(map[string]interface{})
	// 	if filesInterface, ok := storage[utils.ConfKeyFile]; ok {
	// 		var files []string
	// 		for _, file := range filesInterface.([]interface{}) {
	// 			files = append(files, filepath.Dir(file.(string)))
	// 		}
	// 		// mount file
	// 		for _, filePath := range files {
	// 			logger.Info("Add VolumeMounts for file", log.Ctx{"file": filePath})

	// 			volume := corev1.VolumeMount{
	// 				Name:      getPVCName(filePath),
	// 				MountPath: filePath,
	// 			}
	// 			st.Spec.Template.Spec.Containers[0].VolumeMounts = append(st.Spec.Template.Spec.Containers[0].VolumeMounts, volume)
	// 		}
	// 	} else if devicesInterface, ok := storage[utils.ConfKeyDevice]; ok {
	// 		var devices []string
	// 		for _, dev := range devicesInterface.([]interface{}) {
	// 			// Parsing for shadow device
	// 			devices = append(devices, strings.Fields(dev.(string))...)
	// 		}
	// 		// mount device
	// 		for _, device := range devices {
	// 			logger.Info("Add VolumeDevices for device", log.Ctx{"device": device})

	// 			volume := corev1.VolumeDevice{
	// 				Name:       getPVCName(device),
	// 				DevicePath: device,
	// 			}
	// 			st.Spec.Template.Spec.Containers[0].VolumeDevices = append(st.Spec.Template.Spec.Containers[0].VolumeDevices, volume)
	// 		}
	// 	}
	// }
}

func updateStatefulSetAffinity(aeroCluster *aerospikev1alpha1.AerospikeCluster, st *appsv1.StatefulSet, labels map[string]string) {
	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

	// only enable in production, so it can be used in 1 node clusters while debugging (minikube)
	if !aeroCluster.Spec.MultiPodPerHost {
		logger.Info("Adding pod affinity rules for statefulset pod")
		st.Spec.Template.Spec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					{
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: labels,
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		}
	}
}

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
				Name:          utils.ServiceTlsPortName,
				ContainerPort: utils.ServiceTlsPort,
			},
			{
				Name:          utils.HeartbeatPortName,
				ContainerPort: utils.HeartbeatPort,
			},
			{
				Name:          utils.HeartbeatTlsPortName,
				ContainerPort: utils.HeartbeatTlsPort,
			},
			{
				Name:          utils.FabricPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.FabricTlsPortName,
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
				Name:          utils.ServiceTlsPortName,
				ContainerPort: utils.ServiceTlsPort,
				HostPort:      utils.ServiceTlsPort,
			},
			{
				Name:          utils.HeartbeatPortName,
				ContainerPort: utils.HeartbeatPort,
			},
			{
				Name:          utils.HeartbeatTlsPortName,
				ContainerPort: utils.HeartbeatTlsPort,
			},
			{
				Name:          utils.FabricPortName,
				ContainerPort: utils.FabricPort,
			},
			{
				Name:          utils.FabricTlsPortName,
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

// // computeCPURequest computes the amount of cpu to be requested for the aerospike-server container and returns the
// // corresponding resource.Quantity. It currently returns aerospikeServerContainerDefaultCPURequest parsed as a quantity
// // or requested CPU provided by user if it exists as a quantity.
// func computeCPURequest(aeroCluster *aerospikev1alpha1.AerospikeCluster) resource.Quantity {
// 	if aeroCluster.Spec.Resources != nil && aeroCluster.Spec.Resources.Requests.Cpu() != nil {
// 		return *aeroCluster.Spec.Resources.Requests.Cpu()
// 	}
// 	return resource.MustParse(strconv.Itoa(aerospikeServerContainerDefaultCPURequest))
// }

// // computeMemoryRequest computes the amount of memory to be requested for the aerospike-server container based on the
// // value of the memorySize field of each namespace. Compares computed amount of memory with user provided memory request and
// // returns the biggest amount as a resource.Quantity.
// func computeMemoryRequest(aeroCluster *aerospikev1alpha1.AerospikeCluster) resource.Quantity {
// 	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

// 	var sum int64
// 	nsConfList := aeroCluster.Spec.AerospikeConfig[utils.ConfKeyNamespace].([]interface{})
// 	for _, nsConfInterface := range nsConfList {
// 		nsConf := nsConfInterface.(map[string]interface{})
// 		if nsConf[utils.ConfKeyMemorySize] == 0 {
// 			// ns.MemorySize is nil, which means we need to set a value that
// 			// matches the aerospike default for namespace.memory-size
// 			sum += (aerospikeServerContainerDefaultMemoryRequestGi * 1024 * 1024 * 1024)
// 			continue
// 		} else {
// 			sum += nsConf[utils.ConfKeyMemorySize].(int64)
// 		}
// 	}

// 	// TODO: how to calculate. Do we need some extra buffer momory for other apps in container. Adding 1GB extra memory
// 	sum += (1 * 1024 * 1024 * 1024)

// 	memoryInGB := int64(math.Ceil(float64(sum) / (1024 * 1024 * 1024)))
// 	computedMemory := resource.MustParse(fmt.Sprintf("%dGi", memoryInGB))
// 	// user may want to setup manual memory requests bigger than computed ones
// 	if aeroCluster.Spec.Resources != nil && aeroCluster.Spec.Resources.Requests.Memory() != nil && aeroCluster.Spec.Resources.Requests.Memory().Cmp(computedMemory) > 0 {
// 		computedMemory = *aeroCluster.Spec.Resources.Requests.Memory()
// 	}
// 	logger.Debug("Requested total memory for all namespaces", log.Ctx{"inGB": computedMemory.String()})
// 	return computedMemory
// }

// func computeFilesizeRequest(aeroCluster *aerospikev1alpha1.AerospikeCluster) resource.Quantity {
// 	logger := pkglog.New(log.Ctx{"AerospikeCluster": utils.ClusterNamespacedName(aeroCluster)})

// 	var sum int64

// 	nsConfList := aeroCluster.Spec.AerospikeConfig[utils.ConfKeyNamespace].([]interface{})
// 	for _, nsConfInterface := range nsConfList {
// 		nsConf, _ := nsConfInterface.(map[string]interface{})
// 		if _, ok := nsConf[utils.ConfKeyStorageEngine].(string); ok {
// 			// InMemory only, no allocation
// 			continue
// 		}
// 		storage := nsConf[utils.ConfKeyStorageEngine].(map[string]interface{})
// 		if filesize, ok := storage[utils.ConfKeyFilesize]; ok {
// 			sum += filesize.(int64)
// 		} else {
// 			sum += (aerospikeServerNamespaceDefaultFilesizeMemoryRequestGi * 1024 * 1024 * 1024)
// 		}
// 	}
// 	// Adding 1GB extra
// 	sum += (1 * 1024 * 1024 * 1024)

// 	sizeInGB := int64(math.Ceil(float64(sum) / (1024 * 1024 * 1024)))
// 	computedSz := resource.MustParse(fmt.Sprintf("%dGi", sizeInGB))

// 	logger.Debug("Requested total fileSystem size for all namespaces", log.Ctx{"inBytes": sum, "inGB": sizeInGB})
// 	return computedSz
// }

// // computeResourceLimits computes the limit amounts of cpu and memory to be used by the aerospike-server container
// // and returns the corresponding ResourceList.
// func computeResourceLimits(aeroCluster *aerospikev1alpha1.AerospikeCluster) corev1.ResourceList {
// 	// compute configured resource limits, if any
// 	if aeroCluster.Spec.Resources != nil && aeroCluster.Spec.Resources.Limits != nil {
// 		// setup limits for memory and cpu if user provides request limit values for both
// 		if aeroCluster.Spec.Resources.Limits.Cpu() != nil && !aeroCluster.Spec.Resources.Limits.Cpu().IsZero() &&
// 			aeroCluster.Spec.Resources.Limits.Memory() != nil && !aeroCluster.Spec.Resources.Limits.Memory().IsZero() {
// 			return corev1.ResourceList{
// 				corev1.ResourceCPU:    *aeroCluster.Spec.Resources.Limits.Cpu(),
// 				corev1.ResourceMemory: *aeroCluster.Spec.Resources.Limits.Memory(),
// 			}
// 		}
// 		// setup limits for cpu if user provides request limit values for cpu only
// 		if aeroCluster.Spec.Resources.Limits.Cpu() != nil && !aeroCluster.Spec.Resources.Limits.Cpu().IsZero() {
// 			return corev1.ResourceList{
// 				corev1.ResourceCPU: *aeroCluster.Spec.Resources.Limits.Cpu(),
// 			}
// 		}
// 		// setup limits for memory if user provides request limit values for memory only
// 		if aeroCluster.Spec.Resources.Limits.Memory() != nil && !aeroCluster.Spec.Resources.Limits.Memory().IsZero() {
// 			return corev1.ResourceList{
// 				corev1.ResourceMemory: *aeroCluster.Spec.Resources.Limits.Memory(),
// 			}
// 		}
// 	}
// 	return corev1.ResourceList{}
// }

func isClusterResourceUpdated(aeroCluster *aerospikev1alpha1.AerospikeCluster) bool {

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

func getPVCName(path string) string {
	path = strings.Trim(path, "/")
	return strings.Replace(path, "/", "-", -1)
}

func getHeadLessSvcName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	return aeroCluster.Name
}

// const (
// 	// nodeIDPrefix is used as the prefix for node IDs so that they don't begin
// 	// with a leading zero. "a" stands for Aerospike.
// 	nodeIDPrefix = "a"
// )

// // computeNodeId computes the value to be used as the id of the aerospike node
// // that corresponds to podName.
// func computeNodeID(podName string) (string, error) {
// 	// calculate the md5 hash of podName
// 	podHash := md5.New()
// 	_, err := io.WriteString(podHash, podName)
// 	if err != nil {
// 		return "", err
// 	}
// 	// an aerospike node's id cannot exceed 16 characters, so we use the first
// 	// 15 characters of the hash and a prefix to prevent the generated id from
// 	// having leading zeros (which aerospike strips, causing trouble later on)
// 	return fmt.Sprintf("%s%s", nodeIDPrefix, hex.EncodeToString(podHash.Sum(nil))[0:15]), nil
// }
