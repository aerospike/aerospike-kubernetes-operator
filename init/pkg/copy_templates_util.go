package pkg

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"os/exec"
	"path/filepath"
)

func CopyTemplates(source, destination string) error {
	if source == "" {
		return fmt.Errorf("template source volume not specified")
	}

	if destination == "" {
		return fmt.Errorf("template destination volume not specified")
	}

	logrus.Info("installing aerospike.conf into ", destination)

	cmd := exec.Command("cp", "--dereference", filepath.Join(source, "aerospike.template.conf"), destination)
	if err := cmd.Run(); err != nil {
		return err
	}

	cmd = exec.Command("cp", "--dereference", filepath.Join(source, "peers"), destination)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}
