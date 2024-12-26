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

	"github.com/aerospike/aerospike-backup-service/v2/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v2/pkg/validation"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/internal/controller/common"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

type serviceConfig struct {
	portInfo    map[string]int32
	contextPath string
}

var defaultServiceConfig = serviceConfig{
	portInfo: map[string]int32{
		asdbv1beta1.HTTPKey: 8080,
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

	// Skip reconcile if the backup service version is less than 3.0.0.
	// This is to avoid rolling restart of the backup service pods after AKO upgrade
	if err := asdbv1beta1.ValidateBackupSvcVersion(r.aeroBackupService.Spec.Image); err != nil {
		r.Log.Info("Skipping reconcile as backup service version is less than 3.0.0")
		return reconcile.Result{}, nil
	}

	if !r.aeroBackupService.ObjectMeta.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleted AerospikeBackupService")
		r.Recorder.Eventf(
			r.aeroBackupService, corev1.EventTypeNormal, "Deleted",
			"Deleted AerospikeBackupService %s/%s", r.aeroBackupService.Namespace,
			r.aeroBackupService.Name,
		)

		// Stop reconciliation as the Aerospike Backup service is being deleted
		return reconcile.Result{}, nil
	}

	// Set the status to AerospikeClusterInProgress before starting any operations
	if err := r.setStatusPhase(asdbv1beta1.AerospikeBackupServiceInProgress); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.reconcileConfigMap(); err != nil {
		r.Log.Error(err, "Failed to reconcile config map")
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"ConfigMapReconcileFailed", "Failed to reconcile config map %s/%s",
			r.aeroBackupService.Namespace, r.aeroBackupService.Name)

		recErr = err

		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(); err != nil {
		r.Log.Error(err, "Failed to reconcile deployment")
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"DeploymentReconcileFailed", "Failed to reconcile deployment %s/%s",
			r.aeroBackupService.Namespace, r.aeroBackupService.Name)

		recErr = err

		return ctrl.Result{}, err
	}

	if err := r.reconcileService(); err != nil {
		r.Log.Error(err, "Failed to reconcile service")
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"ServiceReconcileFailed", "Failed to reconcile service %s/%s",
			r.aeroBackupService.Namespace, r.aeroBackupService.Name)

		recErr = err

		return ctrl.Result{}, err
	}

	if err := r.updateStatus(); err != nil {
		r.Log.Error(err, "Failed to update status")
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"StatusUpdateFailed", "Failed to update AerospikeBackupService status %s/%s",
			r.aeroBackupService.Namespace, r.aeroBackupService.Name)

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

		r.Log.Info("Creating Backup Service ConfigMap",
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
				"failed to create ConfigMap: %w",
				err,
			)
		}

		r.Log.Info("Created Backup Service ConfigMap",
			"name", getBackupServiceName(r.aeroBackupService))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ConfigMapCreated",
			"Created Backup Service ConfigMap %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

		return nil
	}

	r.Log.Info(
		"Backup Service ConfigMap already exist. Updating existing ConfigMap if required",
		"name", getBackupServiceName(r.aeroBackupService),
	)

	desiredDataMap := make(map[string]interface{})
	currentDataMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackupService.Spec.Config.Raw, &desiredDataMap); err != nil {
		return err
	}

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &currentDataMap); err != nil {
		return err
	}

	// Sync keys
	keys := []string{
		asdbv1beta1.ServiceKey,
		asdbv1beta1.BackupPoliciesKey,
		asdbv1beta1.StorageKey,
		asdbv1beta1.SecretAgentsKey,
	}

	for _, key := range keys {
		if value, ok := desiredDataMap[key]; ok {
			currentDataMap[key] = value
		} else {
			delete(currentDataMap, key)
		}
	}

	// Remove old "secret-agent: null" from configMap
	// This was added internally in AKO (3.4) during backup service configMap update
	delete(currentDataMap, "secret-agent")

	updatedConfig, err := yaml.Marshal(currentDataMap)
	if err != nil {
		return err
	}

	cm.Data[asdbv1beta1.BackupServiceConfigYAML] = string(updatedConfig)

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
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ConfigMapUpdated",
		"Updated Backup Service ConfigMap %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

	return nil
}

