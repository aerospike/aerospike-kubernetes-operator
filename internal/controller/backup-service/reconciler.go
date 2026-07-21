package backupservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	app "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		// finishReconcile returns the error to assign here so we avoid *error params; recErr is Reconcile's named return.
		recErr = r.finishReconcile(ctx, result, recErr)
	}()

	// Skip reconcile if the backup service version is less than 3.0.0.
	// This is to avoid rolling restart of the backup service pods after AKO upgrade
	if _, err := asdbv1beta1.ValidateBackupSvcVersion(r.aeroBackupService.Spec.Image); err != nil {
		r.Log.Info("Skipping reconcile, backup service version unsupported",
			"minVersion", asdbv1beta1.BackupSvcMinSupportedVersion, "err", err)

		return reconcile.Result{}, nil
	}

	if !r.aeroBackupService.DeletionTimestamp.IsZero() {
		r.Log.Info("Deleted AerospikeBackupService")
		r.Recorder.Eventf(
			r.aeroBackupService, corev1.EventTypeNormal, ReasonDeleted,
			"Successfully deleted backup service resources",
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
			"ConfigMapReconcileFailed", "Failed to reconcile backup service ConfigMap %s",
			utils.GetNamespacedNameString(r.aeroBackupService))

		return ctrl.Result{}, err
	}

	if err := r.reconcileService(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"ServiceReconcileFailed", "Failed to reconcile Service for backup service %s",
			utils.GetNamespacedNameString(r.aeroBackupService))

		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"DeploymentReconcileFailed", "Failed to reconcile backup service Deployment %s",
			utils.GetNamespacedNameString(r.aeroBackupService))

		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx); err != nil {
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeWarning,
			"StatusUpdateFailed", "Failed to update status")

		return ctrl.Result{}, err
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
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get backup service ConfigMap: %w", err)
		}

		r.Log.Info("Creating backup service ConfigMap",
			"configMap", utils.GetNamespacedName(r.aeroBackupService))

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
			return fmt.Errorf("set controller reference on backup service ConfigMap: %w", err)
		}

		if err = r.Create(ctx, cm, common.CreateOption); err != nil {
			return fmt.Errorf("create backup service ConfigMap: %w", err)
		}

		r.Log.Info("Created backup service ConfigMap",
			"configMap", utils.GetNamespacedName(cm))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ConfigMapCreated",
			"Created backup service ConfigMap %s", utils.GetNamespacedNameString(r.aeroBackupService))

		return nil
	}

	r.Log.Info("Updating backup service ConfigMap if required",
		"configMap", utils.GetNamespacedName(cm))

	desiredDataMap := make(map[string]interface{})
	currentDataMap := make(map[string]interface{})

	if err := yaml.Unmarshal(r.aeroBackupService.Spec.Config.Raw, &desiredDataMap); err != nil {
		return fmt.Errorf("unmarshal backup service spec config: %w", err)
	}

	data := cm.Data[asdbv1beta1.BackupServiceConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &currentDataMap); err != nil {
		return fmt.Errorf("unmarshal backup service ConfigMap data: %w", err)
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
		return fmt.Errorf("marshal backup service config: %w", err)
	}

	cm.Data[asdbv1beta1.BackupServiceConfigYAML] = string(updatedConfig)

	if err = r.Update(ctx, cm, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service ConfigMap: %w", err)
	}

	r.Log.Info("Updated backup service ConfigMap",
		"configMap", utils.GetNamespacedName(cm))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ConfigMapUpdated",
		"Updated backup service ConfigMap %s", utils.GetNamespacedNameString(r.aeroBackupService))

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
		if !apierrors.IsNotFound(err) {
			return err
		}

		r.Log.Info("Creating backup service Deployment",
			"deployment", utils.GetNamespacedName(r.aeroBackupService))

		deployment, err = r.getDeploymentObject()
		if err != nil {
			return err
		}

		// Set AerospikeBackupService instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroBackupService, deployment, r.Scheme,
		)
		if err != nil {
			return fmt.Errorf("set controller reference on backup service Deployment: %w", err)
		}

		err = r.Create(ctx, deployment, common.CreateOption)
		if err != nil {
			return fmt.Errorf("create backup service Deployment: %w", err)
		}

		r.Log.Info("Created backup service Deployment",
			"deployment", utils.GetNamespacedName(deployment))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "DeploymentCreated",
			"Created backup service Deployment %s", utils.GetNamespacedNameString(r.aeroBackupService))

		return r.waitForDeploymentToBeReady(ctx)
	}

	r.Log.Info("Updating backup service Deployment if required",
		"deployment", utils.GetNamespacedName(deployment))

	oldResourceVersion := deployment.ResourceVersion

	desiredDeployObj, err := r.getDeploymentObject()
	if err != nil {
		return err
	}

	deployment.Spec = desiredDeployObj.Spec

	if err = r.Update(ctx, deployment, common.UpdateOption); err != nil {
		return fmt.Errorf("update backup service Deployment: %w", err)
	}

	r.Log.Info("Updated backup service Deployment",
		"deployment", utils.GetNamespacedName(deployment))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "DeploymentUpdated",
		"Updated backup service Deployment %s", utils.GetNamespacedNameString(r.aeroBackupService))

	if oldResourceVersion != deployment.ResourceVersion {
		r.Log.Info("Updated backup service Deployment spec, rolling restart of backup service Pod",
			"deployment", utils.GetNamespacedName(deployment))

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
		return nil, fmt.Errorf("get backup service Deployment: %w", err)
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
		return fmt.Errorf("fetch backup service config from API: %w", err)
	}

	desiredData, err := common.GetBackupSvcConfigFromCM(ctx, r.Client, backupSvc)
	if err != nil {
		return err
	}

	synced, err := common.IsBackupSvcFullConfigSynced(apiBackupSvcConfig, desiredData, r.Log)
	if err != nil {
		return fmt.Errorf("check backup service config sync: %w", err)
	}

	if synced {
		r.Log.Info("Skipping update, backup service config is already latest")
		return nil
	}

	r.Log.Info("Detected backup service config mismatch, reloading config")

	apiBackupSvcConfigData, err := yaml.Marshal(apiBackupSvcConfig)
	if err != nil {
		return fmt.Errorf("marshal backup service config from API: %w", err)
	}

	if err := yaml.Unmarshal(apiBackupSvcConfigData, &currentConfig); err != nil {
		return fmt.Errorf("unmarshal backup service config from API: %w", err)
	}

	if err := yaml.Unmarshal([]byte(desiredData), &desiredConfig); err != nil {
		return fmt.Errorf("unmarshal desired backup service config: %w", err)
	}

	if err := validation.ValidateStaticFieldChanges(&currentConfig, &desiredConfig); err != nil {
		r.Log.Info("Static config change detected, rolling restart of backup service Pod",
			"err", err)
		// In case of static config change restart the backup service pod
		return r.restartBackupSvcPod(ctx)
	}

	if err := common.ReloadBackupServiceConfigInPods(ctx, r.Client, backupServiceClient, r.Log, backupSvc); err != nil {
		return fmt.Errorf("reload backup service config: %w", err)
	}

	return nil
}

