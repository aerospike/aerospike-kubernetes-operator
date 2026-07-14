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
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

func replicaCount(n int32) *int32 { return &n }

// TestWaitForServerContainersRunning covers the three fast-path branches of
// waitForServerContainersRunning that complete without any sleep:
//
//  1. The pod name is in ignorablePodNames → skip entirely, return nil.
//  2. The pod's server container is already Running → return nil on first poll.
//  3. The pod's server container is in CrashLoopBackOff → PodFailed, return error on first poll.
func TestWaitForServerContainersRunning(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		stsName     = clusterName + "-1"
	)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
		Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(1)},
	}

	t.Run("ignorable pod is skipped without any k8s poll", func(t *testing.T) {
		aeroCluster := newTestAerospikeCluster(namespace, clusterName)
		r := newReconcilerWithObjects(newTestScheme(), aeroCluster, sts)

		// No pod is pre-created; if the function tried to Get it, fake client
		// would return NotFound → error.  The ignorable-skip path must prevent
		// that Get from ever being issued.
		podName := stsName + "-0"

		if err := r.waitForServerContainersRunning(sts, sets.New(podName)); err != nil {
			t.Errorf("expected nil for ignorable pod, got: %v", err)
		}
	})

	t.Run("pod with running server container succeeds on first poll", func(t *testing.T) {
		aeroCluster := newTestAerospikeCluster(namespace, clusterName)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stsName + "-0",
				Namespace: namespace,
			},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
			},
		}
		r := newReconcilerWithObjects(newTestScheme(), aeroCluster, sts, pod)

		if err := r.waitForServerContainersRunning(sts, sets.New[string]()); err != nil {
			t.Errorf("expected nil for running server container, got: %v", err)
		}
	})

	t.Run("server container in CrashLoopBackOff returns error on first poll", func(t *testing.T) {
		aeroCluster := newTestAerospikeCluster(namespace, clusterName)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      stsName + "-0",
				Namespace: namespace,
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
		}
		r := newReconcilerWithObjects(newTestScheme(), aeroCluster, sts, pod)

		if err := r.waitForServerContainersRunning(sts, sets.New[string]()); err == nil {
			t.Error("expected an error for CrashLoopBackOff server container, got nil")
		}
	})

	t.Run("multiple pods: second pod ignorable, first pod running — succeeds", func(t *testing.T) {
		aeroCluster := newTestAerospikeCluster(namespace, clusterName)
		multiSTS := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
			Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(2)},
		}

		pod0 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
			},
		}

		// pod-1 is ignorable; no pod object is pre-created for it so any Get
		// would return NotFound — the skip must fire before the Get.
		ignorable := sets.New(stsName + "-1")
		r := newReconcilerWithObjects(newTestScheme(), aeroCluster, multiSTS, pod0)

		if err := r.waitForServerContainersRunning(multiSTS, ignorable); err != nil {
			t.Errorf("expected nil when running pod + ignorable pod, got: %v", err)
		}
	})
}
