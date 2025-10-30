package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsPodReasonUnschedulable(t *testing.T) {
	type args struct {
		pod *corev1.Pod
	}

	const message = "0/22 nodes are available: 20 node(s) didn't match Pod's node affinity/selector. " +
		"preemption: 0/22\n nodes are available: " +
		"22 Preemption is not helpful for scheduling."

	tests := []struct {
		args                   args
		name                   string
		wantReason             string
		wantIsPodUnschedulable bool
	}{
		{
			name: "[SchedulerError] scheduler error message",
			args: args{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  corev1.PodReasonSchedulerError,
								Message: "Some Error",
							},
						},
					},
				},
			},
			wantIsPodUnschedulable: true,
			wantReason:             "Some Error",
		},
		{
			name: "[Unschedulable] empty last transition time",
			args: args{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  corev1.PodReasonUnschedulable,
								Message: message,
							},
						},
					},
				},
			},
			wantIsPodUnschedulable: true,
			wantReason:             message,
		},
		{
			name: "[Unschedulable] recent unschedulable",
			args: args{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:               corev1.PodScheduled,
								Status:             corev1.ConditionFalse,
								Reason:             corev1.PodReasonUnschedulable,
								LastTransitionTime: metav1.Now(),
								Message:            message,
							},
						},
					},
				},
			},
			wantIsPodUnschedulable: false,
		},
		{
			name: "[Unschedulable] old unschedulable",
			args: args{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:               corev1.PodScheduled,
								Status:             corev1.ConditionFalse,
								Reason:             corev1.PodReasonUnschedulable,
								LastTransitionTime: metav1.NewTime(metav1.Now().Add(-2 * PodSchedulerDelay)),
								Message:            message,
							},
						},
					},
				},
			},
			wantIsPodUnschedulable: true,
			wantReason:             message,
		},
		{
			name: "[Scheduled] scheduled successfully",
			args: args{
				pod: &corev1.Pod{
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodScheduled,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
			},
			wantIsPodUnschedulable: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIsPodUnschedulable, gotReason := IsPodReasonUnschedulable(tt.args.pod)
			if gotIsPodUnschedulable != tt.wantIsPodUnschedulable {
				t.Errorf("IsPodReasonUnschedulable() gotIsPodUnschedulable = %v, want %v",
					gotIsPodUnschedulable, tt.wantIsPodUnschedulable)
			}

			if gotReason != tt.wantReason {
				t.Errorf("IsPodReasonUnschedulable() gotReason = %v, want %v", gotReason, tt.wantReason)
			}
		})
	}
}
