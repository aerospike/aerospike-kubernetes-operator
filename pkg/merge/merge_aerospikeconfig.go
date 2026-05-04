package merge

import (
	"fmt"
	"reflect"

	asdbv1 "github.com/aerospike/aerospike-kubernetes-operator/v4/api/v1"
)

// merge (base, patch)
// merge will create a map by merging the patch map in base map recursively
// - If a new key/value is added in patch map then it will be added in result map
// - If an old key/value in base map is not updated in patch map then it will be added in result map
// - If an old key/value in base map is updated in patch map then
//    - if value type is changed then key/value from the patch map will be added in result map
// 	  - if key is `storage-engine` then storage-engine can be of 3 type device, file and memory.
// 		if its type has been changed then key/value from patch map will be added in result map

// 	  - if value type is same then
//    	- if values are of primitive type then key/value from the patch map will be added in result map
// 		- if values are of map type then they will be recursively merged
// 		- if values are list of primitive type then key/value from the patch map will be added in result map
// 		- if values are list of map then a new list will be created
// 			where New entries in patch will be appended to base list.
// 			corresponding entries will be merged using the same merge algorithm
//      	here order of elements in base will be maintained. This list will be added in result map
// 			(corresponding maps are found by matching special `name` key in maps.
// 			Here this list of map is actually a map of map and main map keys
//			are added in sub-map
// 			with key as `name` to convert map of map to list of map).

func Merge(base, patch map[string]interface{}) (map[string]interface{}, error) {
	if len(patch) == 0 {
		return base, nil
	}

	res := map[string]interface{}{}

	for key, patchValue := range patch {
		baseValue, ok := base[key]
		// value was added
		if !ok {
			res[key] = patchValue
			continue
		}

		// If types have changed, replace completely
		if reflect.TypeOf(baseValue) != reflect.TypeOf(patchValue) {
			res[key] = patchValue
			continue
		}

		// Special check for key "storage-engine"
		// Check value type and replace if it's type has changed
		if key == asdbv1.ConfKeyStorageEngine && isStorageEngineTypeChanged(
			baseValue, patchValue,
		) {
			res[key] = patchValue
			continue
		}

		// Types are the same, compare values
		val, err := handleValues(baseValue, patchValue)
		if err != nil {
			return nil, err
		}

		res[key] = val
	}

	// Now add other values
	for k, v := range base {
		if _, found := patch[k]; !found {
			res[k] = v
		}
	}

	return res, nil
}

func isStorageEngineTypeChanged(base, patch interface{}) bool {
	_, ok1 := base.(string)
	_, ok2 := patch.(string)

	if ok1 && ok2 {
		return false
	}

	baseMap, ok1 := base.(map[string]interface{})
	patchMap, ok2 := patch.(map[string]interface{})

	if ok1 && ok2 {
		_, ok1f := baseMap["files"]
		_, ok1d := baseMap["devices"]
		_, ok2f := patchMap["files"]
		_, ok2d := patchMap["devices"]

		// file replaced with device or device replace with file
		if (ok1f && ok2d) || (ok1d && ok2f) {
			return true
		}
	}

	return false
}

func handleValues(baseValue, patchValue interface{}) (interface{}, error) {
	switch bvt := baseValue.(type) {
	case map[string]interface{}:
		pvt := patchValue.(map[string]interface{})
		return Merge(bvt, pvt)

	case string, float64, bool, int, int64, int32:
		return patchValue, nil

	case []interface{}:
		if isPrimList(baseValue.([]interface{})) && isPrimList(patchValue.([]interface{})) {
			return patchValue, nil
		}

		// Determine the identity key for this list of maps.
		// Legacy config uses "name" (namespaces, tls, xdr.dcs …).
		// New YAML format logging uses "type" instead.
		// We probe the first element of the base list to decide which key to use.
		idKey := listIDKey(baseValue.([]interface{}))
		if idKey == "" {
			// Unknown shape — fall back to full replacement (safe default).
			return patchValue, nil
		}

		var patchedList []interface{}

		// merge and append ele from base
		for _, bEleInt := range baseValue.([]interface{}) {
			bEle, ok := bEleInt.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("object %v should be map", bEleInt)
			}

			bName, ok := bEle[idKey]
			if !ok {
				return "", fmt.Errorf("object %v should have `%s` key", bEle, idKey)
			}

			var found bool

			for _, pEleInt := range patchValue.([]interface{}) {
				pEle, ok := pEleInt.(map[string]interface{})
				if !ok {
					return "", fmt.Errorf("object %v should be map", pEleInt)
				}

				pName, ok := pEle[idKey]
				if !ok {
					return "", fmt.Errorf("object %v should have `%s` key", pEle, idKey)
				}

				if pName == bName {
					mMap, err := Merge(bEle, pEle)
					if err != nil {
						return nil, err
					}

					found = true

					patchedList = append(patchedList, mMap)

					break
				}
			}

			if !found {
				patchedList = append(patchedList, bEle)
			}
		}

		for _, pEleInt := range patchValue.([]interface{}) {
			pEle, ok := pEleInt.(map[string]interface{})
			if !ok {
				continue
			}

			pName := pEle[idKey]

			var found bool

			for _, bEleInt := range baseValue.([]interface{}) {
				bEle, ok := bEleInt.(map[string]interface{})
				if !ok {
					continue
				}

				if bEle[idKey] == pName {
					found = true
					break
				}
			}

			if !found {
				patchedList = append(patchedList, pEleInt)
			}
		}

		return patchedList, nil
	default:
		panic(fmt.Sprintf("Unknown type:%T, value:%v ", baseValue, baseValue))
	}
}
func isPrimList(list []interface{}) bool {
	for _, e := range list {
		switch e.(type) {
		case string, float64, bool, int, int64, int32:
			continue
		default:
			return false
		}
	}

	return true
}

// listIDKey returns the key used to identify items in a list-of-maps.
// Legacy config uses "name"; new YAML format logging uses "type".
// Returns "" when the identity key cannot be determined.
func listIDKey(list []interface{}) string {
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		if _, ok := m["name"]; ok {
			return "name"
		}

		if _, ok := m["type"]; ok {
			return "type"
		}
	}

	return ""
}
