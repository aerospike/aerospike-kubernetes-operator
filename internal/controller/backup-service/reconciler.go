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
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	"github.com/aerospike/aerospike-backup-service/v3/pkg/dto"
	"github.com/aerospike/aerospike-backup-service/v3/pkg/validation"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
	backup_service "github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/backup-service"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
	lib "github.com/aerospike/aerospike-management-lib"
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

func (r *SingleBackupServiceReconciler) Reconcile(ctx context.Context) (result ctrl.Result, recErr error) {
	defer func() {
		switch {
		case recErr != nil:
			r.Log.Error(recErr, "Reconcile failed", "result", "error")

			if err := r.setStatusPhase(ctx, asdbv1beta1.AerospikeBackupServiceError); err != nil {
				recErr = err
			}
		case result.RequeueAfter > 0:
			r.Log.Info("Reconcile requeued", "result", "requeue", "after", result.RequeueAfter)
		default:
			r.Log.Info("Reconciled successfully", "result", "success")
		}
	}()

	// Skip reconcile if the backup service version is less than 3.0.0.
	// This is to avoid rolling restart of the backup service pods after AKO upgrade
	if _, err := asdbv1beta1.ValidateBackupSvcVersion(r.aeroBackupService.Spec.Image); err != nil {
		r.Log.Info("Skipping reconcile, backup service version unsupported",
			"minVersion", asdbv1beta1.BackupSvcMinSupportedVersion)

		return reconcile.Result{}, nil
	}

	if !r.aeroBackupService.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleting AerospikeBackupService")
		r.Recorder.Eventf(
			r.aeroBackupService, corev1.EventTypeNormal, EventReasonDeleted,
			"Deleted AerospikeBackupService %s", utils.GetNamespacedName(r.aeroBackupService),
		)

		// Stop reconciliation as the Aerospike Backup service is being deleted
		return reconcile.Result{}, nil
	}

	// Set the status to AerospikeClusterInProgress before starting any operations
	if err := r.setStatusPhase(ctx, asdbv1beta1.AerospikeBackupServiceInProgress); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.reconcileConfigMap(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			EventReasonConfigMapReconcileFailed, "Failed to reconcile config map %s",
			utils.GetNamespacedName(r.aeroBackupService))

		recErr = err

		return ctrl.Result{}, recErr
	}

	if err := r.reconcileService(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			EventReasonServiceReconcileFailed, "Failed to reconcile service %s",
			utils.GetNamespacedName(r.aeroBackupService))

		recErr = err

		return ctrl.Result{}, recErr
	}

	if err := r.reconcileDeployment(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			EventReasonDeploymentReconcileFailed, "Failed to reconcile deployment %s",
			utils.GetNamespacedName(r.aeroBackupService))

		recErr = err

		return ctrl.Result{}, recErr
	}

	if err := r.updateStatus(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			EventReasonStatusUpdateFailed, "Failed to update AerospikeBackupService status %s",
			utils.GetNamespacedName(r.aeroBackupService))

		recErr = err

		return ctrl.Result{}, recErr
	}

	return ctrl.Result{}, nil
}

func (r *SingleBackupServiceReconciler) reconcileConfigMap(ctx context.Context) error {
	cm := &corev1.ConfigMap{}

	if err := r.Get(ctx,
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, cm,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating backup service ConfigMap",
			"configMap", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))

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

		if err = r.Create(ctx, cm, common.CreateOption); err != nil {
			return fmt.Errorf("create backup service ConfigMap %s: %w",
				utils.GetNamespacedName(r.aeroBackupService), err)
		}

		r.Log.Info("Created backup service ConfigMap",
			"configMap", klog.KObj(cm))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonConfigMapCreated,
			"Created backup service ConfigMap %s", utils.GetNamespacedName(r.aeroBackupService))

		return nil
	}

	r.Log.Info("Updating backup service ConfigMap if required",
		"configMap", klog.KObj(cm))

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

	if err = r.Update(ctx, cm, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service ConfigMap %s: %w",
			utils.GetNamespacedName(r.aeroBackupService), err)
	}

	r.Log.Info("Updated backup service ConfigMap",
		"configMap", klog.KObj(cm))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonConfigMapUpdated,
		"Updated backup service ConfigMap %s", utils.GetNamespacedName(r.aeroBackupService))

	return nil
}

