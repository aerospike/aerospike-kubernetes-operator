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
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	clientGoScheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

const (
	namespace   = "test-ns"
	clusterName = "test-cluster"
)

func newTestScheme() *k8sRuntime.Scheme {
	s := k8sRuntime.NewScheme()
	_ = clientGoScheme.AddToScheme(s)
	_ = asdbv1.AddToScheme(s)

	return s
}

// newReconcilerWithObjects builds a minimal SingleClusterReconciler backed by a
// fake k8s client pre-populated with the supplied objects.
func newReconcilerWithObjects(
	scheme *k8sRuntime.Scheme,
	aeroCluster *asdbv1.AerospikeCluster,
	objects ...client.Object,
) *SingleClusterReconciler {
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&asdbv1.AerospikeCluster{}).
		Build()

	return &SingleClusterReconciler{
		Client:      fakeClient,
		aeroCluster: aeroCluster,
		Log:         logr.Discard(),
	}
}

// clusterLabels returns the standard AKO labels for a cluster with the given name.
func clusterLabels(name string) map[string]string {
	return utils.LabelsForAerospikeCluster(name)
}

// TestCheckPreviouslyFailedCluster covers the three paths of
// checkPreviouslyFailedCluster that do not call recoverFailedCreate (which
// would require a full cluster-deletion pipeline).  The paths are:
//
//  1. Non-empty status  → fast-path success (no k8s API calls needed).
//  2. STS found, healthy pod → ReconcileSuccess (early return from pod loop).
//  3. STS found, all pods in grace period → ReconcileRequeueAfter.
func TestCheckPreviouslyFailedCluster(t *testing.T) {
	scheme := newTestScheme()

	// reusableSTS is a StatefulSet that carries the cluster labels so it is
	// returned by getClusterSTSList.
	reusableSTS := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName + "-sts",
			Namespace: namespace,
			Labels:    clusterLabels(clusterName),
		},
	}

	t.Run("non-empty status returns success without any k8s calls", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Status: asdbv1.AerospikeClusterStatus{
				AerospikeClusterStatusSpec: asdbv1.AerospikeClusterStatusSpec{
					// Non-nil AerospikeConfig marks the status as non-empty.
					AerospikeConfig: &asdbv1.AerospikeConfigSpec{},
				},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster)

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false for a cluster with non-empty status")
		}

		if !res.IsSuccess {
			t.Errorf("expected ReconcileSuccess, got err=%v requeueAfter=%v", res.Err, res.Result.RequeueAfter)
		}
	})

	t.Run("STS present with healthy pod returns success", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}

		healthyPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName + "-0-0",
				Namespace:         namespace,
				Labels:            clusterLabels(clusterName),
				CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Minute)),
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  asdbv1.AerospikeServerContainerName,
						Ready: true,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()},
						},
					},
				},
			},
		}

		r := newReconcilerWithObjects(scheme, aeroCluster, reusableSTS, healthyPod)

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false when a healthy pod is present")
		}

		if !res.IsSuccess {
			t.Errorf("expected ReconcileSuccess, got err=%v requeueAfter=%v", res.Err, res.Result.RequeueAfter)
		}
	})

	t.Run("STS present with pod in grace period returns requeue", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}

		// A pod created only 10 seconds ago that has already entered the Failed
		// phase is within the default 60-second grace period.
		recentFailedPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              clusterName + "-0-0",
				Namespace:         namespace,
				Labels:            clusterLabels(clusterName),
				CreationTimestamp: metav1.NewTime(time.Now().Add(-10 * time.Second)),
			},
			Status: corev1.PodStatus{
				Phase:  corev1.PodFailed,
				Reason: "Error",
			},
		}

		r := newReconcilerWithObjects(scheme, aeroCluster, reusableSTS, recentFailedPod)

		failed, res := r.checkPreviouslyFailedCluster(context.Background())

		if failed {
			t.Error("expected failed=false when pods are still within the grace period")
		}

		if res.IsSuccess {
			t.Error("expected a requeue result, not ReconcileSuccess")
		}

		if res.Err != nil {
			t.Errorf("expected no error for grace-period requeue, got %v", res.Err)
		}

		if res.Result.RequeueAfter == 0 {
			t.Error("expected Result.RequeueAfter > 0 for grace-period path")
		}

		wantAfter := time.Duration(asdbv1.RequeueIntervalSeconds10) * time.Second
		if res.Result.RequeueAfter != wantAfter {
			t.Errorf("expected RequeueAfter=%v, got %v", wantAfter, res.Result.RequeueAfter)
		}
	})
}

// TestSetStatusPhase covers the three paths of setStatusPhase:
//
//  1. Phase already matches — returns nil immediately, no API call issued.
//  2. Phase differs — patches the API server and updates r.aeroCluster in memory.
//  3. Patch fails — returns an error and leaves r.aeroCluster.Status.Phase unchanged.
func TestSetStatusPhase(t *testing.T) {
	scheme := newTestScheme()

	t.Run("no-op when phase already matches", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Status:     asdbv1.AerospikeClusterStatus{Phase: asdbv1.AerospikeClusterInProgress},
		}
		// aeroCluster is intentionally absent from the fake store: any API call
		// would return NotFound, proving the no-op path issues none.
		r := newReconcilerWithObjects(scheme, aeroCluster)

		err := r.setStatusPhase(context.Background(), asdbv1.AerospikeClusterInProgress)
		require.NoError(t, err)
		require.Equal(t, asdbv1.AerospikeClusterInProgress, r.aeroCluster.Status.Phase)
	})

	t.Run("patches API server and updates in-memory phase", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		}
		// Register the cluster in the fake store so Status().Patch() can locate it.
		r := newReconcilerWithObjects(scheme, aeroCluster, aeroCluster)

		err := r.setStatusPhase(context.Background(), asdbv1.AerospikeClusterInProgress)
		require.NoError(t, err)

		// In-memory phase must be updated immediately.
		require.Equal(t, asdbv1.AerospikeClusterInProgress, r.aeroCluster.Status.Phase)

		// The fake client's stored status must reflect the patch.
		got := &asdbv1.AerospikeCluster{}
		require.NoError(t, r.Get(context.Background(), client.ObjectKeyFromObject(aeroCluster), got))
		require.Equal(t, asdbv1.AerospikeClusterInProgress, got.Status.Phase)
	})

	t.Run("error leaves in-memory phase unchanged", func(t *testing.T) {
		aeroCluster := &asdbv1.AerospikeCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Status:     asdbv1.AerospikeClusterStatus{Phase: asdbv1.AerospikeClusterCompleted},
		}
		patchErr := fmt.Errorf("simulated patch failure")
		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			SubResourcePatch: func(
				_ context.Context, _ client.Client, _ string,
				_ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption,
			) error {
				return patchErr
			},
		}, aeroCluster)

		err := r.setStatusPhase(context.Background(), asdbv1.AerospikeClusterInProgress)
		require.Error(t, err)
		// In-memory phase must NOT have changed.
		require.Equal(t, asdbv1.AerospikeClusterCompleted, r.aeroCluster.Status.Phase)
	})
}
