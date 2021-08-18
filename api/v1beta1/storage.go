package v1beta1

import (
	"fmt"
	"path/filepath"
	"reflect"

	v1 "k8s.io/api/core/v1"
)

// ValidateStorageSpecChange indicates if a change to storage spec is safe to apply.
func (v *AerospikeStorageSpec) ValidateStorageSpecChange(new AerospikeStorageSpec) error {
	for _, newVolume := range new.Volumes {
		for _, oldVolume := range v.Volumes {
			if oldVolume.Name == newVolume.Name {
				if !oldVolume.IsSafeChange(newVolume) {
					// Validate same volumes
					return fmt.Errorf("cannot change volumes old: %v new %v", oldVolume, newVolume)
				}
				break
			}
		}
	}

	_, _, err := v.validateAddedOrRemovedVolumes(new)
	return err
}

// validateAddedOrRemovedVolumes returns volumes that were added or removed.
// Currently only configMap volume is allowed to be added or removed dynamically
func (v *AerospikeStorageSpec) validateAddedOrRemovedVolumes(new AerospikeStorageSpec) (addedVolumes []VolumeSpec, removedVolumes []VolumeSpec, err error) {
	// Validate Persistent volume
	for _, newVolume := range new.Volumes {
		matched := false
		for _, oldVolume := range v.Volumes {
			if oldVolume.Name == newVolume.Name {
				matched = true
				break
			}
		}

		if !matched {
			if newVolume.Source.PersistentVolume != nil {
				return []VolumeSpec{}, []VolumeSpec{}, fmt.Errorf("cannot add persistent volume: %v", newVolume)
			}
			addedVolumes = append(addedVolumes, newVolume)
		}
	}

	for _, oldVolume := range v.Volumes {
		matched := false
		for _, newVolume := range new.Volumes {
			if oldVolume.Name == newVolume.Name {
				matched = true
			}
		}

		if !matched {
			if oldVolume.Source.PersistentVolume != nil {
				return []VolumeSpec{}, []VolumeSpec{}, fmt.Errorf("cannot remove persistent volume: %v", oldVolume)
			}
			removedVolumes = append(removedVolumes, oldVolume)
		}
	}

	return addedVolumes, removedVolumes, nil
}

// SetDefaults sets default values for storage spec fields.
func (v *AerospikeStorageSpec) SetDefaults() {
	defaultFilesystemInitMethod := AerospikeVolumeInitMethodNone
	defaultBlockInitMethod := AerospikeVolumeInitMethodNone
	defaultCascadeDelete := false

	// Set storage level defaults.
	v.FileSystemVolumePolicy.SetDefaults(&AerospikePersistentVolumePolicySpec{InitMethod: defaultFilesystemInitMethod, CascadeDelete: defaultCascadeDelete})
	v.BlockVolumePolicy.SetDefaults(&AerospikePersistentVolumePolicySpec{InitMethod: defaultBlockInitMethod, CascadeDelete: defaultCascadeDelete})

	for i := range v.Volumes {
		if v.Volumes[i].Source.PersistentVolume == nil {
			// All other sources are considered to be mount, for now.
			v.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&v.FileSystemVolumePolicy)
		} else if v.Volumes[i].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
			v.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&v.BlockVolumePolicy)
		} else if v.Volumes[i].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
			v.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&v.FileSystemVolumePolicy)
		}
	}
}

// GetConfigMaps returns the config map volumes from the storage spec.
func (v *AerospikeStorageSpec) GetConfigMaps() (configMaps []VolumeSpec) {
	for _, volume := range v.Volumes {
		if volume.Source.ConfigMap != nil {
			configMaps = append(configMaps, volume)
		}
	}
	return configMaps
}

// GetPVs returns the PV volumes from the storage spec.
func (v *AerospikeStorageSpec) GetPVs() (PVs []VolumeSpec) {
	for _, volume := range v.Volumes {
		if volume.Source.PersistentVolume != nil {
			PVs = append(PVs, volume)
		}
	}
	return PVs
}

