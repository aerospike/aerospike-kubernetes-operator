package common

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

// GetConfigSection returns the section of the config with the given name.
func GetConfigSection(config map[string]interface{}, section string) (map[string]interface{}, error) {
	sectionIface, ok := config[section]
	if !ok {
		return map[string]interface{}{}, nil
	}

	sectionMap, ok := sectionIface.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not a map", section)
	}

	return sectionMap, nil
}

func GetBackupServicePodList(k8sClient client.Client, name, namespace string) (*corev1.PodList, error) {
	var podList corev1.PodList

	labelSelector := labels.SelectorFromSet(utils.LabelsForAerospikeBackupService(name))
	listOps := &client.ListOptions{
		Namespace: namespace, LabelSelector: labelSelector,
	}

	if err := k8sClient.List(context.TODO(), &podList, listOps); err != nil {
		return nil, err
	}

	return &podList, nil
}

func RefreshBackupServiceConfigInPods(k8sClient client.Client, name, namespace string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		podList, err := GetBackupServicePodList(k8sClient,
			name,
			namespace,
		)
		if err != nil {
			return fmt.Errorf("failed to get backup service pod list, error: %v", err)
		}

		for idx := range podList.Items {
			pod := podList.Items[idx]
			annotations := pod.Annotations

			if annotations == nil {
				annotations = make(map[string]string)
			}

			count, ok := annotations[v1beta1.RefreshKey]
			if ok {
				countInt, _ := strconv.Atoi(count)
				annotations[v1beta1.RefreshKey] = fmt.Sprintf("%d", countInt+1)
			} else {
				annotations[v1beta1.RefreshKey] = "1"
			}

			pod.Annotations = annotations

			if err := k8sClient.Update(context.TODO(), &pod); err != nil {
				return err
			}
		}

		// Waiting for 1 second so that pods get the latest configMap update.
		time.Sleep(1 * time.Second)

		return nil
	})
}
