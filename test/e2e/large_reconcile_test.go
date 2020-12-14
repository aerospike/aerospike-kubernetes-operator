package e2e

import (
	goctx "context"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/aerospike/aerospike-kubernetes-operator/pkg/apis"
	aerospikev1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/pkg/apis/aerospike/v1alpha1"
	as "github.com/ashishshinde/aerospike-client-go"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestLargeReconcile(t *testing.T) {
	aeroClusterList := &aerospikev1alpha1.AerospikeClusterList{}
	if err := framework.AddToFrameworkScheme(apis.AddToScheme, aeroClusterList); err != nil {
		t.Fatalf("Failed to add AerospikeCluster custom resource scheme to framework: %v", err)
	}

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	// get global framework variables
	f := framework.Global

	initializeOperator(t, f, ctx)

	t.Run("Positive", func(t *testing.T) {
		// get namespace
		namespace, err := ctx.GetNamespace()
		if err != nil {
			t.Fatal(err)
		}
		clusterName := "aerocluster"
		clusterNamespacedName := getClusterNamespacedName(clusterName, namespace)

		// Create a 5 node cluster
		aeroCluster := createDummyAerospikeCluster(clusterNamespacedName, 4)
		networkPolicy := aerospikev1alpha1.AerospikeNetworkPolicy{
			AccessType:             aerospikev1alpha1.AerospikeNetworkTypeHostExternal,
			AlternateAccessType:    aerospikev1alpha1.AerospikeNetworkTypeHostExternal,
			TLSAccessType:          aerospikev1alpha1.AerospikeNetworkTypeHostExternal,
			TLSAlternateAccessType: aerospikev1alpha1.AerospikeNetworkTypeHostExternal,
		}
		aeroCluster.Spec.AerospikeNetworkPolicy = networkPolicy

		if err := deployCluster(t, f, ctx, aeroCluster); err != nil {
			t.Fatal(err)
		}

		if err := loadDataInCluster(aeroCluster); err != nil {
			t.Fatal(err)
		}

		t.Run("ScaleDown", func(t *testing.T) {
			// Create a 5 node cluster
			// Add some data to make migration time taking
			// Change size to 2
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Size = 2
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}

			// Change size to 4 immediately
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Size = 4
			err = f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}

			// Cluster size should never go below 4,
			// as only one node is removed at a time and before reducing 2nd node, we changed the size to 4
			if err := waitForClusterScaleDown(t, f, aeroCluster, int(aeroCluster.Spec.Size), retryInterval, getTimeout(4)); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("RollingRestart", func(t *testing.T) {
			// Create a 5 node cluster
			// Add some data to make migration time taking
			// Change config
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			// oldService := aeroCluster.Spec.AerospikeConfig["service"]
			tempConf := 15000
			aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["proto-fd-max"] = tempConf
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}

			// Change config back to original value
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.AerospikeConfig["service"].(map[string]interface{})["proto-fd-max"] = 16000
			err = f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}

			// Cluster status should never get updated with old conf "tempConf"
			if err := waitForClusterRollingRestart(t, f, aeroCluster, int(aeroCluster.Spec.Size), tempConf, retryInterval, getTimeout(4)); err != nil {
				t.Fatal(err)
			}
		})
		t.Run("Upgrade", func(t *testing.T) {
			// Test1
			// Create a 5 node cluster
			// Add some data to make migration time taking
			// Change build
			aeroCluster := getCluster(t, f, ctx, clusterNamespacedName)
			tempImage := imageToUpgrade
			aeroCluster.Spec.Image = imageToUpgrade
			err := f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
			// Change build back to original
			aeroCluster = getCluster(t, f, ctx, clusterNamespacedName)
			aeroCluster.Spec.Image = latestClusterImage
			err = f.Client.Update(goctx.TODO(), aeroCluster)
			if err != nil {
				t.Fatal(err)
			}
			// Only 1 pod need upgrade
			if err := waitForClusterUpgrade(t, f, aeroCluster, int(aeroCluster.Spec.Size), tempImage, retryInterval, getTimeout(4)); err != nil {
				t.Fatal(err)
			}

			// Test2
			// Create a 5 node cluster
			// Add some data to make migration time taking
			// Change build to build1
			// Change build to build2
			// Only a single pod may have build1 at max in whole upgrade..ultimately all should reach build2
		})
		t.Run("WaitingForStableCluster", func(t *testing.T) {
			t.Run("LargeMigration", func(t *testing.T) {
				// Create a 5 node cluster
				// Add large data to make migration time taking
				// Change size to 2
				// Change size to 4 immediately
				// Cluster size should never go below 4,
			})
			t.Run("ColdStart", func(t *testing.T) {

			})
		})

		deleteCluster(t, f, ctx, aeroCluster)

	})
	t.Run("Negative", func(t *testing.T) {

	})
}

