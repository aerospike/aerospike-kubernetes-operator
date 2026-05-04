package configschema

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	lib "github.com/aerospike/aerospike-management-lib"
)

// schemas/json/aerospike-server contains the YAML map-format schemas used for
// validating aerospikeConfig against the new YAML format (server >= 8.1.1).
//
//go:embed schemas/json/aerospike-server
var schemas embed.FS

const minSupportedVersion = "6.0.0"

type SchemaMap map[string]string

func NewSchemaMap() (SchemaMap, error) {
	schema := make(SchemaMap)

	if err := fs.WalkDir(
		schemas, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() {
				baseName := filepath.Base(path)
				if baseName == "README.md" {
					return nil
				}

				// Skip schemas for versions less than minSupportedVersion
				val, err := lib.CompareVersions(baseName, minSupportedVersion)
				if err != nil {
					return err
				}

				if val < 0 {
					return nil
				}

				content, err := fs.ReadFile(schemas, path)
				if err != nil {
					return err
				}

				key := strings.TrimSuffix(baseName, filepath.Ext(baseName))
				if _, exists := schema[key]; !exists {
					schema[key] = string(content)
				}
			}

			return nil
		},
	); err != nil {
		return nil, err
	}

	return schema, nil
}
