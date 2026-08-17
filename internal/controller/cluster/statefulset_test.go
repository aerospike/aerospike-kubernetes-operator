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
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/internal/controller/common"
)

func replicaCount(n int32) *int32 { return &n }

// TestWaitForSTSPodsServerReady covers the three fast-path branches of
// waitForSTSPodsServerReady that complete without any sleep:
//
//  1. The pod name is in ignorablePodNames → skip entirely, return nil.
//  2. The pod's server container is already ready → return nil on first poll.
//  3. The pod's server container is in CrashLoopBackOff → PodFailed, return error on first poll.
func TestWaitForSTSPodsServerReady(t *testing.T) {
	const stsName = clusterName + "-1"

	scheme := newTestScheme()
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
		Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(1)},
		Status:     appsv1.StatefulSetStatus{Replicas: 1},
	}

	t.Run("ignorable pod is skipped without any k8s poll", func(t *testing.T) {
		r := newReconcilerWithObjects(scheme, aeroCluster, sts)

		// No pod is pre-created; if the function tried to Get it, fake client
		// would return NotFound → error.  The ignorable-skip path must prevent
		// that Get from ever being issued.
		podName := stsName + "-0"

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New(podName)); err != nil {
			t.Errorf("expected nil for ignorable pod, got: %v", err)
		}
	})

	t.Run("pod with running server container succeeds on first poll", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster, sts, pod)

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New[string]()); err != nil {
			t.Errorf("expected nil for running server container, got: %v", err)
		}
	})

	t.Run("server container in CrashLoopBackOff returns error on first poll", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverCrashLoopContainer()},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster, sts, pod)

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New[string]()); err == nil {
			t.Error("expected an error for CrashLoopBackOff server container, got nil")
		}
	})

	t.Run("multiple pods: second pod ignorable, first pod running — succeeds", func(t *testing.T) {
		multiSTS := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
			Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(2)},
			Status:     appsv1.StatefulSetStatus{Replicas: 2},
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
		r := newReconcilerWithObjects(scheme, aeroCluster, multiSTS, pod0)

		if err := r.waitForSTSPodsServerReady(context.Background(), multiSTS, ignorable); err != nil {
			t.Errorf("expected nil when running pod + ignorable pod, got: %v", err)
		}
	})

	t.Run("pod stuck in ContainerCreating exhausts retries and returns ErrSTSNotReady", func(t *testing.T) {
		// Override retry knobs so the test completes in < 1 ms.
		origMax, origInterval := podStatusMaxRetry, podStatusRetryInterval
		podStatusMaxRetry = 1
		podStatusRetryInterval = 0

		defer func() { podStatusMaxRetry, podStatusRetryInterval = origMax, origInterval }()

		// Pod exists but server container is not ready (ContainerCreating — no
		// State set, Ready=false). CheckServerFailedWithGrace returns PodHealthy
		// for this state, so the function retries until the limit and wraps
		// common.ErrSTSNotReady.
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: asdbv1.AerospikeServerContainerName, Ready: false},
				},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster, sts, pod)

		err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New[string]())
		if !errors.Is(err, common.ErrSTSNotReady) {
			t.Errorf("expected common.ErrSTSNotReady for stuck pod, got: %v", err)
		}
	})
}
