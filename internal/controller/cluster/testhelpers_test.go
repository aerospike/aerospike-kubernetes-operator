/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// serverContainer builds a ContainerStatus for the Aerospike server container.
func serverContainer(ready bool) corev1.ContainerStatus {
	cs := corev1.ContainerStatus{
		Name:  asdbv1.AerospikeServerContainerName,
		Ready: ready,
	}
	if ready {
		cs.State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	}

	return cs
}

// serverCrashLoopContainer returns a server ContainerStatus in CrashLoopBackOff.
func serverCrashLoopContainer() corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: asdbv1.AerospikeServerContainerName,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		},
	}
}

// sidecarContainer builds a ContainerStatus for a sidecar container.
//

func sidecarContainer(name string, ready, crashLoop bool) corev1.ContainerStatus {
	cs := corev1.ContainerStatus{Name: name, Ready: ready}
	if crashLoop {
		cs.State = corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
		}
	}

	return cs
}

// runningPod creates a pod in Running phase created well outside the grace period.
func runningPod(name string, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: statuses,
		},
	}
}

// recentPod creates a pod that is within the default grace period (~10 s old).
func recentPod(name string, statuses ...corev1.ContainerStatus) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Second)),
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: statuses,
		},
	}
}
