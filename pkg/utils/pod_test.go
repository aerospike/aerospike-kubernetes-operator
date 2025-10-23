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
		pod             *corev1.Pod
		name            string
		description     string
		allowGrace      bool
		expectedInGrace bool
		expectedError   bool
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
			allowGrace:      true,
			expectedInGrace: false,
			expectedError:   false,
			description:     "should not be failed or in grace",
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
			allowGrace:      true,
			expectedInGrace: true,
			expectedError:   true,
			description:     "should be in grace period when allowGrace=true",
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
			allowGrace:      false,
			expectedInGrace: false,
			expectedError:   true,
			description:     "should not be in grace when allowGrace=false",
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
			allowGrace:      true,
			expectedInGrace: true,
			expectedError:   true,
			description:     "unschedulable pod should be in grace period",
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
			allowGrace:      true,
			expectedInGrace: false,
			expectedError:   false,
			description:     "terminating pod should not be considered failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inGrace, err := CheckPodFailedWithGrace(tt.pod, tt.allowGrace)

			if inGrace != tt.expectedInGrace {
				t.Errorf("CheckPodFailedWithGrace() inGrace = %v, expected %v (%s)",
					inGrace, tt.expectedInGrace, tt.description)
			}

			hasError := err != nil
			if hasError != tt.expectedError {
				t.Errorf("CheckPodFailedWithGrace() error = %v, expected error = %v (%s)",
					err, tt.expectedError, tt.description)
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
