package v1beta1

import (
	"fmt"
	"path/filepath"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ValidateStorageSpecChange indicates if a change to storage spec is safe to apply.
func (s *AerospikeStorageSpec) ValidateStorageSpecChange(new AerospikeStorageSpec) error {
	for _, newVolume := range new.Volumes {
		for _, oldVolume := range s.Volumes {
			if oldVolume.Name == newVolume.Name {
				if !oldVolume.IsSafeChange(newVolume) {
					// Validate same volumes
					return fmt.Errorf(
						"cannot change volumes old: %v new %v", oldVolume,
						newVolume,
					)
				}
				break
			}
		}
	}

	_, _, err := s.validateAddedOrRemovedVolumes(new)
	return err
}

// validateAddedOrRemovedVolumes returns volumes that were added or removed.
// Currently, only configMap volume is allowed to be added or removed dynamically
func (s *AerospikeStorageSpec) validateAddedOrRemovedVolumes(new AerospikeStorageSpec) (
	addedVolumes []VolumeSpec, removedVolumes []VolumeSpec, err error,
) {
	// Validate Persistent volume
	for _, newVolume := range new.Volumes {
		matched := false
		for _, oldVolume := range s.Volumes {
			if oldVolume.Name == newVolume.Name {
				matched = true
				break
			}
		}

		if !matched {
			if newVolume.Source.PersistentVolume != nil {
				return []VolumeSpec{}, []VolumeSpec{}, fmt.Errorf(
					"cannot add persistent volume: %v", newVolume,
				)
			}
			addedVolumes = append(addedVolumes, newVolume)
		}
	}

	for _, oldVolume := range s.Volumes {
		matched := false
		for _, newVolume := range new.Volumes {
			if oldVolume.Name == newVolume.Name {
				matched = true
			}
		}

		if !matched {
			if oldVolume.Source.PersistentVolume != nil {
				return []VolumeSpec{}, []VolumeSpec{}, fmt.Errorf(
					"cannot remove persistent volume: %v", oldVolume,
				)
			}
			removedVolumes = append(removedVolumes, oldVolume)
		}
	}

	return addedVolumes, removedVolumes, nil
}

// SetDefaults sets default values for storage spec fields.
func (s *AerospikeStorageSpec) SetDefaults() {
	defaultFilesystemInitMethod := AerospikeVolumeInitMethodNone
	defaultBlockInitMethod := AerospikeVolumeInitMethodNone
	// Set storage level defaults.
	s.FileSystemVolumePolicy.SetDefaults(
		&AerospikePersistentVolumePolicySpec{
			InitMethod: defaultFilesystemInitMethod, CascadeDelete: false,
		},
	)
	s.BlockVolumePolicy.SetDefaults(
		&AerospikePersistentVolumePolicySpec{
			InitMethod: defaultBlockInitMethod, CascadeDelete: false,
		},
	)

	for i := range s.Volumes {
		if s.Volumes[i].Source.PersistentVolume == nil {
			// All other sources are considered to be mounted, for now.
			s.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&s.FileSystemVolumePolicy)
		} else if s.Volumes[i].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
			s.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&s.BlockVolumePolicy)
		} else if s.Volumes[i].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
			s.Volumes[i].AerospikePersistentVolumePolicySpec.SetDefaults(&s.FileSystemVolumePolicy)
		}
	}
}

// GetConfigMaps returns the config map volumes from the storage spec.
func (s *AerospikeStorageSpec) GetConfigMaps() (configMaps []VolumeSpec) {
	for _, volume := range s.Volumes {
		if volume.Source.ConfigMap != nil {
			configMaps = append(configMaps, volume)
		}
	}
	return configMaps
}

// GetPVs returns the PV volumes from the storage spec.
func (s *AerospikeStorageSpec) GetPVs() (PVs []VolumeSpec) {
	for _, volume := range s.Volumes {
		if volume.Source.PersistentVolume != nil {
			PVs = append(PVs, volume)
		}
	}
	return PVs
}

// GetNonPVs returns the non PV volumes from the storage spec.
func (s *AerospikeStorageSpec) GetNonPVs() (nonPVs []VolumeSpec) {
	for _, volume := range s.Volumes {
		if volume.Source.PersistentVolume == nil {
			nonPVs = append(nonPVs, volume)
		}
	}
	return nonPVs
}

