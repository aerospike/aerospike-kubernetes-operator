package cluster

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

func getSTSHeadLessSvcName(aeroCluster *asdbv1.AerospikeCluster) string {
	return aeroCluster.Name
}

func (r *SingleClusterReconciler) createOrUpdateSTSHeadlessSvc() error {
	specHeadlessSvc := &r.aeroCluster.Spec.HeadlessService
	serviceName := getSTSHeadLessSvcName(r.aeroCluster)
	service := &corev1.Service{}

	defaultMetadata := asdbv1.AerospikeObjectMeta{
		Annotations: map[string]string{
			// deprecation in 1.10, supported until at least 1.13, breaks peer-finder/kube-dns if not used
			"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
		},
		Labels: utils.LabelsForAerospikeCluster(r.aeroCluster.Name),
	}

	err := r.Get(
		context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: r.aeroCluster.Namespace,
		}, service,
	)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating headless service for statefulSet")

		if specHeadlessSvc.Metadata.Annotations != nil {
			maps.Copy(defaultMetadata.Annotations, specHeadlessSvc.Metadata.Annotations)
		}

		if specHeadlessSvc.Metadata.Labels != nil {
			maps.Copy(defaultMetadata.Labels, specHeadlessSvc.Metadata.Labels)
		}

		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceName,
				Namespace:   r.aeroCluster.Namespace,
				Annotations: defaultMetadata.Annotations,
				Labels:      defaultMetadata.Labels,
			},
			Spec: corev1.ServiceSpec{
				// deprecates service.alpha.kubernetes.io/tolerate-unready-endpoints as of 1.
				// 10? see: kubernetes/kubernetes#49239 Fixed in 1.11 as of #63742
				PublishNotReadyAddresses: true,
				ClusterIP:                corev1.ClusterIPNone,
				Selector:                 utils.LabelsForAerospikeCluster(r.aeroCluster.Name),
			},
		}

		service.Spec.Ports = r.getServicePorts()

		// Set AerospikeCluster instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroCluster, service, r.Scheme,
		)
		if err != nil {
			return err
		}

		if err = r.Create(
			context.TODO(), service, common.CreateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to create headless service for statefulset: %v",
				err,
			)
		}

		r.Log.Info("Created new headless service",
			"name", utils.NamespacedName(service.Namespace, service.Name))

		return nil
	}

	r.Log.Info("Headless service already exist, checking for update",
		"name", utils.NamespacedName(service.Namespace, service.Name))

	return r.updateService(
		service,
		r.aeroCluster.Status.HeadlessService.Metadata,
		specHeadlessSvc.Metadata,
		defaultMetadata,
	)
}

func (r *SingleClusterReconciler) reconcileSTSLoadBalancerSvc() error {
	loadBalancer := r.aeroCluster.Spec.SeedsFinderServices.LoadBalancer
	serviceName := r.aeroCluster.Name + "-lb"

	if loadBalancer == nil {
		return r.deleteLBServiceIfPresent(serviceName, r.aeroCluster.Namespace)
	}

	service := &corev1.Service{}
	servicePort := r.getLBServicePort(loadBalancer)

	if err := r.Get(
		context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: r.aeroCluster.Namespace,
		}, service,
	); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating LoadBalancer service for cluster")
			ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)

			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        serviceName,
					Namespace:   r.aeroCluster.Namespace,
					Annotations: loadBalancer.Annotations,
					Labels:      ls,
				},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeLoadBalancer,
					Selector: ls,
					Ports: []corev1.ServicePort{
						servicePort,
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
			if nErr := controllerutil.SetControllerReference(
				r.aeroCluster, service, r.Scheme,
			); nErr != nil {
				return nErr
			}

			if nErr := r.Create(
				context.TODO(), service, common.CreateOption,
			); nErr != nil {
				return nErr
			}

			r.Log.Info("Created new LoadBalancer service",
				"name", service.GetName())

			return nil
		}

		return err
	}

	r.Log.Info("LoadBalancer service already exist for cluster, checking for update",
		"name", utils.NamespacedName(service.Namespace, service.Name))

	return r.updateLBService(service, &servicePort, loadBalancer)
}

