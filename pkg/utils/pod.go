package utils

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// PodHealthState represents the health state of a pod
type PodHealthState int

const (
	// PodHealthy indicates the pod is running normally
	PodHealthy PodHealthState = iota
	// PodFailedInGrace indicates the pod has failed but is within the grace period
	PodFailedInGrace
	// PodFailed indicates the pod has failed and grace period has expired (or grace not allowed)
	PodFailed
)

// PodState contains the health state and failure reason for a pod
type PodState struct {
	Reason string // Human-readable failure reason (empty if healthy)
	State  PodHealthState
}

// IsPodRunningAndReady returns true if pod is in the PodRunning Phase, if it has a condition of PodReady.
// TODO: check if its is properly running, error crashLoop also passed in this
func IsPodRunningAndReady(pod *corev1.Pod) bool {
	return !IsPodTerminating(pod) && pod.Status.Phase == corev1.PodRunning && IsPodReady(pod)
}

// CheckPodFailed checks if pod has failed or has terminated or is in an irrecoverable waiting state.
// Returns an error if the pod has failed, nil otherwise.
func CheckPodFailed(pod *corev1.Pod) error {
	podState := CheckPodFailedWithGrace(pod, false)
	if podState.State == PodFailed {
		return fmt.Errorf("%s", podState.Reason)
	}

	return nil
}

// CheckPodFailedWithGrace checks if pod has failed or has terminated or is in an irrecoverable waiting state.
// Returns PodState containing:
// - State: PodHealthy, PodFailedInGrace, or PodFailed
// - Reason: Human-readable description of failure (empty if healthy)
func CheckPodFailedWithGrace(pod *corev1.Pod, allowGrace bool) PodState {
	if IsPodTerminating(pod) {
		return PodState{State: PodHealthy}
	}

	failureReason := checkPodFailureReason(pod, true)
	if failureReason == "" {
		return PodState{State: PodHealthy}
	}

	if allowGrace {
		since := getPodFailureSince(pod)
		grace := GetFailedPodGracePeriod()

		if time.Since(since) < grace {
			return PodState{
				State:  PodFailedInGrace,
				Reason: failureReason,
			}
		}
	}

	return PodState{
		State:  PodFailed,
		Reason: failureReason,
	}
}

// CheckServerFailedWithGrace checks if the pod's Aerospike server container (or
// a blocking init container) has a terminal failure, optionally within a grace
// period. Sidecar container failures are intentionally ignored — use
// CheckSidecarFailedWithGrace for those.
// Returns PodState containing:
// - State: PodHealthy, PodFailedInGrace, or PodFailed
// - Reason: Human-readable description of failure (empty if healthy)
func CheckServerFailedWithGrace(pod *corev1.Pod, allowGrace bool) PodState {
	if IsPodTerminating(pod) {
		return PodState{State: PodHealthy}
	}

	failureReason := checkPodFailureReason(pod, false)
	if failureReason == "" {
		return PodState{State: PodHealthy}
	}

	if allowGrace {
		since := getPodFailureSince(pod)
		grace := GetFailedPodGracePeriod()

		if time.Since(since) < grace {
			return PodState{
				State:  PodFailedInGrace,
				Reason: failureReason,
			}
		}
	}

	return PodState{
		State:  PodFailed,
		Reason: failureReason,
	}
}

// checkPodFailureReason inspects the pod's phase, scheduling conditions, and
// container statuses and returns a human-readable reason string if the pod is in
// a failure state, or an empty string if the pod is healthy.
// It does NOT apply any grace-period logic.
// When includeSidecarFailures is true, failing sidecar containers are also
// treated as a pod failure; otherwise only init-container and server-container
// failures are reported.
func checkPodFailureReason(pod *corev1.Pod, includeSidecarFailures bool) string {
	if pod.Status.Phase == corev1.PodFailed {
		return fmt.Sprintf("pod %s has failed status with reason: %s and message: %s",
			pod.Name, pod.Status.Reason, pod.Status.Message)
	}

	if pod.Status.Phase == corev1.PodPending {
		if isPodUnschedulable, reason := IsPodReasonUnschedulable(pod); isPodUnschedulable {
			return fmt.Sprintf("pod %s is in unschedulable state and reason is %s", pod.Name, reason)
		}
	}

	return checkContainerFailures(pod, includeSidecarFailures)
}

// checkContainerFailures checks for container-level failure states and returns a failure reason.
// Returns an empty string if no failure is found.
// All init container failures are reported because a failing init container prevents the
// aerospike server from starting. Among regular containers, the aerospike server container
// is always checked. Sidecar containers are only checked when includeSidecarFailures is true.
func checkContainerFailures(pod *corev1.Pod, includeSidecarFailures bool) string {
	// Check all init containers — their failures block server startup entirely.
	for idx := range pod.Status.InitContainerStatuses {
		container := &pod.Status.InitContainerStatuses[idx]
		if waiting := container.State.Waiting; waiting != nil &&
			(isPodImageError(waiting.Reason) || isPodCrashError(waiting.Reason) || isPodError(waiting.Reason)) {
			return fmt.Sprintf(
				"pod failed message in container %s: %s reason: %s",
				container.Name, waiting.Message, waiting.Reason,
			)
		}
	}

	// Inspect regular containers: always check the aerospike server container;
	// check sidecars only when includeSidecarFailures is true.
	for idx := range pod.Status.ContainerStatuses {
		container := &pod.Status.ContainerStatuses[idx]
		if container.Name != asdbv1.AerospikeServerContainerName && !includeSidecarFailures {
			continue
		}

		if waiting := container.State.Waiting; waiting != nil &&
			(isPodImageError(waiting.Reason) || isPodCrashError(waiting.Reason) || isPodError(waiting.Reason)) {
			return fmt.Sprintf(
				"pod failed message in container %s: %s reason: %s",
				container.Name, waiting.Message, waiting.Reason,
			)
		}
	}

	// no failure state was found
	return ""
}

