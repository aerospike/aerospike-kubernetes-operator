package pkg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"gomodules.xyz/jsonpatch/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	asdbv1beta1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1beta1"
	"github.com/aerospike/aerospike-kubernetes-operator/pkg/utils"
)

const (
	fileSystemMountPoint = "/workdir/filesystem-volumes"
	blockMountPoint      = "/workdir/block-volumes"
	baseWipeVersion      = 6
)

type Volume struct {
	podName             string
	volumeMode          string
	volumeName          string
	effectiveWipeMethod string
	effectiveInitMethod string
	aerospikeVolumePath string
}

func (v *Volume) getMountPoint() string {
	if v.volumeMode == string(corev1.PersistentVolumeBlock) {
		return filepath.Join(blockMountPoint, v.volumeName)
	}

	return filepath.Join(fileSystemMountPoint, v.volumeName)
}

func getImageVersion(image string) (majorVersion int, err error) {
	ver, err := asdbv1beta1.GetImageVersion(image)
	if err != nil {
		return 0, err
	}

	res := strings.Split(ver, ".")

	majorVersion, err = strconv.Atoi(res[0])
	if err != nil {
		return 0, err
	}

	return majorVersion, err
}

func execute(cmd []string, stderr *os.File) error {
	if len(cmd) == 0 {
		return nil
	}

	command := exec.Command(cmd[0], cmd[1:]...) //nolint:gosec // internal command array

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

	return nil
}

func (initp *InitParams) getPodImage(ctx context.Context, podNamespacedName types.NamespacedName) (string, error) {
	initp.logger.Info("Get pod image", "podName", podNamespacedName)

	pod := &corev1.Pod{}
	if err := initp.k8sClient.Get(ctx, podNamespacedName, pod); err != nil {
		return "", err
	}

	return pod.Spec.Containers[0].Image, nil
}

func (initp *InitParams) getNodeMetadata() *asdbv1beta1.AerospikePodStatus {
	podPort := initp.networkInfo.podPort
	servicePort := initp.networkInfo.mappedPort

	if tlsEnabled, _ := strconv.ParseBool(myPodTLSEnabled); tlsEnabled {
		podPort = initp.networkInfo.podTLSPort
		servicePort = initp.networkInfo.mappedTLSPort
	}

	metadata := &asdbv1beta1.AerospikePodStatus{
		PodIP:          initp.networkInfo.podIP,
		HostInternalIP: initp.networkInfo.internalIP,
		HostExternalIP: initp.networkInfo.externalIP,
		PodPort:        int(podPort),
		Aerospike: asdbv1beta1.AerospikeInstanceSummary{
			ClusterName: clusterName,
			NodeID:      initp.nodeID,
			TLSName:     os.Getenv("MY_POD_TLS_NAME"),
		},
	}

	if initp.podServiceCreated() {
		metadata.ServicePort = servicePort
	}

	return metadata
}

func getInitializedVolumes(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster) []string {
	logger.Info("Looking for initialized volumes in status.pod initializedVolume", "podname", podName)

	if aeroCluster.Status.Pods[podName].InitializedVolumes != nil {
		return aeroCluster.Status.Pods[podName].InitializedVolumes
	}

	logger.Info("Looking for initialized volumes in status.pod initializedVolumePaths", "podname", podName)

	if aeroCluster.Status.Pods[podName].InitializedVolumePaths != nil {
		return aeroCluster.Status.Pods[podName].InitializedVolumePaths
	}

	return nil
}

func getDirtyVolumes(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster) []string {
	logger.Info("Looking for dirty volumes in status.pod dirtyVolumes", "podname", podName)

	return aeroCluster.Status.Pods[podName].DirtyVolumes
}

func getAttachedVolumes(logger logr.Logger, rack *asdbv1beta1.Rack) []asdbv1beta1.VolumeSpec {
	logger.Info("Looking for volumes in rack.effectiveStorage.volumes", "rack-id", rack.ID)

	return rack.Storage.Volumes
}

