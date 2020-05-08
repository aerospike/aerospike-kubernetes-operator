package utils

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	aerospikev1alpha1 "github.com/citrusleaf/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
)

var pkglog = log.New(log.Ctx{"module": "utils"})

const (
	ServiceTlsPort     = 4333
	ServiceTlsPortName = "svc-tls-port"
	ServicePort        = 3000
	ServicePortName    = "service"

	DockerHubImagePrefix = "docker.io/"

	HeartbeatTlsPort     = 3012
	HeartbeatTlsPortName = "hb-tls-port"
	HeartbeatPort        = 3002
	HeartbeatPortName    = "heartbeat"

	FabricTlsPort     = 3011
	FabricTlsPortName = "fb-tls-port"
	FabricPort        = 3001
	FabricPortName    = "fabric"

	InfoPort     = 3003
	InfoPortName = "info"

	// terminal state reasons when pod status is Pending
	// container image pull failed
	ReasonImagePullBackOff = "ImagePullBackOff"
	// unable to inspect image
	ReasonImageInspectError = "ImageInspectError"
	// general image pull error
	ReasonErrImagePull = "ErrImagePull"
	// get http error when pulling image from registry
	ReasonRegistryUnavailable = "RegistryUnavailable"
)

// ClusterNamespacedName return namespaced name
func ClusterNamespacedName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// IsPodRunningAndReady returns true if pod is in the PodRunning Phase, if it has a condition of PodReady.
// TODO: check if its is properly running, error crashLoop also passed in this
func IsPodRunningAndReady(pod *v1.Pod) bool {
	return !isTerminating(pod) && pod.Status.Phase == v1.PodRunning
}

// PodFailedStatus check if pod is in failed status
func PodFailedStatus(pod *v1.Pod) error {
	if isTerminating(pod) {
		return nil
	}

	// if the value of ".status.phase" is "Failed", trhe pod is trivially in a failure state
	if pod.Status.Phase == v1.PodFailed {
		return fmt.Errorf("Pod has failed status")
	}

	// grab the status of every container in the pod (including its init containers)
	containerStatus := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)

	// inspect the status of each individual container for common failure states
	for _, container := range containerStatus {
		// if the container is marked as "Terminated", check if its exit code is non-zero since this may still represent
		// a container that has terminated successfully (such as an init container)
		// if terminated := container.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
		// 	return fmt.Errorf("Pod has terminated status")
		// }
		// if the container is marked as "Waiting", check for common image-related errors
		if waiting := container.State.Waiting; waiting != nil && isImageError(waiting.Reason) {
			return fmt.Errorf("Pod image pull failed with given container message: %s", waiting.Message)
		}
	}

	// no failure state was found
	return nil
}

// IsPodInFailureState attempts to checks if the specified pod has reached an error condition from which it is not
// expected to recover.
func isPodInFailureState(pod *v1.Pod) bool {
	// if the value of ".status.phase" is "Failed", trhe pod is trivially in a failure state
	if pod.Status.Phase == v1.PodFailed {
		return true
	}

	// grab the status of every container in the pod (including its init containers)
	containerStatus := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)

	// inspect the status of each individual container for common failure states
	for _, container := range containerStatus {
		// if the container is marked as "Terminated", check if its exit code is non-zero since this may still represent
		// a container that has terminated successfully (such as an init container)
		if terminated := container.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
			return true
		}
		// if the container is marked as "Waiting", check for common image-related errors
		if waiting := container.State.Waiting; waiting != nil && isImageError(waiting.Reason) {
			return true
		}
	}

	// no failure state was found
	return false
}

//  IsImageEqual returns true if image name image1 is equal to image name image2.
func IsImageEqual(image1 string, image2 string) bool {
	desiredImage := strings.TrimPrefix(image1, DockerHubImagePrefix)
	actualImage := strings.TrimPrefix(image2, DockerHubImagePrefix)
	return actualImage == desiredImage
}

// isImageError indicated whether the specified reason corresponds to an error while pulling or inspecting a container
// image.
func isImageError(reason string) bool {
	return reason == ReasonErrImagePull || reason == ReasonImageInspectError || reason == ReasonImagePullBackOff || reason == ReasonRegistryUnavailable
}

// isCreated returns true if pod has been created and is maintained by the API server
func isCreated(pod *v1.Pod) bool {
	return pod.Status.Phase != ""
}

// isFailed returns true if pod has a Phase of PodFailed
func isFailed(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodFailed
}

// isTerminating returns true if pod's DeletionTimestamp has been set
func isTerminating(pod *v1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// IsPodUpgraded assume that all container have same image or take containerID
func IsPodUpgraded(pod *corev1.Pod, image string) bool {
	pkglog.Info("Checking pod image")

	if !IsPodRunningAndReady(pod) {
		return false
	}

	for _, ps := range pod.Status.ContainerStatuses {
		if !ps.Ready || !IsImageEqual(ps.Image, image) {
			return false
		}
	}
	return true
}

func readHTTPBody(resp *http.Response) error {
	defer resp.Body.Close()
	htmlData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return fmt.Errorf("Status: %v, http Body: %v", resp.Status, string(htmlData))
}

// ContainsString check whether list contains given string
func ContainsString(list []string, ele string) bool {
	for _, listEle := range list {
		if ele == listEle {
			return true
		}
	}
	return false
}

func removeString(list []string, ele string) []string {
	var newList []string
	for _, listEle := range list {
		if ele == listEle {
			continue
		}
		newList = append(newList, listEle)
	}
	return newList
}

// GetPod get pod from pod list by name
func GetPod(podName string, pods []corev1.Pod) *corev1.Pod {
	for _, p := range pods {
		if podName == p.Name {
			return &p
		}
	}
	return nil
}

// LabelsForAerospikeCluster returns the labels for selecting the resources
// belonging to the given AerospikeCluster CR name.
func LabelsForAerospikeCluster(name string) map[string]string {
	return map[string]string{"app": "aerospike-cluster", "aerospike-cluster_cr": name, "podName": ""}
}

// GetPodNames returns the pod names of the array of pods passed in
func GetPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}
