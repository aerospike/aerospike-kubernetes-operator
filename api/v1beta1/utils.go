package v1beta1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
	lib "github.com/aerospike/aerospike-management-lib"
)

const BackupSvcMinSupportedVersion = "3.0.0"

func ValidateBackupSvcVersion(image string) error {
	version, err := asdbv1.GetImageVersion(image)
	if err != nil {
		return err
	}

	val, err := lib.CompareVersions(version, BackupSvcMinSupportedVersion)
	if err != nil {
		return fmt.Errorf("failed to check backup service image version: %v", err)
	}

	if val < 0 {
		return fmt.Errorf("backup service version %s is not supported. Minimum supported version is %s",
			version, BackupSvcMinSupportedVersion)
	}

	return nil
}

// ValidateBackupSvcSupportedVersion validates the supported backup service version.
// It returns an error if the backup service version is less than 3.0.0.
func ValidateBackupSvcSupportedVersion(k8sClient client.Client, name, namespace string) error {
	var backupSvc AerospikeBackupService

	if err := k8sClient.Get(context.TODO(),
		types.NamespacedName{Name: name, Namespace: namespace},
		&backupSvc,
	); err != nil {
		return err
	}

	return ValidateBackupSvcVersion(backupSvc.Spec.Image)
}

func NamePrefix(nsNm types.NamespacedName) string {
	return nsNm.Namespace + "-" + nsNm.Name
}
