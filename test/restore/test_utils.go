package restore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aerospike/aerospike-backup-service/pkg/model"
	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
)

const (
	timeout   = 2 * time.Minute
	interval  = 2 * time.Second
	namespace = "test"
)

var testCtx = context.TODO()

var backupServiceName, backupServiceNamespace string

var backupDataPath string

var pkgLog = ctrl.Log.WithName("aerospikerestore")

var backupNsNm = types.NamespacedName{
	Name:      "sample-backup",
	Namespace: namespace,
}

var sourceAerospikeClusterNsNm = types.NamespacedName{
	Name:      "aerocluster",
	Namespace: namespace,
}

var destinationAerospikeClusterNsNm = types.NamespacedName{
	Name:      "destination-aerocluster",
	Namespace: namespace,
}

func newRestore(restoreNsNm types.NamespacedName, restoreType asdbv1beta1.RestoreType,
) (*asdbv1beta1.AerospikeRestore, error) {
	configBytes, err := getRestoreConfBytes(backupDataPath)
	if err != nil {
		return nil, err
	}

	restore := newRestoreWithEmptyConfig(restoreNsNm, restoreType)

	restore.Spec.Config = runtime.RawExtension{
		Raw: configBytes,
	}

	return restore, nil
}

func newRestoreWithConfig(restoreNsNm types.NamespacedName, restoreType asdbv1beta1.RestoreType, configBytes []byte,
) *asdbv1beta1.AerospikeRestore {
	restore := newRestoreWithEmptyConfig(restoreNsNm, restoreType)

	restore.Spec.Config = runtime.RawExtension{
		Raw: configBytes,
	}

	return restore
}

func newRestoreWithEmptyConfig(restoreNsNm types.NamespacedName, restoreType asdbv1beta1.RestoreType,
) *asdbv1beta1.AerospikeRestore {
	return &asdbv1beta1.AerospikeRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      restoreNsNm.Name,
			Namespace: restoreNsNm.Namespace,
		},
		Spec: asdbv1beta1.AerospikeRestoreSpec{
			BackupService: asdbv1beta1.BackupService{
				Name:      backupServiceName,
				Namespace: backupServiceNamespace,
			},
			Type: restoreType,
		},
	}
}

func getRestoreObj(cl client.Client, restoreNsNm types.NamespacedName) (*asdbv1beta1.AerospikeRestore, error) {
	var restore asdbv1beta1.AerospikeRestore

	if err := cl.Get(testCtx, restoreNsNm, &restore); err != nil {
		return nil, err
	}

	return &restore, nil
}

func createRestore(cl client.Client, restore *asdbv1beta1.AerospikeRestore) error {
	if err := cl.Create(testCtx, restore); err != nil {
		return err
	}

	return waitForRestore(cl, restore, timeout)
}

func createRestoreWithTO(cl client.Client, restore *asdbv1beta1.AerospikeRestore, timeout time.Duration) error {
	if err := cl.Create(testCtx, restore); err != nil {
		return err
	}

	return waitForRestore(cl, restore, timeout)
}

func deleteRestore(cl client.Client, restore *asdbv1beta1.AerospikeRestore) error {
	if err := cl.Delete(testCtx, restore); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	for {
		_, err := getRestoreObj(cl, types.NamespacedName{
			Namespace: restore.Namespace,
			Name:      restore.Name,
		})

		if err != nil {
			if k8serrors.IsNotFound(err) {
				break
			}

			return err
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}

func waitForRestore(cl client.Client, restore *asdbv1beta1.AerospikeRestore,
	timeout time.Duration) error {
	namespaceName := types.NamespacedName{
		Name: restore.Name, Namespace: restore.Namespace,
	}

	if err := wait.PollUntilContextTimeout(
		testCtx, 1*time.Second,
		timeout, true, func(ctx context.Context) (bool, error) {
			if err := cl.Get(ctx, namespaceName, restore); err != nil {
				return false, nil
			}

			if restore.Status.Phase != asdbv1beta1.AerospikeRestoreCompleted {
				pkgLog.Info(fmt.Sprintf("Restore is in %s phase", restore.Status.Phase))
				return false, nil
			}

			return true, nil
		},
	); err != nil {
		return err
	}

	pkgLog.Info(fmt.Sprintf("Restore is in %s phase", restore.Status.Phase))

	if restore.Status.JobID == nil {
		return fmt.Errorf("restore job id is not set")
	}

	if restore.Status.RestoreResult.Raw == nil {
		return fmt.Errorf("restore result is not set")
	}

	var restoreResult model.RestoreJobStatus

	if err := json.Unmarshal(restore.Status.RestoreResult.Raw, &restoreResult); err != nil {
		return err
	}

	if restoreResult.Status != model.JobStatusDone {
		return fmt.Errorf("restore job status is not done")
	}

	if restoreResult.Error != "" {
		return fmt.Errorf("restore job failed with error: %s", restoreResult.Error)
	}

	return nil
}

func getRestoreConfBytes(backupPath string) ([]byte, error) {
	restoreConfig := getRestoreConfigInMap(backupPath)

	configBytes, err := json.Marshal(restoreConfig)
	if err != nil {
		return nil, err
	}

	pkgLog.Info(string(configBytes))

	return configBytes, nil
}

func getRestoreConfigInMap(backupPath string) map[string]interface{} {
	return map[string]interface{}{
		"destination": map[string]interface{}{
			"label": "destinationCluster",
			"credentials": map[string]interface{}{
				"password": "admin123",
				"user":     "admin",
			},
			"seed-nodes": []map[string]interface{}{
				{
					"host-name": fmt.Sprintf("%s.%s.svc.cluster.local",
						destinationAerospikeClusterNsNm.Name, destinationAerospikeClusterNsNm.Namespace,
					),
					"port": 3000,
				},
			},
		},
		"policy": map[string]interface{}{
			"parallel":      3,
			"no-generation": true,
			"no-indexes":    true,
		},
		"source": map[string]interface{}{
			"path": backupPath,
			"type": "local",
		},
	}
}
