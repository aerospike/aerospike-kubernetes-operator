package e2e

import (
	goctx "context"
	"testing"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
)

// PodSpecTest tests podSpec changes
func PodSpecTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
	// get namespace
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	clusterName := "aerocluster"

	clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

	sidecar1 := corev1.Container{
		Name:  "nginx1",
		Image: "nginx:1.14.2",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 80,
			},
		},
	}

	sidecar2 := corev1.Container{
		Name:    "box",
		Image:   "busybox:1.28",
		Command: []string{"sh", "-c", "echo The app is running! && sleep 3600"},
	}

	t.Run("Positive", func(t *testing.T) {
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("AddContainer1", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("AddContainer2", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar2)

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("UpdateContainer2", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			aeroCluster.Spec.PodSpec.Sidecars[1].Command = []string{"sh", "-c", "updating command, should do rolling restart! && sleep 3600"}

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RemoveContainer", func(t *testing.T) {
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)

			aeroCluster.Spec.PodSpec.Sidecars = []corev1.Container{}

			if err := updateAndWait(t, f, ctx, aeroCluster); err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("Negative", func(t *testing.T) {
		// aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 2)
		// if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
		// 	t.Fatal(err)
		// }

		t.Run("AddSameContainer", func(t *testing.T) {
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)

			err := f.Client.Update(goctx.TODO(), aeroCluster)
			validateError(t, err, "should fail for adding container with same name")
		})

		// Add test for bad container config, cluster status should not get updated, if pod is not running and ready
		t.Run("AddBadContainer", func(t *testing.T) {
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, sidecar1)

			// adding sidecar1 with changed name and port.
			badsidecar := corev1.Container{
				Name:  "nginxbad",
				Image: "nginx-bad:1.14.2",
				Ports: []corev1.ContainerPort{
					{
						ContainerPort: 81,
					},
				},
			}
			aeroCluster.Spec.PodSpec.Sidecars = append(aeroCluster.Spec.PodSpec.Sidecars, badsidecar)

			err := updateAndWait(t, f, ctx, aeroCluster)
			validateError(t, err, "should fail for adding the bad container")

		})
	})

}
