package cluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

//nolint:unparam // for future use
func newTestAerospikeCluster(namespace, name string) *asdbv1.AerospikeCluster {
	aeroConfig := asdbv1.AerospikeConfigSpec{
		Value: map[string]interface{}{
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

func newTestRackState(aeroCluster *asdbv1.AerospikeCluster) *RackState {
	return &RackState{
		Rack: &aeroCluster.Spec.RackConfig.Racks[0],
		Size: aeroCluster.Spec.Size,
	}
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

// rackPodLabels returns the labels that getOrderedRackPodList uses to find pods
// for a given cluster+rack.
func rackPodLabels(clusterName string, rackID int) map[string]string {
	return utils.LabelsForAerospikeClusterRack(clusterName, rackID, "")
}

// imageFailedPod returns a pod in ErrImagePull waiting state.
func imageFailedPod(name, namespace, clusterName string, rackID int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    rackPodLabels(clusterName, rackID),
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: asdbv1.AerospikeServerContainerName,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"},
					},
				},
			},
		},
	}
}

// crashLoopPod returns a pod whose server container is in CrashLoopBackOff.
//
//nolint:unparam // for future use
func crashLoopServerPod(name, namespace, clusterName string, rackID int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    rackPodLabels(clusterName, rackID),
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{serverCrashLoopContainer()},
		},
	}
}

// sidecarCrashPod returns a pod with a healthy server but a crashing sidecar.
func sidecarCrashPod(name, namespace, clusterName string, rackID int) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    rackPodLabels(clusterName, rackID),
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				serverContainer(true),
				sidecarContainer("sidecar", false, true),
			},
		},
	}
}

// TestCheckPodsFailedAfterRackOp validates that checkPodsFailedAfterRackOp
// blocks scale-up when existing pods are in a terminal failure state, honouring
// the IgnoreSidecarFailure flag. The pre-flight check was moved from
// scaleUpRack into checkPodsFailedAfterRackOp (called from
// upgradeOrRollingRestartRack) so that scale-up is only deferred when a
// pod-level operation was also performed in the same reconcile cycle.
func TestCheckPodsFailedAfterRackOp(t *testing.T) {
	const rackID = 1

	// clusterWith builds a test AerospikeCluster with the given IgnoreSidecarFailure value.
	clusterWith := func(ignoreSidecar bool) *asdbv1.AerospikeCluster {
		ac := newTestAerospikeCluster(namespace, clusterName)
		ac.Spec.IgnoreSidecarFailure = &ignoreSidecar

		return ac
	}

	rackState := &RackState{
		Rack: &asdbv1.Rack{ID: rackID},
		Size: 2,
	}
	noIgnorables := sets.New[string]()

	t.Run("blocked: server container ErrImagePull with IgnoreSidecarFailure=false", func(t *testing.T) {
		pod := imageFailedPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(false), &interceptor.Funcs{}, pod)

		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, noIgnorables)

		require.False(t, res.IsSuccess)
	})

	t.Run("blocked: server container CrashLoopBackOff with IgnoreSidecarFailure=false", func(t *testing.T) {
		pod := crashLoopServerPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(false), &interceptor.Funcs{}, pod)

		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, noIgnorables)

		require.False(t, res.IsSuccess)
	})

	t.Run("blocked: server container CrashLoopBackOff with IgnoreSidecarFailure=true", func(t *testing.T) {
		pod := crashLoopServerPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(true), &interceptor.Funcs{}, pod)

		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, noIgnorables)

		require.False(t, res.IsSuccess,
			"server container failure must block even when IgnoreSidecarFailure=true")
	})

	t.Run("blocked: crashing sidecar with IgnoreSidecarFailure=false", func(t *testing.T) {
		pod := sidecarCrashPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(false), &interceptor.Funcs{}, pod)

		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, noIgnorables)

		require.False(t, res.IsSuccess)
	})

	t.Run("allowed: crashing sidecar with IgnoreSidecarFailure=true", func(t *testing.T) {
		pod := sidecarCrashPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(true), &interceptor.Funcs{}, pod)

		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, noIgnorables)

		require.True(t, res.IsSuccess,
			"crashing sidecar must not block when IgnoreSidecarFailure=true")
	})

	t.Run("allowed: ignorable pod is skipped", func(t *testing.T) {
		podName := clusterName + "-1-0"
		pod := crashLoopServerPod(podName, namespace, clusterName, rackID)
		r := newTestReconciler(t, clusterWith(false), &interceptor.Funcs{}, pod)

		ignorables := sets.New(podName)
		res := r.checkPodsFailedAfterRackOp(context.Background(), rackState, ignorables)

		require.True(t, res.IsSuccess,
			"pod in ignorablePodNames must be skipped regardless of its state")
	})
}

