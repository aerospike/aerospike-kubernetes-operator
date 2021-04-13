package utils

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	log "github.com/inconshreveable/log15"
	"golang.org/x/crypto/ripemd160"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
)

var pkglog = log.New(log.Ctx{"module": "utils"})

const (
	// DefaultRackID is the ID for the default rack created when no racks are specified.
	DefaultRackID = 0
	MaxRackID     = 1000000
	MinRackID     = 1

	ServiceTLSPort     = 4333
	ServiceTLSPortName = "svc-tls-port"
	ServicePort        = 3000
	ServicePortName    = "service"

	DockerHubImagePrefix = "docker.io/"

	HeartbeatTLSPort     = 3012
	HeartbeatTLSPortName = "hb-tls-port"
	HeartbeatPort        = 3002
	HeartbeatPortName    = "heartbeat"

	FabricTLSPort     = 3011
	FabricTLSPortName = "fb-tls-port"
	FabricPort        = 3001
	FabricPortName    = "fabric"

	InfoPort     = 3003
	InfoPortName = "info"

	// ReasonImagePullBackOff when pod status is Pending as container image pull failed.
	ReasonImagePullBackOff = "ImagePullBackOff"
	// ReasonImageInspectError is error inspecting image.
	ReasonImageInspectError = "ImageInspectError"
	// ReasonErrImagePull is general image pull error.
	ReasonErrImagePull = "ErrImagePull"
	// ReasonRegistryUnavailable is the http error when pulling image from registry.
	ReasonRegistryUnavailable = "RegistryUnavailable"

	aerospikeConfConfigMapPrefix = "aerospike-conf"
)

const (
	AerospikeServerContainerName     string = "aerospike-server"
	AerospikeServerInitContainerName string = "aerospike-init"
)

// ClusterNamespacedName return namespaced name
func ClusterNamespacedName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	return NamespacedName(aeroCluster.Namespace, aeroCluster.Name)
}

// ConfigMapName returns the name to use for a aeroCluster cluster's config map.
func ConfigMapName(aeroCluster *aerospikev1alpha1.AerospikeCluster) string {
	return fmt.Sprintf("%s-%s", aerospikeConfConfigMapPrefix, aeroCluster.Name)
}

