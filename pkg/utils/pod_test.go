package utils

import (
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

func TestGetFailedPodGracePeriod(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "default value when env var not set",
			envValue: "",
			expected: time.Duration(asdbv1.DefaultFailedPodGracePeriodSeconds) * time.Second,
		},
		{
			name:     "custom value from env var",
			envValue: "120",
			expected: 120 * time.Second,
		},
		{
			name:     "invalid env var falls back to default",
			envValue: "invalid",
			expected: time.Duration(asdbv1.DefaultFailedPodGracePeriodSeconds) * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env var
			originalEnv := os.Getenv("FAILED_POD_GRACE_PERIOD_SECONDS")

			defer func() {
				if originalEnv != "" {
					os.Setenv("FAILED_POD_GRACE_PERIOD_SECONDS", originalEnv)
				} else {
					os.Unsetenv("FAILED_POD_GRACE_PERIOD_SECONDS")
				}
			}()

			// Set test env var
			if tt.envValue != "" {
				os.Setenv("FAILED_POD_GRACE_PERIOD_SECONDS", tt.envValue)
			} else {
				os.Unsetenv("FAILED_POD_GRACE_PERIOD_SECONDS")
			}

			result := GetFailedPodGracePeriod()
			if result != tt.expected {
				t.Errorf("GetFailedPodGracePeriod() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCheckPodFailedWithGrace(t *testing.T) {
	now := time.Now()

	tests := []struct {
		pod           *corev1.Pod
		name          string
		description   string
		allowGrace    bool
		expectReason  bool
		expectedState PodHealthState
	}{
		{
			name: "healthy running pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			allowGrace:    true,
			expectedState: PodHealthy,
			expectReason:  false,
			description:   "should be healthy",
		},
		{
			name: "failed pod within grace period",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "failed-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Second)), // Recent
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Error",
				},
			},
			allowGrace:    true,
			expectedState: PodFailedInGrace,
			expectReason:  true,
			description:   "should be in grace period when allowGrace=true",
		},
		{
			name: "failed pod within grace period but allowGrace=false",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "failed-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Second)),
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Error",
				},
			},
			allowGrace:    false,
			expectedState: PodFailed,
			expectReason:  true,
			description:   "should be failed when allowGrace=false",
		},
		{
			name: "unschedulable pod within grace period",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "unschedulable-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-30 * time.Second)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{
						{
							Type:               corev1.PodScheduled,
							Status:             corev1.ConditionFalse,
							Reason:             corev1.PodReasonUnschedulable,
							LastTransitionTime: metav1.NewTime(now.Add(-25 * time.Second)),
						},
					},
				},
			},
			allowGrace:    true,
			expectedState: PodFailedInGrace,
			expectReason:  true,
			description:   "unschedulable pod should be in grace period",
		},
		{
			name: "terminating pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "terminating-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
					DeletionTimestamp: &metav1.Time{Time: now.Add(-1 * time.Minute)},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			allowGrace:    true,
			expectedState: PodHealthy,
			expectReason:  false,
			description:   "terminating pod should not be considered failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podState := CheckPodFailedWithGrace(tt.pod, tt.allowGrace)

			if podState.State != tt.expectedState {
				t.Errorf("CheckPodFailedWithGrace() state = %v, expected %v (%s)",
					podState.State, tt.expectedState, tt.description)
			}

			hasReason := podState.Reason != ""
			if hasReason != tt.expectReason {
				t.Errorf("CheckPodFailedWithGrace() has reason = %v, expected %v (%s)",
					hasReason, tt.expectReason, tt.description)
			}
		})
	}
}

func TestIsAerospikeServerRunning(t *testing.T) {
	// serverStatus builds a ContainerStatus for the aerospike-server container.
	serverStatus := func(ready bool) corev1.ContainerStatus {
		return corev1.ContainerStatus{
			Name:  asdbv1.AerospikeServerContainerName,
			Ready: ready,
		}
	}

	// sidecarStatus builds a ContainerStatus for a sidecar container.
	sidecarStatus := func(name string, ready bool) corev1.ContainerStatus {
		return corev1.ContainerStatus{Name: name, Ready: ready}
	}

	now := metav1.Now()

	tests := []struct {
		pod      *corev1.Pod
		name     string
		expected bool
	}{
		{
			name: "terminating pod returns false regardless of container state",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{serverStatus(true)},
				},
			},
			expected: false,
		},
		{
			name: "non-Running phase (Pending) returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase:             corev1.PodPending,
					ContainerStatuses: []corev1.ContainerStatus{serverStatus(true)},
				},
			},
			expected: false,
		},
		{
			name: "server container Ready=true returns true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{serverStatus(true)},
				},
			},
			expected: true,
		},
		{
			name: "server container Ready=false returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase:             corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{serverStatus(false)},
				},
			},
			expected: false,
		},
		{
			// Key invariant: a crashing sidecar must not influence the result.
			name: "server Ready=true with crashing sidecar returns true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						serverStatus(true),
						sidecarStatus("monitoring-sidecar", false),
					},
				},
			},
			expected: true,
		},
		{
			name: "server Ready=false with healthy sidecar returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						serverStatus(false),
						sidecarStatus("monitoring-sidecar", true),
					},
				},
			},
			expected: false,
		},
		{
			name: "no server container in statuses returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						sidecarStatus("monitoring-sidecar", true),
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAerospikeServerRunning(tt.pod)
			if got != tt.expected {
				t.Errorf("IsAerospikeServerRunning() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCheckServerFailedWithGrace_IgnoresSidecarFailures(t *testing.T) {
	// The central invariant: CheckServerFailedWithGrace must return PodHealthy
	// for a pod whose Aerospike server is running but whose sidecar is crashing.
	// CheckPodFailedWithGrace on the same pod returns PodFailed because it
	// includes sidecar failures. This contrast is the core behaviour change.
	sidecarCrashingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pod-with-crashing-sidecar",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-5 * time.Minute)),
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  asdbv1.AerospikeServerContainerName,
					Ready: true,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{},
					},
				},
				{
					Name: "crashing-sidecar",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "CrashLoopBackOff",
							Message: "back-off 5m0s restarting failed container",
						},
					},
				},
			},
		},
	}

	serverState := CheckServerFailedWithGrace(sidecarCrashingPod, false)
	if serverState.State != PodHealthy {
		t.Errorf("CheckServerFailedWithGrace() = %v, want PodHealthy — sidecar failure must be ignored",
			serverState.State)
	}

	// Contrast: the full-pod check should see the crashing sidecar as a failure.
	fullState := CheckPodFailedWithGrace(sidecarCrashingPod, false)
	if fullState.State != PodFailed {
		t.Errorf("CheckPodFailedWithGrace() = %v, want PodFailed — sidecar failure should be reported",
			fullState.State)
	}
}