// GetAerospikeStorageList gives blockStorageDeviceList and fileStorageList
func (s *AerospikeStorageSpec) GetAerospikeStorageList() (
	blockStorageDeviceList []string, fileStorageList []string, err error,
) {

	for _, volume := range s.Volumes {

		if volume.Aerospike != nil {
			// TODO: Do we need to check for other type of sources
			if volume.Source.PersistentVolume == nil {
				continue
			}
			if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
				blockStorageDeviceList = append(
					blockStorageDeviceList, volume.Aerospike.Path,
				)
			} else if volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
				fileStorageList = append(fileStorageList, volume.Aerospike.Path)
			} else {
				return nil, nil, fmt.Errorf(
					"invalid volumemode %s, valid volumemods %s, %s",
					volume.Source.PersistentVolume.VolumeMode,
					v1.PersistentVolumeBlock, v1.PersistentVolumeFilesystem,
				)
			}
		}
	}
	return blockStorageDeviceList, fileStorageList, nil
}

// IsVolumePresentForAerospikePath checks if configuration has a volume defined for given path for Aerospike server container.
func (s *AerospikeStorageSpec) IsVolumePresentForAerospikePath(path string) bool {
	return s.GetVolumeForAerospikePath(path) != nil
}

// GetVolumeForAerospikePath returns volume defined for given path for Aerospike server container.
func (s *AerospikeStorageSpec) GetVolumeForAerospikePath(path string) *VolumeSpec {
	var matchedVolume *VolumeSpec
	for i := range s.Volumes {
		volume := &s.Volumes[i]
		if volume.Aerospike != nil && isPathParentOrSame(
			volume.Aerospike.Path, path,
		) {
			if matchedVolume == nil || matchedVolume.Aerospike.Path < volume.Aerospike.Path {
				matchedVolume = volume
			}
		}
	}
	return matchedVolume
}

func validateStorage(
	storage *AerospikeStorageSpec, podSpec *AerospikePodSpec,
) error {
	reservedPaths := map[string]int{
		// Reserved mount paths for the operator.
		"/etc/aerospike": 1,
		"/configs":       1,
	}

	storagePaths := map[string]int{}

	sidecarAttachmentPaths := map[string]map[string]int{}
	initContainerAttachmentPaths := map[string]map[string]int{}

	for _, volume := range storage.Volumes {
		errors := validation.IsDNS1123Label(volume.Name)
		if len(errors) > 0 {
			return fmt.Errorf(
				"invalid volume name '%s' - %v", volume.Name,
				errors,
			)
		}

		if err := validateStorageVolumeSource(volume); err != nil {
			return fmt.Errorf(
				"invalid volumeSource: %v, volume %v", err, volume,
			)
		}

		if volume.Aerospike == nil && len(volume.Sidecars) == 0 && len(volume.InitContainers) == 0 {
			return fmt.Errorf(
				"invalid volume, no container provided to attach the volume. At least one of the following must be provided: volume.Aerospike, volume.Sidecars, volume.InitContainers, volume %v",
				volume,
			)
		}

		// Validate Aerospike attachment
		if volume.Aerospike != nil {
			if !filepath.IsAbs(volume.Aerospike.Path) {
				return fmt.Errorf(
					"volume path should be absolute: %s", volume.Aerospike.Path,
				)
			}

			if _, ok := reservedPaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf(
					"reserved volume path %s", volume.Aerospike.Path,
				)
			}

			if _, ok := storagePaths[volume.Aerospike.Path]; ok {
				return fmt.Errorf(
					"duplicate volume path %s", volume.Aerospike.Path,
				)
			}
			storagePaths[volume.Aerospike.Path] = 1
		}

		// Validate sidecars attachments
		if err := validateAttachment(
			volume.Sidecars, getContainerNames(podSpec.Sidecars),
		); err != nil {
			return fmt.Errorf("sidecar volumeMount validation failed, %v", err)
		}
		// Validate initContainer attachments
		if err := validateAttachment(
			volume.InitContainers, getContainerNames(podSpec.InitContainers),
		); err != nil {
			return fmt.Errorf(
				"initContainer volumeMount validation failed, %v", err,
			)
		}

		// Validate attachments path
		if err := validateContainerAttachmentPaths(
			volume.Sidecars, sidecarAttachmentPaths,
		); err != nil {
			return err
		}
		if err := validateContainerAttachmentPaths(
			volume.InitContainers, initContainerAttachmentPaths,
		); err != nil {
			return err
		}
	}
	return nil
}