func getPersistentVolumes(volumes []asdbv1beta1.VolumeSpec) (volumeList []asdbv1beta1.VolumeSpec) {
	for idx := range volumes {
		volume := &volumes[idx]
		if volume.Source.PersistentVolume != nil {
			volumeList = append(volumeList, *volume)
		}
	}

	return volumeList
}

func newVolume(podName string, vol *asdbv1beta1.VolumeSpec) *Volume {
	var volume Volume

	volume.podName = podName
	volume.volumeMode = string(vol.Source.PersistentVolume.VolumeMode)
	volume.volumeName = vol.Name
	volume.effectiveWipeMethod = string(vol.WipeMethod)
	volume.effectiveInitMethod = string(vol.InitMethod)

	if vol.Aerospike != nil {
		volume.aerospikeVolumePath = vol.Aerospike.Path
	}

	return &volume
}

func runDD(logger logr.Logger, cmd []string, wg *sync.WaitGroup, guard chan struct{}) {
	stderr, err := os.CreateTemp("/tmp", "init-stderr")
	if err != nil {
		panic(err.Error())
	}

	defer func() {
		stderr.Close()
		os.Remove(stderr.Name())
		wg.Done()
	}()

	if err := execute(cmd, stderr); err != nil {
		dat, err := os.ReadFile(stderr.Name())
		if err != nil {
			panic(err.Error())
		}

		if !strings.Contains(string(dat), "No space left on device") {
			panic(errors.New(string(dat)))
		}
	}

	logger.Info("Execution completed", "cmd", cmd)
	<-guard
}

func runBlkdiscard(logger logr.Logger, cmd []string, wg *sync.WaitGroup, guard chan struct{}) {
	defer wg.Done()

	if err := execute(cmd, nil); err != nil {
		panic(err.Error())
	}

	logger.Info("Execution completed", "cmd", cmd)
	<-guard
}

