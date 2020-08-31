package admission

import (
	"fmt"
	"reflect"
)

func merge(base, patch map[string]interface{}) (map[string]interface{}, error) {
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
		if key == "storage-engine" && isStorageEngineTypeChanged(baseValue, patchValue) {
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
	// storage-engine = "memory"
	_, ok1 := base.(string)
	_, ok2 := patch.(string)

	if ok1 && ok2 {
		return false
	}

	baseMap, ok1 := base.(map[string]interface{})
	patchMap, ok2 := patch.(map[string]interface{})

	if ok1 && ok2 {
		_, ok1f := baseMap["file"]
		_, ok1d := baseMap["device"]
		_, ok2f := patchMap["file"]
		_, ok2d := patchMap["device"]

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
		return merge(bvt, pvt)

	case string, float64, bool, int, int64, int32:
		return patchValue, nil

	case []interface{}:
		if isPrimList(baseValue.([]interface{})) && isPrimList(patchValue.([]interface{})) {
			return patchValue, nil
		}

		var patchedList []interface{}

		// merge and append ele from base
		for _, bEleInt := range baseValue.([]interface{}) {
			bEle, ok := bEleInt.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("object %v should be map", bEleInt)
			}
			// get namespace name in base
			bName, ok := bEle["name"]
			if !ok {
				return "", fmt.Errorf("object %v should have `name` key", bEle)
			}

			var found bool
			for _, pEleInt := range patchValue.([]interface{}) {
				pEle, ok := pEleInt.(map[string]interface{})
				if !ok {
					return "", fmt.Errorf("object %v should be map", pEleInt)
				}
				// get namespace name in patch
				pName, ok := pEle["name"]
				if !ok {
					return "", fmt.Errorf("object %v should have `name` key", pEle)
				}

				if pName == bName {
					mMap, err := merge(bEle, pEle)
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
			pName := pEleInt.(map[string]interface{})["name"]

			var found bool
			for _, bEleInt := range baseValue.([]interface{}) {
				bName := bEleInt.(map[string]interface{})["name"]

				if pName == bName {
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

func isMapList(list []interface{}) bool {
	for _, e := range list {
		me, ok := e.(map[string]interface{})
		if !ok {
			return false
		}
		// Check name key
		if _, ok := me["name"]; !ok {
			return false
		}
	}
	return true
}
