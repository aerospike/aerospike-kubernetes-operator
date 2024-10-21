package common

import "fmt"

// GetConfigSection returns the section of the config with the given name.
func GetConfigSection(config map[string]interface{}, section string) (map[string]interface{}, error) {
	sectionIface, ok := config[section]
	if !ok {
		return map[string]interface{}{}, nil
	}

	sectionMap, ok := sectionIface.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not a map", section)
	}

	return sectionMap, nil
}
