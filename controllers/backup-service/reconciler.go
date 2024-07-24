package backupservice

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	app "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

// BackupServiceConfigYAML is the backup service configuration yaml file
const BackupServiceConfigYAML = "aerospike-backup-service.yml"

type serviceConfig struct {
	portInfo    map[string]int32
	contextPath string
}

var defaultServiceConfig = serviceConfig{
	portInfo: map[string]int32{
		common.HTTPKey: 8080,
	},
	contextPath: "/",
}

// SingleBackupServiceReconciler reconciles a single AerospikeBackupService
type SingleBackupServiceReconciler struct {
	client.Client
	Recorder          record.EventRecorder
	aeroBackupService *asdbv1beta1.AerospikeBackupService
	KubeConfig        *rest.Config
	Scheme            *k8sRuntime.Scheme
	Log               logr.Logger
}

func (r *SingleBackupServiceReconciler) Reconcile() (result ctrl.Result, recErr error) {
	// Set the status phase to Error if the recErr is not nil
	// recErr is only set when reconcile failure should result in Error phase of the Backup service operation
	defer func() {
		if recErr != nil {
			r.Log.Error(recErr, "Reconcile failed")

			if err := r.setStatusPhase(asdbv1beta1.AerospikeBackupServiceError); err != nil {
				recErr = err
			}
		}
	}()

	if !r.aeroBackupService.ObjectMeta.DeletionTimestamp.IsZero() {
		// Stop reconciliation as the Aerospike Backup service is being deleted
		return reconcile.Result{}, nil
	}

	// Set the status to AerospikeClusterInProgress before starting any operations
	if err := r.setStatusPhase(asdbv1beta1.AerospikeBackupServiceInProgress); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.reconcileConfigMap(); err != nil {
		recErr = err
		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(); err != nil {
		recErr = err
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(); err != nil {
		recErr = err
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SingleBackupServiceReconciler) reconcileConfigMap() error {
	cm := &corev1.ConfigMap{}

	if err := r.Client.Get(context.TODO(),
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, cm,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Create Backup Service ConfigMap",
			"name", getBackupServiceName(r.aeroBackupService))

		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.aeroBackupService.Name,
				Namespace: r.aeroBackupService.Namespace,
				Labels:    utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name),
			},
			Data: r.getConfigMapData(),
		}

		// Set AerospikeBackupService instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroBackupService, cm, r.Scheme,
		)
		if err != nil {
			return err
		}

		if err = r.Client.Create(
			context.TODO(), cm, common.CreateOption,
		); err != nil {
			return fmt.Errorf(
				"failed to create ConfigMap: %v",
				err,
			)
		}

		r.Log.Info("Created new Backup Service ConfigMap",
			"name", getBackupServiceName(r.aeroBackupService))

		return nil
	}

	r.Log.Info(
		"ConfigMap already exist. Updating existing ConfigMap if required",
		"name", getBackupServiceName(r.aeroBackupService),
	)

	desiredDataMap := make(map[string]interface{})
	currentDataMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackupService.Spec.Config.Raw, &desiredDataMap); err != nil {
		return err
	}

	data := cm.Data[BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &currentDataMap); err != nil {
		return err
	}

	currentDataMap[common.ServiceKey] = desiredDataMap[common.ServiceKey]
	currentDataMap[common.BackupPoliciesKey] = desiredDataMap[common.BackupPoliciesKey]
	currentDataMap[common.StorageKey] = desiredDataMap[common.StorageKey]
	currentDataMap[common.SecretAgentsKey] = desiredDataMap[common.SecretAgentsKey]

	updatedConfig, err := yaml.Marshal(currentDataMap)
	if err != nil {
		return err
	}

	cm.Data[BackupServiceConfigYAML] = string(updatedConfig)

	if err = r.Client.Update(
		context.TODO(), cm, common.UpdateOption,
	); err != nil {
		return fmt.Errorf(
			"failed to update Backup Service ConfigMap: %v",
			err,
		)
	}

	r.Log.Info("Updated Backup Service ConfigMap",
		"name", getBackupServiceName(r.aeroBackupService))

	return nil
}

func (r *SingleBackupServiceReconciler) getConfigMapData() map[string]string {
	data := make(map[string]string)
	data[BackupServiceConfigYAML] = string(r.aeroBackupService.Spec.Config.Raw)

	return data
}

