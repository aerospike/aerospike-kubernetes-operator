package configschema

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	lib "github.com/aerospike/aerospike-management-lib"
)

//go:embed schemas/json/aerospike
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
				schema[key] = string(content)
			}

			return nil
		},
	); err != nil {
		return nil, err
	}

	// 8.2.1.json is not in the build. An 8.2.1 server tag looks up that key, so
	// validate it with the 8.2.0 schema until the real file is added.
	aliasSchema(schema, "8.2.1", "8.2.0")

	return schema, nil
}

// aliasSchema copies source onto missing when missing is not already loaded.
func aliasSchema(schema SchemaMap, missing, source string) {
	if _, ok := schema[missing]; ok {
		return
	}

	if src, ok := schema[source]; ok {
		schema[missing] = src
	}
}
