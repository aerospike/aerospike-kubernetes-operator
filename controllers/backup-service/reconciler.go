package backupservice

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	app "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/controllers/common"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const BackupConfigYAML = "aerospike-backup-service.yml"

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
	if err := r.reconcileConfigMap(); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileDeployment(); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileService(); err != nil {
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

		// Set AerospikeCluster instance as the owner and controller
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

	data := cm.Data[BackupConfigYAML]

	if err := yaml.Unmarshal([]byte(data), &currentDataMap); err != nil {
		return err
	}

	currentDataMap["service"] = desiredDataMap["service"]
	currentDataMap[common.BackupPoliciesKey] = desiredDataMap[common.BackupPoliciesKey]
	currentDataMap[common.StorageKey] = desiredDataMap[common.StorageKey]
	currentDataMap["secret-agent"] = desiredDataMap["secret-agent"]

	updatedConfig, err := yaml.Marshal(currentDataMap)
	if err != nil {
		return err
	}

	cm.Data[BackupConfigYAML] = string(updatedConfig)

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
	data[BackupConfigYAML] = string(r.aeroBackupService.Spec.Config.Raw)

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

		return nil
	}

	r.Log.Info(
		"Backup Service deployment already exist. Updating existing deployment if required",
		"name", getBackupServiceName(r.aeroBackupService),
	)

	oldResourceVersion := deploy.ResourceVersion

	deployObj, err := r.getDeploymentObject()
	if err != nil {
		return err
	}

	deploy.Spec = deployObj.Spec

	if err = r.Client.Update(context.TODO(), &deploy, common.UpdateOption); err != nil {
		return fmt.Errorf("failed to update Backup service deployment: %v", err)
	}

	if oldResourceVersion != deploy.ResourceVersion {
		r.Log.Info("Deployment spec is updated, will result in rolling restart")
		return nil
	}

	hash, err := utils.GetHash(string(r.aeroBackupService.Spec.Config.Raw))
	if err != nil {
		return err
	}

	// If there is a change in config hash, then restart the deployment pod
	if r.aeroBackupService.Status.ConfigHash != "" && hash != r.aeroBackupService.Status.ConfigHash {
		r.Log.Info("Config hash is updated, will result in rolling restart")

		var podList corev1.PodList

		labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(r.aeroBackupService.Name))
		listOps := &client.ListOptions{
			Namespace: r.aeroBackupService.Namespace, LabelSelector: labelSelector,
		}

		err = r.Client.List(context.TODO(), &podList, listOps)
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

		return nil
	}

	return nil
}

func getBackupServiceName(aeroBackupService *asdbv1beta1.AerospikeBackupService) types.NamespacedName {
	return types.NamespacedName{Name: aeroBackupService.Name, Namespace: aeroBackupService.Namespace}
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
					// TODO: Finalise on this. Who should create this SA?
					ServiceAccountName: common.AerospikeBackupService,
					Containers: []corev1.Container{
						{
							Name:            common.AerospikeBackupService,
							Image:           r.aeroBackupService.Spec.Image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts:    volumeMounts,
							Resources:       r.aeroBackupService.Spec.Resources,
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
		MountPath: fmt.Sprintf("/etc/aerospike-backup-service/%s", BackupConfigYAML),
		SubPath:   BackupConfigYAML,
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

func (r *SingleBackupServiceReconciler) updateStatus() error {
	svcConfig, err := r.getBackupServiceConfig()
	if err != nil {
		return err
	}

	hash, err := utils.GetHash(string(r.aeroBackupService.Spec.Config.Raw))
	if err != nil {
		return err
	}

	r.aeroBackupService.Status.ContextPath = svcConfig.contextPath
	r.aeroBackupService.Status.Port = svcConfig.portInfo[common.HTTPKey]
	r.aeroBackupService.Status.ConfigHash = hash

	r.Log.Info(fmt.Sprintf("Updating status: %+v", r.aeroBackupService.Status))

	return r.Client.Status().Update(context.Background(), r.aeroBackupService)
}
