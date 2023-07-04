package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

func getSTSHeadLessSvcName(aeroCluster *asdbv1.AerospikeCluster) string {
	return aeroCluster.Name
}

func (r *SingleClusterReconciler) createSTSHeadlessSvc() error {
	r.Log.Info("Create headless service for statefulSet")

	ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)

	serviceName := getSTSHeadLessSvcName(r.aeroCluster)
	service := &corev1.Service{}

	err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: r.aeroCluster.Namespace,
		}, service,
	)
	if err != nil {
		if errors.IsNotFound(err) {
			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					// Headless service has the same name as AerospikeCluster
					Name:      serviceName,
					Namespace: r.aeroCluster.Namespace,
					// deprecation in 1.10, supported until at least 1.13,  breaks peer-finder/kube-dns if not used
					Annotations: map[string]string{
						"service.alpha.kubernetes.io/tolerate-unready-endpoints": "true",
					},
					Labels: ls,
				},
				Spec: corev1.ServiceSpec{
					// deprecates service.alpha.kubernetes.io/tolerate-unready-endpoints as of 1.
					// 10? see: kubernetes/kubernetes#49239 Fixed in 1.11 as of #63742
					PublishNotReadyAddresses: true,
					ClusterIP:                "None",
					Selector:                 ls,
				},
			}

			r.appendServicePorts(service)

			// Set AerospikeCluster instance as the owner and controller
			err = controllerutil.SetControllerReference(
				r.aeroCluster, service, r.Scheme,
			)
			if err != nil {
				return err
			}

			if err = r.Client.Create(
				context.TODO(), service, createOption,
			); err != nil {
				return fmt.Errorf(
					"failed to create headless service for statefulset: %v",
					err,
				)
			}

			r.Log.Info("Created new headless service")

			return nil
		}

		return err
	}

	r.Log.Info(
		"Service already exist for statefulSet. Using existing service", "name",
		utils.NamespacedName(service.Namespace, service.Name),
	)

	return nil
}

func (r *SingleClusterReconciler) createSTSLoadBalancerSvc() error {
	loadBalancer := r.aeroCluster.Spec.SeedsFinderServices.LoadBalancer
	if loadBalancer == nil {
		r.Log.Info("LoadBalancer is not configured. Skipping...")
		return nil
	}

	serviceName := r.aeroCluster.Name + "-lb"
	service := &corev1.Service{}

	if err := r.Client.Get(
		context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: r.aeroCluster.Namespace,
		}, service,
	); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating LoadBalancer service for cluster")
			ls := utils.LabelsForAerospikeCluster(r.aeroCluster.Name)

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
				// if port is specified in CR.
				port = loadBalancer.Port
			} else {
				port = targetPort
			}

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
						{
							Port:       port,
							Name:       loadBalancer.PortName,
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
			if nErr := controllerutil.SetControllerReference(
				r.aeroCluster, service, r.Scheme,
			); nErr != nil {
				return nErr
			}

			if nErr := r.Client.Create(
				context.TODO(), service, createOption,
			); nErr != nil {
				return nErr
			}

			r.Log.Info(
				"Created new LoadBalancer service.", "serviceName",
				service.GetName(),
			)

			return nil
		}

		return err
	}

	r.Log.Info(
		"LoadBalancer Service already exist for cluster. Using existing service",
		"name", utils.NamespacedName(service.Namespace, service.Name),
	)

	return nil
}

func (r *SingleClusterReconciler) createPodService(pName, pNamespace string) error {
	service := &corev1.Service{}
	if err := r.Client.Get(
		context.TODO(),
		types.NamespacedName{Name: pName, Namespace: pNamespace}, service,
	); err == nil {
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
			ExternalTrafficPolicy: "Local",
		},
	}

	r.appendServicePorts(service)

	// Set AerospikeCluster instance as the owner and controller.
	// It is created before Pod, so Pod cannot be the owner
	err := controllerutil.SetControllerReference(
		r.aeroCluster, service, r.Scheme,
	)
	if err != nil {
		return err
	}

	if err := r.Client.Create(
		context.TODO(), service, createOption,
	); err != nil {
		return fmt.Errorf(
			"failed to create new service for pod %s: %v", pName, err,
		)
	}

	return nil
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

func (r *SingleClusterReconciler) appendServicePorts(service *corev1.Service) {
	if svcPort := asdbv1.GetServicePort(
		r.aeroCluster.Spec.
			AerospikeConfig,
	); svcPort != nil {
		service.Spec.Ports = append(
			service.Spec.Ports, corev1.ServicePort{
				Name: asdbv1.ServicePortName,
				Port: int32(*svcPort),
			},
		)
	}

	if _, tlsPort := asdbv1.GetServiceTLSNameAndPort(
		r.aeroCluster.Spec.AerospikeConfig,
	); tlsPort != nil {
		service.Spec.Ports = append(
			service.Spec.Ports, corev1.ServicePort{
				Name: asdbv1.ServiceTLSPortName,
				Port: int32(*tlsPort),
			},
		)
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

func podServiceNeeded(multiPodPerHost bool, networkPolicy *asdbv1.AerospikeNetworkPolicy) bool {
	if !multiPodPerHost || networkPolicy == nil {
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

func (r *SingleClusterReconciler) createPodServiceIfNeeded(pods []*corev1.Pod) error {
	if !podServiceNeeded(r.aeroCluster.Status.PodSpec.MultiPodPerHost, &r.aeroCluster.Status.AerospikeNetworkPolicy) &&
		podServiceNeeded(r.aeroCluster.Spec.PodSpec.MultiPodPerHost, &r.aeroCluster.Spec.AerospikeNetworkPolicy) {
		// Create services for all pods if network policy is changed and rely on nodePort service
		for idx := range pods {
			if err := r.createPodService(
				pods[idx].Name, r.aeroCluster.Namespace,
			); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
		}
	}

	return nil
}
