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
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

const (
	namespace   = "test-ns"
	clusterName = "test-cluster"
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

// getMinimalCluster returns a cluster with no spec, which is all most condition tests need.
// Generation is set because ObservedGeneration is stamped from it. Use
// newTestAerospikeCluster when the code under test reads the spec (e.g. updateStatus).
func getMinimalCluster() *asdbv1.AerospikeCluster {
	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       clusterName,
			Namespace:  namespace,
			Generation: minimalClusterGeneration,
		},
	}
}

// getCluster fetches ac from the fake API server.
func getCluster(t *testing.T, c client.Client, ac *asdbv1.AerospikeCluster) *asdbv1.AerospikeCluster {
	t.Helper()

	got := &asdbv1.AerospikeCluster{}
	if err := c.Get(t.Context(), types.NamespacedName{Name: ac.Name, Namespace: ac.Namespace}, got); err != nil {
		t.Fatalf("get cluster: %v", err)
	}

	return got
}

func newTestReconciler(
	t *testing.T, aeroCluster *asdbv1.AerospikeCluster, funcs *interceptor.Funcs,
	existingObjects ...client.Object,
) *SingleClusterReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, asdbv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	// Seed aeroCluster itself so code under test can Get/Update/Patch it. It is deep-copied
	// so the tracker's copy stays distinct from the reconciler's in-memory r.aeroCluster —
	// otherwise a missing copy-back would go unnoticed.
	objects := make([]client.Object, 0, len(existingObjects)+1)
	if aeroCluster != nil {
		objects = append(objects, aeroCluster.DeepCopy())
	}

	objects = append(objects, existingObjects...)

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(*funcs).
		WithStatusSubresource(&asdbv1.AerospikeCluster{}).
		WithObjects(objects...).
		Build()

	return &SingleClusterReconciler{
		Client:      fakeClient,
		Log:         logr.Discard(),
		Scheme:      scheme,
		aeroCluster: aeroCluster,
		Recorder:    record.NewFakeRecorder(10),
	}
}

// clusterPhaseSeries reports how many aerospike_ako_aerospikecluster_phase series currently exist
// for the named cluster. Gathering through a private registry keeps the count scoped to that one
// cluster, so it is unaffected by whatever else has been written to the package-global GaugeVec.
func clusterPhaseSeries(t *testing.T, cluster string) int {
	t.Helper()

	reg := prometheus.NewRegistry()
	if err := reg.Register(aerospikeClusterPhase); err != nil {
		t.Fatalf("register phase gauge: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	count := 0

	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == clusterLabelKey && label.GetValue() == cluster {
					count++

					break
				}
			}
		}
	}

	return count
}

// failOnAnyAPIRead returns interceptors that fail the test if the code under test issues any
// read against the API server. Use it to assert that a fast path returns before touching the
// client.
func failOnAnyAPIRead(t *testing.T) *interceptor.Funcs {
	t.Helper()

	return &interceptor.Funcs{
		Get: func(
			_ context.Context, _ client.WithWatch, key client.ObjectKey, _ client.Object,
			_ ...client.GetOption,
		) error {
			t.Fatalf("unexpected Get(%s): this path must not call the API server", key)
			return nil
		},
		List: func(
			_ context.Context, _ client.WithWatch, list client.ObjectList,
			_ ...client.ListOption,
		) error {
			t.Fatalf("unexpected List(%T): this path must not call the API server", list)
			return nil
		},
	}
}

//nolint:unparam // for future use
func newTestAerospikeCluster(namespace, name string) *asdbv1.AerospikeCluster {
	aeroConfig := asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
			asdbv1.ConfKeyService: map[string]interface{}{},
			asdbv1.ConfKeyNetwork: map[string]interface{}{
				asdbv1.ConfKeyNetworkService: map[string]interface{}{
					asdbv1.ConfKeyPort: float64(3000),
				},
			},
		},
	}

	// getFQDNsForCluster (invoked while building the ConfigMap) walks
	// Spec.RackConfig.Racks to size each rack, so it must contain the same
	// rack referenced by the RackState passed to createEmptyRack.
	rack := asdbv1.Rack{
		ID:              1,
		AerospikeConfig: aeroConfig,
	}

	return &asdbv1.AerospikeCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: asdbv1.AerospikeClusterSpec{
			Size:            1,
			Image:           "aerospike/aerospike-server-enterprise:7.0.0.0",
			AerospikeConfig: &aeroConfig,
			RackConfig: asdbv1.RackConfig{
				Racks: []asdbv1.Rack{rack},
			},
		},
	}
}