func initVolumes(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster,
	initializedVolumes []string) ([]string, error) {
	var wg sync.WaitGroup

	rack, err := getRack(logger, podName, aeroCluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	persistentVolumes := getPersistentVolumes(getAttachedVolumes(logger, rack))
	volumeNames := make([]string, 0, len(persistentVolumes))
	guard := make(chan struct{}, workerThreads)

	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if utils.ContainsString(initializedVolumes, vol.Name) {
			continue
		}

		volume := newVolume(podName, vol)
		logger.Info("Starting initialisation", "volume", volume)

		if _, err = os.Stat(volume.getMountPoint()); err != nil {
			return volumeNames, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
		}

		switch volume.volumeMode {
		case string(corev1.PersistentVolumeBlock):
			switch volume.effectiveInitMethod {
			case string(asdbv1beta1.AerospikeVolumeMethodDD):
				dd := []string{string(asdbv1beta1.AerospikeVolumeMethodDD), "if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

				wg.Add(1)
				guard <- struct{}{}

				go runDD(logger, dd, &wg, guard)
				logger.Info("Command submitted", "volume", volume)

			case string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard):
				blkdiscard := []string{string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard), volume.getMountPoint()}

				wg.Add(1)
				guard <- struct{}{}

				go runBlkdiscard(logger, blkdiscard, &wg, guard)
				logger.Info("Command submitted", "volume", volume)

			case "none":
				logger.Info("Pass through", "volume", volume)

			default:
				return volumeNames, fmt.Errorf("invalid effective_init_method %s", volume.effectiveInitMethod)
			}

		case string(corev1.PersistentVolumeFilesystem):
			switch volume.effectiveInitMethod {
			case string(asdbv1beta1.AerospikeVolumeMethodDeleteFiles):
				find := []string{"find", volume.getMountPoint(), "-type", "f", "-delete"}

				err = execute(find, nil)
				if err != nil {
					logger.Error(err, "Failed to run find command")
				}

				logger.Info("Filepath initialised", "filepath", volume.getMountPoint())

			case "none":
				logger.Info("Pass through", "volume", volume)

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

	volumeNames = append(volumeNames, initializedVolumes...)
	logger.Info("Extended initialised volume list", "initializedVolumes", volumeNames)

	return volumeNames, nil
}

func getNamespaceVolumePaths(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster) (
	devicePaths, filePaths []string, err error) {
	rack, err := getRack(logger, podName, aeroCluster)
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
				logger.Info("Got device paths", "pod-name", podName, " device-type",
					storageEngine["type"], " devices", deviceInterface.(string))
				devicePathsSet.Insert(strings.Fields(deviceInterface.(string))...)
			}
		}

		if storageEngine["files"] != nil {
			for _, fileInterface := range storageEngine["files"].([]interface{}) {
				logger.Info("Got device paths ", "pod-name", podName, " device-type",
					storageEngine["type"], " files", fileInterface.(string))
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

func cleanDirtyVolumes(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster,
	dirtyVolumes []string) ([]string, error) {
	var wg sync.WaitGroup

	nsDevicePaths, _, err := getNamespaceVolumePaths(logger, podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get namespaced volume paths %v", err)
	}

	rack, err := getRack(logger, podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	guard := make(chan struct{}, workerThreads)

	persistentVolumes := getPersistentVolumes(getAttachedVolumes(logger, rack))
	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if vol.Aerospike == nil || !utils.ContainsString(dirtyVolumes, vol.Name) ||
			!utils.ContainsString(nsDevicePaths, vol.Aerospike.Path) {
			continue
		}

		volume := newVolume(podName, vol)
		if volume.volumeMode == string(corev1.PersistentVolumeBlock) {
			if _, err = os.Stat(volume.getMountPoint()); err != nil {
				return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
			}

			switch volume.effectiveWipeMethod {
			case string(asdbv1beta1.AerospikeVolumeMethodDD):
				dd := []string{string(asdbv1beta1.AerospikeVolumeMethodDD), "if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

				wg.Add(1)
				guard <- struct{}{}

				go runDD(logger, dd, &wg, guard)
				logger.Info("Command submitted", "volume", volume)

			case string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard):
				blkdiscard := []string{string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard), volume.getMountPoint()}

				wg.Add(1)
				guard <- struct{}{}

				go runBlkdiscard(logger, blkdiscard, &wg, guard)
				logger.Info("Command submitted", "volume", volume)

			default:
				return dirtyVolumes, fmt.Errorf("invalid effective_wipe_method %s", volume.effectiveWipeMethod)
			}

			dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
		}
	}

	close(guard)
	wg.Wait()
	logger.Info("All cleanup jobs finished successfully")

	return dirtyVolumes, nil
}

func wipeVolumes(logger logr.Logger, podName string, aeroCluster *asdbv1beta1.AerospikeCluster,
	dirtyVolumes []string) ([]string, error) {
	var wg sync.WaitGroup

	nsDevicePaths, nsFilePaths, err := getNamespaceVolumePaths(logger, podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get namespaced volume paths %v", err)
	}

	rack, err := getRack(logger, podName, aeroCluster)
	if err != nil {
		return dirtyVolumes, fmt.Errorf("failed to get rack of pod %s %v", podName, err)
	}

	workerThreads := rack.Storage.CleanupThreads
	guard := make(chan struct{}, workerThreads)

	persistentVolumes := getPersistentVolumes(getAttachedVolumes(logger, rack))
	for volIndex := range persistentVolumes {
		vol := &persistentVolumes[volIndex]
		if vol.Aerospike == nil {
			continue
		}

		volume := newVolume(podName, vol)
		switch volume.volumeMode {
		case string(corev1.PersistentVolumeBlock):
			if utils.ContainsString(nsDevicePaths, volume.aerospikeVolumePath) {
				logger.Info("Wiping volume", "volume", volume)

				if _, err := os.Stat(volume.getMountPoint()); err != nil {
					return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
				}

				switch volume.effectiveWipeMethod {
				case string(asdbv1beta1.AerospikeVolumeMethodDD):
					dd := []string{string(asdbv1beta1.AerospikeVolumeMethodDD),
						"if=/dev/zero", "of=" + volume.getMountPoint(), "bs=1M"}

					wg.Add(1)
					guard <- struct{}{}

					go runDD(logger, dd, &wg, guard)
					logger.Info("Command submitted", "volume", volume)

				case string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard):
					blkdiscard := []string{string(asdbv1beta1.AerospikeVolumeMethodBlkdiscard), volume.getMountPoint()}

					wg.Add(1)
					guard <- struct{}{}

					go runBlkdiscard(logger, blkdiscard, &wg, guard)
					logger.Info("Command submitted", "volume", volume)

				default:
					return dirtyVolumes, fmt.Errorf("invalid effective_init_method %s", volume.effectiveInitMethod)
				}

				dirtyVolumes = remove(dirtyVolumes, volume.volumeName)
			}
		case string(corev1.PersistentVolumeFilesystem):
			if volume.effectiveWipeMethod == string(asdbv1beta1.AerospikeVolumeMethodDeleteFiles) {
				if _, err := os.Stat(volume.getMountPoint()); err != nil {
					return dirtyVolumes, fmt.Errorf("mounting point %s does not exist %v", volume.getMountPoint(), err)
				}

				for _, nsFilePath := range nsFilePaths {
					if strings.HasPrefix(nsFilePath, volume.aerospikeVolumePath) {
						_, fileName := filepath.Split(nsFilePath)

						filePath := filepath.Join(volume.getMountPoint(), fileName)
						if _, err := os.Stat(filePath); err != nil {
							if os.IsNotExist(err) {
								logger.Info("Namespace file path does not exist", "filepath", filePath)
							} else {
								return dirtyVolumes, fmt.Errorf("failed to delete file %s %v", filePath, err)
							}
						} else {
							os.Remove(filePath)
							logger.Info("Deleted", "filepath", filePath)
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
	logger.Info("All wipe jobs finished successfully")

	return dirtyVolumes, nil
}

func (initp *InitParams) manageVolumesAndUpdateStatus(ctx context.Context, restartType string) error {
	podNamespacedName := getNamespacedName(initp.podName, initp.namespace)

	podImage, err := initp.getPodImage(ctx, podNamespacedName)
	if err != nil {
		return err
	}

	prevImage := ""

	if _, ok := initp.aeroCluster.Status.Pods[initp.podName]; ok {
		prevImage = initp.aeroCluster.Status.Pods[initp.podName].Image
		initp.logger.Info("Restarted", "podname", initp.podName)
	} else {
		initp.logger.Info("Initializing", "podname", initp.podName)
	}

	initializedVolumes := getInitializedVolumes(initp.logger, initp.podName, initp.aeroCluster)
	dirtyVolumes := getDirtyVolumes(initp.logger, initp.podName, initp.aeroCluster)

	initp.logger.Info("Checking if initialization needed", "podname", initp.podName, " restart-type", restartType)

	if restartType == "podRestart" {
		initializedVolumes, err = initVolumes(initp.logger, initp.podName, initp.aeroCluster, initializedVolumes)
		if err != nil {
			return err
		}

		initp.logger.Info("Checking if volumes should be wiped", "podname", initp.podName)

		if prevImage != "" {
			prevMajorVer, imageErr := getImageVersion(prevImage)
			if imageErr != nil {
				return imageErr
			}

			nextMajorVer, imageErr := getImageVersion(podImage)
			if imageErr != nil {
				return imageErr
			}

			if (nextMajorVer >= baseWipeVersion && baseWipeVersion > prevMajorVer) ||
				(nextMajorVer < baseWipeVersion && baseWipeVersion <= prevMajorVer) {
				dirtyVolumes, err = wipeVolumes(initp.logger, initp.podName, initp.aeroCluster, dirtyVolumes)
				if err != nil {
					return err
				}
			} else {
				initp.logger.Info("Volumes should not be wiped", "nextMajorVer", nextMajorVer, "prevMajorVer", prevMajorVer)
			}
		} else {
			initp.logger.Info("Volumes should not be wiped")
		}

		dirtyVolumes, err = cleanDirtyVolumes(initp.logger, initp.podName, initp.aeroCluster, dirtyVolumes)
		if err != nil {
			return err
		}
	}

	metadata := initp.getNodeMetadata()
	metadata.Image = podImage
	metadata.InitializedVolumes = initializedVolumes
	metadata.DirtyVolumes = dirtyVolumes

	initp.logger.Info("Updating pod status", "podname", initp.podName)

	if err := initp.updateStatus(ctx, metadata); err != nil {
		return err
	}

	return nil
}

func (initp *InitParams) updateStatus(ctx context.Context,
	metadata *asdbv1beta1.AerospikePodStatus) error {
	confHashBytes, err := os.ReadFile(filepath.Join(configMapDir, "aerospikeConfHash"))
	if err != nil {
		return fmt.Errorf("failed to read aerospikeConfHash file %v", err)
	}

	metadata.AerospikeConfigHash = string(confHashBytes)

	networkPolicyHashBytes, err := os.ReadFile(filepath.Join(configMapDir, "networkPolicyHash"))
	if err != nil {
		return fmt.Errorf("failed to read networkPolicyHash file %v", err)
	}

	metadata.NetworkPolicyHash = string(networkPolicyHashBytes)

	podSpecHashBytes, err := os.ReadFile(filepath.Join(configMapDir, "podSpecHash"))
	if err != nil {
		return fmt.Errorf("failed to read podSpecHash file %v", err)
	}

	metadata.PodSpecHash = string(podSpecHashBytes)

	metadata.Aerospike.AccessEndpoints = initp.getEndpoints(access)
	metadata.Aerospike.AlternateAccessEndpoints = initp.getEndpoints(alternateAccess)
	metadata.Aerospike.TLSAccessEndpoints = initp.getEndpoints(tlsAccess)
	metadata.Aerospike.TLSAlternateAccessEndpoints = initp.getEndpoints(tlsAlternateAccess)

	var patches []jsonpatch.JsonPatchOperation

	patch := jsonpatch.JsonPatchOperation{
		Operation: "replace",
		Path:      "/status/pods/" + initp.podName,
		Value:     *metadata,
	}
	patches = append(patches, patch)

	jsonPatchJSON, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("error creating json-patch : %v", err)
	}

	constantPatch := client.RawPatch(types.JSONPatchType, jsonPatchJSON)
	if err = initp.k8sClient.Status().Patch(
		ctx, initp.aeroCluster, constantPatch, client.FieldOwner("pod"),
	); err != nil {
		return fmt.Errorf("error updating status: %v", err)
	}

	return nil
}

func (initp *InitParams) getEndpoints(addressType string) []string {
	var (
		endPoints       []string
		globalAddresses []string
		globalPort      int32
	)

	switch addressType {
	case access:
		globalAddresses = initp.networkInfo.globalAddressesAndPorts.globalAccessAddress
		globalPort = initp.networkInfo.globalAddressesAndPorts.globalAccessPort

	case alternateAccess:
		globalAddresses = initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessAddress
		globalPort = initp.networkInfo.globalAddressesAndPorts.globalAlternateAccessPort

	case tlsAccess:
		globalAddresses = initp.networkInfo.globalAddressesAndPorts.globalTLSAccessAddress
		globalPort = initp.networkInfo.globalAddressesAndPorts.globalTLSAccessPort

	case tlsAlternateAccess:
		globalAddresses = initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessAddress
		globalPort = initp.networkInfo.globalAddressesAndPorts.globalTLSAlternateAccessPort
	}

	for _, addr := range globalAddresses {
		host := net.ParseIP(addr)

		switch {
		case host == nil:
			continue
		case host.To4() != nil:
			accessPoint := host.String() + ":" + strconv.Itoa(int(globalPort))
			endPoints = append(endPoints, accessPoint)
		case host.To16() != nil:
			accessPoint := "[" + host.String() + "]" + ":" + strconv.Itoa(int(globalPort))
			endPoints = append(endPoints, accessPoint)
		default:
			panic("invalid address-type")
		}
	}

	return endPoints
}
