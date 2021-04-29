package utils

// import (
// 	"fmt"
// 	"strings"

// 	asdbv1alpha1 "github.com/aerospike/aerospike-kubernetes-operator/api/v1alpha1"
// )

// const (
// 	// Namespace keys.
// 	confKeyNamespace     = "namespaces"
// 	confKeyMemorySize    = "memory-size"
// 	confKeyStorageEngine = "storage-engine"
// 	confKeyFilesize      = "filesize"
// 	confKeyDevice        = "devices"
// 	confKeyFile          = "files"
// 	confKeyNetwork       = "network"
// 	confKeyTLS           = "tls"
// 	confKeySecurity      = "security"

// 	// XDR keys.
// 	confKeyXdr         = "xdr"
// 	confKeyXdrDlogPath = "xdr-digestlog-path"

// 	// Service section keys.
// 	confKeyService       = "service"
// 	confKeyWorkDirectory = "work-directory"

// 	// Defaults.
// 	defaultWorkDirectory = "/opt/aerospike"
// )

// // GetWorkDirectory returns the Aerospike work directory to use for aerospikeConfig.
// func GetWorkDirectory(aerospikeConfig asdbv1alpha1.Values) string {
// 	// Get namespace config.
// 	serviceTmp := aerospikeConfig[confKeyService]

// 	if serviceTmp != nil {
// 		serviceConf := serviceTmp.(map[string]interface{})
// 		workDir, ok := serviceConf[confKeyWorkDirectory]
// 		if ok {
// 			return workDir.(string)
// 		}
// 	}

// 	return defaultWorkDirectory
// }
