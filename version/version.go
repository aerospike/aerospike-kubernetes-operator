package version

import (
	"os"
)

var (
	// Version is the current operator version.
	Version string
)

func init() {
	Version = os.Getenv("OPERATOR_VERSION")
	if Version == "" {
		Version = "N/A"
	}
}