func (r *SingleBackupServiceReconciler) restartBackupSvcPod(ctx context.Context) error {
	podList, err := common.GetBackupServicePodList(ctx, r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace)
	if err != nil {
		return err
	}

	for idx := range podList.Items {
		pod := &podList.Items[idx]

		err = r.Delete(ctx, pod)
		if err != nil {
			return fmt.Errorf("delete backup service Pod %s: %w", utils.GetNamespacedNameString(pod), err)
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
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get Service for backup service: %w", err)
		}

		r.Log.Info("Creating Service for backup service",
			"service", utils.GetNamespacedName(r.aeroBackupService))

		svc, err := r.getServiceObject()
		if err != nil {
			return err
		}

		// Set AerospikeBackupService instance as the owner and controller
		err = controllerutil.SetControllerReference(
			r.aeroBackupService, svc, r.Scheme,
		)
		if err != nil {
			return fmt.Errorf("set controller reference on Service for backup service: %w", err)
		}

		err = r.Create(ctx, svc, common.CreateOption)
		if err != nil {
			return fmt.Errorf("create Service for backup service: %w", err)
		}

		r.Log.Info("Created Service for backup service",
			"service", utils.GetNamespacedName(svc))
		r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ServiceCreated",
			"Created Service for backup service %s", utils.GetNamespacedNameString(r.aeroBackupService))

		return nil
	}

	r.Log.Info("Updating Service for backup service if required",
		"service", utils.GetNamespacedName(r.aeroBackupService))

	svc, err := r.getServiceObject()
	if err != nil {
		return err
	}

	service.Spec = svc.Spec

	if err = r.Update(ctx, &service, common.UpdateOption); err != nil {
		return fmt.Errorf("update Service for backup service: %w", err)
	}

	r.Log.Info("Updated Service for backup service",
		"service", utils.GetNamespacedName(r.aeroBackupService))
	r.Recorder.Eventf(r.aeroBackupService, corev1.EventTypeNormal, "ServiceUpdated",
		"Updated Service for backup service %s", utils.GetNamespacedNameString(r.aeroBackupService))

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
		return nil, fmt.Errorf("unmarshal backup service spec config: %w", err)
	}

	if _, ok := config[asdbv1beta1.ServiceKey]; !ok {
		r.Log.Info("Missing section in backup service config, using defaults",
			"section", asdbv1beta1.ServiceKey)

		return &defaultServiceConfig, nil
	}

	svc, ok := config[asdbv1beta1.ServiceKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("backup service config %q section is not in correct format", asdbv1beta1.ServiceKey)
	}

	if _, ok = svc[asdbv1beta1.HTTPKey]; !ok {
		r.Log.Info("Missing section in backup service config, using defaults",
			"section", asdbv1beta1.HTTPKey)

		return &defaultServiceConfig, nil
	}

	httpConf, ok := svc[asdbv1beta1.HTTPKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("backup service config %q section is not in correct format", asdbv1beta1.HTTPKey)
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

	r.Log.Info("Waiting for backup service Deployment to be ready",
		"deployment", utils.GetNamespacedName(r.aeroBackupService),
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
				r.Log.Info("Waiting for backup service Deployment rollout to begin",
					"deployment", utils.GetNamespacedName(deployment))

				return false, nil
			}

			podList, err := common.GetBackupServicePodList(
				pollCtx, r.Client, r.aeroBackupService.Name, r.aeroBackupService.Namespace,
			)
			if err != nil {
				return false, err
			}

			if len(podList.Items) == 0 {
				r.Log.Info("No Pod found for backup service Deployment",
					"deployment", utils.GetNamespacedName(deployment))

				return false, nil
			}

			for idx := range podList.Items {
				pod := &podList.Items[idx]

				if err := utils.CheckPodFailed(pod); err != nil {
					return false, fmt.Errorf("check Deployment Pod %s: %w", utils.GetNamespacedNameString(pod), err)
				}

				if !utils.IsPodRunningAndReady(pod) {
					r.Log.Info("Pod is not ready", "pod", utils.GetNamespacedName(pod))
					return false, nil
				}
			}

			if deployment.Status.Replicas != *deployment.Spec.Replicas {
				return false, nil
			}

			return true, nil
		},
	); err != nil {
		return fmt.Errorf("wait for backup service Deployment ready: %w", err)
	}

	r.Log.Info("Backup service Deployment is ready",
		"deployment", utils.GetNamespacedName(r.aeroBackupService))

	return nil
}

