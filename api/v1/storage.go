package v1

import (
	"fmt"
	"path/filepath"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// validateStorageSpecChange indicates if a change to storage spec is safe to apply.
func (s *AerospikeStorageSpec) validateStorageSpecChange(newStorage *AerospikeStorageSpec) error {
	for newVolIdx := range newStorage.Volumes {
		newVolume := &newStorage.Volumes[newVolIdx]

		for oldVolIdx := range s.Volumes {
			oldVolume := &s.Volumes[oldVolIdx]
			if oldVolume.Name == newVolume.Name {
				if !oldVolume.isSafeChange(newVolume) {
					// Validate same volumes
					return fmt.Errorf(
						"cannot change volumes old: %v newStorage %v", oldVolume,
						newVolume,
					)
				}

				break
			}
		}
	}

	_, _, err := s.validateAddedOrRemovedVolumes(newStorage)

	return err
}

// validateAddedOrRemovedVolumes returns volumes that were added or removed.
// Currently, only configMap volume is allowed to be added or removed dynamically
func (s *AerospikeStorageSpec) validateAddedOrRemovedVolumes(newStorage *AerospikeStorageSpec) (
	addedVolumes []VolumeSpec, removedVolumes []VolumeSpec, err error,
) {
	// Validate Persistent volume
	for newVolIdx := range newStorage.Volumes {
		newVolume := &newStorage.Volumes[newVolIdx]
		matched := false

		for oldVolIdx := range s.Volumes {
			oldVolume := &s.Volumes[oldVolIdx]
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

			addedVolumes = append(addedVolumes, *newVolume)
		}
	}

	for oldVolIdx := range s.Volumes {
		oldVolume := &s.Volumes[oldVolIdx]
		matched := false

		for newVolIdx := range newStorage.Volumes {
			newVolume := &newStorage.Volumes[newVolIdx]
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

			removedVolumes = append(removedVolumes, *oldVolume)
		}
	}

	return addedVolumes, removedVolumes, nil
}

// SetDefaults sets default values for storage spec fields.
func (s *AerospikeStorageSpec) SetDefaults() {
	defaultFilesystemInitMethod := AerospikeVolumeMethodNone
	defaultFilesystemWipeMethod := AerospikeVolumeMethodDeleteFiles
	defaultBlockInitMethod := AerospikeVolumeMethodNone
	defaultBlockWipeMethod := AerospikeVolumeMethodDD
	defaultCleanupThreads := AerospikeVolumeSingleCleanupThread
	// Set storage level defaults.
	s.FileSystemVolumePolicy.SetDefaults(
		&AerospikePersistentVolumePolicySpec{
			InitMethod: defaultFilesystemInitMethod, WipeMethod: defaultFilesystemWipeMethod, CascadeDelete: false,
		},
	)
	s.BlockVolumePolicy.SetDefaults(
		&AerospikePersistentVolumePolicySpec{
			InitMethod: defaultBlockInitMethod, WipeMethod: defaultBlockWipeMethod, CascadeDelete: false,
		},
	)

	if s.CleanupThreads == 0 {
		s.CleanupThreads = defaultCleanupThreads
	}

	for idx := range s.Volumes {
		switch {
		case s.Volumes[idx].Source.PersistentVolume == nil:
			// All other sources are considered to be mounted, for now.
			s.Volumes[idx].AerospikePersistentVolumePolicySpec.SetDefaults(&s.FileSystemVolumePolicy)
		case s.Volumes[idx].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock:
			s.Volumes[idx].AerospikePersistentVolumePolicySpec.SetDefaults(&s.BlockVolumePolicy)
		case s.Volumes[idx].Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem:
			s.Volumes[idx].AerospikePersistentVolumePolicySpec.SetDefaults(&s.FileSystemVolumePolicy)
		}
	}
}

// SetDefaults applies default values to unset fields of the policy using corresponding fields from defaultPolicy
func (p *AerospikePersistentVolumePolicySpec) SetDefaults(defaultPolicy *AerospikePersistentVolumePolicySpec) {
	if p.InputInitMethod == nil {
		p.InitMethod = defaultPolicy.InitMethod
	} else {
		p.InitMethod = *p.InputInitMethod
	}

	if p.InputWipeMethod == nil {
		p.WipeMethod = defaultPolicy.WipeMethod
	} else {
		p.WipeMethod = *p.InputWipeMethod
	}

	if p.InputCascadeDelete == nil {
		p.CascadeDelete = defaultPolicy.CascadeDelete
	} else {
		p.CascadeDelete = *p.InputCascadeDelete
	}
}

// GetPVs returns the PV volumes from the storage spec.
func (s *AerospikeStorageSpec) GetPVs() (pVs []VolumeSpec) {
	for idx := range s.Volumes {
		volume := s.Volumes[idx]
		if volume.Source.PersistentVolume != nil {
			pVs = append(pVs, volume)
		}
	}

	return pVs
}

// GetNonPVs returns the non PV volumes from the storage spec.
func (s *AerospikeStorageSpec) GetNonPVs() (nonPVs []VolumeSpec) {
	for idx := range s.Volumes {
		volume := s.Volumes[idx]
		if volume.Source.PersistentVolume == nil {
			nonPVs = append(nonPVs, volume)
		}
	}

	return nonPVs
}

// getAerospikeStorageList gives blockStorageDeviceList and fileStorageList
func (s *AerospikeStorageSpec) getAerospikeStorageList() (
	blockStorageDeviceList []string, fileStorageList []string, err error,
) {
	for idx := range s.Volumes {
		volume := &s.Volumes[idx]
		if volume.Aerospike != nil {
			// TODO: Do we need to check for other type of sources
			if volume.Source.PersistentVolume == nil {
				continue
			}

			switch {
			case volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeBlock:
				blockStorageDeviceList = append(blockStorageDeviceList, volume.Aerospike.Path)
			case volume.Source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem:
				fileStorageList = append(fileStorageList, volume.Aerospike.Path)
			default:
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

// isVolumePresentForAerospikePath checks if configuration has a volume defined for given path for Aerospike server
// container.
func (s *AerospikeStorageSpec) isVolumePresentForAerospikePath(path string) bool {
	return s.GetVolumeForAerospikePath(path) != nil
}

// GetVolumeForAerospikePath returns volume defined for given path for Aerospike server container.
func (s *AerospikeStorageSpec) GetVolumeForAerospikePath(path string) *VolumeSpec {
	var matchedVolume *VolumeSpec

	for idx := range s.Volumes {
		volume := &s.Volumes[idx]
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

	for idx := range storage.Volumes {
		volume := &storage.Volumes[idx]

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
				"invalid volume, no container provided to attach the volume. "+
					"At least one of the following must be provided: volume.Aerospike, volume.Sidecars, "+
					"volume.InitContainers, volume %v",
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
		if attachment.ContainerName == AerospikeInitContainerName {
			return fmt.Errorf(
				"cannot attach volumes to: %s",
				AerospikeInitContainerName,
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

// isSafeChange indicates if a change to a volume is safe to allow.
func (v *VolumeSpec) isSafeChange(newVolume *VolumeSpec) bool {
	// Allow all type of sources, except pv
	if v.Source.PersistentVolume == nil && newVolume.Source.PersistentVolume == nil {
		return true
	}

	if (v.Source.PersistentVolume != nil && newVolume.Source.PersistentVolume == nil) ||
		(v.Source.PersistentVolume == nil && newVolume.Source.PersistentVolume != nil) {
		return false
	}

	oldPV := v.Source.PersistentVolume
	newPV := newVolume.Source.PersistentVolume

	return reflect.DeepEqual(oldPV, newPV)
}

func validateStorageVolumeSource(volume *VolumeSpec) error {
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
			if volume.InitMethod == AerospikeVolumeMethodDeleteFiles {
				return fmt.Errorf(
					"invalid init method %v for block volume: %v",
					volume.InitMethod, volume,
				)
			}

			if volume.WipeMethod != AerospikeVolumeMethodBlkdiscard && volume.WipeMethod != AerospikeVolumeMethodDD {
				return fmt.Errorf("invalid wipe method: %s for block volume: %s", volume.WipeMethod, volume.Name)
			}
		} else if source.PersistentVolume.VolumeMode == v1.PersistentVolumeFilesystem {
			if volume.InitMethod != AerospikeVolumeMethodNone && volume.InitMethod != AerospikeVolumeMethodDeleteFiles {
				return fmt.Errorf(
					"invalid init method %v for filesystem volume: %v",
					volume.InitMethod, volume,
				)
			}

			if volume.WipeMethod != AerospikeVolumeMethodDeleteFiles {
				return fmt.Errorf(
					"invalid wipe method %s for filesystem volume: %s",
					volume.WipeMethod, volume.Name,
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
