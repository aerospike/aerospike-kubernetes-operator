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

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

func replicaCount(n int32) *int32 { return &n }

// TestWaitForSTSPodsServerReady covers the three fast-path branches of
// waitForSTSPodsServerReady that complete without any sleep:
//
//  1. The pod name is in ignorablePodNames → skip entirely, return nil.
//  2. The pod's server container is already ready → return nil on first poll.
//  3. The pod's server container is in CrashLoopBackOff → PodFailed, return error on first poll.
func TestWaitForSTSPodsServerReady(t *testing.T) {
	const stsName = clusterName + "-1"

	scheme := newTestScheme()
	aeroCluster := newTestAerospikeCluster(namespace, clusterName)

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
		Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(1)},
		Status:     appsv1.StatefulSetStatus{Replicas: 1},
	}

	t.Run("ignorable pod is skipped without any k8s poll", func(t *testing.T) {
		r := newReconcilerWithObjects(scheme, aeroCluster, sts)

		// No pod is pre-created; if the function tried to Get it, fake client
		// would return NotFound → error.  The ignorable-skip path must prevent
		// that Get from ever being issued.
		podName := stsName + "-0"

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New(podName)); err != nil {
			t.Errorf("expected nil for ignorable pod, got: %v", err)
		}
	})

	t.Run("pod with running server container succeeds on first poll", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster, sts, pod)

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New[string]()); err != nil {
			t.Errorf("expected nil for running server container, got: %v", err)
		}
	})

	t.Run("server container in CrashLoopBackOff returns error on first poll", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverCrashLoopContainer()},
			},
		}
		r := newReconcilerWithObjects(scheme, aeroCluster, sts, pod)

		if err := r.waitForSTSPodsServerReady(context.Background(), sts, sets.New[string]()); err == nil {
			t.Error("expected an error for CrashLoopBackOff server container, got nil")
		}
	})

	t.Run("multiple pods: second pod ignorable, first pod running — succeeds", func(t *testing.T) {
		multiSTS := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
			Spec:       appsv1.StatefulSetSpec{Replicas: replicaCount(2)},
			Status:     appsv1.StatefulSetStatus{Replicas: 2},
		}

		pod0 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: stsName + "-0", Namespace: namespace},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{serverContainer(true)},
			},
		}

		// pod-1 is ignorable; no pod object is pre-created for it so any Get
		// would return NotFound — the skip must fire before the Get.
		ignorable := sets.New(stsName + "-1")
		r := newReconcilerWithObjects(scheme, aeroCluster, multiSTS, pod0)

		if err := r.waitForSTSPodsServerReady(context.Background(), multiSTS, ignorable); err != nil {
			t.Errorf("expected nil when running pod + ignorable pod, got: %v", err)
		}
	})
}

// TestGetFinalVolumeAttachmentsForVolume validates the selective init-container
// auto-mount logic introduced to avoid mounting irrelevant volumes (ConfigMap,
// Secret, EmptyDir without an Aerospike path) in the aerospike-init container.
//
// Rules under test:
//   - PV volumes (block or filesystem) are always auto-mounted in the init container.
//   - Non-PV volumes with an Aerospike attachment are auto-mounted (the init container
//     needs them to set up the workdir).
//   - Non-PV volumes without an Aerospike attachment are NOT auto-mounted; the init
//     container has no code that touches them.
//   - Explicit volume.InitContainers entries are always honoured regardless of the
//     above rules.
func TestGetFinalVolumeAttachmentsForVolume(t *testing.T) {
	const workDir = "/opt/aerospike"

	// hasInitContainerAutoMount returns true when the returned initContainerAttachments
	// contains the automatic aerospike-init attachment (identified by ContainerName).
	hasInitContainerAutoMount := func(attachments []asdbv1.VolumeAttachment) bool {
		for _, a := range attachments {
			if a.ContainerName == asdbv1.AerospikeInitContainerName {
				return true
			}
		}

		return false
	}

	t.Run("PV block volume is auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name: "block-vol",
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					VolumeMode: corev1.PersistentVolumeBlock,
				},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.True(t, hasInitContainerAutoMount(initAttachments),
			"PV block volume must be auto-mounted in the init container for wiping")
	})

	t.Run("PV filesystem volume is auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name: "fs-vol",
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					VolumeMode: corev1.PersistentVolumeFilesystem,
				},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.True(t, hasInitContainerAutoMount(initAttachments),
			"PV filesystem volume must be auto-mounted in the init container for initialization")
	})

	t.Run("non-PV volume with Aerospike attachment is auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name: "hostpath-workdir",
			Source: asdbv1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/data"},
			},
			Aerospike: &asdbv1.AerospikeServerVolumeAttachment{
				Path: "/opt/aerospike/data",
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.True(t, hasInitContainerAutoMount(initAttachments),
			"non-PV volume used by Aerospike must be auto-mounted so the init container can set up the workdir")
	})

	t.Run("ConfigMap volume without Aerospike attachment is NOT auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name: "sidecar-config",
			Source: asdbv1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-config"},
				},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.False(t, hasInitContainerAutoMount(initAttachments),
			"ConfigMap volume not used by Aerospike must not be auto-mounted in the init container")
	})

	t.Run("Secret volume without Aerospike attachment is NOT auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name: "sidecar-secret",
			Source: asdbv1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.False(t, hasInitContainerAutoMount(initAttachments),
			"Secret volume not used by Aerospike must not be auto-mounted in the init container")
	})

	t.Run("EmptyDir volume without Aerospike attachment is NOT auto-mounted in init container", func(t *testing.T) {
		vol := &asdbv1.VolumeSpec{
			Name:   "scratch",
			Source: asdbv1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		assert.False(t, hasInitContainerAutoMount(initAttachments),
			"EmptyDir volume not used by Aerospike must not be auto-mounted in the init container")
	})

	t.Run("explicit volume.InitContainers entries are always honoured", func(t *testing.T) {
		const customInitContainer = "my-custom-init"

		vol := &asdbv1.VolumeSpec{
			Name:   "sidecar-config",
			Source: asdbv1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}},
			InitContainers: []asdbv1.VolumeAttachment{
				{ContainerName: customInitContainer, Path: "/custom/path"},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		// The aerospike-init auto-mount must be absent, but the explicit user entry must be present.
		assert.False(t, hasInitContainerAutoMount(initAttachments),
			"aerospike-init auto-mount must not appear for a non-PV, non-Aerospike volume")

		found := false

		for _, a := range initAttachments {
			if a.ContainerName == customInitContainer {
				found = true
				break
			}
		}

		assert.True(t, found, "explicit volume.InitContainers entry must always be included")
	})

	t.Run("auto-mount path uses volume name as path segment", func(t *testing.T) {
		const volName = "my-pv"

		vol := &asdbv1.VolumeSpec{
			Name: volName,
			Source: asdbv1.VolumeSource{
				PersistentVolume: &asdbv1.PersistentVolumeSpec{
					VolumeMode: corev1.PersistentVolumeFilesystem,
				},
			},
		}

		initAttachments, _ := getFinalVolumeAttachmentsForVolume(vol, workDir)

		for _, a := range initAttachments {
			if a.ContainerName == asdbv1.AerospikeInitContainerName {
				assert.Equal(t, "/"+volName, a.Path,
					"auto-mount path must be /<volumeName>")

				return
			}
		}

		t.Fatal("aerospike-init auto-mount attachment not found")
	})
}
