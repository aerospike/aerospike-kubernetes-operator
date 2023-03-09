package pkg

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (initp *InitParams) copyTemplates(source, destination string) error {
	if source == "" {
		return fmt.Errorf("template source volume not specified")
	}

	if destination == "" {
		return fmt.Errorf("template destination volume not specified")
	}

	logrus.Info("Installing aerospike.conf into ", destination)

	filesToCopy := [2]string{"aerospike.template.conf", "peers"}
	for _, file := range filesToCopy {
		path := filepath.Join(source, file)

		cmd := exec.Command("cp", "--dereference", path, destination)
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	return nil
}
