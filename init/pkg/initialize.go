package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
)

const configVolume = "/etc/aerospike"

func (initp *InitParams) ColdRestart() error {
	if err := initp.makeWorkDir(); err != nil {
		return err
	}

	// Copy required files to config volume for initialization.
	if err := initp.copyTemplates("/configs", configVolume); err != nil {
		return err
	}

	filesToCopy := [2]string{"/workdir/bin/akoinit", "/configs/features.conf"}
	for _, file := range filesToCopy {
		if _, err := os.Stat(file); err == nil {
			cmd := exec.Command("cp", "--dereference", file, configVolume)
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}

	configMapDir := filepath.Join(configVolume, "configmap")
	if err := os.MkdirAll(configMapDir, 0745); err != nil { //nolint:gocritic // file permission
		return err
	}

	matches, err := filepath.Glob("/configs/*")
	if err != nil {
		return err
	}

	for _, path := range matches {
		cmd := exec.Command("cp", "--dereference", "--recursive", path, configMapDir)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	if err := initp.createAerospikeConf(); err != nil {
		return err
	}

	if err := initp.manageVolumesAndUpdateStatus("podRestart"); err != nil {
		return err
	}

	return nil
}