func (r *SingleBackupServiceReconciler) reconcileDeployment() error {
	var deploy app.Deployment

	if err := r.Client.Get(context.TODO(),
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, &deploy,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Create Backup Service deployment",
			"name", getBackupServiceName(r.aeroBackupService))

		deployment, err := r.getDeploymentObject()
		if err != nil {
			return err
		}

		// Set AerospikeBackupService instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroBackupService, deployment, r.Scheme,
		)
		if err != nil {
			return err
		}

		err = r.Client.Create(context.TODO(), deployment, common.CreateOption)
		if err != nil {
			return fmt.Errorf("failed to deploy Backup service deployment: %v", err)
		}

		return r.waitForDeploymentToBeReady()
	}

	r.Log.Info(
		"Backup Service deployment already exist. Updating existing deployment if required",
		"name", getBackupServiceName(r.aeroBackupService),
	)

	oldResourceVersion := deploy.ResourceVersion

	desiredDeployObj, err := r.getDeploymentObject()
	if err != nil {
		return err
	}

	deploy.Spec = desiredDeployObj.Spec

	if err = r.Client.Update(context.TODO(), &deploy, common.UpdateOption); err != nil {
		return fmt.Errorf("failed to update Backup service deployment: %v", err)
	}

	if oldResourceVersion != deploy.ResourceVersion {
		r.Log.Info("Deployment spec is updated, will result in rolling restart")
		return r.waitForDeploymentToBeReady()
	}

	// If status is empty then no need for config Hash comparison
	if len(r.aeroBackupService.Status.Config.Raw) == 0 {
		return r.waitForDeploymentToBeReady()
	}

	desiredHash, err := utils.GetHash(string(r.aeroBackupService.Spec.Config.Raw))
	if err != nil {
		return err
	}

	currentHash, err := utils.GetHash(string(r.aeroBackupService.Status.Config.Raw))
	if err != nil {
		return err
	}

	// If there is a change in config hash, then restart the deployment pod
	if desiredHash != currentHash {
		r.Log.Info("BackupService config is updated, will result in rolling restart")

		podList, err := r.getBackupServicePodList()
		if err != nil {
			return err
		}

		for idx := range podList.Items {
			pod := &podList.Items[idx]

			err = r.Client.Delete(context.TODO(), pod)
			if err != nil {
				return err
			}
		}
	}

	return r.waitForDeploymentToBeReady()
}

func getBackupServiceName(aeroBackupService *asdbv1beta1.AerospikeBackupService) types.NamespacedName {
	return types.NamespacedName{Name: aeroBackupService.Name, Namespace: aeroBackupService.Namespace}
}

func (r *SingleBackupServiceReconciler) getBackupServicePodList() (*corev1.PodList, error) {
	var podList corev1.PodList

	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name))
	listOps := &client.ListOptions{
		Namespace: r.aeroBackupService.Namespace, LabelSelector: labelSelector,
	}

	if err := r.Client.List(context.TODO(), &podList, listOps); err != nil {
		return nil, err
	}

	return &podList, nil
}