func (r *SingleBackupServiceReconciler) getConfigMapData() map[string]string {
	data := make(map[string]string)
	data[asdbv1beta1.BackupServiceConfigYAML] = string(r.aeroBackupService.Spec.Config.Raw)

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

		r.Log.Info("Creating Backup Service deployment",
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

		r.Log.Info("Created Backup Service deployment",
			"name", getBackupServiceName(r.aeroBackupService))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "DeploymentCreated",
			"Created Backup Service Deployment %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

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

	r.Log.Info("Updated Backup Service deployment",
		"name", getBackupServiceName(r.aeroBackupService))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "DeploymentUpdated",
		"Updated Backup Service Deployment %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

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

	// If there is a change in config hash, then reload the config or restart the deployment pod
	if desiredHash != currentHash {
		r.Log.Info("BackupService config mismatch, will reload the config")

		if err := r.updateBackupSvcConfig(); err != nil {
			return err
		}

		r.Log.Info("Reloaded backup service")
	}

	return nil
}

func (r *SingleBackupServiceReconciler) updateBackupSvcConfig() error {
	var currentConfig, desiredConfig dto.Config

	if err := yaml.Unmarshal(r.aeroBackupService.Status.Config.Raw, &currentConfig); err != nil {
		return err
	}

	if err := yaml.Unmarshal(r.aeroBackupService.Spec.Config.Raw, &desiredConfig); err != nil {
		return err
	}

	if err := validation.ValidateStaticFieldChanges(&currentConfig, &desiredConfig); err != nil {
		r.Log.Info("Static config change detected, will result in rolling restart")
		// In case of static config change restart the backup service pod
		return r.restartBackupSvcPod()
	}

	return common.ReloadBackupServiceConfigInPods(r.Client, r.Log,
		&asdbv1beta1.BackupService{
			Name:      r.aeroBackupService.Name,
			Namespace: r.aeroBackupService.Namespace},
	)
}

func (r *SingleBackupServiceReconciler) restartBackupSvcPod() error {
	podList, err := common.GetBackupServicePodList(r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace)
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

	return r.waitForDeploymentToBeReady()
}

func getBackupServiceName(aeroBackupService *asdbv1beta1.AerospikeBackupService) types.NamespacedName {
	return types.NamespacedName{Name: aeroBackupService.Name, Namespace: aeroBackupService.Namespace}
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
					ServiceAccountName: asdbv1beta1.AerospikeBackupServiceKey,
					Containers: []corev1.Container{
						{
							Name:            asdbv1beta1.AerospikeBackupServiceKey,
							Image:           r.aeroBackupService.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    volumeMounts,
							Resources:       resources,
							Ports:           containerPorts,
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
		MountPath: "/etc/aerospike-backup-service",
	})

	// Backup service configMap
	volumes = append(volumes, corev1.Volume{
		Name: "backup-service-config",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: r.aeroBackupService.Name,
				},
			},
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

		r.Log.Info("Creating Backup Service",
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

		r.Log.Info("Created Backup Service",
			"name", getBackupServiceName(r.aeroBackupService))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ServiceCreated",
			"Created Backup Service %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

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
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ServiceUpdated",
		"Updated Backup Service %s/%s", r.aeroBackupService.Namespace, r.aeroBackupService.Name)

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

	if _, ok := config[asdbv1beta1.ServiceKey]; !ok {
		r.Log.Info("Service config not found")
		return &defaultServiceConfig, nil
	}

	svc, ok := config[asdbv1beta1.ServiceKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("service config is not in correct format")
	}

	if _, ok = svc[asdbv1beta1.HTTPKey]; !ok {
		r.Log.Info("HTTP config not found")
		return &defaultServiceConfig, nil
	}

	httpConf, ok := svc[asdbv1beta1.HTTPKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("http config is not in correct format")
	}

	var svcConfig serviceConfig

	port, ok := httpConf["port"]
	if !ok {
		svcConfig.portInfo = defaultServiceConfig.portInfo
	} else {
		svcConfig.portInfo = map[string]int32{asdbv1beta1.HTTPKey: int32(port.(float64))}
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
			podList, err := common.GetBackupServicePodList(r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace)
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
	status.Port = svcConfig.portInfo[asdbv1beta1.HTTPKey]
	status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted

	r.aeroBackupService.Status = *status

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