// NamespacedName return namespaced name
func NamespacedName(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

// IsPodRunningAndReady returns true if pod is in the PodRunning Phase, if it has a condition of PodReady.
// TODO: check if its is properly running, error crashLoop also passed in this
func IsPodRunningAndReady(pod *v1.Pod) bool {
	return !IsTerminating(pod) && pod.Status.Phase == v1.PodRunning && isPodReady(pod)
}

// CheckPodFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state.
func CheckPodFailed(pod *v1.Pod) error {
	if IsTerminating(pod) {
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
		// if the container is marked as "Waiting", check for common image-related errors or container crashing.
		if waiting := container.State.Waiting; waiting != nil && (isImageError(waiting.Reason) || isCrashError(waiting.Reason)) {
			return fmt.Errorf("Pod failed message: %s reason: %s", waiting.Message, waiting.Reason)
		}
	}

	// no failure state was found
	return nil
}

// CheckPodImageFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state due to an image related error.
func CheckPodImageFailed(pod *v1.Pod) error {
	if IsTerminating(pod) {
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

// IsCrashed returns true if pod is running and the aerospike container has crashed.
func IsCrashed(pod *v1.Pod) bool {
	if pod.Status.Phase != v1.PodRunning {
		// Assume a pod that is not running has not crashed.
		return false
	}

	// Get aerospike server container status.
	ps := pod.Status.ContainerStatuses[0]
	return ps.State.Waiting != nil && isCrashError(ps.State.Waiting.Reason)
}

// isPodReady return true if all the container of the pod are in ready state
func isPodReady(pod *v1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}
	return true
}

// IsImageEqual returns true if image name image1 is equal to image name image2.
func IsImageEqual(image1 string, image2 string) bool {
	desiredImageWithVersion := strings.TrimPrefix(image1, DockerHubImagePrefix)
	actualImageWithVersion := strings.TrimPrefix(image2, DockerHubImagePrefix)

	desiredRegistry, desiredName, desiredVersion := ParseDockerImageTag(desiredImageWithVersion)
	actualRegistry, actualName, actualVersion := ParseDockerImageTag(actualImageWithVersion)

	// registry name, image name and version should match.
	return desiredRegistry == actualRegistry && desiredName == actualName && (desiredVersion == actualVersion || (desiredVersion == ":latest" && actualVersion == "") || (actualVersion == ":latest" && desiredVersion == ""))
}

// GetImageVersion extracts image version from image string/tag.
func GetImageVersion(tag string) (string, error) {
	_, _, version := ParseDockerImageTag(tag)

	if version == "" || strings.ToLower(version) == "latest" {
		return "", fmt.Errorf("Image version is mandatory for image: %v", tag)
	}

	return version, nil
}

// ParseDockerImageTag parses input tag into registry, name and version.
func ParseDockerImageTag(tag string) (registry string, name string, version string) {
	if tag == "" {
		return "", "", ""
	}
	r := regexp.MustCompile(`(?P<registry>[^/]+/)?(?P<image>[^:]+)(?P<version>:.+)?`)
	matches := r.FindStringSubmatch(tag)
	return matches[1], matches[2], strings.TrimPrefix(matches[3], ":")
}

// isImageError indicates whether the specified reason corresponds to an error while pulling or inspecting a container
// image.
func isImageError(reason string) bool {
	return reason == ReasonErrImagePull || reason == ReasonImageInspectError || reason == ReasonImagePullBackOff || reason == ReasonRegistryUnavailable
}

// isCrashError indicates whether the specified reason corresponds to an crash of the container.
func isCrashError(reason string) bool {
	return strings.HasPrefix(reason, "Crash")
}

// isCreated returns true if pod has been created and is maintained by the API server
func isCreated(pod *v1.Pod) bool {
	return pod.Status.Phase != ""
}

// isFailed returns true if pod has a Phase of PodFailed
func isFailed(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodFailed
}

// IsTerminating returns true if pod's DeletionTimestamp has been set
func IsTerminating(pod *v1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// IsPVCTerminating returns true if pvc's DeletionTimestamp has been set
func IsPVCTerminating(pvc *corev1.PersistentVolumeClaim) bool {
	return pvc.DeletionTimestamp != nil
}

// GetDesiredImage returns the desired image for the input containerName from the aeroCluster spec.
func GetDesiredImage(aeroCluster *aerospikev1alpha1.AerospikeCluster, containerName string) (string, error) {
	if containerName == AerospikeServerContainerName {
		return aeroCluster.Spec.Image, nil
	}

	for _, sidecar := range aeroCluster.Spec.PodSpec.Sidecars {
		if sidecar.Name == containerName {
			return sidecar.Image, nil
		}
	}

	return "", fmt.Errorf("Container %s not found", containerName)
}

// IsPodUpgraded assume that all container have same image or take containerID
func IsPodUpgraded(pod *corev1.Pod, aeroCluster *aerospikev1alpha1.AerospikeCluster) bool {
	pkglog.Info("Checking pod image")

	if !IsPodRunningAndReady(pod) {
		return false
	}

	return IsPodOnDesiredImage(pod, aeroCluster)
}

// getPodContainerStatus provides status for container in a pod if present else nil.
// Abstracted into a function because in come cases pod.Status.ContainerStatuses was empty.
func getPodContainerStatus(pod *corev1.Pod, containerName string) *corev1.ContainerStatus {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == containerName {
			return &status
		}
	}

	return nil
}

// IsPodOnDesiredImage indicates of pod is ready and on desired images for all containers.
func IsPodOnDesiredImage(pod *corev1.Pod, aeroCluster *aerospikev1alpha1.AerospikeCluster) bool {
	for _, ps := range pod.Spec.Containers {
		desiredImage, err := GetDesiredImage(aeroCluster, ps.Name)
		if err != nil {
			// Maybe a deleted sidecar. Ignore.
			desiredImage = ps.Image
		}

		// TODO: Should we check status here?
		// status may not be ready due to any bad config (e.g. bad podSpec).
		// Due to this check, flow will be stuck at this place (upgradeImage)

		// status := getPodContainerStatus(pod, ps.Name)
		// if status == nil || !status.Ready || !IsImageEqual(ps.Image, desiredImage) {
		// 	return false
		// }

		if !IsImageEqual(ps.Image, desiredImage) {
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

// RemoveString removes ele from list if ele is present in the list. The original order is preserved.
func RemoveString(list []string, ele string) []string {
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
func LabelsForAerospikeCluster(clName string) map[string]string {
	return map[string]string{"app": "aerospike-cluster", "aerospike.com/cr": clName}
}

// LabelsForAerospikeClusterRack returns the labels for specific rack
func LabelsForAerospikeClusterRack(clName string, rackID int) map[string]string {
	labels := LabelsForAerospikeCluster(clName)
	labels["aerospike.com/rack-id"] = strconv.Itoa(rackID)
	return labels
}

// GetPodNames returns the pod names of the array of pods passed in
func GetPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

// PrettyPrint any data
func PrettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "    ")
	return string(s)
}

// GetHash return ripmd160 hash for given string
func GetHash(str string) (string, error) {
	var digest []byte
	hash := ripemd160.New()
	hash.Reset()
	if _, err := hash.Write([]byte(str)); err != nil {
		return "", err
	}
	res := hash.Sum(digest)
	return hex.EncodeToString(res), nil
}

func GetRackIDFromPodName(podName string) (*int, error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return nil, fmt.Errorf("Failed to get rackID from podName %s", podName)
	}
	// Podname format stsname-ordinal
	// stsname ==> clustername-rackid
	rackStr := parts[len(parts)-2]
	rackID, err := strconv.Atoi(rackStr)
	if err != nil {
		return nil, err
	}
	return &rackID, nil
}

func GetRackIDFromStatefulSetName(statefulSetName string) (*int, error) {
	parts := strings.Split(statefulSetName, "-")
	if len(parts) < 2 {
		return nil, fmt.Errorf("Failed to get rackID from statefulSetName %s", statefulSetName)
	}
	// stsname ==> clustername-rackid
	rackStr := parts[len(parts)-1]
	rackID, err := strconv.Atoi(rackStr)
	if err != nil {
		return nil, err
	}
	return &rackID, nil
}