func (r *SingleBackupServiceReconciler) getDeploymentObject() (*app.Deployment, error) {
	svcLabels := utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name)
	volumeMounts, volumes := r.getVolumeAndMounts()

	resources := corev1.ResourceRequirements{}

	if r.aeroBackupService.Spec.Resources != nil {
		resources = *r.aeroBackupService.Spec.Resources
	}

	svcConf, err := r.getBackupServiceConfig()
	if err != nil {
		return nil, err
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(svcConf.portInfo))

	for name, port := range svcConf.portInfo {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          name,
			ContainerPort: port,
		})
	}

	deploy := &app.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.aeroBackupService.Name,
			Namespace: r.aeroBackupService.Namespace,
			Labels:    svcLabels,
		},
		Spec: app.DeploymentSpec{
			Replicas: func(replica int32) *int32 { return &replica }(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: svcLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: svcLabels,
				},
				Spec: corev1.PodSpec{
					// TODO: Finalise on this. Who should create this SA?
					ServiceAccountName: common.AerospikeBackupService,
					Containers: []corev1.Container{
						{
							Name:            common.AerospikeBackupService,
							Image:           r.aeroBackupService.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    volumeMounts,
							Resources:       resources,
							Ports:           containerPorts,
						},
					},
					// Init-container is used to copy configMap data to work-dir(emptyDir).
					// There is a limitation of read-only file-system for mounted configMap volumes
					// Remove this init-container when backup-service start supporting hot reload
					InitContainers: []corev1.Container{
						{
							Name:  "init-backup-service",
							Image: "busybox",
							Command: []string{
								"sh",
								"-c",
								"cp /etc/aerospike-backup-service/aerospike-backup-service.yml /work-dir/aerospike-backup-service.yml",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "backup-service-config-configmap",
									MountPath: "/etc/aerospike-backup-service/",
								},
								{
									Name:      "backup-service-config",
									MountPath: "/work-dir",
								},
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	return deploy, nil
}

func (r *SingleBackupServiceReconciler) getVolumeAndMounts() ([]corev1.VolumeMount, []corev1.Volume) {
	volumes := make([]corev1.Volume, 0, len(r.aeroBackupService.Spec.SecretMounts))
	volumeMounts := make([]corev1.VolumeMount, 0, len(r.aeroBackupService.Spec.SecretMounts))

	for idx := range r.aeroBackupService.Spec.SecretMounts {
		secretMount := r.aeroBackupService.Spec.SecretMounts[idx]
		volumeMounts = append(volumeMounts, secretMount.VolumeMount)

		volumes = append(volumes, corev1.Volume{
			Name: secretMount.VolumeMount.Name,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretMount.SecretName,
				},
			},
		})
	}

	// Backup service configMap mountPath
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      "backup-service-config",
		MountPath: fmt.Sprintf("/etc/aerospike-backup-service/%s", BackupServiceConfigYAML),
		SubPath:   BackupServiceConfigYAML,
	})

	// Backup service configMap
	volumes = append(volumes, corev1.Volume{
		Name: "backup-service-config-configmap",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: r.aeroBackupService.Name,
				},
			},
		},
	})

	// EmptyDir for init-container to copy configMap data to work-dir
	// Remove this volume when backup-service starts supporting hot reload
	volumes = append(volumes, corev1.Volume{
		Name: "backup-service-config",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})

	return volumeMounts, volumes
}

func (r *SingleBackupServiceReconciler) reconcileService() error {
	var service corev1.Service

	if err := r.Client.Get(context.TODO(),
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, &service,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Create Backup Service",
			"name", getBackupServiceName(r.aeroBackupService))

		svc, err := r.getServiceObject()
		if err != nil {
			return err
		}

		// Set AerospikeBackupService instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroBackupService, svc, r.Scheme,
		)
		if err != nil {
			return err
		}

		err = r.Client.Create(context.TODO(), svc, common.CreateOption)
		if err != nil {
			return fmt.Errorf("failed to create Backup Service: %v", err)
		}

		return nil
	}

	r.Log.Info(
		"Backup Service already exist. Updating existing service if required",
		"name", getBackupServiceName(r.aeroBackupService),
	)

	svc, err := r.getServiceObject()
	if err != nil {
		return err
	}

	service.Spec = svc.Spec

	if err = r.Client.Update(context.TODO(), &service, common.UpdateOption); err != nil {
		return fmt.Errorf("failed to update Backup service: %v", err)
	}

	r.Log.Info("Updated Backup Service", "name", getBackupServiceName(r.aeroBackupService))

	return nil
}

func (r *SingleBackupServiceReconciler) getServiceObject() (*corev1.Service, error) {
	svcConfig, err := r.getBackupServiceConfig()
	if err != nil {
		return nil, err
	}

	servicePort := make([]corev1.ServicePort, 0, len(svcConfig.portInfo))

	for name, port := range svcConfig.portInfo {
		servicePort = append(servicePort, corev1.ServicePort{
			Name: name,
			Port: port,
		})
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.aeroBackupService.Name,
			Namespace: r.aeroBackupService.Namespace,
			Labels:    utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name),
		},
		Spec: corev1.ServiceSpec{
			Selector: utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name),
			Ports:    servicePort,
		},
	}

	if r.aeroBackupService.Spec.Service != nil {
		svc.Spec.Type = r.aeroBackupService.Spec.Service.Type
	}

	return svc, nil
}