func TestCheckServerFailedWithGrace(t *testing.T) {
	now := time.Now()

	tests := []struct {
		pod           *corev1.Pod
		name          string
		expectedState PodHealthState
		allowGrace    bool
	}{
		{
			name: "terminating pod is healthy",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "terminating",
					DeletionTimestamp: &metav1.Time{Time: now},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning},
			},
			allowGrace:    false,
			expectedState: PodHealthy,
		},
		{
			name: "server and sidecars all healthy",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "healthy",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			allowGrace:    false,
			expectedState: PodHealthy,
		},
		{
			name: "server container in CrashLoopBackOff is PodFailed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "server-crash",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: asdbv1.AerospikeServerContainerName,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			allowGrace:    false,
			expectedState: PodFailed,
		},
		{
			name: "server container in CrashLoopBackOff within grace period is PodFailedInGrace",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "server-crash-grace",
					CreationTimestamp: metav1.NewTime(now.Add(-10 * time.Second)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name: asdbv1.AerospikeServerContainerName,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			allowGrace:    true,
			expectedState: PodFailedInGrace,
		},
		{
			name: "failing init container is PodFailed",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "init-crash",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "aerospike-init",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			allowGrace:    false,
			expectedState: PodFailed,
		},
		{
			// Sidecar-only failure: server is healthy, sidecar is in CrashLoopBackOff.
			// CheckServerFailedWithGrace must return PodHealthy (sidecars ignored).
			name: "crashing sidecar with healthy server is PodHealthy",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "sidecar-crash",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  asdbv1.AerospikeServerContainerName,
							Ready: true,
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						},
						{
							Name: "crashing-sidecar",
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason: "CrashLoopBackOff",
								},
							},
						},
					},
				},
			},
			allowGrace:    false,
			expectedState: PodHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckServerFailedWithGrace(tt.pod, tt.allowGrace)
			if got.State != tt.expectedState {
				t.Errorf("CheckServerFailedWithGrace() state = %v, want %v", got.State, tt.expectedState)
			}
		})
	}
}

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		pod      *corev1.Pod
		name     string
		expected bool
	}{
		{
			name:     "no container statuses returns true (vacuously all ready)",
			pod:      &corev1.Pod{Status: corev1.PodStatus{}},
			expected: true,
		},
		{
			name: "all containers ready returns true",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: asdbv1.AerospikeServerContainerName, Ready: true},
						{Name: "sidecar", Ready: true},
					},
				},
			},
			expected: true,
		},
		{
			name: "server ready but sidecar not ready returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: asdbv1.AerospikeServerContainerName, Ready: true},
						{Name: "sidecar", Ready: false},
					},
				},
			},
			expected: false,
		},
		{
			name: "server not ready returns false",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: asdbv1.AerospikeServerContainerName, Ready: false},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPodReady(tt.pod)
			if got != tt.expected {
				t.Errorf("IsPodReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCheckPodFailed(t *testing.T) {
	now := time.Now()

	tests := []struct {
		pod           *corev1.Pod
		name          string
		description   string
		expectedError bool
	}{
		{
			name: "healthy pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "healthy-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expectedError: false,
			description:   "should not have error for healthy pod",
		},
		{
			name: "failed pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "failed-pod",
					CreationTimestamp: metav1.NewTime(now.Add(-5 * time.Minute)),
				},
				Status: corev1.PodStatus{
					Phase:  corev1.PodFailed,
					Reason: "Error",
				},
			},
			expectedError: true,
			description:   "should have error for failed pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPodFailed(tt.pod)
			hasError := err != nil

			if hasError != tt.expectedError {
				t.Errorf("CheckPodFailed() error = %v, expected error = %v (%s)",
					err, tt.expectedError, tt.description)
			}
		})
	}
}