func loadDataInCluster(aeroCluster *aerospikev1alpha1.AerospikeCluster) error {
	// Current user pass set in dummycluster
	clientP, err := getClient(aeroCluster, &framework.Global.Client.Client)
	if err != nil {
		return fmt.Errorf("Error creating client: %v", err)
	}

	client := *clientP
	defer client.Close()

	keyPrefix := "testkey"

	size := 1000
	bufferSize := 10000
	token := make([]byte, bufferSize)
	rand.Read(token)

	fmt.Printf("Loading record, isClusterConnected %v\n", clientP.IsConnected())
	// loads size * bufferSize data
	for i := 0; i < size; i++ {
		key, err := as.NewKey(aeroCluster.Namespace, "", keyPrefix+strconv.Itoa(i))
		if err != nil {
			return err
		}
		binMap := map[string]interface{}{
			"testbin": token,
		}
		wp := as.NewWritePolicy(1, 500)
		wp.MaxRetries = 10
		wp.TotalTimeout = time.Second * 1
		err = client.Put(wp, key, binMap)
		if err != nil {
			return err
		}
		fmt.Print(strconv.Itoa(i) + ", ")
	}

	return nil
}

func waitForClusterScaleDown(t *testing.T, f *framework.Framework, aeroCluster *aerospikev1alpha1.AerospikeCluster, replicas int, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Fetch the AerospikeCluster instance
		newCluster := &aerospikev1alpha1.AerospikeCluster{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
				return false, nil
			}
			return false, err
		}
		t.Logf("Waiting for full availability of %s AerospikeCluster (%d/%d)\n", aeroCluster.Name, aeroCluster.Status.Size, replicas)

		if int(newCluster.Status.Size) < replicas {
			err := fmt.Errorf("Cluster size can not go below replica size")
			t.Logf(err.Error())
			return false, err
		}

		podList, err := getClusterPodList(f, aeroCluster)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < replicas {
			err := fmt.Errorf("Cluster pods number can not go below replica size")
			t.Logf(err.Error())
			return false, err
		}

		return isClusterStateValid(t, f, aeroCluster, newCluster, replicas), nil
	})
	if err != nil {
		return err
	}
	t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	return nil
}

func waitForClusterRollingRestart(t *testing.T, f *framework.Framework, aeroCluster *aerospikev1alpha1.AerospikeCluster, replicas int, tempConf int, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Fetch the AerospikeCluster instance
		newCluster := &aerospikev1alpha1.AerospikeCluster{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
				return false, nil
			}
			return false, err
		}
		t.Logf("Waiting for full availability of %s AerospikeCluster (%d/%d)\n", aeroCluster.Name, aeroCluster.Status.Size, replicas)

		protofdmax := newCluster.Status.AerospikeConfig["service"].(map[string]interface{})["proto-fd-max"].(int)
		if protofdmax == tempConf {
			err := fmt.Errorf("Cluster status can not be updated with intermediate conf value %d, it should have only final value", tempConf)
			t.Logf(err.Error())
			return false, err
		}
		t.Logf("conf value to check proto-fd-max %d", protofdmax)

		return isClusterStateValid(t, f, aeroCluster, newCluster, replicas), nil
	})
	if err != nil {
		return err
	}
	t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	return nil
}

func waitForClusterUpgrade(t *testing.T, f *framework.Framework, aeroCluster *aerospikev1alpha1.AerospikeCluster, replicas int, tempImage string, retryInterval, timeout time.Duration) error {
	err := wait.Poll(retryInterval, timeout, func() (done bool, err error) {
		// Fetch the AerospikeCluster instance
		newCluster := &aerospikev1alpha1.AerospikeCluster{}
		err = f.Client.Get(goctx.TODO(), types.NamespacedName{Name: aeroCluster.Name, Namespace: aeroCluster.Namespace}, newCluster)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of %s AerospikeCluster\n", aeroCluster.Name)
				return false, nil
			}
			return false, err
		}
		t.Logf("Waiting for full availability of %s AerospikeCluster (%d/%d)\n", aeroCluster.Name, aeroCluster.Status.Size, replicas)

		if newCluster.Status.Image == tempImage {
			err := fmt.Errorf("Cluster status can not be updated with intermediate image value %s, it should have only final value", tempImage)
			t.Logf(err.Error())
			return false, err
		}

		return isClusterStateValid(t, f, aeroCluster, newCluster, replicas), nil
	})
	if err != nil {
		return err
	}
	t.Logf("AerospikeCluster available (%d/%d)\n", replicas, replicas)

	return nil
}