func (r *SingleClusterReconciler) deleteLBServiceIfPresent(svcName, svcNamespace string) error {
	service := &corev1.Service{}
	service.Name = svcName
	service.Namespace = svcNamespace

	// Get the LB service
	if err := r.Get(
		context.TODO(), types.NamespacedName{
			Name: svcName, Namespace: svcNamespace,
		}, service,
	); err != nil {
		if errors.IsNotFound(err) {
			// LB service is already deleted
			return nil
		}

		return err
	}

	if !utils.IsOwnedBy(service, r.aeroCluster) {
		r.Log.Info(
			"LoadBalancer service is not created/owned by operator. Skipping delete",
			"name", utils.NamespacedName(service.Namespace, service.Name),
		)

		return nil
	}

	// Delete the LB service
	return r.Delete(context.TODO(), service)
}

func (r *SingleClusterReconciler) updateLBService(service *corev1.Service, servicePort *corev1.ServicePort,
	loadBalancer *asdbv1.LoadBalancerSpec) error {
	updateLBService := false

	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != servicePort.Port ||
		service.Spec.Ports[0].TargetPort != servicePort.TargetPort ||
		service.Spec.Ports[0].Name != servicePort.Name {
		updateLBService = true
		service.Spec.Ports = []corev1.ServicePort{
			*servicePort,
		}
	}

	if !reflect.DeepEqual(service.Annotations, loadBalancer.Annotations) {
		service.Annotations = loadBalancer.Annotations
		updateLBService = true
	}

	if !reflect.DeepEqual(service.Spec.LoadBalancerSourceRanges, loadBalancer.LoadBalancerSourceRanges) {
		service.Spec.LoadBalancerSourceRanges = loadBalancer.LoadBalancerSourceRanges
		updateLBService = true
	}

	if !reflect.DeepEqual(service.Spec.ExternalTrafficPolicy, loadBalancer.ExternalTrafficPolicy) {
		service.Spec.ExternalTrafficPolicy = loadBalancer.ExternalTrafficPolicy
		updateLBService = true
	}

	if updateLBService {
		if err := r.Update(
			context.TODO(), service, common.UpdateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to update service %s: %v", service.Name, err,
			)
		}
	} else {
		r.Log.Info("LoadBalancer service update not required, skipping",
			"name", utils.NamespacedName(service.Namespace, service.Name))

		return nil
	}

	r.Log.Info("LoadBalancer service updated",
		"name", utils.NamespacedName(service.Namespace, service.Name))

	return nil
}

func (r *SingleClusterReconciler) reconcilePodService(rackState *RackState) error {
	// PodService is only created if MultiPodPerHost is enabled
	if !asdbv1.GetBool(r.aeroCluster.Spec.PodSpec.MultiPodPerHost) {
		return nil
	}

	// Safe check to delete all dangling pod services which are no longer required
	if !podServiceNeeded(r.aeroCluster.Spec.PodSpec.MultiPodPerHost, &r.aeroCluster.Spec.AerospikeNetworkPolicy) {
		return r.cleanupDanglingPodServices(rackState)
	}

	return r.createOrUpdatePodServiceIfNeeded(r.getRackPodNames(rackState))
}

