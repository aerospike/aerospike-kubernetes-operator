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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/stretchr/testify/require"
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
