package utils

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// IsPodRunningAndReady returns true if pod is in the PodRunning Phase, if it has a condition of PodReady.
// TODO: check if its is properly running, error crashLoop also passed in this
func IsPodRunningAndReady(pod *corev1.Pod) bool {
	return !IsPodTerminating(pod) && pod.Status.Phase == corev1.PodRunning && isPodReady(pod)
}

// CheckPodFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state.
func CheckPodFailed(pod *corev1.Pod) error {
	if IsPodTerminating(pod) {
		return nil
	}

	// if the value of ".status.phase" is "Failed", the pod is trivially in a failure state
	if pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("pod %s has failed status", pod.Name)
	}

	// grab the status of every container in the pod (including its init containers)
	containerStatus := append(
		pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...,
	)

	// inspect the status of each individual container for common failure states
	for _, container := range containerStatus {
		// if the container is marked as "Terminated", check if its exit code is non-zero since this may still represent
		// a container that has terminated successfully (such as an init container)
		// if terminated := container.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
		// 	return fmt.Errorf("pod has terminated status")
		// }
		// if the container is marked as "Waiting", check for common image-related errors or container crashing.
		if waiting := container.State.Waiting; waiting != nil && (isPodImageError(waiting.Reason) || isPodCrashError(waiting.Reason)) {
			return fmt.Errorf(
				"pod failed message in container %s: %s reason: %s",
				container.Name, waiting.Message, waiting.Reason,
			)
		}
	}

	// no failure state was found
	return nil
}

// CheckPodImageFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state due to an image related error.
func CheckPodImageFailed(pod *corev1.Pod) error {
	if IsPodTerminating(pod) {
		return nil
	}

	// if the value of ".status.phase" is "Failed", trhe pod is trivially in a failure state
	if pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("pod has failed status")
	}

	// grab the status of every container in the pod (including its init containers)
	containerStatus := append(
		pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...,
	)

	// inspect the status of each individual container for common failure states
	for _, container := range containerStatus {
		// if the container is marked as "Terminated", check if its exit code is non-zero since this may still represent
		// a container that has terminated successfully (such as an init container)
		// if terminated := container.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
		// 	return fmt.Errorf("pod has terminated status")
		// }
		// if the container is marked as "Waiting", check for common image-related errors
		if waiting := container.State.Waiting; waiting != nil && isPodImageError(waiting.Reason) {
			return fmt.Errorf(
				"pod image pull failed with given container message: %s",
				waiting.Message,
			)
		}
	}

	// no failure state was found
	return nil
}

// IsPodCrashed returns true if pod is running and the aerospike container has crashed.
func IsPodCrashed(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		// Assume a pod that is not running has not crashed.
		return false
	}

	// Get aerospike server container status.
	ps := pod.Status.ContainerStatuses[0]
	return ps.State.Waiting != nil && isPodCrashError(ps.State.Waiting.Reason)
}

// isPodReady return true if all the container of the pod are in ready state
func isPodReady(pod *corev1.Pod) bool {
	for _, status := range pod.Status.ContainerStatuses {
		if !status.Ready {
			return false
		}
	}
	return true
}

// isPodCreated returns true if pod has been created and is maintained by the API server
func isPodCreated(pod *corev1.Pod) bool {
	return pod.Status.Phase != ""
}

// isPodFailed returns true if pod has a Phase of PodFailed
func isPodFailed(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodFailed
}

// IsPodTerminating returns true if pod's DeletionTimestamp has been set
func IsPodTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// IsPodUpgraded assume that all container have same image or take containerID
func IsPodUpgraded(
	pod *corev1.Pod, aeroCluster *asdbv1beta1.AerospikeCluster,
) bool {
	if !IsPodRunningAndReady(pod) {
		return false
	}

	return IsPodOnDesiredImage(pod, aeroCluster)
}

// IsPodOnDesiredImage indicates of pod is ready and on desired images for all containers.
func IsPodOnDesiredImage(
	pod *corev1.Pod, aeroCluster *asdbv1beta1.AerospikeCluster,
) bool {
	return allContainersAreOnDesiredImages(aeroCluster, pod.Spec.Containers) &&
		allContainersAreOnDesiredImages(aeroCluster, pod.Spec.InitContainers)
}

func allContainersAreOnDesiredImages(
	aeroCluster *asdbv1beta1.AerospikeCluster, containers []corev1.Container,
) bool {
	for _, ps := range containers {
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

// GetPodNames returns the pod names of the array of pods passed in
func GetPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
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

// GetRackIDFromPodName returns the rack id given a pod name.
func GetRackIDFromPodName(podName string) (*int, error) {
	parts := strings.Split(podName, "-")
	if len(parts) < 3 {
		return nil, fmt.Errorf("failed to get rackID from podName %s", podName)
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

// Exec executes a non interactive command on a pod.
func Exec(
	pod *corev1.Pod, container string, cmd []string,
	kubeClient *kubernetes.Clientset, kubeConfig *rest.Config,
) (string, string, error) {
	request := kubeClient.
		CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(
			&corev1.PodExecOptions{
				Command:   cmd,
				Container: container,
				Stdout:    true,
				Stderr:    true,
				TTY:       true,
			}, scheme.ParameterCodec,
		)

	exec, err := remotecommand.NewSPDYExecutor(
		kubeConfig, http.MethodPost, request.URL(),
	)
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(
		remotecommand.StreamOptions{
			Stdout: &stdout, Stderr: &stderr,
		},
	)
	return stdout.String(), stderr.String(), err
}

// isPodImageError indicates whether the specified reason corresponds to an error while pulling or inspecting a container
// image.
func isPodImageError(reason string) bool {
	return reason == ReasonErrImagePull || reason == ReasonImageInspectError || reason == ReasonImagePullBackOff || reason == ReasonRegistryUnavailable
}

// isPodCrashError indicates whether the specified reason corresponds to an crash of the container.
func isPodCrashError(reason string) bool {
	return strings.HasPrefix(reason, "Crash")
}