// IsAerospikeServerRunning returns true if the aerospike server container is
// ready — i.e. the process is running and the TCP readiness probe has passed.
// Checking Ready (rather than just State.Running) ensures the Aerospike node
// is actually accepting connections before it is considered healthy. Sidecar
// containers are deliberately skipped so a crashing sidecar never influences
// this result.
func IsAerospikeServerRunning(pod *corev1.Pod) bool {
	if IsPodTerminating(pod) || pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for idx := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[idx]
		if cs.Name != asdbv1.AerospikeServerContainerName {
			continue
		}

		return cs.Ready
	}

	return false
}

// IsPodReady returns true if all containers in the pod are in the ready state.
func IsPodReady(pod *corev1.Pod) bool {
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

// GetRackIDAndRevisionFromPodName returns the rack id and revision from a given pod name.
func GetRackIDAndRevisionFromPodName(clusterName, podName string) (rackID int, rackRevision string, err error) {
	prefix := clusterName + "-"

	rackAndPodIndexPart := strings.TrimPrefix(podName, prefix)
	// parts contain only the rack-id, rack-revision (optional), and pod-index.
	parts := strings.Split(rackAndPodIndexPart, "-")

	if len(parts) < 2 {
		// Needs at least rack-id and pod-index.
		return 0, "", fmt.Errorf(
			"invalid pod name format %q: expected at least <rack-id>-<pod-index> after cluster name", podName,
		)
	}

	// The pod index is always the last part. We don't need it here, but we use its position.
	// The rack ID is always the first part.
	rackIDStr := parts[0]

	// The rack-revision is everything in between the rack ID and the pod index.
	if len(parts) == 2 {
		// Format: <cluster-name>-<rack-id>-<pod-index>
		// Example: parts is ["0", "0"]
		rackRevision = ""
	} else {
		// Format: <cluster-name>-<rack-id>-<rack-revision>-<pod-index>
		// Example: parts is ["0", "a", "0"]
		// Revision should be "a"
		rackRevision = strings.Join(parts[1:len(parts)-1], "-")
	}

	rackID, err = strconv.Atoi(rackIDStr)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse rackID from pod name %q: %w", podName, err)
	}

	return rackID, rackRevision, nil
}

// Exec executes a non-interactive command on a pod.
func Exec(podNamespacedName types.NamespacedName, container string, cmd []string, kubeClient *kubernetes.Clientset,
	kubeConfig *rest.Config) (stdoutStr, stderrStr string, err error) {
	request := kubeClient.
		CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(podNamespacedName.Namespace).
		Name(podNamespacedName.Name).
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

func IsPodReasonUnschedulable(pod *corev1.Pod) (isPodUnschedulable bool, reason string) {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && (condition.Reason == corev1.PodReasonUnschedulable ||
			condition.Reason == corev1.PodReasonSchedulerError) {
			return true, condition.Message
		}
	}

	return false, ""
}

// getPodFailureSince returns the earliest time when the pod entered any failure state.
// This includes Unschedulable conditions, Failed phase, or creation time as fallback.
func getPodFailureSince(pod *corev1.Pod) time.Time {
	var earliestFailure *time.Time

	// Check all pod conditions in a single loop to find the earliest failure time
	for _, condition := range pod.Status.Conditions {
		var conditionTime time.Time

		isFailureCondition := false

		//nolint:exhaustive // Only required condition types are checked
		switch condition.Type {
		case corev1.PodScheduled:
			// Check for Unschedulable condition
			if condition.Reason == corev1.PodReasonUnschedulable || condition.Reason == corev1.PodReasonSchedulerError {
				conditionTime = condition.LastTransitionTime.Time
				isFailureCondition = true
			}
		case corev1.PodReady, corev1.PodInitialized:
			// Check for Failed phase transition (Ready or Initialized = False)
			if condition.Status == corev1.ConditionFalse {
				conditionTime = condition.LastTransitionTime.Time
				isFailureCondition = true
			}
		}

		// Track the earliest failure condition time
		if isFailureCondition {
			if earliestFailure == nil || conditionTime.Before(*earliestFailure) {
				earliestFailure = &conditionTime
			}
		}
	}

	// If we found a failure condition, use its time; otherwise fall back to creation time
	if earliestFailure == nil {
		earliestFailure = &pod.CreationTimestamp.Time
	}

	return *earliestFailure
}

// GetFailedPodGracePeriod reads FAILED_POD_GRACE_PERIOD_SECONDS env.
// Defaults to 60 seconds (1 minute) if unset or invalid.
func GetFailedPodGracePeriod() time.Duration {
	sec := asdbv1.DefaultFailedPodGracePeriodSeconds

	if v := os.Getenv("FAILED_POD_GRACE_PERIOD_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			sec = parsed
		}
	}

	return time.Duration(sec) * time.Second
}

// IsAerospikePod checks if the given pod is an Aerospike pod
func IsAerospikePod(pod *corev1.Pod) bool {
	labels := pod.GetLabels()
	if labels == nil {
		return false
	}

	// Check for Aerospike-specific labels
	appLabel, hasAppLabel := labels[asdbv1.AerospikeAppLabel]
	_, hasCustomResourceLabel := labels[asdbv1.AerospikeCustomResourceLabel]

	// Pod is considered an Aerospike pod if it has both required labels
	return hasAppLabel && appLabel == asdbv1.AerospikeAppLabelValue && hasCustomResourceLabel
}
