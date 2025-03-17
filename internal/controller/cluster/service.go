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

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

func getSTSHeadLessSvcName(aeroCluster *asdbv1.AerospikeCluster) string {
	return aeroCluster.Name
}

func (r *SingleClusterReconciler) createOrUpdateSTSHeadlessSvc() error {
	headlessSvc := r.aeroCluster.Spec.HeadlessService
	serviceName := getSTSHeadLessSvcName(r.aeroCluster)
	service := &corev1.Service{}

	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: r.aeroCluster.Namespace,
		}, service,
	)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating headless service for statefulSet")

		annotations := map[string]string{
			// deprecation in 1.10, supported until at least 1.13,  breaks peer-finder/kube-dns if not used
			"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
		}
		if headlessSvc.Metadata.Annotations != nil {
			maps.Copy(headlessSvc.Metadata.Annotations, annotations)
		}

		if headlessSvc.Metadata.Labels != nil {
			maps.Copy(headlessSvc.Metadata.Labels, utils.LabelsForAerospikeCluster(r.aeroCluster.Name))
		}

		service = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        serviceName,
				Namespace:   r.aeroCluster.Namespace,
				Annotations: headlessSvc.Metadata.Annotations,
				Labels:      headlessSvc.Metadata.Labels,
			},
			Spec: corev1.ServiceSpec{
				// deprecates service.alpha.kubernetes.io/tolerate-unready-endpoints as of 1.
				// 10? see: kubernetes/kubernetes#49239 Fixed in 1.11 as of #63742
				PublishNotReadyAddresses: true,
				ClusterIP:                "None",
				Selector:                 headlessSvc.Metadata.Labels,
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

		if err = r.Client.Create(
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

	if err := r.updateServiceMetadata(service, headlessSvc.Metadata); err != nil {
		return err
	}

	return r.updateServicePorts(service)
}

func (r *SingleClusterReconciler) createOrUpdateSTSLoadBalancerSvc() error {
	loadBalancer := r.aeroCluster.Spec.SeedsFinderServices.LoadBalancer
	if loadBalancer == nil {
		r.Log.Info("LoadBalancer is not configured. Skipping...")
		return nil
	}

	serviceName := r.aeroCluster.Name + "-lb"
	service := &corev1.Service{}
	servicePort := r.getLBServicePort(loadBalancer)

	if err := r.Client.Get(
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

			if nErr := r.Client.Create(
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

	if !reflect.DeepEqual(service.ObjectMeta.Annotations, loadBalancer.Annotations) {
		service.ObjectMeta.Annotations = loadBalancer.Annotations
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
		if err := r.Client.Update(
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

func (r *SingleClusterReconciler) createOrUpdatePodService(pName, pNamespace string) error {
	podService := r.aeroCluster.Spec.PodService
	service := &corev1.Service{}

	err := r.Client.Get(
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

		if err := r.Client.Create(
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

	if err := r.updateServiceMetadata(service, podService.Metadata); err != nil {
		return err
	}

	return r.updateServicePorts(service)
}

func (r *SingleClusterReconciler) deletePodService(pName, pNamespace string) error {
	service := &corev1.Service{}

	serviceName := types.NamespacedName{Name: pName, Namespace: pNamespace}
	if err := r.Client.Get(context.TODO(), serviceName, service); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info(
				"Pod service not found for deletion. Skipping...",
				"service", serviceName,
			)

			return nil
		}

		return fmt.Errorf("failed to get service for pod %s: %v", pName, err)
	}

	if err := r.Client.Delete(context.TODO(), service); err != nil {
		return fmt.Errorf("failed to delete service for pod %s: %v", pName, err)
	}

	return nil
}

func (r *SingleClusterReconciler) updateServicePorts(service *corev1.Service) error {
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
		r.Log.Info("Service update not required, skipping",
			"name", utils.NamespacedName(service.Namespace, service.Name))

		return nil
	}

	service.Spec.Ports = servicePorts
	if err := r.Client.Update(
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

func (r *SingleClusterReconciler) getServicePorts() []corev1.ServicePort {
	servicePorts := make([]corev1.ServicePort, 0)

	if svcPort := asdbv1.GetServicePort(
		r.aeroCluster.Spec.
			AerospikeConfig,
	); svcPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.ServicePortName,
				Port: int32(*svcPort),
			},
		)
	}

	if _, tlsPort := asdbv1.GetServiceTLSNameAndPort(
		r.aeroCluster.Spec.AerospikeConfig,
	); tlsPort != nil {
		servicePorts = append(
			servicePorts, corev1.ServicePort{
				Name: asdbv1.ServiceTLSPortName,
				Port: int32(*tlsPort),
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
		targetPort = int32(*tlsPort)
	} else {
		targetPort = int32(*asdbv1.GetServicePort(r.aeroCluster.Spec.AerospikeConfig))
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
	podList, err := r.getRackPodList(rackState.Rack.ID)
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

	// If len of set is more than 2, it means network type different from "pod" and "customInterface" are present.
	// In that case, pod service is required
	return networkSet.Len() > 2
}

func (r *SingleClusterReconciler) createOrUpdatePodServiceIfNeeded(pods []string) error {
	if podServiceNeeded(r.aeroCluster.Spec.PodSpec.MultiPodPerHost, &r.aeroCluster.Spec.AerospikeNetworkPolicy) {
		// Create services for all pods if network policy is changed and rely on nodePort service
		for idx := range pods {
			if err := r.createOrUpdatePodService(
				pods[idx], r.aeroCluster.Namespace,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *SingleClusterReconciler) getServiceTLSNameAndPortIfConfigured() (tlsName string, port *int) {
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

func (r *SingleClusterReconciler) updateServiceMetadata(
	service *corev1.Service,
	metadata asdbv1.AerospikeObjectMeta,
) error {
	updateMetadata := false

	if !reflect.DeepEqual(service.ObjectMeta.Annotations, metadata.Annotations) {
		service.ObjectMeta.Annotations = metadata.Annotations
		updateMetadata = true
	}

	if !reflect.DeepEqual(service.ObjectMeta.Labels, metadata.Labels) {
		service.ObjectMeta.Labels = metadata.Labels
		updateMetadata = true
	}

	if updateMetadata {
		if err := r.Client.Update(
			context.TODO(), service, common.UpdateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to update metadata for service %s: %v", service.Name, err,
			)
		}

		r.Log.Info("Updated metadata for service",
			"name", utils.NamespacedName(service.Namespace, service.Name))
	}

	return nil
}
