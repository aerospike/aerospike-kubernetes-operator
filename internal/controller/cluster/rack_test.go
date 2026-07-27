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
	k8ssets "k8s.io/apimachinery/pkg/util/sets"
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
) *SingleClusterReconciler {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, asdbv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(*funcs).
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
func crashLoopServerPod(name, namespace, clusterName string, rackID int) *corev1.Pod {
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
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
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
				{
					Name:  asdbv1.AerospikeServerContainerName,
					Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
				{
					Name: "sidecar",
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
}

// TestScaleUpRack_PreFlightCheck validates that scaleUpRack blocks scale-up
// when existing pods are in a terminal failure state and proceeds when they are
// healthy, honouring the IgnoreSidecarFailure flag.
func TestScaleUpRack_PreFlightCheck(t *testing.T) {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		rackID      = 1
	)

	scheme := runtime.NewScheme()
	require.NoError(t, asdbv1.AddToScheme(scheme))
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	makeSTS := func(currentReplicas int32) *appsv1.StatefulSet {
		return &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName + "-1",
				Namespace: namespace,
			},
			Spec:   appsv1.StatefulSetSpec{Replicas: &currentReplicas},
			Status: appsv1.StatefulSetStatus{Replicas: currentReplicas},
		}
	}

	makeReconciler := func(
		t *testing.T,
		ignoreSidecar bool,
		existingObjects ...client.Object,
	) *SingleClusterReconciler {
		t.Helper()

		aeroCluster := newTestAerospikeCluster(namespace, clusterName)
		aeroCluster.Spec.IgnoreSidecarFailure = &ignoreSidecar

		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(existingObjects...).
			WithObjects(aeroCluster).
			Build()

		return &SingleClusterReconciler{
			Client:      fakeClient,
			Log:         logr.Discard(),
			Scheme:      scheme,
			aeroCluster: aeroCluster,
			Recorder:    record.NewFakeRecorder(10),
		}
	}

	ignorableNames := k8ssets.New[string]()
	desiredState := &RackState{
		Rack: &asdbv1.Rack{ID: rackID},
		Size: 2, // scale up from 1 → 2
	}

	t.Run("blocked: server container ErrImagePull with IgnoreSidecarFailure=false", func(t *testing.T) {
		sts := makeSTS(1)
		pod := imageFailedPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := makeReconciler(t, false, sts, pod)

		_, res := r.scaleUpRack(context.Background(), sts, desiredState, ignorableNames)

		require.False(t, res.IsSuccess)
		require.Error(t, res.Err, "expected error when server container has ErrImagePull")
	})

	t.Run("blocked: server container CrashLoopBackOff with IgnoreSidecarFailure=false", func(t *testing.T) {
		sts := makeSTS(1)
		pod := crashLoopServerPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := makeReconciler(t, false, sts, pod)

		_, res := r.scaleUpRack(context.Background(), sts, desiredState, ignorableNames)

		require.False(t, res.IsSuccess)
		require.Error(t, res.Err, "expected error when server container is in CrashLoopBackOff")
	})

	t.Run("blocked: server container CrashLoopBackOff with IgnoreSidecarFailure=true", func(t *testing.T) {
		sts := makeSTS(1)
		pod := crashLoopServerPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := makeReconciler(t, true, sts, pod)

		_, res := r.scaleUpRack(context.Background(), sts, desiredState, ignorableNames)

		require.False(t, res.IsSuccess)
		require.Error(t, res.Err,
			"expected error when server container is in CrashLoopBackOff even with IgnoreSidecarFailure=true")
	})

	t.Run("blocked: crashing sidecar with IgnoreSidecarFailure=false", func(t *testing.T) {
		sts := makeSTS(1)
		pod := sidecarCrashPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := makeReconciler(t, false, sts, pod)

		_, res := r.scaleUpRack(context.Background(), sts, desiredState, ignorableNames)

		require.False(t, res.IsSuccess)
		require.Error(t, res.Err, "expected error when sidecar is crashing and IgnoreSidecarFailure=false")
	})

	t.Run("allowed: crashing sidecar with IgnoreSidecarFailure=true", func(t *testing.T) {
		sts := makeSTS(1)
		pod := sidecarCrashPod(clusterName+"-1-0", namespace, clusterName, rackID)
		r := makeReconciler(t, true, sts, pod)

		// Scale-up proceeds past the pre-flight check; it may fail later in
		// cleanupDanglingPodsRack/createOrUpdatePodServiceIfNeeded but must NOT
		// return an error attributable to the pre-flight pod-state check.
		_, res := r.scaleUpRack(context.Background(), sts, desiredState, ignorableNames)

		if res.Err != nil {
			require.NotContains(t, res.Err.Error(), "is in failed state",
				"pre-flight check must not block when sidecar crashes and IgnoreSidecarFailure=true")
		}
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