func (r *SingleBackupServiceReconciler) setStatusPhase(
	ctx context.Context, phase asdbv1beta1.AerospikeBackupServicePhase,
) error {
	if r.aeroBackupService.Status.Phase != phase {
		r.aeroBackupService.Status.Phase = phase

		if err := r.Client.Status().Update(ctx, r.aeroBackupService); err != nil {
			return fmt.Errorf("set AerospikeBackupService status phase %s: %w", phase, err)
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

	if err := r.Client.Status().Update(ctx, r.aeroBackupService); err != nil {
		return fmt.Errorf("update AerospikeBackupService status: %w", err)
	}

	return nil
}

// finishReconcile runs at end of Reconcile; return value is assigned to Reconcile's named recErr in defer.
func (r *SingleBackupServiceReconciler) finishReconcile(ctx context.Context, result ctrl.Result, recErr error) error {
	logValues := reconcileExitLogValues(result, recErr)
	if recErr != nil {
		if err := r.setStatusPhase(ctx, asdbv1beta1.AerospikeBackupServiceError); err != nil {
			recErr = errors.Join(
				recErr,
				fmt.Errorf("set AerospikeBackupService error phase: %w", err),
			)
		}

		r.Log.Error(recErr, "Reconcile failed", logValues...)

		return recErr
	}

	r.Log.Info("Reconcile completed", logValues...)

	return nil
}

func reconcileExitLogValues(result ctrl.Result, recErr error) []interface{} {
	const resultKey = "result"

	if recErr != nil {
		return []interface{}{resultKey, "error"}
	}

	if result.RequeueAfter > 0 {
		return []interface{}{
			resultKey, "requeue",
			"requeueAfter", result.RequeueAfter.String(),
		}
	}

	return []interface{}{resultKey, "success"}
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
