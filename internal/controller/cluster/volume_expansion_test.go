package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	"github.com/aerospike/aerospike-kubernetes-operator/v4/pkg/utils"
)

const (
	testVolume  = "data-1"
	testPodName = clusterName + "-1-0"
	testPVCName = testVolume + "-" + testPodName
)

func expansionCluster(mode corev1.PersistentVolumeMode) *asdbv1.AerospikeCluster {
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)
	aeroCluster.Spec.RackConfig.Racks[0].Storage = asdbv1.AerospikeStorageSpec{
		Volumes: []asdbv1.VolumeSpec{{
			Name: testVolume,
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					StorageClass: "expandable",
					VolumeMode:   mode,
					Size:         resource.MustParse("2Gi"),
				},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{Path: "/dev/" + testVolume},
		}},
	}

	return aeroCluster
}

func expansionPVC(testPodName, requested, capacity string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        testVolume + "-" + testPodName,
			Namespace:   namespace,
			Labels:      utils.LabelsForAerospikeClusterRack(clusterName, 1, ""),
			Annotations: map[string]string{storageVolumeAnnotationKey: testVolume},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(requested)},
			},
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(capacity)},
		},
	}
}

func expansionSTS(templateSize string) *appsv1.StatefulSet {
	replicas := int32(1)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName + "-1", Namespace: namespace},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: testVolume},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(templateSize)},
					},
				},
			}},
		},
	}
}

func getTestPVC(t *testing.T, r *SingleClusterReconciler) *corev1.PersistentVolumeClaim {
	t.Helper()

	pvc := &corev1.PersistentVolumeClaim{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: testPVCName}, pvc))

	return pvc
}

func getTestPod(t *testing.T, r *SingleClusterReconciler) *corev1.Pod {
	t.Helper()

	pod := &corev1.Pod{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: testPodName}, pod))

	return pod
}

func TestReconcileRackVolumeExpansion(t *testing.T) {
	t.Run("block PVC below the spec is expanded and its pod marked", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{},
			rackPod(testPodName, 1, false), expansionPVC(testPodName, "1Gi", "1Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("1Gi"), newTestRackState(aeroCluster))

		require.False(t, res.IsSuccess, "must requeue until the expansion completes")
		require.NoError(t, res.Err)

		requested := getTestPVC(t, r).Spec.Resources.Requests[corev1.ResourceStorage]
		require.Equal(t, "2Gi", requested.String())
		require.Contains(t, getTestPod(t, r).Annotations, volumeExpansionRestartAnnotation)
	})

	t.Run("filesystem PVC is expanded without marking the pod", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeFilesystem)
		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{},
			rackPod(testPodName, 1, false), expansionPVC(testPodName, "1Gi", "1Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("1Gi"), newTestRackState(aeroCluster))

		require.False(t, res.IsSuccess)

		requested := getTestPVC(t, r).Spec.Resources.Requests[corev1.ResourceStorage]
		require.Equal(t, "2Gi", requested.String())
		require.NotContains(t, getTestPod(t, r).Annotations, volumeExpansionRestartAnnotation)
	})

	t.Run("expansion in progress keeps requeuing without a new patch", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)

		var pvcPatches int

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					pvcPatches++
				}

				return c.Patch(ctx, obj, patch, opts...)
			},
		}, rackPod(testPodName, 1, false), expansionPVC(testPodName, "2Gi", "1Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("1Gi"), newTestRackState(aeroCluster))

		require.False(t, res.IsSuccess)
		require.Zero(t, pvcPatches)
	})

	t.Run("PVC larger than the spec is never shrunk", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{},
			rackPod(testPodName, 1, false), expansionPVC(testPodName, "3Gi", "3Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("2Gi"), newTestRackState(aeroCluster))

		require.True(t, res.IsSuccess)

		requested := getTestPVC(t, r).Spec.Resources.Requests[corev1.ResourceStorage]
		require.Equal(t, "3Gi", requested.String())
		require.NotContains(t, getTestPod(t, r).Annotations, volumeExpansionRestartAnnotation)
	})

	t.Run("nothing to do when PVCs and template match the spec", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)

		var stsDeleted bool

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if _, ok := obj.(*appsv1.StatefulSet); ok {
					stsDeleted = true
				}

				return c.Delete(ctx, obj, opts...)
			},
		}, rackPod(testPodName, 1, false), expansionPVC(testPodName, "2Gi", "2Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("2Gi"), newTestRackState(aeroCluster))

		require.True(t, res.IsSuccess)
		require.False(t, stsDeleted)
	})

	t.Run("outdated template is realigned with orphan propagation once PVCs are expanded", func(t *testing.T) {
		origRetry, origInterval := podStatusMaxRetry, podStatusRetryInterval
		podStatusMaxRetry, podStatusRetryInterval = 1, time.Millisecond

		t.Cleanup(func() { podStatusMaxRetry, podStatusRetryInterval = origRetry, origInterval })

		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
		sts := expansionSTS("1Gi")

		var propagation *metav1.DeletionPropagation

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if _, ok := obj.(*appsv1.StatefulSet); ok && propagation == nil {
					deleteOpts := &client.DeleteOptions{}
					deleteOpts.ApplyOptions(opts)
					propagation = deleteOpts.PropagationPolicy
				}

				return c.Delete(ctx, obj, opts...)
			},
		}, sts, rackPod(testPodName, 1, false), expansionPVC(testPodName, "2Gi", "2Gi"))

		// The recreated StatefulSet never becomes ready with the fake client, only the delete matters here.
		_, _ = r.reconcileRackVolumeExpansion(context.TODO(), sts, newTestRackState(aeroCluster))

		require.NotNil(t, propagation)
		require.Equal(t, metav1.DeletePropagationOrphan, *propagation)
	})

	t.Run("expansion refused by the API server is reported", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					return k8serrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumeclaims"}, obj.GetName(),
						errForbiddenExpansion)
				}

				return c.Patch(ctx, obj, patch, opts...)
			},
		}, rackPod(testPodName, 1, false), expansionPVC(testPodName, "1Gi", "1Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("1Gi"), newTestRackState(aeroCluster))

		require.False(t, res.IsSuccess)
		require.Error(t, res.Err)

		requested := getTestPVC(t, r).Spec.Resources.Requests[corev1.ResourceStorage]
		require.Equal(t, "1Gi", requested.String())
		require.NotContains(t, getTestPod(t, r).Annotations, volumeExpansionRestartAnnotation,
			"a refused expansion must not leave the pod marked for a warm restart")
	})

	t.Run("mark of an earlier expansion survives a refused one", func(t *testing.T) {
		aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
		pod := rackPod(testPodName, 1, false)
		pod.Annotations = map[string]string{volumeExpansionRestartAnnotation: "true"}

		r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{
			Patch: func(
				ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption,
			) error {
				if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
					return errForbiddenExpansion
				}

				return c.Patch(ctx, obj, patch, opts...)
			},
		}, pod, expansionPVC(testPodName, "1Gi", "1Gi"))

		_, res := r.reconcileRackVolumeExpansion(context.TODO(), expansionSTS("1Gi"), newTestRackState(aeroCluster))

		require.False(t, res.IsSuccess)
		require.Contains(t, getTestPod(t, r).Annotations, volumeExpansionRestartAnnotation)
	})
}

