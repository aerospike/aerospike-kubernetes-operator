package pkg

import (
	goctx "context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/jsonpatch"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const (
	FileSystemMountPoint = "/workdir/filesystem-volumes"
	BlockMountPoint      = "/workdir/block-volumes"
	BaseWipeVersion      = 6
	BlockVolumeMode      = "Block"
	FilesystemVolumeMode = "Filesystem"
	DDMethod             = "dd"
	BLKDiscardMethod     = "blkdiscard"
)

var AddressTypeName = map[string]string{
	"access":               "accessEndpoints",
	"alternate-access":     "alternateAccessEndpoints",
	"tls-access":           "tlsAccessEndpoints",
	"tls-alternate-access": "tlsAlternateAccessEndpoints",
}

type Volume struct {
	podName             string
	volumeMode          string
	volumeName          string
	effectiveWipeMethod string
	effectiveInitMethod string
	attachmentType      string
	aerospikeVolumePath string
}

func (v *Volume) getMountPoint() string {
	if v.volumeMode == BlockVolumeMode {
		return filepath.Join(BlockMountPoint, v.volumeName)
	}

	return filepath.Join(FileSystemMountPoint, v.volumeName)
}

func (v *Volume) getAttachmentPath() string {
	return v.aerospikeVolumePath
}

func getImageVersion(image string) (version int, err error) {
	ver, err := asdbv1beta1.GetImageVersion(image)
	if err != nil {
		return 0, err
	}

	res := strings.Split(ver, ".")

	version, err = strconv.Atoi(res[0])
	if err != nil {
		return 0, err
	}

	return version, err
}

func execute(cmd []string, stderr *os.File) error {
	if len(cmd) > 0 {
		var command *exec.Cmd
		if len(cmd) > 1 {
			command = exec.Command(cmd[0], cmd[1:]...)
		} else {
			command = exec.Command(cmd[0])
		}

		if stderr != nil {
			command.Stdout = stderr
			command.Stderr = stderr
		} else {
			command.Stdout = os.Stdout
			command.Stderr = os.Stderr
		}

		if err := command.Run(); err != nil {
			return err
		}
	}

	return nil
}

func getNamespacedName(name, namespace string) types.NamespacedName {
	return types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
}

func getCluster(k8sClient client.Client, ctx goctx.Context,
	clusterNamespacedName types.NamespacedName) (*asdbv1beta1.AerospikeCluster, error) {
	log.Println("INFO: Get aerospike cluster", "cluster-name", clusterNamespacedName)

	aeroCluster := &asdbv1beta1.AerospikeCluster{}
	if err := k8sClient.Get(ctx, clusterNamespacedName, aeroCluster); err != nil {
		return nil, err
	}

	return aeroCluster, nil
}

func getPodImage(k8sClient client.Client, ctx goctx.Context, podNamespacedName types.NamespacedName) (string, error) {
	log.Println("INFO: Get pod image", "pod-name", podNamespacedName)

	pod := &corev1.Pod{}
	if err := k8sClient.Get(ctx, podNamespacedName, pod); err != nil {
		return "", err
	}

	return pod.Spec.Containers[0].Image, nil
}

//nolint:unparam // generic function
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func getNodeMetadata() *asdbv1beta1.AerospikePodStatus {
	podPort := os.Getenv("POD_PORT")
	servicePort := os.Getenv("MAPPED_PORT")

	if tlsEnabled, _ := strconv.ParseBool(getEnv("MY_POD_TLS_ENABLED", "")); tlsEnabled {
		podPort = os.Getenv("POD_TLSPORT")
		servicePort = os.Getenv("MAPPED_TLSPORT")
	}

	podPortInt, _ := strconv.Atoi(podPort)
	servicePortInt, _ := strconv.Atoi(servicePort)
	metadata := asdbv1beta1.AerospikePodStatus{
		PodIP:          getEnv("PODIP", ""),
		HostInternalIP: getEnv("INTERNALIP", ""),
		HostExternalIP: getEnv("EXTERNALIP", ""),
		PodPort:        podPortInt,
		ServicePort:    int32(servicePortInt),
		Aerospike: asdbv1beta1.AerospikeInstanceSummary{
			ClusterName: getEnv("MY_POD_CLUSTER_NAME", ""),
			NodeID:      getEnv("NODE_ID", ""),
			TLSName:     getEnv("MY_POD_TLS_NAME", ""),
		},
	}

	return &metadata
}

func getInitializedVolumes(podName string, aeroCluster *asdbv1beta1.AerospikeCluster) []string {
	log.Println("INFO: Looking for initialized volumes in status.pod.{pod_name}.initializedVolumes", "pod_name", podName)

	if aeroCluster.Status.Pods[podName].InitializedVolumes != nil {
		return aeroCluster.Status.Pods[podName].InitializedVolumes
	}

	log.Println("INFO: Looking for initialized volumes in "+
		"status.pod.{pod_name}.initializedVolumePaths", "pod_name", podName)

	if aeroCluster.Status.Pods[podName].InitializedVolumePaths != nil {
		return aeroCluster.Status.Pods[podName].InitializedVolumePaths
	}

	return nil
}

func getDirtyVolumes(podName string, aeroCluster *asdbv1beta1.AerospikeCluster) []string {
	log.Println("INFO: Looking for dirty volumes in status.pod.{pod_name}.dirtyVolumes", "pod_name", podName)
	return aeroCluster.Status.Pods[podName].DirtyVolumes
}

func getAttachedVolumes(rack *asdbv1beta1.Rack, aeroCluster *asdbv1beta1.AerospikeCluster) []asdbv1beta1.VolumeSpec {
	if volumes := rack.Storage.Volumes; len(volumes) != 0 {
		log.Println("INFO: Looking for volumes in rack.effectiveStorage.volumes", "rack-id", rack.ID)
		return volumes
	}

	log.Println("INFO: Looking for volumes in spec.storage.volumes")

	return aeroCluster.Spec.Storage.Volumes
}

func getPersistentVolumes(volumes []asdbv1beta1.VolumeSpec) []asdbv1beta1.VolumeSpec {
	var volumeList []asdbv1beta1.VolumeSpec

	for idx := range volumes {
		volume := &volumes[idx]
		if volume.Source.PersistentVolume != nil {
			volumeList = append(volumeList, *volume)
		}
	}

	return volumeList
}

func newVolume(podName string, vol *asdbv1beta1.VolumeSpec) Volume {
	var volume Volume

	volume.podName = podName
	volume.volumeMode = string(vol.Source.PersistentVolume.VolumeMode)
	volume.volumeName = vol.Name
	volume.effectiveWipeMethod = string(vol.WipeMethod)
	volume.effectiveInitMethod = string(vol.InitMethod)

	if vol.Aerospike != nil {
		volume.attachmentType = "aerospike"
		volume.aerospikeVolumePath = vol.Aerospike.Path
	}

	return volume
}

func initVolumes(podName string, aeroCluster *asdbv1beta1.AerospikeCluster, volumes []string) ([]string, error) {
	var wg sync.WaitGroup

	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	persistentVolumes := getPersistentVolumes(getAttachedVolumes(rack, aeroCluster))
	volumeNames := make([]string, 0, len(persistentVolumes))
	guard := make(chan struct{}, workerThreads)

	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if utils.ContainsString(volumes, vol.Name) {
			continue
		}

		volume := newVolume(podName, vol)
		log.Println("INFO: Starting initialisation", "volume", volume)

		if _, err = os.Stat(volume.getMountPoint()); err != nil {
			return volumeNames, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
		}

		switch volume.volumeMode {
		case BlockVolumeMode:
			switch volume.effectiveInitMethod {
			case DDMethod:
				stderr, err := os.CreateTemp("/tmp", "init-stderr")
				if err != nil {
					return volumeNames, err
				}

				dd := []string{DDMethod, "if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

				wg.Add(1)
				guard <- struct{}{}

				go func(cmd []string) {
					defer wg.Done()

					if err := execute(cmd, stderr); err != nil {
						dat, err := os.ReadFile(stderr.Name())
						if err != nil {
							stderr.Close()
							os.Remove(stderr.Name())
							panic(err.Error())
						}

						if !strings.Contains(string(dat), "No space left on device") {
							panic(err.Error())
						}
					}

					stderr.Close()
					os.Remove(stderr.Name())
					log.Println("INFO: Execution completed", "cmd", cmd)
					<-guard
				}(dd)
				log.Println("INFO: Command submitted", "volume", volume)

			case BLKDiscardMethod:
				blkdiscard := []string{BLKDiscardMethod, volume.getMountPoint()}

				wg.Add(1)
				guard <- struct{}{}

				go func(cmd []string) {
					defer wg.Done()

					if err = execute(cmd, nil); err != nil {
						panic(err.Error())
					}

					log.Println("INFO: Execution completed", "cmd", cmd)
					<-guard
				}(blkdiscard)
				log.Println("INFO: Command submitted", "volume", volume)

			case "none":
				log.Println("INFO: Pass through", "volume", volume)

			default:
				return volumeNames, fmt.Errorf("invalid effective_init_method %s", volume.effectiveInitMethod)
			}

		case FilesystemVolumeMode:
			switch volume.effectiveInitMethod {
			case "deleteFiles":
				find := []string{"find", volume.getMountPoint(), "-type", "f", "-delete"}

				err = execute(find, nil)
				if err != nil {
					log.Println("WARN: Failed to run find command", "error", err)
				}

				log.Println("INFO: Filepath initialised", "filepath", volume.getMountPoint())

			case "none":
				log.Println("INFO: Pass through", "volume", volume)

			default:
				return volumeNames, fmt.Errorf("invalid effective_init_method %s", volume.effectiveInitMethod)
			}

		default:
			return volumeNames, fmt.Errorf("invalid volume-mode %s", volume.volumeMode)
		}

		volumeNames = append(volumeNames, volume.volumeName)
	}

	close(guard)
	wg.Wait()

	volumeNames = append(volumeNames, volumes...)
	log.Println("INFO: Extended initialised volume list", "volumes", volumeNames)

	return volumeNames, nil
}

func getRack(podName string, aeroCluster *asdbv1beta1.AerospikeCluster) (*asdbv1beta1.Rack, error) {
	res := strings.Split(podName, "-")

	rackID, err := strconv.Atoi(res[len(res)-2])
	if err != nil {
		return nil, err
	}

	log.Println("INFO: Checking for rack in rackConfig", "rack-id", rackID)

	racks := aeroCluster.Spec.RackConfig.Racks
	for idx := range racks {
		rack := &racks[idx]
		if rack.ID == rackID {
			return rack, nil
		}
	}

	return nil, fmt.Errorf("rack with rack-id %d not found", rackID)
}

func getNamespaceVolumePaths(podName string, aeroCluster *asdbv1beta1.AerospikeCluster) (
	devicePaths, filePaths []string, err error) {
	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return devicePaths, filePaths, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	devicePathsSet := sets.NewString()
	filePathsSet := sets.NewString()

	namespaces := rack.AerospikeConfig.Value["namespaces"].([]interface{})
	for _, namespace := range namespaces {
		storageEngine := namespace.(map[string]interface{})["storage-engine"].(map[string]interface{})

		if storageEngine["devices"] != nil {
			for _, deviceInterface := range storageEngine["devices"].([]interface{}) {
				log.Println("INFO: Got device paths", "pod-name", podName, "device-type",
					storageEngine["type"], "devices", deviceInterface.(string))
				devicePathsSet.Insert(strings.Fields(deviceInterface.(string))...)
			}
		}

		if storageEngine["files"] != nil {
			for _, fileInterface := range storageEngine["files"].([]interface{}) {
				log.Println("INFO: Got device paths", "pod-name", podName, "device-type",
					storageEngine["type"], "files", fileInterface.(string))
				filePathsSet.Insert(strings.Fields(fileInterface.(string))...)
			}
		}
	}

	devicePaths = devicePathsSet.List()
	filePaths = filePathsSet.List()

	return devicePaths, filePaths, nil
}

func remove(s []string, r string) []string {
	for i, v := range s {
		if v == r {
			return append(s[:i], s[i+1:]...)
		}
	}

	return s
}

func cleanDirtyVolumes(podName string, aeroCluster *asdbv1beta1.AerospikeCluster,
	dirtyVolumes []string) ([]string, error) {
	var wg sync.WaitGroup

	nsDevicePaths, _, err := getNamespaceVolumePaths(podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get namespaced volume paths %v", err)
	}

	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	guard := make(chan struct{}, workerThreads)

	persistentVolumes := getPersistentVolumes(getAttachedVolumes(rack, aeroCluster))
	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if vol.Aerospike == nil || !utils.ContainsString(dirtyVolumes, vol.Name) ||
			!utils.ContainsString(nsDevicePaths, vol.Aerospike.Path) {
			continue
		}

		volume := newVolume(podName, vol)
		if volume.volumeMode == BlockVolumeMode {
			if _, err = os.Stat(volume.getMountPoint()); err != nil {
				return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
			}

			switch volume.effectiveWipeMethod {
			case DDMethod:
				stderr, err := os.CreateTemp("/tmp", "init-stderr")
				if err != nil {
					return dirtyVolumes, err
				}

				dd := []string{DDMethod, "if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

				wg.Add(1)
				guard <- struct{}{}

				go func(cmd []string) {
					defer wg.Done()

					if err := execute(cmd, stderr); err != nil {
						dat, err := os.ReadFile(stderr.Name())
						if err != nil {
							stderr.Close()
							os.Remove(stderr.Name())
							panic(err.Error())
						}

						if !strings.Contains(string(dat), "No space left on device") {
							panic(err.Error())
						}
					}

					stderr.Close()
					os.Remove(stderr.Name())
					log.Println("INFO: Execution completed", "cmd", cmd)
					<-guard

					dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
				}(dd)
				log.Println("INFO: Command submitted", "volume", volume)

			case BLKDiscardMethod:
				blkdiscard := []string{BLKDiscardMethod, volume.getMountPoint()}

				wg.Add(1)
				guard <- struct{}{}

				go func(cmd []string) {
					defer wg.Done()

					if err := execute(cmd, nil); err != nil {
						panic(err.Error())
					}

					log.Println("INFO: Execution completed", "cmd", cmd)
					<-guard

					dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
				}(blkdiscard)
				log.Println("INFO: Command submitted", "volume", volume)

			default:
				return dirtyVolumes, fmt.Errorf("invalid effective_wipe_method %s", volume.effectiveWipeMethod)
			}
		}
	}

	close(guard)
	wg.Wait()
	log.Println("INFO: All cleanup jobs finished successfully")

	return dirtyVolumes, nil
}

func wipeVolumes(podName string, aeroCluster *asdbv1beta1.AerospikeCluster, dirtyVolumes []string) ([]string, error) {
	var wg sync.WaitGroup

	nsDevicePaths, nsFilePaths, err := getNamespaceVolumePaths(podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get namespaced volume paths %v", err)
	}

	rack, err := getRack(podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	guard := make(chan struct{}, workerThreads)

	persistentVolumes := getPersistentVolumes(getAttachedVolumes(rack, aeroCluster))
	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if vol.Aerospike == nil {
			continue
		}

		volume := newVolume(podName, vol)
		switch volume.volumeMode {
		case BlockVolumeMode:
			if utils.ContainsString(nsDevicePaths, volume.aerospikeVolumePath) {
				log.Println("INFO: Wiping volume", "volume", volume)

				if _, err := os.Stat(volume.getMountPoint()); err != nil {
					return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
				}

				switch volume.effectiveWipeMethod {
				case DDMethod:
					stderr, err := os.CreateTemp("/tmp", "init-stderr")
					if err != nil {
						return dirtyVolumes, err
					}

					dd := []string{DDMethod, "if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

					wg.Add(1)
					guard <- struct{}{}

					go func(cmd []string) {
						wg.Done()

						if err := execute(cmd, stderr); err != nil {
							dat, err := os.ReadFile(stderr.Name())
							if err != nil {
								stderr.Close()
								os.Remove(stderr.Name())
								panic(err.Error())
							}

							if !strings.Contains(string(dat), "No space left on device") {
								panic(err.Error())
							}
						}

						stderr.Close()
						os.Remove(stderr.Name())
						log.Println("INFO: Execution completed", "cmd", cmd)
						<-guard

						dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
					}(dd)
					log.Println("INFO: Command submitted", "volume", volume)

				case BLKDiscardMethod:
					blkdiscard := []string{BLKDiscardMethod, volume.getMountPoint()}

					wg.Add(1)
					guard <- struct{}{}

					go func(cmd []string) {
						wg.Done()

						if err := execute(cmd, nil); err != nil {
							panic(err.Error())
						}

						log.Println("INFO: Execution completed", "cmd", cmd)
						<-guard

						dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
					}(blkdiscard)
					log.Println("INFO: Command submitted", "volume", volume)

				default:
					return dirtyVolumes, fmt.Errorf("invalid effective_init_method %s", volume.effectiveInitMethod)
				}
			}
		case FilesystemVolumeMode:
			if volume.effectiveWipeMethod == "deleteFiles" {
				if _, err := os.Stat(volume.getMountPoint()); err != nil {
					return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
				}

				for _, nsFilePath := range nsFilePaths {
					if strings.HasPrefix(nsFilePath, volume.getAttachmentPath()) {
						_, fileName := filepath.Split(nsFilePath)
						filePath := filepath.Join(volume.getMountPoint(), fileName)
						if _, err := os.Stat(filePath); err == nil {
							os.Remove(filePath)
							log.Println("INFO: Deleted", "filepath", filePath)
						} else if errors.IsNotFound(err) {
							log.Println("INFO: Namespace file path does not exist", "filepath", filePath)
						} else {
							return dirtyVolumes, fmt.Errorf("failed to delete file %s %v", filePath, err)
						}
					}
				}
			} else {
				return dirtyVolumes, fmt.Errorf("invalid effective_wipe_method %s", volume.effectiveWipeMethod)
			}

		default:
			return dirtyVolumes, fmt.Errorf("invalid volume-mode %s", volume.volumeMode)
		}
	}

	close(guard)
	wg.Wait()
	log.Println("INFO: All wipe jobs finished successfully")

	return dirtyVolumes, nil
}

func ManageVolumesAndUpdateStatus(podName, namespace, clusterName string, restartType *string) error {
	cfg := ctrl.GetConfigOrDie()

	if err := clientgoscheme.AddToScheme(clientgoscheme.Scheme); err != nil {
		return err
	}

	if err := asdbv1beta1.AddToScheme(clientgoscheme.Scheme); err != nil {
		return err
	}

	k8sClient, err := client.New(
		cfg, client.Options{Scheme: clientgoscheme.Scheme},
	)

	if err != nil {
		return err
	}

	clusterNamespacedName := getNamespacedName(clusterName, namespace)
	podNamespacedName := getNamespacedName(podName, namespace)

	aeroCluster, err := getCluster(k8sClient, goctx.TODO(), clusterNamespacedName)
	if err != nil {
		return err
	}

	podImage, err := getPodImage(k8sClient, goctx.TODO(), podNamespacedName)
	if err != nil {
		return err
	}

	prevImage := ""

	if _, ok := aeroCluster.Status.Pods[podName]; ok {
		prevImage = aeroCluster.Status.Pods[podName].Image
		log.Println("INFO: Restarted", "podname", podName)
	} else {
		log.Println("INFO: Initializing", "podname", podName)
	}

	volumes := getInitializedVolumes(podName, aeroCluster)
	dirtyVolumes := getDirtyVolumes(podName, aeroCluster)

	log.Println("INFO: Checking if initialization needed", "podname", podName, "restart-type", *restartType)

	if *restartType == "podRestart" {
		volumes, err = initVolumes(podName, aeroCluster, volumes)
		if err != nil {
			return err
		}

		log.Println("INFO: Checking if volumes should be wiped", "podname", podName)

		if prevImage != "" {
			prevMajorVer, err := getImageVersion(prevImage)
			if err != nil {
				return err
			}

			nextMajorVer, err := getImageVersion(podImage)
			if err != nil {
				return err
			}

			if (nextMajorVer >= BaseWipeVersion && BaseWipeVersion < prevMajorVer) ||
				(nextMajorVer < BaseWipeVersion && BaseWipeVersion <= prevMajorVer) {
				dirtyVolumes, err = wipeVolumes(podName, aeroCluster, dirtyVolumes)
				if err != nil {
					return err
				}
			} else {
				log.Println("INFO: Volumes should not be wiped", "nextMajorVer", nextMajorVer, "prevMajorVer", prevMajorVer)
			}
		} else {
			log.Println("INFO: Volumes should not be wiped")
		}

		dirtyVolumes, err = cleanDirtyVolumes(podName, aeroCluster, dirtyVolumes)
		if err != nil {
			return err
		}
	}

	metadata := getNodeMetadata()

	log.Println("INFO: Updating pod status", "podname", podName)

	if err := updateStatus(k8sClient, goctx.TODO(), aeroCluster, podName, podImage,
		metadata, volumes, dirtyVolumes); err != nil {
		return err
	}

	return nil
}

func updateStatus(k8sClient client.Client, ctx goctx.Context, aeroCluster *asdbv1beta1.AerospikeCluster,
	podName, podImage string, metadata *asdbv1beta1.AerospikePodStatus, volumes, dirtyVolumes []string) error {
	data, err := os.ReadFile("aerospikeConfHash")
	if err != nil {
		return fmt.Errorf("failed to read aerospikeConfHash file %v", err)
	}

	confHash := string(data)

	data, err = os.ReadFile("networkPolicyHash")
	if err != nil {
		return fmt.Errorf("failed to read networkPolicyHash file %v", err)
	}

	networkPolicyHash := string(data)

	data, err = os.ReadFile("podSpecHash")
	if err != nil {
		return fmt.Errorf("failed to read podSpecHash file %v", err)
	}

	podSpecHash := string(data)
	metadata.Image = podImage
	metadata.InitializedVolumes = volumes
	metadata.DirtyVolumes = dirtyVolumes
	metadata.AerospikeConfigHash = confHash
	metadata.NetworkPolicyHash = networkPolicyHash
	metadata.PodSpecHash = podSpecHash

	for podAddrName, confAddrName := range AddressTypeName {
		switch confAddrName {
		case "access":
			metadata.Aerospike.AccessEndpoints = getEndpoints(podAddrName)
		case "alternate-access":
			metadata.Aerospike.AlternateAccessEndpoints = getEndpoints(podAddrName)
		case "tls-access":
			metadata.Aerospike.TLSAccessEndpoints = getEndpoints(podAddrName)
		case "tls-alternate-access":
			metadata.Aerospike.TLSAlternateAccessEndpoints = getEndpoints(podAddrName)
		}
	}

	var patches []jsonpatch.JsonPatchOperation

	patch := jsonpatch.JsonPatchOperation{
		Operation: "replace",
		Path:      "/status/pods/" + podName,
		Value:     *metadata,
	}
	patches = append(patches, patch)

	jsonPatchJSON, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("error creating json-patch : %v", err)
	}

	constantPatch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)
	if err = k8sClient.Status().Patch(
		ctx, aeroCluster, constantPatch, client.FieldOwner("pod"),
	); err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}

	return nil
}

func getEndpoints(addressType string) []string {
	var endpoint []string

	addrType := strings.ReplaceAll(addressType, "-", "_")
	globalAddr := "global_" + addrType + "_address"
	globalPort := "global_" + addrType + "_port"
	host := net.ParseIP(os.Getenv(globalAddr))

	port := os.Getenv(globalPort)
	if host.To4() != nil {
		accessPoint := string(host) + ":" + port
		endpoint = append(endpoint, accessPoint)
	} else if host.To16() != nil {
		accessPoint := "[" + string(host) + "]" + ":" + port
		endpoint = append(endpoint, accessPoint)
	} else {
		panic("invalid address-type")
	}

	return endpoint
}