func (r *SingleClusterReconciler) createOrUpdatePodService(pName, pNamespace string) error {
	podService := &r.aeroCluster.Spec.PodService
	service := &corev1.Service{}

	err := r.Get(
		context.TODO(), types.NamespacedName{
			Name: pName, Namespace: pNamespace,
		}, service,
	)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating new service for pod")
		// NodePort will be allocated automatically
		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        pName,
				Namespace:   pNamespace,
				Annotations: podService.Metadata.Annotations,
				Labels:      podService.Metadata.Labels,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"statefulset.kubernetes.io/pod-name": pName,
				},
				ExternalTrafficPolicy: "Local",
			},
		}

		service.Spec.Ports = r.getServicePorts()

		// Set AerospikeCluster instance as the owner and controller.
		// It is created before Pod, so Pod cannot be the owner
		err := controllerutil.SetControllerReference(
			r.aeroCluster, service, r.Scheme,
		)
		if err != nil {
			return err
		}

		if err := r.Create(
			context.TODO(), service, common.CreateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to create new service for pod %s: %v", pName, err,
			)
		}

		r.Log.Info("Created new service for pod",
			"name", utils.NamespacedName(service.Namespace, service.Name))

		return nil
	}

	r.Log.Info("Service already exist, checking for update",
		"name", utils.NamespacedName(service.Namespace, service.Name))

	return r.updateService(
		service,
		r.aeroCluster.Status.PodService.Metadata,
		podService.Metadata,
		asdbv1.AerospikeObjectMeta{},
	)
}

func (r *SingleClusterReconciler) deletePodService(pName, pNamespace string) error {
	service := &corev1.Service{}
	service.Name = pName
	service.Namespace = pNamespace
	serviceName := types.NamespacedName{Name: pName, Namespace: pNamespace}

	if err := r.Delete(context.TODO(), service); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info(
				"Pod service not found for deletion. Skipping...",
				"service", serviceName,
			)

			return nil
		}

		return fmt.Errorf("failed to delete service for pod %s: %v", pName, err)
	}

	return nil
}

func (r *SingleClusterReconciler) areServicePortsUpdated(service *corev1.Service) bool {
	servicePorts := r.getServicePorts()

	servicePortsMap := make(map[string]int32)
	for _, port := range servicePorts {
		servicePortsMap[port.Name] = port.Port
	}

	specPortsMap := make(map[string]int32)
	for _, port := range service.Spec.Ports {
		specPortsMap[port.Name] = port.Port
	}

	if reflect.DeepEqual(servicePortsMap, specPortsMap) {
		return false
	}

	service.Spec.Ports = servicePorts

	return true
}

func (r *SingleClusterReconciler) getServicePorts() []corev1.ServicePort {
	servicePorts := make([]corev1.ServicePort, 0)

	if svcPort := asdbv1.GetServicePort(
		r.aeroCluster.Spec.
			AerospikeConfig,
	); svcPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.ServicePortName,
				Port: *svcPort,
			},
		)
	}

	if _, tlsPort := asdbv1.GetServiceTLSNameAndPort(
		r.aeroCluster.Spec.AerospikeConfig,
	); tlsPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.ServiceTLSPortName,
				Port: *tlsPort,
			},
		)
	}

	if adminPort := asdbv1.GetAdminPort(
		r.aeroCluster.Spec.
			AerospikeConfig,
	); adminPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.AdminPortName,
				Port: *adminPort,
			},
		)
	}

	if _, adminTLSPort := asdbv1.GetAdminTLSNameAndPort(
		r.aeroCluster.Spec.AerospikeConfig,
	); adminTLSPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.AdminTLSPortName,
				Port: *adminTLSPort,
			},
		)
	}

	return servicePorts
}

func (r *SingleClusterReconciler) getLBServicePort(loadBalancer *asdbv1.LoadBalancerSpec) corev1.ServicePort {
	var targetPort int32
	if loadBalancer.TargetPort >= 1024 {
		// if target port is specified in CR.
		targetPort = loadBalancer.TargetPort
	} else if tlsName, tlsPort := asdbv1.GetServiceTLSNameAndPort(
		r.aeroCluster.Spec.AerospikeConfig,
	); tlsName != "" && tlsPort != nil {
		targetPort = *tlsPort
	} else {
		targetPort = *asdbv1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig)
	}

	var port int32
	if loadBalancer.Port >= 1024 {
		// If port is specified in CR.
		port = loadBalancer.Port
	} else {
		port = targetPort
	}

	return corev1.ServicePort{
		Port:       port,
		Name:       loadBalancer.PortName,
		TargetPort: intstr.FromInt(int(targetPort)),
	}
}