// GetNonPVs returns the non PV volumes from the storage spec.
func (v *AerospikeStorageSpec) GetNonPVs() (nonPVs []VolumeSpec) {
	for _, volume := range v.Volumes {
		if volume.Source.PersistentVolume == nil {
			nonPVs = append(nonPVs, volume)
		}
	}
	return nonPVs
}

// GetAerospikeStorageList gives blockStorageDeviceList and fileStorageList
func (v *AerospikeStorageSpec) GetAerospikeStorageList() (blockStorageDeviceList []string, fileStorageList []string, err error) {

	for _, volume := range v.Volumes {

		if volume.Aerospike != nil {
			// TODO: Do we need to check for other type of sources
			if volume.Source.PersistentVolume == nil {
				continue
			}
			if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
				if volume.InitMethod == AerospikeVolumeInitMethodDeleteFiles {
					return nil, nil, fmt.Errorf("invalid init method %v for block volume: %v", volume.InitMethod, volume)
				}

				blockStorageDeviceList = append(blockStorageDeviceList, volume.Aerospike.Path)
				// TODO: Add validation for invalid initMethod (e.g. any random value)
			} else if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
				if volume.InitMethod != AerospikeVolumeInitMethodNone && volume.InitMethod != AerospikeVolumeInitMethodDeleteFiles {
					return nil, nil, fmt.Errorf("invalid init method %v for filesystem volume: %v2", volume.InitMethod, volume)
				}

				fileStorageList = append(fileStorageList, volume.Aerospike.Path)
			} else {
				return nil, nil, fmt.Errorf("invalid volumemode %s, valid volumemods %s, %s", volume.Source.PersistentVolume.VolumeMode, v1.PersistentVolumeBlock, v1.PersistentVolumeFilesystem)
			}

		}
	}
	return blockStorageDeviceList, fileStorageList, nil
}

func (v *AerospikeStorageSpec) IsVolumeExistForAerospikePath(path string) bool {
	for _, volume := range v.Volumes {
		if volume.Aerospike != nil && volume.Aerospike.Path == path {
			return true
		}
	}
	return false
}

func validateStorage(storage *AerospikeStorageSpec, podSpec *AerospikePodSpec) error {
	reservedPaths := map[string]int{
		// Reserved mount paths for the operator.
		"/etc/aerospike": 1,
		"/configs":       1,
	}

	storagePaths := map[string]int{}

	sidecarAttachmentPaths := map[string]map[string]int{}
	initContainerAttachmentPaths := map[string]map[string]int{}

	for _, volume := range storage.Volumes {
		if err := validateStorageVolumeSource(volume.Source); err != nil {
			return fmt.Errorf("invalid volumeSource: %v, volume %v", err, volume)
		}

		if volume.Aerospike == nil && len(volume.Sidecars) == 0 && len(volume.InitContainers) == 0 {
			return fmt.Errorf("invalid volume, no container provided to attach the volume. At least one of the following must be provided: volume.Aerospike, volume.Sidecars, volume.InitContainers, volume %v", volume)
		}

		// Validate Aerospike attachment
		if volume.Aerospike != nil {
			if !filepath.IsAbs(volume.Aerospike.Path) {
				return fmt.Errorf("volume path should be absolute: %s", volume.Aerospike.Path)
			}

			if _, ok := reservedPaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf("reserved volume path %s", volume.Aerospike.Path)
			}

			if _, ok := storagePaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf("duplicate volume path %s", volume.Aerospike.Path)
			}
			storagePaths[volume.Aerospike.Path] = 1
		}

		// Validate sidecars attachments
		if err := validateAttachment(volume.Sidecars, getContainerNames(podSpec.Sidecars)); err != nil {
			return fmt.Errorf("sidecar volumeMount validation failed, %v", err)
		}
		// Validate initcontainer attachments
		if err := validateAttachment(volume.InitContainers, getContainerNames(podSpec.InitContainers)); err != nil {
			return fmt.Errorf("additionalInitContainer volumeMount validation failed, %v", err)
		}

		// Validate attachments path
		if err := validateContainerAttachmentPaths(volume.Sidecars, sidecarAttachmentPaths); err != nil {
			return err
		}
		if err := validateContainerAttachmentPaths(volume.InitContainers, initContainerAttachmentPaths); err != nil {
			return err
		}
	}
	return nil
}

