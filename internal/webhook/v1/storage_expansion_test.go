package v1

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

func pvVolume(name, size string) asdbv1.VolumeSpec {
	return asdbv1.VolumeSpec{
		Name: name,
		Source: asdbv1.VolumeSource{
			PersistentVolume: &asdbv1.PersistentVolumeSpec{
				StorageClass: "ssd",
				VolumeMode:   corev1.PersistentVolumeBlock,
				Size:         resource.MustParse(size),
			},
		},
		Aerospike: &asdbv1.AerospikeServerVolumeAttachment{Path: "/dev/" + name},
	}
}

func TestIsSafeChangePersistentVolume(t *testing.T) {
	tests := []struct {
		mutate func(pv *asdbv1.PersistentVolumeSpec)
		name   string
		safe   bool
	}{
		{name: "unchanged", mutate: func(*asdbv1.PersistentVolumeSpec) {}, safe: true},
		{name: "size increase", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Size = resource.MustParse("2Gi")
		}, safe: true},
		{name: "same size in another unit", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Size = resource.MustParse("1024Mi")
		}, safe: true},
		{name: "size decrease", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Size = resource.MustParse("512Mi")
		}, safe: false},
		{name: "storage class change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.StorageClass = "hdd"
		}, safe: false},
		{name: "volume mode change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.VolumeMode = corev1.PersistentVolumeFilesystem
		}, safe: false},
		{name: "access modes change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		}, safe: false},
		{name: "selector change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}}
		}, safe: false},
		{name: "metadata change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Labels = map[string]string{"a": "b"}
		}, safe: false},
		{name: "size increase with storage class change", mutate: func(pv *asdbv1.PersistentVolumeSpec) {
			pv.Size = resource.MustParse("2Gi")
			pv.StorageClass = "hdd"
		}, safe: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldVolume := pvVolume("data-1", "1Gi")
			newVolume := pvVolume("data-1", "1Gi")
			tt.mutate(newVolume.Source.PersistentVolume)

			if got := isSafeChange(&oldVolume, &newVolume); got != tt.safe {
				t.Fatalf("isSafeChange() = %v, want %v", got, tt.safe)
			}
		})
	}
}

func storageCluster(revision string, volumes ...asdbv1.VolumeSpec) *asdbv1.AerospikeCluster {
	return &asdbv1.AerospikeCluster{
		Spec: asdbv1.AerospikeClusterSpec{
			Size: 3,
			RackConfig: asdbv1.RackConfig{
				Racks: []asdbv1.Rack{{
					ID:       1,
					Revision: revision,
					Storage:  asdbv1.AerospikeStorageSpec{Volumes: volumes},
				}},
			},
		},
	}
}

func withStatusStorage(
	cluster *asdbv1.AerospikeCluster, revision string, volumes ...asdbv1.VolumeSpec,
) *asdbv1.AerospikeCluster {
	cluster.Status.RackConfig.Racks = []asdbv1.Rack{{
		ID:       1,
		Revision: revision,
		Storage:  asdbv1.AerospikeStorageSpec{Volumes: volumes},
	}}

	return cluster
}

func TestValidateRackUpdateVolumeSize(t *testing.T) {
	tests := []struct {
		name    string
		oldObj  *asdbv1.AerospikeCluster
		newObj  *asdbv1.AerospikeCluster
		wantErr string
	}{
		{
			name:   "increase at constant revision",
			oldObj: withStatusStorage(storageCluster("v1", pvVolume("data-1", "1Gi")), "v1", pvVolume("data-1", "1Gi")),
			newObj: storageCluster("v1", pvVolume("data-1", "2Gi")),
		},
		{
			name:   "two volumes increased at once",
			oldObj: storageCluster("v1", pvVolume("data-1", "1Gi"), pvVolume("data-2", "1Gi")),
			newObj: storageCluster("v1", pvVolume("data-1", "2Gi"), pvVolume("data-2", "3Gi")),
		},
		{
			name:    "decrease below the applied size",
			oldObj:  withStatusStorage(storageCluster("v1", pvVolume("data-1", "2Gi")), "v1", pvVolume("data-1", "2Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "rack storage config cannot be updated",
		},
		{
			name:    "decrease without any applied status",
			oldObj:  storageCluster("v1", pvVolume("data-1", "2Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "rack storage config cannot be updated",
		},
		{
			name:   "revert of an increase not applied yet",
			oldObj: withStatusStorage(storageCluster("v1", pvVolume("data-1", "2Gi")), "v1", pvVolume("data-1", "1Gi")),
			newObj: storageCluster("v1", pvVolume("data-1", "1Gi")),
		},
		{
			name:    "revert below the applied size",
			oldObj:  withStatusStorage(storageCluster("v1", pvVolume("data-1", "3Gi")), "v1", pvVolume("data-1", "2Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "rack storage config cannot be updated",
		},
		{
			name:    "status of another revision does not allow a decrease",
			oldObj:  withStatusStorage(storageCluster("v1", pvVolume("data-1", "2Gi")), "v0", pvVolume("data-1", "1Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "rack storage config cannot be updated",
		},
		{
			name:   "increase with a revision bump",
			oldObj: withStatusStorage(storageCluster("v1", pvVolume("data-1", "1Gi")), "v1", pvVolume("data-1", "1Gi")),
			newObj: storageCluster("v2", pvVolume("data-1", "2Gi")),
		},
		{
			name: "rollback to an old revision with a larger size",
			oldObj: withStatusStorage(
				storageCluster("v2", pvVolume("data-1", "1Gi")), "v1", pvVolume("data-1", "1Gi"),
			),
			newObj: storageCluster("v1", pvVolume("data-1", "2Gi")),
		},
		{
			name: "rollback to an old revision with a smaller size",
			oldObj: withStatusStorage(
				storageCluster("v2", pvVolume("data-1", "2Gi")), "v1", pvVolume("data-1", "2Gi"),
			),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "already exists with different storage config",
		},
		{
			name:    "adding a persistent volume at constant revision",
			oldObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi"), pvVolume("data-2", "1Gi")),
			wantErr: "cannot add persistent volume",
		},
		{
			name:    "removing a persistent volume at constant revision",
			oldObj:  storageCluster("v1", pvVolume("data-1", "1Gi"), pvVolume("data-2", "1Gi")),
			newObj:  storageCluster("v1", pvVolume("data-1", "1Gi")),
			wantErr: "cannot remove persistent volume",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRackUpdate(logr.Discard(), tt.oldObj, tt.newObj)

			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("validateRackUpdate() unexpected error: %v", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("validateRackUpdate() = nil, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("validateRackUpdate() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