func validateContainerAttachmentPaths(
	volumeAttachments []VolumeAttachment,
	containerAttachmentPaths map[string]map[string]int,
) error {
	for _, attachment := range volumeAttachments {
		if _, ok := containerAttachmentPaths[attachment.ContainerName]; !ok {
			containerAttachmentPaths[attachment.ContainerName] = map[string]int{attachment.Path: 1}
			continue
		}

		if _, ok := containerAttachmentPaths[attachment.ContainerName][attachment.Path]; ok {
			return fmt.Errorf(
				"duplicate path %s for volumeAttachments %v in container %s",
				attachment.Path, volumeAttachments, attachment.ContainerName,
			)
		}
		containerAttachmentPaths[attachment.ContainerName][attachment.Path] = 1
	}
	return nil
}

func validateAttachment(
	volumeAttachments []VolumeAttachment, containerNames []string,
) error {
	attachmentContainers := map[string]int{}

	for _, attachment := range volumeAttachments {
		if attachment.ContainerName == AerospikeServerInitContainerName {
			return fmt.Errorf(
				"cannot attach volumes to: %s",
				AerospikeServerInitContainerName,
			)
		}

		if attachment.ContainerName == AerospikeServerContainerName {
			return fmt.Errorf(
				"use storage.aerospike for attaching volumes to Aerospike server container %s for path %s",
				AerospikeServerContainerName, attachment.Path,
			)
		}

		if !filepath.IsAbs(attachment.Path) {
			return fmt.Errorf(
				"volume path should be absolute: %s", attachment.Path,
			)
		}

		// Check for duplicate container entries
		if _, ok := attachmentContainers[attachment.ContainerName]; ok {
			return fmt.Errorf(
				"duplicate container name %s in volumeAttachments %v",
				attachment.ContainerName, volumeAttachments,
			)
		}
		attachmentContainers[attachment.ContainerName] = 1

		// Check container name, should be valid.
		var found bool
		for _, name := range containerNames {
			if attachment.ContainerName == name {
				found = true
				break
			}
		}

		if !found {
			return fmt.Errorf(
				"volumeMount containerName `%s` not found in container names %v",
				attachment.ContainerName, containerNames,
			)
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

	oldPV := v.Source.PersistentVolume
	newPV := new.Source.PersistentVolume
	return reflect.DeepEqual(oldPV, newPV)
}

func validateStorageVolumeSource(volume VolumeSpec) error {
	source := volume.Source

	var sourceFound bool
	if source.EmptyDir != nil {
		sourceFound = true
	}
	if source.Secret != nil {
		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		}
		sourceFound = true
	}
	if source.ConfigMap != nil {
		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		}
		sourceFound = true
	}
	if source.PersistentVolume != nil {
		// Validate InitMethod
		if source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock {
			if volume.InitMethod == AerospikeVolumeInitMethodDeleteFiles {
				return fmt.Errorf(
					"invalid init method %v for block volume: %v",
					volume.InitMethod, volume,
				)
			}
			// Note: Add validation for invalid initMethod if new get added.
		} else if source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
			if volume.InitMethod != AerospikeVolumeInitMethodNone && volume.InitMethod != AerospikeVolumeInitMethodDeleteFiles {
				return fmt.Errorf(
					"invalid init method %v for filesystem volume: %v2",
					volume.InitMethod, volume,
				)
			}
		}

		// Validate VolumeMode
		vm := source.PersistentVolume.VolumeMode

		if vm != v1.PersistentVolumeBlock &&
			vm != v1.PersistentVolumeFilesystem {
			return fmt.Errorf(
				"invalid VolumeMode `%s`. Valid VolumeModes: %s, %s", vm,
				v1.PersistentVolumeBlock, v1.PersistentVolumeFilesystem,
			)
		}

		// Validate accessModes
		for _, am := range source.PersistentVolume.AccessModes {
			if am != v1.ReadOnlyMany &&
				am != v1.ReadWriteMany &&
				am != v1.ReadWriteOnce {
				return fmt.Errorf(
					"invalid AccessMode `%s`. Valid AccessModes: %s, %s, %s",
					am, v1.ReadOnlyMany, v1.ReadWriteMany, v1.ReadWriteOnce,
				)
			}
		}

		if sourceFound {
			return fmt.Errorf("can not specify more than 1 source")
		}
		sourceFound = true
	}

	if !sourceFound {
		return fmt.Errorf("no volume source found")
	}

	return nil
}