func (r *SingleBackupServiceReconciler) getConfigMapData() map[string]string {
	data := make(map[string]string)
	data[asdbv1beta1.BackupServiceConfigYAML] = string(r.aeroBackupService.Spec.Config.Raw)

	return data
}

func (r *SingleBackupServiceReconciler) reconcileDeployment(ctx context.Context) error {
	deployment, err := r.getBackupSvcDeployment(ctx)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating backup service deployment",
			"deployment", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))

		deployment, err = r.getDeploymentObject()
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

		err = r.Create(ctx, deployment, common.CreateOption)
		if err != nil {
			return fmt.Errorf("create backup service deployment %s: %w",
				utils.GetNamespacedName(r.aeroBackupService), err)
		}

		r.Log.Info("Created backup service deployment",
			"deployment", klog.KObj(deployment))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonDeploymentCreated,
			"Created backup service deployment %s", utils.GetNamespacedName(r.aeroBackupService))

		return r.waitForDeploymentToBeReady(ctx)
	}

	r.Log.Info("Updating backup service deployment if required",
		"deployment", klog.KObj(deployment))

	oldResourceVersion := deployment.ResourceVersion

	desiredDeployObj, err := r.getDeploymentObject()
	if err != nil {
		return err
	}

	deployment.Spec = desiredDeployObj.Spec

	if err = r.Update(ctx, deployment, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service deployment %s: %w",
			utils.GetNamespacedName(r.aeroBackupService), err)
	}

	r.Log.Info("Updated backup service deployment",
		"deployment", klog.KObj(deployment))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonDeploymentUpdated,
		"Updated backup service deployment %s", utils.GetNamespacedName(r.aeroBackupService))

	if oldResourceVersion != deployment.ResourceVersion {
		r.Log.Info("Deployment spec updated, rolling restart of backup service pod",
			"deployment", klog.KObj(deployment))

		return r.waitForDeploymentToBeReady(ctx)
	}

	// Wait for deployment pods to be ready before doing any operation related to the backup service
	if err := r.waitForDeploymentToBeReady(ctx); err != nil {
		return err
	}

	return r.updateBackupSvcConfig(ctx)
}

