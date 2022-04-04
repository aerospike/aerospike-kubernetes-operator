pipeline {
    agent any
    tools {
        go 'go-1.17'
    }

    environment {
        GOPATH="/var/lib/jenkins/go"
        // Operator sdk command "operator-sdk" should be present in PATH or at
        // /usr/local/operator-sdk-1.10.1/
        PATH="/usr/local/operator-sdk-1.10.1/:${GOPATH}/bin:${env.PATH}"
        GO_REPO_ROOT="${env.GOPATH}/src/github.com"
        GO_REPO="${env.GO_REPO_ROOT}/aerospike-kubernetes-operator"
        DOCKER_REGISTRY="docker.io"
        DOCKER_ACCOUNT="aerospike"
        OPERATOR_NAME = "aerospike-kubernetes-operator"
        OPERATOR_VERSION = getVersion()
        OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME = "${env.DOCKER_REGISTRY}/${DOCKER_ACCOUNT}/${env.OPERATOR_NAME}-nightly:${env.OPERATOR_VERSION}"
        OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME = "${env.DOCKER_REGISTRY}/${DOCKER_ACCOUNT}/${env.OPERATOR_NAME}-bundle-nightly:${env.OPERATOR_VERSION}"
        OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME = "${env.DOCKER_REGISTRY}/${DOCKER_ACCOUNT}/${env.OPERATOR_NAME}-catalog-nightly:${env.OPERATOR_VERSION}"

        // Variable names used in the operator make file.
        VERSION="${env.OPERATOR_VERSION}"
        IMG="${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
        BUNDLE_IMG="${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME}"
    }

    stages {
        stage("Pipeline" ) {
            options {
              lock("gke-k8s-cluster")
            }

            stages {
                stage("Checkout") {
                    steps {
                        checkout([
                            $class: 'GitSCM',
                            branches: scm.branches,
                            extensions: scm.extensions + [[$class: 'CleanBeforeCheckout']],
                            userRemoteConfigs: scm.userRemoteConfigs
                        ])
                    }
                }

                stage('Build') {
                    steps {
                        sh 'mkdir -p $GO_REPO_ROOT'
                        sh 'ln -sfn ${WORKSPACE} ${GO_REPO}'

                        dir("${env.GO_REPO}") {
                            // Changing directory again otherwise operator generates binary with the symlink name.
                            sh "cd ${GO_REPO} && make docker-build  IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make docker-push  IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make bundle IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make bundle-build bundle-push BUNDLE_IMG=${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make bundle-build bundle-push BUNDLE_IMG=${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make catalog-build catalog-push CATALOG_IMG=${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                        }
                    }
                }

                stage('Test') {
                    steps {
                        dir("${env.GO_REPO}") {
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
                            sh "./test/test.sh -c ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME}"
                        }
                    }
                }
            }
        }
    }

    post {
        always {
            junit testResults: '**/junit.xml', keepLongStdio: true
        }
    }
}

boolean isNightly() {
    return env.BRANCH_NAME == null
}

String getVersion() {
    def prefix = "2.0.0"
    def candidateName = ""
    if(isNightly()) {
        def timestamp = new Date().format("yyyy-MM-dd")
        candidateName =  "nightly-${timestamp}-${env.BUILD_NUMBER}"
    } else {
        candidateName =  "candidate-${env.BRANCH_NAME}-${env.BUILD_NUMBER}"
    }

    version = "${prefix}-${candidateName}"
    return normalizeVersion(version)
}

String normalizeVersion(String version) {
    return version.toLowerCase().replaceAll(/[^a-zA-Z0-9-.]+/, "-").replaceAll(/(^-+)|(-+$)/,"")
}