func validateContainerAttachmentPaths(volumeAttachments []VolumeAttachment, containerAttachmentPaths map[string]map[string]int) error {
	for _, attachment := range volumeAttachments {
		if _, ok := containerAttachmentPaths[attachment.ContainerName]; !ok {
			containerAttachmentPaths[attachment.ContainerName] = map[string]int{attachment.Path: 1}
			continue
		}

		if _, ok := containerAttachmentPaths[attachment.ContainerName][attachment.Path]; ok {
			return fmt.Errorf("duplicate path %s for volumeAttachments %v in container %s", attachment.Path, volumeAttachments, attachment.ContainerName)
		}
		containerAttachmentPaths[attachment.ContainerName][attachment.Path] = 1
	}
	return nil
}

func validateAttachment(volumeAttachments []VolumeAttachment, containerNames []string) error {
	attachmentConatiners := map[string]int{}

	for _, attachment := range volumeAttachments {
		if !filepath.IsAbs(attachment.Path) {
			return fmt.Errorf("volume path should be absolute: %s", attachment.Path)
		}

		// Check for duplicate path
		if _, ok := attachmentConatiners[attachment.ContainerName]; ok {
			return fmt.Errorf("duplicate container name %s in volumeAttachments %v", attachment.ContainerName, volumeAttachments)
		}
		attachmentConatiners[attachment.ContainerName] = 1

		// Check container name, should be valid
		var found bool
		for _, name := range containerNames {
			if attachment.ContainerName == name {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("volumeMount containerName `%s` not found in container names %v", attachment.ContainerName, containerNames)
		}
	}
	return nil
}

func getContainerNames(containers []v1.Container) []string {
	var containerNames []string
	for _, container := range containers {
		containerNames = append(containerNames, container.Name)
	}
	return containerNames
}

// IsSafeChange indicates if a change to a volume is safe to allow.
func (v *VolumeSpec) IsSafeChange(new VolumeSpec) bool {
	// Allow all type of sources, except pv
	if v.Source.PersistentVolume == nil && new.Source.PersistentVolume == nil {
		return true
	}

	if (v.Source.PersistentVolume != nil && new.Source.PersistentVolume == nil) ||
		(v.Source.PersistentVolume == nil && new.Source.PersistentVolume != nil) {
		return false
	}

	oldpv := v.Source.PersistentVolume
	newpv := new.Source.PersistentVolume
	return reflect.DeepEqual(oldpv, newpv)
}

func validateStorageVolumeSource(source VolumeSource) error {
	var sourceFound bool
	if source.EmptyDir != nil {
		sourceFound = true
	}
	if source.Secret != nil {
		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		} else {
			sourceFound = true
		}
	}
	if source.ConfigMap != nil {
		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		} else {
			sourceFound = true
		}
	}
	if source.PersistentVolume != nil {
		// Validate VolumeMode
		vm := source.PersistentVolume.VolumeMode
		if vm != v1.PersistentVolumeBlock &&
			vm != v1.PersistentVolumeFilesystem {
			return fmt.Errorf("invalid VolumeMode `%s`. Valid VolumeModes: %s, %s", vm, v1.PersistentVolumeBlock, v1.PersistentVolumeFilesystem)
		}

		// Validate accessModes
		for _, am := range source.PersistentVolume.AccessModes {
			if am != v1.ReadOnlyMany &&
				am != v1.ReadWriteMany &&
				am != v1.ReadWriteOnce {
				return fmt.Errorf("invalid AccessMode `%s`. Valid AccessModes: %s, %s, %s", am, v1.ReadOnlyMany, v1.ReadWriteMany, v1.ReadWriteOnce)
			}
		}

		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		} else {
			sourceFound = true
		}
	}
	if !sourceFound {
		return fmt.Errorf("no volume source found")
	}

	return nil
}