func (r *SingleBackupServiceReconciler) getBackupSvcDeployment(ctx context.Context) (*app.Deployment, error) {
	var deployment app.Deployment

	if err := r.Get(ctx,
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, &deployment,
	); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func (r *SingleBackupServiceReconciler) updateBackupSvcConfig(ctx context.Context) error {
	if r.aeroBackupService.Status.Config.Raw == nil {
		r.Log.Info("Skipping backup service config reload as status is empty")
		return nil
	}

	var currentConfig, desiredConfig dto.Config

	backupSvc := &asdbv1beta1.BackupService{
		Name:      r.aeroBackupService.Name,
		Namespace: r.aeroBackupService.Namespace,
	}

	svcConfig, err := r.getBackupServiceConfig()
	if err != nil {
		return err
	}

	// Always create client with the latest config in spec
	backupServiceClient := backup_service.NewClient(
		fmt.Sprintf("%s.%s.svc", backupSvc.Name, backupSvc.Namespace),
		svcConfig.portInfo[asdbv1beta1.HTTPKey],
		svcConfig.contextPath,
	)

	apiBackupSvcConfig, err := backupServiceClient.GetBackupServiceConfig()
	if err != nil {
		return err
	}

	desiredData, err := common.GetBackupSvcConfigFromCM(r.Client, backupSvc)
	if err != nil {
		return err
	}

	synced, err := common.IsBackupSvcFullConfigSynced(apiBackupSvcConfig, desiredData, r.Log)
	if err != nil {
		return err
	}

	if synced {
		r.Log.Info("Backup service config already latest, skipping update")
		return nil
	}

	r.Log.Info("Backup service config mismatch, will reload the config")

	apiBackupSvcConfigData, err := yaml.Marshal(apiBackupSvcConfig)
	if err != nil {
		return err
	}

	if err := yaml.Unmarshal(apiBackupSvcConfigData, &currentConfig); err != nil {
		return err
	}

	if err := yaml.Unmarshal([]byte(desiredData), &desiredConfig); err != nil {
		return err
	}

	if err := validation.ValidateStaticFieldChanges(&currentConfig, &desiredConfig); err != nil {
		r.Log.Info("Static config change detected, will result in rolling restart of backup service pod")
		// In case of static config change restart the backup service pod
		return r.restartBackupSvcPod(ctx)
	}

	return common.ReloadBackupServiceConfigInPods(r.Client, backupServiceClient, r.Log, backupSvc)
}

func (r *SingleBackupServiceReconciler) restartBackupSvcPod(ctx context.Context) error {
	podList, err := common.GetBackupServicePodList(r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace)
	if err != nil {
		return err
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		err = r.Delete(ctx, pod)
		if err != nil {
			return err
		}
	}

	return r.waitForDeploymentToBeReady(ctx)
}

func (r *SingleBackupServiceReconciler) getDeploymentObject() (*app.Deployment, error) {
	svcLabels := utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name)
	volumeMounts, volumes := r.getVolumeAndMounts()

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
					ServiceAccountName: r.getServiceAccount(),
					Containers: []corev1.Container{
						{
							Name:            asdbv1beta1.AerospikeBackupServiceKey,
							Image:           r.aeroBackupService.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    volumeMounts,
							Ports:           containerPorts,
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	r.updateDeploymentFromPodSpec(deploy)

	return deploy, nil
}

func (r *SingleBackupServiceReconciler) getServiceAccount() string {
	if r.aeroBackupService.Spec.PodSpec.ServiceAccountName != "" {
		return r.aeroBackupService.Spec.PodSpec.ServiceAccountName
	}

	return asdbv1beta1.AerospikeBackupServiceKey
}

func (r *SingleBackupServiceReconciler) updateDeploymentFromPodSpec(deploy *app.Deployment) {
	r.updateDeploymentSchedulingPolicy(deploy)

	defaultLabels := utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name)
	userDefinedLabels := r.aeroBackupService.Spec.PodSpec.ObjectMeta.Labels
	mergedLabels := utils.MergeLabels(defaultLabels, userDefinedLabels)
	deploy.Spec.Template.Labels = mergedLabels

	deploy.Spec.Template.Annotations = r.aeroBackupService.Spec.PodSpec.ObjectMeta.Annotations

	deploy.Spec.Template.Spec.ImagePullSecrets = r.aeroBackupService.Spec.PodSpec.ImagePullSecrets

	r.updateBackupServiceContainer(deploy)
}

func (r *SingleBackupServiceReconciler) updateDeploymentSchedulingPolicy(deploy *app.Deployment) {
	deploy.Spec.Template.Spec.Affinity = r.aeroBackupService.Spec.PodSpec.Affinity
	deploy.Spec.Template.Spec.NodeSelector = r.aeroBackupService.Spec.PodSpec.NodeSelector
	deploy.Spec.Template.Spec.Tolerations = r.aeroBackupService.Spec.PodSpec.Tolerations
}

func (r *SingleBackupServiceReconciler) updateBackupServiceContainer(deploy *app.Deployment) {
	resources := r.aeroBackupService.Spec.PodSpec.ServiceContainerSpec.Resources
	if resources != nil {
		deploy.Spec.Template.Spec.Containers[0].Resources = *resources
	} else {
		deploy.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{}
	}

	deploy.Spec.Template.Spec.Containers[0].SecurityContext =
		r.aeroBackupService.Spec.PodSpec.ServiceContainerSpec.SecurityContext
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

func (r *SingleBackupServiceReconciler) reconcileService(ctx context.Context) error {
	var service corev1.Service

	if err := r.Get(ctx,
		types.NamespacedName{
			Namespace: r.aeroBackupService.Namespace,
			Name:      r.aeroBackupService.Name,
		}, &service,
	); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating backup service",
			"service", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))

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

		err = r.Create(ctx, svc, common.CreateOption)
		if err != nil {
			return fmt.Errorf("create backup service %s: %w",
				utils.GetNamespacedName(r.aeroBackupService), err)
		}

		r.Log.Info("Created backup service",
			"service", klog.KObj(svc))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonServiceCreated,
			"Created backup service %s", utils.GetNamespacedName(r.aeroBackupService))

		return nil
	}

	r.Log.Info("Updating backup service if required",
		"service", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))

	svc, err := r.getServiceObject()
	if err != nil {
		return err
	}

	service.Spec = svc.Spec

	if err = r.Update(ctx, &service, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service %s: %w",
			utils.GetNamespacedName(r.aeroBackupService), err)
	}

	r.Log.Info("Updated backup service",
		"service", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, EventReasonServiceUpdated,
		"Updated backup service %s", utils.GetNamespacedName(r.aeroBackupService))

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