func (r *SingleClusterReconciler) cleanupDanglingPodServices(rackState *RackState) error {
	podList, err := r.getRackPodList(rackState.Rack.ID, rackState.Rack.Revision)
	if err != nil {
		return err
	}

	for idx := range podList.Items {
		if err := r.deletePodService(podList.Items[idx].Name, podList.Items[idx].Namespace); err != nil {
			return err
		}
	}

	return nil
}

func podServiceNeeded(multiPodPerHost *bool, networkPolicy *asdbv1.AerospikeNetworkPolicy) bool {
	if !asdbv1.GetBool(multiPodPerHost) || networkPolicy == nil {
		return false
	}

	networkSet := sets.NewString(
		string(asdbv1.AerospikeNetworkTypePod),
		string(asdbv1.AerospikeNetworkTypeCustomInterface),
		string(networkPolicy.AccessType),
		string(networkPolicy.TLSAccessType),
		string(networkPolicy.AlternateAccessType),
		string(networkPolicy.TLSAlternateAccessType),
	)

	// If len of set is more than 2, it means the network type different from "pod" and "customInterface" are present.
	// In that case, pod service is required
	return networkSet.Len() > 2
}

func (r *SingleClusterReconciler) createOrUpdatePodServiceIfNeeded(pods []string) error {
	if !podServiceNeeded(r.aeroCluster.Spec.PodSpec.MultiPodPerHost, &r.aeroCluster.Spec.AerospikeNetworkPolicy) {
		return nil
	}
	// Create services for all pods if the network policy is changed and rely on nodePort service
	for idx := range pods {
		if err := r.createOrUpdatePodService(
			pods[idx], r.aeroCluster.Namespace,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *SingleClusterReconciler) getServiceTLSNameAndPortIfConfigured() (tlsName string, port *int32) {
	tlsName, port = asdbv1.GetServiceTLSNameAndPort(r.aeroCluster.Spec.AerospikeConfig)
	if tlsName != "" && r.aeroCluster.Status.AerospikeConfig != nil {
		statusTLSName, _ := asdbv1.GetServiceTLSNameAndPort(r.aeroCluster.Status.AerospikeConfig)
		statusPort := asdbv1.GetServicePort(r.aeroCluster.Status.AerospikeConfig)

		if statusTLSName == "" && statusPort != nil {
			tlsName = ""
		}
	}

	return tlsName, port
}

func (r *SingleClusterReconciler) isServiceMetadataUpdated(
	service *corev1.Service,
	statusMetadata,
	specMetadata,
	defaultMetadata asdbv1.AerospikeObjectMeta,

) bool {
	var needsUpdate bool

	if !reflect.DeepEqual(statusMetadata.Annotations, specMetadata.Annotations) {
		annotations := make(map[string]string)

		maps.Copy(annotations, defaultMetadata.Annotations)
		maps.Copy(annotations, specMetadata.Annotations)
		service.Annotations = annotations
		needsUpdate = true
	}

	if !reflect.DeepEqual(statusMetadata.Labels, specMetadata.Labels) {
		labels := make(map[string]string)

		maps.Copy(labels, defaultMetadata.Labels)
		maps.Copy(labels, specMetadata.Labels)
		service.Labels = labels
		needsUpdate = true
	}

	return needsUpdate
}

func (r *SingleClusterReconciler) updateService(
	service *corev1.Service,
	statusMetadata,
	specMetadata,
	defaultMetadata asdbv1.AerospikeObjectMeta,
) error {
	var needsUpdate bool

	if r.isServiceMetadataUpdated(service, statusMetadata, specMetadata, defaultMetadata) {
		needsUpdate = true
	}

	if r.areServicePortsUpdated(service) {
		needsUpdate = true
	}

	if needsUpdate {
		if err := r.Update(
			context.TODO(), service, common.UpdateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to update service %s: %v", service.Name, err,
			)
		}

		r.Log.Info("Service updated",
			"name", utils.NamespacedName(service.Namespace, service.Name))

		return nil
	}

	r.Log.Info("Service update not required, skipping",
		"name", utils.NamespacedName(service.Namespace, service.Name))

	return nil
}