func (r *SingleBackupServiceReconciler) getBackupServiceConfig() (*serviceConfig, error) {
	config := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackupService.Spec.Config.Raw, &config); err != nil {
		return nil, err
	}

	if _, ok := config[common.ServiceKey]; !ok {
		r.Log.Info("Service config not found")
		return &defaultServiceConfig, nil
	}

	svc, ok := config[common.ServiceKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("service config is not in correct format")
	}

	if _, ok = svc[common.HTTPKey]; !ok {
		r.Log.Info("HTTP config not found")
		return &defaultServiceConfig, nil
	}

	httpConf, ok := svc[common.HTTPKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("http config is not in correct format")
	}

	var svcConfig serviceConfig

	port, ok := httpConf["port"]
	if !ok {
		svcConfig.portInfo = defaultServiceConfig.portInfo
	} else {
		svcConfig.portInfo = map[string]int32{common.HTTPKey: int32(port.(float64))}
	}

	ctxPath, ok := httpConf["context-path"]
	if !ok {
		svcConfig.contextPath = defaultServiceConfig.contextPath
	} else {
		svcConfig.contextPath = ctxPath.(string)
	}

	return &svcConfig, nil
}

func (r *SingleBackupServiceReconciler) waitForDeploymentToBeReady() error {
	const (
		podStatusTimeout       = 2 * time.Minute
		podStatusRetryInterval = 5 * time.Second
	)

	r.Log.Info(
		"Waiting for deployment to be ready", "WaitTimePerPod", podStatusTimeout,
	)

	if err := wait.PollUntilContextTimeout(context.TODO(),
		podStatusRetryInterval, podStatusTimeout, true, func(ctx context.Context) (done bool, err error) {
			podList, err := r.getBackupServicePodList()
			if err != nil {
				return false, err
			}

			if len(podList.Items) == 0 {
				r.Log.Info("No pod found for deployment")
				return false, nil
			}

			for idx := range podList.Items {
				pod := &podList.Items[idx]

				if err := utils.CheckPodFailed(pod); err != nil {
					return false, fmt.Errorf("pod %s failed: %v", pod.Name, err)
				}

				if !utils.IsPodRunningAndReady(pod) {
					r.Log.Info("Pod is not ready", "pod", pod.Name)
					return false, nil
				}
			}

			var deploy app.Deployment
			if err := r.Client.Get(
				ctx,
				types.NamespacedName{Name: r.aeroBackupService.Name, Namespace: r.aeroBackupService.Namespace},
				&deploy,
			); err != nil {
				return false, err
			}

			if deploy.Status.Replicas != *deploy.Spec.Replicas {
				return false, nil
			}

			return true, nil
		},
	); err != nil {
		return err
	}

	r.Log.Info("Deployment is ready")

	return nil
}

func (r *SingleBackupServiceReconciler) setStatusPhase(phase asdbv1beta1.AerospikeBackupServicePhase) error {
	if r.aeroBackupService.Status.Phase != phase {
		r.aeroBackupService.Status.Phase = phase

		if err := r.Client.Status().Update(context.Background(), r.aeroBackupService); err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to set backup service status to %s", phase))
			return err
		}
	}

	return nil
}

func (r *SingleBackupServiceReconciler) updateStatus() error {
	svcConfig, err := r.getBackupServiceConfig()
	if err != nil {
		return err
	}

	status := r.CopySpecToStatus()
	status.ContextPath = svcConfig.contextPath
	status.Port = svcConfig.portInfo[common.HTTPKey]
	status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted

	r.aeroBackupService.Status = *status

	r.Log.Info(fmt.Sprintf("Updating status: %+v", r.aeroBackupService.Status))

	return r.Client.Status().Update(context.Background(), r.aeroBackupService)
}

func (r *SingleBackupServiceReconciler) CopySpecToStatus() *asdbv1beta1.AerospikeBackupServiceStatus {
	status := asdbv1beta1.AerospikeBackupServiceStatus{}
	status.Image = r.aeroBackupService.Spec.Image
	status.Config = r.aeroBackupService.Spec.Config
	status.Resources = r.aeroBackupService.Spec.Resources
	status.SecretMounts = r.aeroBackupService.Spec.SecretMounts
	status.Service = r.aeroBackupService.Spec.Service

	return &status
}
