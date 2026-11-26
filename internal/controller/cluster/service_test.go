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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

// TestCreateOrUpdatePodService verifies invariants of the NodePort service that
// is created (or updated) per pod:
//   - PublishNotReadyAddresses is always true so that sidecar-failing pods
//     (whose Aerospike server is still reachable) continue to receive traffic.
//   - The call is idempotent — a second invocation must not return an error.
//   - The behaviour is the same regardless of the IgnoreSidecarFailure flag.
func TestCreateOrUpdatePodService(t *testing.T) {
	const (
		podName    = "test-cluster-1-0"
		clusterUID = "test-uid"
	)

	tests := []struct {
		checkSvc      func(*testing.T, *corev1.Service)
		name          string
		ignoreSidecar bool
		callTwice     bool
	}{
		{
			name: "service has PublishNotReadyAddresses=true and ServiceTypeNodePort",
			checkSvc: func(t *testing.T, svc *corev1.Service) {
				t.Helper()

				if !svc.Spec.PublishNotReadyAddresses {
					t.Error("expected PublishNotReadyAddresses=true, got false")
				}

				if svc.Spec.Type != corev1.ServiceTypeNodePort {
					t.Errorf("expected ServiceTypeNodePort, got %s", svc.Spec.Type)
				}
			},
		},
		{
			name:      "idempotent: second call succeeds and flag is preserved",
			callTwice: true,
			checkSvc: func(t *testing.T, svc *corev1.Service) {
				t.Helper()

				if !svc.Spec.PublishNotReadyAddresses {
					t.Error("expected PublishNotReadyAddresses=true after idempotent call, got false")
				}
			},
		},
		{
			name:          "PublishNotReadyAddresses=true even with IgnoreSidecarFailure",
			ignoreSidecar: true,
			checkSvc: func(t *testing.T, svc *corev1.Service) {
				t.Helper()

				if !svc.Spec.PublishNotReadyAddresses {
					t.Error("expected PublishNotReadyAddresses=true even with IgnoreSidecarFailure, got false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aeroCluster := newTestAerospikeCluster(namespace, clusterName)
			aeroCluster.UID = clusterUID

			if tt.ignoreSidecar {
				boolTrue := true
				aeroCluster.Spec.IgnoreSidecarFailure = &boolTrue
			}

			r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})
			r.Recorder = record.NewFakeRecorder(10)

			require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace), "first call")

			if tt.callTwice {
				require.NoError(t, r.createOrUpdatePodService(context.Background(), podName, namespace), "second call")
			}

			svc := &corev1.Service{}
			require.NoError(t, r.Get(context.Background(), types.NamespacedName{Name: podName, Namespace: namespace}, svc))

			tt.checkSvc(t, svc)
		})
	}
}

func TestIsPodServiceManaged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		multiPodPerHost bool
		specNLB         bool
		statusNLB       bool
		want            bool
	}{
		{name: "nothing to manage"},
		{name: "multiPodPerHost", multiPodPerHost: true, want: true},
		{name: "spec networkLoadBalancer", specNLB: true, want: true},
		{name: "status networkLoadBalancer still cleans up", statusNLB: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isPodServiceManaged(tt.multiPodPerHost, tt.specNLB, tt.statusNLB)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPodServiceNeeded(t *testing.T) {
	t.Parallel()

	host := &asdbv1.AerospikeNetworkPolicy{
		AccessType:          asdbv1.AerospikeNetworkTypeHostInternal,
		AlternateAccessType: asdbv1.AerospikeNetworkTypeHostExternal,
	}
	podOnly := &asdbv1.AerospikeNetworkPolicy{
		AccessType:             asdbv1.AerospikeNetworkTypePod,
		AlternateAccessType:    asdbv1.AerospikeNetworkTypePod,
		TLSAccessType:          asdbv1.AerospikeNetworkTypePod,
		TLSAlternateAccessType: asdbv1.AerospikeNetworkTypePod,
	}
	nlb := &asdbv1.AerospikeNetworkPolicy{
		AlternateAccessType: asdbv1.AerospikeNetworkTypeNetworkLoadBalancer,
	}

	tests := []struct {
		multi  *bool
		policy *asdbv1.AerospikeNetworkPolicy
		name   string
		want   bool
	}{
		{name: "nil policy", multi: ptr.To(true)},
		{name: "single pod per host does not need a service", multi: ptr.To(false), policy: host},
		{name: "multi pod per host needs a service", multi: ptr.To(true), policy: host, want: true},
		{name: "pod addresses do not need a service", multi: ptr.To(true), policy: podOnly},
		{
			name:   "networkLoadBalancer needs a service when multiPodPerHost is false",
			multi:  ptr.To(false),
			policy: nlb,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, podServiceNeeded(tt.multi, tt.policy))
		})
	}
}

func TestReconcilePodServiceKeepsServiceWhenLeavingNLB(t *testing.T) {
	t.Parallel()

	const podName = clusterName + "-1-0"

	// The network-policy functional test covers one pod per host, where leaving
	// networkLoadBalancer deletes the Service. multiPodPerHost still needs it.
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	aeroCluster.Spec.PodSpec.MultiPodPerHost = ptr.To(true)
	aeroCluster.Spec.AerospikeNetworkPolicy = asdbv1.AerospikeNetworkPolicy{
		AccessType:          asdbv1.AerospikeNetworkTypeHostInternal,
		AlternateAccessType: asdbv1.AerospikeNetworkTypeHostExternal,
	}
	aeroCluster.Status.AerospikeNetworkPolicy = asdbv1.AerospikeNetworkPolicy{
		AlternateAccessType:                asdbv1.AerospikeNetworkTypeNetworkLoadBalancer,
		LoadBalancerAlternateAccessAddress: "aerospike-nlb.example.com",
	}

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, nlbTestPod(podName), nlbTestService(podName))
	rackState := &RackState{Rack: &aeroCluster.Spec.RackConfig.Racks[0], Size: 1}

	require.NoError(t, r.reconcilePodService(t.Context(), rackState))

	svc := &corev1.Service{}
	require.NoError(t, r.Get(t.Context(), types.NamespacedName{Name: podName, Namespace: namespace}, svc))
	require.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
}

func nlbTestPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    utils.LabelsForAerospikeClusterRack(clusterName, 1, ""),
		},
	}
}

func nlbTestService(name string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeNodePort,
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{{
				Name: asdbv1.ServicePortName,
				Port: 3000,
			}},
		},
	}
}
