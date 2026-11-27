package cluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

func rackPod(name string, rackID int, terminating bool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    utils.LabelsForAerospikeClusterRack(clusterName, rackID, ""),
		},
	}

	if terminating {
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		pod.Finalizers = []string{"test/keep"}
	}

	return pod
}

// TestGetExistingRackSize covers the size a missing rack StatefulSet is recreated at. Recreating it
// below the highest known ordinal makes the StatefulSet controller delete the pods above it and the
// dangling-pod cleanup delete their PVCs.
func TestGetExistingRackSize(t *testing.T) {
	tests := []struct {
		name       string
		pods       []client.Object
		statusPods []string
		want       int32
	}{
		{
			name: "new rack: no pods and no status",
			want: 0,
		},
		{
			name: "orphaned pods are all re-adopted",
			pods: []client.Object{
				rackPod("test-cluster-1-0", 1, false),
				rackPod("test-cluster-1-1", 1, false),
				rackPod("test-cluster-1-2", 1, false),
			},
			want: 3,
		},
		{
			name: "terminating pods are ignored",
			pods: []client.Object{
				rackPod("test-cluster-1-0", 1, false),
				rackPod("test-cluster-1-1", 1, true),
			},
			want: 1,
		},
		{
			name: "a pod known in status but gone keeps its ordinal",
			pods: []client.Object{
				rackPod("test-cluster-1-0", 1, false),
			},
			statusPods: []string{"test-cluster-1-0", "test-cluster-1-1", "test-cluster-1-2"},
			want:       3,
		},
		{
			name: "pods and status of other racks are ignored",
			pods: []client.Object{
				rackPod("test-cluster-2-0", 2, false),
				rackPod("test-cluster-2-1", 2, false),
			},
			statusPods: []string{"test-cluster-2-0", "test-cluster-2-1"},
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aeroCluster := newTestAerospikeCluster(namespace, clusterName)

			aeroCluster.Status.Pods = map[string]asdbv1.AerospikePodStatus{}
			for _, podName := range tt.statusPods {
				aeroCluster.Status.Pods[podName] = asdbv1.AerospikePodStatus{}
			}

			r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, tt.pods...)

			got, err := r.getExistingRackSize(context.TODO(), 1, "")
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestCreateEmptyRack_OrphansPodsWhenReadinessFails ensures the rollback of a failed rack creation
// never cascades to the pods: a rack recreated to re-adopt running pods would otherwise lose them.
func TestCreateEmptyRack_OrphansPodsWhenReadinessFails(t *testing.T) {
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	rackState := newTestRackState(aeroCluster)

	var propagation *metav1.DeletionPropagation

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
		Delete: func(
			ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption,
		) error {
			if _, ok := obj.(*appsv1.StatefulSet); ok {
				deleteOpts := &client.DeleteOptions{}
				deleteOpts.ApplyOptions(opts)
				propagation = deleteOpts.PropagationPolicy
			}

			return c.Delete(ctx, obj, opts...)
		},
	})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster-1-0", Namespace: namespace},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	require.NoError(t, r.Create(context.TODO(), pod))

	found, res := r.createEmptyRack(context.TODO(), rackState)

	require.Nil(t, found)
	require.False(t, res.IsSuccess)
	require.NotNil(t, propagation, "the StatefulSet rollback must set a propagation policy")
	require.Equal(t, metav1.DeletePropagationOrphan, *propagation)
}
