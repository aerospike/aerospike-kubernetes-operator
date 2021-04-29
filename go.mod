module github.com/aerospike/aerospike-kubernetes-operator

go 1.13

require (
	bou.ke/monkey v1.0.1 // indirect
	cloud.google.com/go v0.76.0 // indirect
	github.com/Azure/go-autorest/autorest v0.11.17 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.11 // indirect
	github.com/Masterminds/semver v1.5.0 // indirect
	github.com/aerospike/aerospike-management-lib v0.0.0-20210414182131-82c9c425ad34
	github.com/ashishshinde/aerospike-client-go v3.0.4-0.20200924015406-d85b25081637+incompatible
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/coreos/prometheus-operator v0.38.1-0.20200424145508-7e176fda06cc // indirect
	github.com/docker/docker v1.4.2-0.20200203170920-46ec8731fbce
	github.com/docker/spdystream v0.0.0-20181023171402-6480d4af844c // indirect
	github.com/emicklei/go-restful v2.11.1+incompatible // indirect
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/fvbommel/sortorder v1.0.1 // indirect
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0 // indirect
	github.com/go-openapi/spec v0.19.4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/gophercloud/gophercloud v0.15.0 // indirect
	github.com/imdario/mergo v0.3.11 // indirect
	github.com/inconshreveable/log15 v0.0.0-20201112154412-8562bdadbbac
	github.com/jinzhu/copier v0.2.3 // indirect
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51 // indirect
	github.com/mattn/go-colorable v0.1.8 // indirect
	github.com/mikefarah/yq/v3 v3.0.0-20201202084205-8846255d1c37 // indirect
	github.com/mrunalp/fileutils v0.5.0 // indirect
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/operator-framework/operator-lib v0.3.0
	github.com/operator-framework/operator-sdk v1.3.2 // indirect
	github.com/prometheus/client_golang v1.9.0 // indirect
	github.com/prometheus/procfs v0.3.0 // indirect
	github.com/qdm12/reprint v0.0.0-20200326205758-722754a53494 // indirect
	github.com/rogpeppe/go-internal v1.5.0 // indirect
	github.com/spf13/cobra v1.1.1 // indirect
	github.com/stretchr/testify v1.6.1
	github.com/tmc/scp v0.0.0-20170824174625-f7b48647feef // indirect
	github.com/travelaudience/aerospike-operator v0.0.0-20191002090530-354c1a4e7e2a
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	github.com/yuin/gopher-lua v0.0.0-20200816102855-ee81675732da // indirect
	go.etcd.io/etcd v0.5.0-alpha.5.0.20200910180754-dd1b699fc489 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	golang.org/x/crypto v0.0.0-20201221181555-eec23a3978ad
	golang.org/x/oauth2 v0.0.0-20210201163806-010130855d6c // indirect
	golang.org/x/term v0.0.0-20201210144234-2321bbc49cbf // indirect
	golang.org/x/time v0.0.0-20201208040808-7e3f01d25324 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
	k8s.io/api v0.19.4
	k8s.io/apimachinery v0.19.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/component-helpers v0.20.0-alpha.2 // indirect
	k8s.io/gengo v0.0.0-20201113003025-83324d819ded // indirect
	k8s.io/helm v2.17.0+incompatible // indirect
	k8s.io/klog/v2 v2.5.0 // indirect
	k8s.io/kube-state-metrics v1.9.7 // indirect
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.0.14 // indirect
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/kubebuilder v1.0.9-0.20201021204649-36124ae2e027 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.0.2 // indirect
	sigs.k8s.io/testing_frameworks v0.1.2 // indirect
)

replace github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309 // Required by Helm

replace github.com/openshift/api => github.com/openshift/api v0.0.0-20190924102528-32369d4db2ad // Required until https://github.com/operator-framework/operator-lifecycle-manager/pull/1241 is resolved

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.19.2 // Required by prometheus-operator
)