// TestCreateEmptyRack_NilSTSOnCreateFailure reproduces the KO-586 panic: forcing the
// StatefulSet Create call to fail drives createSTS down the `return nil, err` path
// (statefulset.go), and createEmptyRack must handle that nil result without panicking.
func TestCreateEmptyRack_NilSTSOnCreateFailure(t *testing.T) {
	aeroCluster := newTestAerospikeCluster("test-ns", "test-cluster")
	rackState := newTestRackState(aeroCluster)

	createErr := fmt.Errorf("simulated create failure")

	var stsDeleteAttempted bool

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
		Create: func(
			ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption,
		) error {
			if _, ok := obj.(*appsv1.StatefulSet); ok {
				return createErr
			}

			return c.Create(ctx, obj, opts...)
		},
		Delete: func(
			ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption,
		) error {
			if _, ok := obj.(*appsv1.StatefulSet); ok {
				stsDeleteAttempted = true
			}

			return c.Delete(ctx, obj, opts...)
		},
	})

	require.NotPanics(t, func() {
		found, res := r.createEmptyRack(context.TODO(), rackState)

		require.Nil(t, found)
		require.False(t, res.IsSuccess)
	})

	// No StatefulSet was ever created, so there is nothing to clean up.
	require.False(t, stsDeleteAttempted, "deleteSTS should not be invoked when createSTS never created a StatefulSet")
}

// TestCreateEmptyRack_NilSTSOnOwnerRefFailure covers the other nil-returning error path in
// createSTSConfigMap (controllerutil.SetControllerReference failing before r.Create is ever reached),
// to confirm the nil guard in createEmptyRack isn't narrowly coupled to just one failure site.
func TestCreateEmptyRack_NilSTSOnOwnerRefFailure(t *testing.T) {
	aeroCluster := newTestAerospikeCluster("test-ns", "test-cluster")
	rackState := newTestRackState(aeroCluster)

	// A scheme with no registered types makes SetControllerReference fail immediately,
	// before either the ConfigMap or the StatefulSet is ever created.
	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{})
	r.Scheme = runtime.NewScheme()

	require.NotPanics(t, func() {
		found, res := r.createEmptyRack(context.TODO(), rackState)

		require.Nil(t, found)
		require.False(t, res.IsSuccess)
	})
}

// TestCreateEmptyRack_DeletesSTSWhenReadinessFails covers the case createEmptyRack's nil
// guard must not accidentally suppress: createSTS creates the StatefulSet successfully
// (r.Create succeeds) but then fails, here via waitForSTSToBeReady, returning a non-nil
// StatefulSet alongside a non-nil error. createEmptyRack must call deleteSTS to roll back
// the StatefulSet it just created.
func TestCreateEmptyRack_DeletesSTSWhenReadinessFails(t *testing.T) {
	aeroCluster := newTestAerospikeCluster("test-ns", "test-cluster")
	rackState := newTestRackState(aeroCluster)

	var stsDeleteAttempted bool

	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
		Delete: func(
			ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption,
		) error {
			if _, ok := obj.(*appsv1.StatefulSet); ok {
				stsDeleteAttempted = true
			}

			return c.Delete(ctx, obj, opts...)
		},
	})

	// Pre-create the StatefulSet's pod-0 in a Failed phase so waitForSTSToBeReady
	// (invoked by createSTS right after the StatefulSet is created) fails immediately via
	// utils.CheckPodFailed, instead of polling for pod readiness for up to 3 minutes.
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-1-0",
			Namespace: "test-ns",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}
	require.NoError(t, r.Create(context.TODO(), pod))

	found, res := r.createEmptyRack(context.TODO(), rackState)

	require.Nil(t, found)
	require.False(t, res.IsSuccess)
	require.True(t, stsDeleteAttempted,
		"deleteSTS should be invoked to roll back the StatefulSet created before readiness failed")

	err := r.Get(context.TODO(), types.NamespacedName{Name: "test-cluster-1", Namespace: "test-ns"}, &appsv1.StatefulSet{})
	require.Error(t, err, "the StatefulSet created by createSTS should have been deleted by the cleanup path")
}
