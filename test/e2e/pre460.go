package e2e

// func DeployClusterForAllBuildsPre460(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) {
// 	// get namespace
// 	namespace, err := ctx.GetNamespace()
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	versions := []string{"4.5.3.13", "4.5.2.13", "4.5.1.18", "4.5.0.21", "4.4.0.14", "4.3.1.14", "4.3.0.8", "4.2.0.10", "4.1.0.6", "4.0.0.5"}

// 	for _, v := range versions {
// 		clusterName := "aerocluster"
// 		build := fmt.Sprintf("aerospike/aerospike-server:%s", v)

// 		// Deploy non tls cluster
// 		aeroCluster := createAerospikeClusterPre460NoTLS(clusterName, namespace, 2, build)
// 		t.Run(fmt.Sprintf("Deploy-%sNoTLS", v), func(t *testing.T) {
// 			err := deployCluster(t, f, ctx, aeroCluster)

// 			deleteCluster(t, f, ctx, aeroCluster)
// 			if err != nil {
// 				t.Fatal(err)
// 			}
// 		})

// 		// Currently no enterprise build available before 4.6.*
// 		// // Deploy tls cluster
// 		// build := fmt.Sprintf("aerospike/aerospike-server-enterprise:%s", v)

// 		// aeroCluster = createAerospikeClusterPre460TLS(clusterName, namespace, 2, build)
// 		// t.Run(fmt.Sprintf("Deploy-%sTLS", v), func(t *testing.T) {
// 		// 	err := deployCluster(t, f, ctx, aeroCluster)

// 		// 	deleteCluster(t, f, ctx, aeroCluster)
// 		// 	if err != nil {
// 		// 		t.Fatal(err)
// 		// 	}
// 		// })
// 	}
// }

// func createAerospikeClusterPre460TLS(clusterName, namespace string, size int32, build string) *aerospikev1alpha1.AerospikeCluster {
// 	// create memcached custom resource
// 	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      clusterName,
// 			Namespace: namespace,
// 		},
// 		Spec: aerospikev1alpha1.AerospikeClusterSpec{
// 			Size: size,
// 			// Build: "aerospike/aerospike-server:4.7.0.9",
// 			Build: build,
// 			BlockStorage: []aerospikev1alpha1.BlockStorageSpec{
// 				aerospikev1alpha1.BlockStorageSpec{
// 					StorageClass: "ssd",
// 					Device: []aerospikev1alpha1.DeviceSpec{
// 						aerospikev1alpha1.DeviceSpec{
// 							Name:     "/test/dev/xvdf",
// 							SizeInGB: 1,
// 						},
// 					},
// 				},
// 			},
// 			AerospikeAuthSecret: aerospikev1alpha1.AerospikeAuthSecretSpec{
// 				SecretName: authSecretName,
// 			},
// 			AerospikeConfigSecret: aerospikev1alpha1.AerospikeConfigSecretSpec{
// 				SecretName: tlsSecretName,
// 				MountPath:  "/etc/aerospike/secret",
// 			},
// 			MultiPodPerHost: true,
// 			AerospikeConfig: aerospikev1alpha1.Values{
// 				"service": map[string]interface{}{
// 					"feature-key-file": "/etc/aerospike/secret/features.conf",
// 				},
// 				"security": map[string]interface{}{
// 					"enable-security": true,
// 				},
// 				"network": map[string]interface{}{
// 					"service": map[string]interface{}{
// 						"tls-name":                "bob-cluster-a",
// 						"tls-authenticate-client": "any",
// 					},
// 					"heartbeat": map[string]interface{}{
// 						"tls-name": "bob-cluster-b",
// 					},
// 					"fabric": map[string]interface{}{
// 						"tls-name": "bob-cluster-c",
// 					},
// 					"tls": []map[string]interface{}{
// 						map[string]interface{}{
// 							"name":      "bob-cluster-a",
// 							"cert-file": "/etc/aerospike/secret/svc_cluster_chain.pem",
// 							"key-file":  "/etc/aerospike/secret/svc_key.pem",
// 							"ca-file":   "/etc/aerospike/secret/cacert.pem",
// 						},
// 						map[string]interface{}{
// 							"name":      "bob-cluster-b",
// 							"cert-file": "/etc/aerospike/secret/hb_cluster_chain.pem",
// 							"key-file":  "/etc/aerospike/secret/hb_key.pem",
// 							"ca-file":   "/etc/aerospike/secret/cacert.pem",
// 						},
// 						map[string]interface{}{
// 							"name":      "bob-cluster-c",
// 							"cert-file": "/etc/aerospike/secret/fb_cluster_chain.pem",
// 							"key-file":  "/etc/aerospike/secret/fb_key.pem",
// 							"ca-file":   "/etc/aerospike/secret/cacert.pem",
// 						},
// 					},
// 				},
// 				"namespace": []interface{}{
// 					map[string]interface{}{
// 						"name":               "test",
// 						"memory-size":        2000955200,
// 						"replication-factor": 1,
// 						"storage-engine": map[string]interface{}{
// 							"device": []interface{}{"/test/dev/xvdf"},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}
// 	return aeroCluster
// }

// // This is for community for now
// func createAerospikeClusterPre460NoTLS(clusterName, namespace string, size int32, build string) *aerospikev1alpha1.AerospikeCluster {
// 	// create memcached custom resource
// 	aeroCluster := &aerospikev1alpha1.AerospikeCluster{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      clusterName,
// 			Namespace: namespace,
// 		},
// 		Spec: aerospikev1alpha1.AerospikeClusterSpec{
// 			Size: size,
// 			// Build: "aerospike/aerospike-server:4.7.0.9",
// 			Build: build,
// 			BlockStorage: []aerospikev1alpha1.BlockStorageSpec{
// 				aerospikev1alpha1.BlockStorageSpec{
// 					StorageClass: "ssd",
// 					Device: []aerospikev1alpha1.DeviceSpec{
// 						aerospikev1alpha1.DeviceSpec{
// 							Name:     "/test/dev/xvdf",
// 							SizeInGB: 1,
// 						},
// 					},
// 				},
// 			},
// 			MultiPodPerHost: true,
// 			AerospikeConfig: aerospikev1alpha1.Values{
// 				"namespace": []interface{}{
// 					map[string]interface{}{
// 						"name":               "test",
// 						"memory-size":        2000955200,
// 						"replication-factor": 1,
// 						"storage-engine": map[string]interface{}{
// 							"device": []interface{}{"/test/dev/xvdf"},
// 						},
// 					},
// 				},
// 			},
// 		},
// 	}
// 	return aeroCluster
// }