var errForbiddenExpansion = k8serrors.NewBadRequest("only dynamically provisioned pvc can be resized")

func TestIsVolumeExpansionRestartNeeded(t *testing.T) {
	markedPod := func() *corev1.Pod {
		pod := rackPod(testPodName, 1, false)
		pod.Annotations = map[string]string{volumeExpansionRestartAnnotation: "true"}

		return pod
	}

	tests := []struct {
		pod  *corev1.Pod
		pvc  *corev1.PersistentVolumeClaim
		name string
		want bool
	}{
		{name: "not marked", pod: rackPod(testPodName, 1, false), pvc: expansionPVC(testPodName, "2Gi", "2Gi"), want: false},
		{name: "marked, expansion pending", pod: markedPod(), pvc: expansionPVC(testPodName, "2Gi", "1Gi"), want: false},
		{name: "marked, expansion done", pod: markedPod(), pvc: expansionPVC(testPodName, "2Gi", "2Gi"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
			r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, tt.pod, tt.pvc)

			got, err := r.isVolumeExpansionRestartNeeded(context.TODO(), newTestRackState(aeroCluster), tt.pod)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestClearVolumeExpansionRestartMark(t *testing.T) {
	pod := rackPod(testPodName, 1, false)
	pod.Annotations = map[string]string{volumeExpansionRestartAnnotation: "true", "keep": "me"}

	aeroCluster := expansionCluster(corev1.PersistentVolumeBlock)
	r := newTestReconciler(t, aeroCluster, &interceptor.Funcs{}, pod)

	require.NoError(t, r.clearVolumeExpansionRestartMark(context.TODO(), getTestPod(t, r)))

	annotations := getTestPod(t, r).Annotations
	require.NotContains(t, annotations, volumeExpansionRestartAnnotation)
	require.Equal(t, "me", annotations["keep"])
}

func TestGetPVCResizeFailure(t *testing.T) {
	pvc := func(mutate func(*corev1.PersistentVolumeClaim)) *corev1.PersistentVolumeClaim {
		p := expansionPVC("p", "2Gi", "1Gi")
		mutate(p)

		return p
	}

	require.Empty(t, getPVCResizeFailure(pvc(func(*corev1.PersistentVolumeClaim) {})))
	require.Empty(t, getPVCResizeFailure(pvc(func(p *corev1.PersistentVolumeClaim) {
		p.Status.AllocatedResourceStatuses = map[corev1.ResourceName]corev1.ClaimResourceStatus{
			corev1.ResourceStorage: corev1.PersistentVolumeClaimControllerResizeInProgress,
		}
	})))
	require.Equal(t, "ControllerResizeInfeasible", getPVCResizeFailure(pvc(func(p *corev1.PersistentVolumeClaim) {
		p.Status.AllocatedResourceStatuses = map[corev1.ResourceName]corev1.ClaimResourceStatus{
			corev1.ResourceStorage: corev1.PersistentVolumeClaimControllerResizeInfeasible,
		}
	})))
	require.Contains(t, getPVCResizeFailure(pvc(func(p *corev1.PersistentVolumeClaim) {
		p.Status.Conditions = []corev1.PersistentVolumeClaimCondition{{
			Type: corev1.PersistentVolumeClaimNodeResizeError, Status: corev1.ConditionTrue, Message: "disk full",
		}}
	})), "disk full")
}

func TestIsSTSVolumeClaimTemplateOutdated(t *testing.T) {
	volumes := expansionCluster(corev1.PersistentVolumeBlock).Spec.RackConfig.Racks[0].Storage.Volumes

	require.True(t, isSTSVolumeClaimTemplateOutdated(expansionSTS("1Gi"), volumes))
	require.False(t, isSTSVolumeClaimTemplateOutdated(expansionSTS("2Gi"), volumes))
	require.False(t, isSTSVolumeClaimTemplateOutdated(expansionSTS("3Gi"), volumes))
	require.False(t, isSTSVolumeClaimTemplateOutdated(expansionSTS("2048Mi"), volumes))
}
