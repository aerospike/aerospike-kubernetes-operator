module github.com/aerospike/aerospike-kubernetes-operator

go 1.16

require (
	cloud.google.com/go v0.76.0 // indirect
	github.com/Azure/go-autorest/autorest v0.11.17 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.11 // indirect
	github.com/aerospike/aerospike-management-lib v0.0.0-20210711071602-62b56ba49fbd
	github.com/ashishshinde/aerospike-client-go/v5 v5.0.0-20210711030305-604f00410716
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/imdario/mergo v0.3.11 // indirect
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/kr/pretty v0.2.1 // indirect
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/prometheus/client_golang v1.9.0 // indirect
	github.com/prometheus/procfs v0.3.0 // indirect
	github.com/qdm12/reprint v0.0.0-20200326205758-722754a53494 // indirect
	github.com/stretchr/testify v1.6.1
	github.com/tmc/scp v0.0.0-20170824174625-f7b48647feef // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/xeipuuv/gojsonschema v1.2.0 // indirect
	github.com/yuin/gopher-lua v0.0.0-20200816102855-ee81675732da // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/crypto v0.0.0-20210220033148-5ea612d1eb83
	golang.org/x/oauth2 v0.0.0-20210201163806-010130855d6c // indirect
	golang.org/x/tools v0.1.4 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.19.2
	k8s.io/kubectl v0.19.2
	sigs.k8s.io/controller-runtime v0.7.2

)

replace github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309 // Required by Helm

replace github.com/openshift/api => github.com/openshift/api v0.0.0-20190924102528-32369d4db2ad // Required until https://github.com/operator-framework/operator-lifecycle-manager/pull/1241 is resolved

//replace github.com/ashishshinde/aerospike-client-go => /home/ashish/go/src/github.com/ashishshinde/aerospike-client-go
