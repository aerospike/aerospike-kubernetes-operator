package utils

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

	if pod.Status.Phase == corev1.PodPending && isPodReasonUnschedulable(pod) {
		return fmt.Errorf("pod %s is in unschedulable state", pod.Name)
	}

	// grab the status of every container in the pod (including its init containers)
	var containerStatus []corev1.ContainerStatus
	containerStatus = append(containerStatus, pod.Status.InitContainerStatuses...)
	containerStatus = append(containerStatus, pod.Status.ContainerStatuses...)

	// inspect the status of each individual container for common failure states
	for idx := range containerStatus {
		container := &containerStatus[idx]
		// if the container is marked as "Terminated", check if its exit code is non-zero since this may still represent
		// a container that has terminated successfully (such as an init container)
		// if terminated := container.State.Terminated; terminated != nil && terminated.ExitCode != 0 {
		// 	return fmt.Errorf("pod has terminated status")
		// }
		// if the container is marked as "Waiting", check for common image-related errors or container crashing.
		if waiting := container.State.Waiting; waiting != nil &&
			(isPodImageError(waiting.Reason) || isPodCrashError(waiting.Reason) || isPodError(waiting.Reason)) {
			return fmt.Errorf(
				"pod failed message in container %s: %s reason: %s",
				container.Name, waiting.Message, waiting.Reason,
			)
		}
	}

	// no failure state was found
	return nil
}

// CheckPodImageFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state due to an
// image related error.
func CheckPodImageFailed(pod *corev1.Pod) error {
	if IsPodTerminating(pod) {
		return nil
	}

	// if the value of ".status.phase" is "Failed", the pod is trivially in a failure state
	if pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("pod has failed status")
	}

	// grab the status of every container in the pod (including its init containers)
	var containerStatus []corev1.ContainerStatus
	containerStatus = append(containerStatus, pod.Status.InitContainerStatuses...)
	containerStatus = append(containerStatus, pod.Status.ContainerStatuses...)

	// inspect the status of each individual container for common failure states
	for idx := range containerStatus {
		container := &containerStatus[idx]
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

// isPodReady return true if all the container of the pod are in ready state
func isPodReady(pod *corev1.Pod) bool {
	statuses := pod.Status.ContainerStatuses
	for idx := range statuses {
		if !statuses[idx].Ready {
			return false
		}
	}

	return true
}

// IsPodTerminating returns true if pod's DeletionTimestamp has been set
func IsPodTerminating(pod *corev1.Pod) bool {
	return pod.DeletionTimestamp != nil
}

// GetPod get pod from pod list by name
func GetPod(podName string, pods []corev1.Pod) *corev1.Pod {
	for idx := range pods {
		if podName == pods[idx].Name {
			return &pods[idx]
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
) (stdoutStr, stderrStr string, err error) {
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

	err = exec.StreamWithContext(
		context.TODO(),
		remotecommand.StreamOptions{
			Stdout: &stdout, Stderr: &stderr,
		},
	)

	return stdout.String(), stderr.String(), err
}

// isPodImageError indicates whether the specified reason corresponds to an error while pulling or inspecting a
// container image.
func isPodImageError(reason string) bool {
	return reason == ReasonErrImagePull || reason == ReasonImageInspectError ||
		reason == ReasonImagePullBackOff || reason == ReasonRegistryUnavailable
}

// isPodCrashError indicates whether the specified reason corresponds to a crash of the container.
func isPodCrashError(reason string) bool {
	return strings.HasPrefix(reason, "Crash")
}

// isPodError indicates whether the specified reason corresponds to a generic error like CreateContainerConfigError
// in the container.
// https://github.com/kubernetes/kubernetes/blob/bad4c8c464d7f92db561de9a0073aab89bbd61c8/pkg/kubelet/kuberuntime/kuberuntime_container.go
//
//nolint:lll // URL
func isPodError(reason string) bool {
	return strings.HasSuffix(reason, "Error")
}

func isPodReasonUnschedulable(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Reason == corev1.PodReasonUnschedulable {
			return true
		}
	}

	return false
}
