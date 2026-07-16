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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stretchr/testify/require"
)

// TestCreateOrUpdatePodService_PublishNotReadyAddresses verifies that the
// NodePort service created for a pod has PublishNotReadyAddresses=true.
// This flag was added in the sidecar-failure PR so that the service continues
// to forward traffic to sidecar-failing pods (whose Aerospike server is still
// reachable) even though the pod is not yet Ready.
func TestCreateOrUpdatePodService_PublishNotReadyAddresses(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		podName     = "test-cluster-1-0"
		clusterUID  = "test-uid"
	)

	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	// A non-empty UID is required by controllerutil.SetControllerReference to
	// populate the OwnerReference correctly.
	aeroCluster.UID = clusterUID

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})
	r.Recorder = record.NewFakeRecorder(10)

	require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace))

	svc := &corev1.Service{}
	require.NoError(t, r.Get(
		context.Background(),
		types.NamespacedName{Name: podName, Namespace: namespace},
		svc,
	))

	if !svc.Spec.PublishNotReadyAddresses {
		t.Error("expected PublishNotReadyAddresses=true on the pod NodePort service, got false")
	}

	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("expected ServiceTypeNodePort, got %s", svc.Spec.Type)
	}
}

// TestCreateOrUpdatePodService_Idempotent verifies that calling
// createOrUpdatePodService a second time (service already exists) does not
// return an error (the update path is exercised).
func TestCreateOrUpdatePodService_Idempotent(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		podName     = "test-cluster-1-0"
		clusterUID  = "test-uid"
	)

	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	aeroCluster.UID = clusterUID

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})
	r.Recorder = record.NewFakeRecorder(10)

	require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace), "first call")
	require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace), "second call (update path)")

	// Service should still have the correct flag after update.
	svc := &corev1.Service{}
	require.NoError(t, r.Get(
		context.Background(),
		types.NamespacedName{Name: podName, Namespace: namespace},
		svc,
	))

	if !svc.Spec.PublishNotReadyAddresses {
		t.Error("expected PublishNotReadyAddresses=true after idempotent call, got false")
	}
}

// TestCreateOrUpdatePodService_IgnorablePodNames is a guard to ensure that
// even when IgnoreSidecarFailure is set, the service is still created with
// PublishNotReadyAddresses=true (the field is unconditional in the spec).
func TestCreateOrUpdatePodService_IgnoreSidecarFailure(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		podName     = "test-cluster-1-0"
		clusterUID  = "test-uid"
	)

	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	aeroCluster.UID = clusterUID

	boolTrue := true
	aeroCluster.Spec.IgnoreSidecarFailure = &boolTrue

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})
	r.Recorder = record.NewFakeRecorder(10)

	require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace))

	svc := &corev1.Service{}
	require.NoError(t, r.Get(
		context.Background(),
		types.NamespacedName{Name: podName, Namespace: namespace},
		svc,
	))

	if !svc.Spec.PublishNotReadyAddresses {
		t.Error("expected PublishNotReadyAddresses=true even with IgnoreSidecarFailure, got false")
	}

	// Verify the unused ignorable set variable doesn't cause test issues.
	_ = sets.New[string]()
}
