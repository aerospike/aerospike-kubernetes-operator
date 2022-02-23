module github.com/aerospike/aerospike-kubernetes-operator

go 1.17

require (
	cloud.google.com/go v0.76.0 // indirect
	github.com/Azure/go-autorest/autorest v0.11.17 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.11 // indirect
	github.com/aerospike/aerospike-management-lib v0.0.0-20211124100005-a13f1803f868
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d
	github.com/ashishshinde/aerospike-client-go/v5 v5.0.0-20210915134909-922798c88e83
	github.com/evanphx/json-patch v4.11.0+incompatible
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-logr/logr v0.4.0
	github.com/onsi/ginkgo/v2 v2.1.2
	github.com/onsi/gomega v1.18.1
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/stretchr/testify v1.7.0
	github.com/xeipuuv/gojsonpointer v0.0.0-20190905194746-02993c407bfb // indirect
	golang.org/x/crypto v0.0.0-20210817164053-32db794688a5
	golang.org/x/net v0.0.0-20220127200216-cd36cc0744dd // indirect
	golang.org/x/oauth2 v0.0.0-20210201163806-010130855d6c // indirect
	golang.org/x/sys v0.0.0-20220114195835-da31bd327af9 // indirect
	golang.org/x/tools v0.1.4 // indirect
	k8s.io/api v0.21.2
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/kubectl v0.21.2
	sigs.k8s.io/controller-runtime v0.9.2
)

replace github.com/docker/docker => github.com/moby/moby v0.7.3-0.20190826074503-38ab9da00309 // Required by Helm

replace github.com/openshift/api => github.com/openshift/api v0.0.0-20190924102528-32369d4db2ad // Required until https://github.com/operator-framework/operator-lifecycle-manager/pull/1241 is resolved

//replace github.com/ashishshinde/aerospike-client-go => /home/ashish/go/src/github.com/ashishshinde/aerospike-client-go