func (r *SingleBackupServiceReconciler) waitForDeploymentToBeReady(ctx context.Context) error {
	const (
		podStatusTimeout       = 2 * time.Minute
		podStatusRetryInterval = 5 * time.Second
	)

	r.Log.Info("Waiting for deployment to be ready",
		"deployment", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name),
		"waitTimePerPod", podStatusTimeout,
	)

	if err := wait.PollUntilContextTimeout(ctx,
		podStatusRetryInterval, podStatusTimeout, true, func(pollCtx context.Context) (done bool, err error) {
			deployment, err := r.getBackupSvcDeployment(pollCtx)
			if err != nil {
				return false, err
			}

			// This check is for the condition when deployment rollout is yet to begin, and
			// pods with new spec are yet to be created.
			if deployment.Generation > deployment.Status.ObservedGeneration {
				r.Log.Info("Waiting for deployment rollout to begin",
					"deployment", klog.KObj(deployment))

				return false, nil
			}

			podList, err := common.GetBackupServicePodList(r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace)
			if err != nil {
				return false, err
			}

			if len(podList.Items) == 0 {
				r.Log.Info("No pod found for deployment",
					"deployment", klog.KObj(deployment))

				return false, nil
			}

			for idx := range podList.Items {
				pod := &podList.Items[idx]

				if err := utils.CheckPodFailed(pod); err != nil {
					return false, fmt.Errorf("pod %s failed: %w", utils.GetNamespacedName(pod), err)
				}

				if !utils.IsPodRunningAndReady(pod) {
					r.Log.Info("Pod is not ready", "pod", klog.KObj(pod))
					return false, nil
				}
			}

			if deployment.Status.Replicas != *deployment.Spec.Replicas {
				return false, nil
			}

			return true, nil
		},
	); err != nil {
		return err
	}

	r.Log.Info("Deployment is ready",
		"deployment", klog.KRef(r.aeroBackupService.Namespace, r.aeroBackupService.Name))

	return nil
}

func (r *SingleBackupServiceReconciler) setStatusPhase(
	ctx context.Context, phase asdbv1beta1.AerospikeBackupServicePhase,
) error {
	if r.aeroBackupService.Status.Phase != phase {
		r.aeroBackupService.Status.Phase = phase

		if err := r.Client.Status().Update(ctx, r.aeroBackupService); err != nil {
			return fmt.Errorf("set backup service status %s: %w", phase, err)
		}
	}

	return nil
}

func (r *SingleBackupServiceReconciler) updateStatus(ctx context.Context) error {
	svcConfig, err := r.getBackupServiceConfig()
	if err != nil {
		return err
	}

	status := r.CopySpecToStatus()
	status.ContextPath = svcConfig.contextPath
	status.Port = svcConfig.portInfo[asdbv1beta1.HTTPKey]
	status.Phase = asdbv1beta1.AerospikeBackupServiceCompleted

	r.aeroBackupService.Status = *status

	return r.Client.Status().Update(ctx, r.aeroBackupService)
}

func (r *SingleBackupServiceReconciler) CopySpecToStatus() *asdbv1beta1.AerospikeBackupServiceStatus {
	status := asdbv1beta1.AerospikeBackupServiceStatus{}
	status.Image = r.aeroBackupService.Spec.Image
	status.Config = r.aeroBackupService.Spec.Config
	statusServicePodSpec := lib.DeepCopy(r.aeroBackupService.Spec.PodSpec).(asdbv1beta1.ServicePodSpec)
	status.PodSpec = statusServicePodSpec
	//nolint:staticcheck // SA1019 - backward compat for deprecated Resources field
	status.Resources = r.aeroBackupService.Spec.Resources
	status.SecretMounts = r.aeroBackupService.Spec.SecretMounts
	status.Service = r.aeroBackupService.Spec.Service

	return &status
}
