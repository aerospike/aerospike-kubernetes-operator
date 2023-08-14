pipeline {
    agent any
    tools {
        go 'go-1.19'
    }

    environment {
        GOPATH="/var/lib/jenkins/go"
        // Operator sdk command "operator-sdk" should be present in PATH or at
        // /usr/local/operator-sdk-1.28.0/
        PATH="/usr/local/operator-sdk-1.28.0/:${GOPATH}/bin:/usr/local/bin:${env.PATH}"
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

        AEROSPIKE_CUSTOM_INIT_REGISTRY="568976754000.dkr.ecr.ap-south-1.amazonaws.com"
    }

    stages {
        stage("Pipeline" ) {
            options {
              lock("gke-k8s-cluster")
            }

            stages {
                stage("Checkout") {
                    steps {
                        sh "chmod -R 744 bin || true"
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
                            // Changing directory again otherwise operator generates binary with the symlink name
                            sh "cd ${GO_REPO} && make docker-buildx IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make bundle IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make bundle-build bundle-push BUNDLE_IMG=${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "cd ${GO_REPO} && make docker-buildx-catalog CATALOG_IMG=${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} IMG=${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                        }
                    }
                }

                stage("Vulnerability scanning") {
                    steps {
                        script {
                           dir("${env.GO_REPO}") {
                               // Install synk
                               sh "wget https://static.snyk.io/cli/latest/snyk-linux"
                               sh "chmod +x snyk-linux"
                               sh "set +x; ./snyk-linux auth \$(cat ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/third-party-credentials/snyk); set -x"

                               // Scan the dependencies
                               sh "./snyk-linux test  --severity-threshold=high"

                               // Scan the operator images
                               sh "./snyk-linux container test  ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME} --severity-threshold=high --file=Dockerfile --policy-path=.snyk"
                               sh "./snyk-linux container test  ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} --severity-threshold=high --file=Dockerfile --policy-path=.snyk"
                           }
                        }
                    }
                }

                stage('Test') {
                    steps {
                        dir("${env.GO_REPO}") {
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
							sh "set +x; docker login --username AWS  568976754000.dkr.ecr.ap-south-1.amazonaws.com -p \$(aws ecr get-login-password --region ap-south-1); set -x"
                            sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY}"

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
        cleanup {
            script {
                sh "chmod -R 744 bin || true"
            }
            cleanWs()
        }
    }
}

boolean isNightly() {
    return env.BRANCH_NAME == null
}

String getVersion() {
    def prefix = "3.0.0"
    def candidateName = ""
    if(isNightly()) {
        def timestamp = new Date().format("yyyy-MM-dd")
        candidateName =  "nightly-${timestamp}"
    } else {
        candidateName =  "candidate-${env.BRANCH_NAME}"
    }

    def candidateNameMax = 30 - prefix.length()
    candidateName = abbreviate(candidateName, candidateNameMax)
    version = "${prefix}-${candidateName}-${env.BUILD_NUMBER}"
    return normalizeVersion(version)
}

String normalizeVersion(String version) {
    return version.toLowerCase().replaceAll(/[^a-zA-Z0-9-.]+/, "-").replaceAll(/(^-+)|(-+$)/,"")
}

String abbreviate(String str, int length) {
  if(str.length() <= length) {
    return str
  }

  def parts = str.split("[._\\-/]")
  def abbreviated = ""
  for(part in parts) {
    abbreviated += part.substring(0,Math.min(part.length(), 2))
    abbreviated += "-"
  }

  return abbreviated.substring(0, abbreviated.length()-1)
}