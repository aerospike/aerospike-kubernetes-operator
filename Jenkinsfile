pipeline {
    agent any
    tools {
        go 'go-1.23'
    }

    environment {
        GOPATH="/var/lib/jenkins/go"
        GO_REPO_ROOT="${env.GOPATH}/src/github.com"
        GO_REPO="${env.GO_REPO_ROOT}/aerospike-kubernetes-operator"
        PATH="${GOPATH}/bin:/usr/local/bin:${env.PATH}:${GO_REPO}/bin"
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
        AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE="aerospike"
        AEROSPIKE_CUSTOM_INIT_NAME_TAG="aerospike-kubernetes-init:2.3.0-dev3"
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
                            extensions: scm.extensions + [[$class: 'CleanBeforeCheckout']] +
                                        [[$class: 'SubmoduleOption',
                                        disableSubmodules: false,
                                        parentCredentials: false,
                                        recursiveSubmodules: true,
                                        reference: '',
                                        trackingSubmodules: false]],
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
                               sh "./snyk-linux test  --severity-threshold=high --fail-on=all"

                               // Scan the operator images
                               sh "./snyk-linux container test ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME} --severity-threshold=high --file=Dockerfile --policy-path=.snyk --fail-on=all"
                               sh "./snyk-linux container test ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} --severity-threshold=high --file=Dockerfile --policy-path=.snyk --fail-on=all"
                           }
                        }
                    }
                }

                stage('Detect Changes') {
                    steps {
                        script {
                            dir("${env.GO_REPO}") {
                                if(isNightly() || env.BRANCH_NAME == 'master') {
                                    // Run all-test cases for master and nightly builds
                                    env.RUN_ALL_TEST = 'true'
                                }
                                else {
                                    // Identify changed files using git diff
                                    def changedFiles = sh(script: "git diff --name-only origin/master...HEAD", returnStdout: true).trim()
                                    if(changedFiles.isEmpty()) {
                                        return
                                    }
                                    
                                    changedFiles = changedFiles.split('\n')
                                    // clusterTest=true, if cluster related files are modified.
                                    // (e.g. internal/controller/cluster/reconciler.go)
                                    def clusterTest = changedFiles.any {
                                        it.endsWith('.go') &&
                                        (it.contains('cluster/') ||
                                        it.contains('v1/'))
                                    }
                                    // backupTest=true, if backup related files are modified.
                                    // (e.g. internal/controller/backup/reconciler.go)
                                    def backupTest = changedFiles.any {
                                        it.endsWith('.go') &&
                                        (it.contains('/backup') ||
                                        it.contains('restore/') ||
                                        it.contains('v1beta1/'))
                                    }
                                    // allTest=true, if common files are modified.
                                    // (e.g. cmd/main.go)
                                    def allTest = changedFiles.any {
                                        it.startsWith('cmd/') ||
                                        (it.startsWith('internal/controller/common/') && !it.contains('backup_config_util.go')) ||
                                        it.startsWith('pkg/utils/') ||
                                        it.startsWith('errors/')
                                    }
                                    // smokeTest=true, if non-code files (excluding .go files) or some specific directories files are modified.
                                    // (e.g. config/webhook/manifests.yaml)
                                    def smokeTest = changedFiles.any {
                                        (it.startsWith('pkg/') && !it.contains('backup-service/') && !it.contains('utils/')) ||
                                        (it.startsWith('test/') && !it.contains('cluster/') && !it.contains('backup/') &&
                                        !it.contains('restore/') && !it.contains('backup_service/')) ||
                                        (!it.startsWith('helm-charts/') && !it.endsWith('.go') && !it.endsWith('_cr.yaml'))
                                    }
                                    // sampleFilesTest=true, if sample-files are modified.
                                    // (e.g. config/samples/dim_nostorage_cluster_cr.yaml)
                                    def sampleFilesTest = changedFiles.any {
                                        it.startsWith('config/samples/') && !it.contains('openldap/') && !it.contains('secrets/') && !it.contains('storage/') && it.endsWith('_cr.yaml')
                                    }

                                    env.RUN_CLUSTER_TEST = clusterTest.toString()
                                    env.RUN_BACKUP_TEST = backupTest.toString()
                                    env.RUN_ALL_TEST = allTest.toString()
                                    env.RUN_SMOKE_TEST = smokeTest.toString()
                                    env.RUN_SAMPLE_FILES_TEST = sampleFilesTest.toString()

                                }
                            }
                        }
                    }
                }

                stage('Cluster Test') {
                    when {
                        expression { env.RUN_CLUSTER_TEST == 'true' && env.RUN_ALL_TEST == 'false'}
                    }
                    steps {
                        dir("${env.GO_REPO}") {
                            // Run all cluster-related test cases
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
							sh "set +x; docker login --username AWS  568976754000.dkr.ecr.ap-south-1.amazonaws.com -p \$(aws ecr get-login-password --region ap-south-1); set -x"
                            sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY} -n ${AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE} -i ${AEROSPIKE_CUSTOM_INIT_NAME_TAG} -t cluster-test"

                        }
                    }
                }

                stage('Backup Test') {
                    when {
                        expression { env.RUN_BACKUP_TEST == 'true' && env.RUN_ALL_TEST == 'false'}
                    }
                    steps {
                        dir("${env.GO_REPO}") {
                            // Run all backup-related test cases, including backup, backup-service, and restore test suites
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
							sh "set +x; docker login --username AWS  568976754000.dkr.ecr.ap-south-1.amazonaws.com -p \$(aws ecr get-login-password --region ap-south-1); set -x"
                            sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY} -n ${AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE} -i ${AEROSPIKE_CUSTOM_INIT_NAME_TAG} -t backup-test"

                        }
                    }
                }

                stage('All Test') {
                    when {
                        expression { env.RUN_ALL_TEST == 'true'}
                    }
                    steps {
                        dir("${env.GO_REPO}") {
                            // Run all-test cases
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
							sh "set +x; docker login --username AWS  568976754000.dkr.ecr.ap-south-1.amazonaws.com -p \$(aws ecr get-login-password --region ap-south-1); set -x"
                            sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY} -n ${AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE} -i ${AEROSPIKE_CUSTOM_INIT_NAME_TAG} -t all-test"

                        }
                    }
                }

                stage('Smoke Test') {
                    when {
                        expression { (env.RUN_SMOKE_TEST == 'true' || env.RUN_SAMPLE_FILES_TEST == 'true') && env.RUN_CLUSTER_TEST == 'false' && env.RUN_ALL_TEST == 'false'}
                    }
                    steps {
                        dir("${env.GO_REPO}") {
                            script {
                                sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ config/samples/secrets"
							    sh "set +x; docker login --username AWS  568976754000.dkr.ecr.ap-south-1.amazonaws.com -p \$(aws ecr get-login-password --region ap-south-1); set -x"
                                if(env.RUN_SMOKE_TEST == 'true') {
                                    // Run smoke test
                                    sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY} -n ${AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE} -i ${AEROSPIKE_CUSTOM_INIT_NAME_TAG} -t cluster-test -f 'Sample files validation|UpdateAerospikeCluster'"
                                } 
                                else if(env.RUN_SAMPLE_FILES_TEST == 'true') {
                                    // Run sample-files test cases
                                    sh "./test/test.sh -b ${OPERATOR_BUNDLE_IMAGE_CANDIDATE_NAME} -c ${OPERATOR_CATALOG_IMAGE_CANDIDATE_NAME} -r ${AEROSPIKE_CUSTOM_INIT_REGISTRY} -n ${AEROSPIKE_CUSTOM_INIT_REGISTRY_NAMESPACE} -i ${AEROSPIKE_CUSTOM_INIT_NAME_TAG} -t cluster-test -f 'Sample files validation'"
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    post {
        always {
            junit testResults: '**/junit*.xml', keepLongStdio: true
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
    def prefix = "4.1.0-preview"
    def candidateName = ""
    if(isNightly()) {
        def timestamp = new Date().format("yyyy-MM-dd")
        candidateName =  "nightly-${timestamp}"
    } else {
        candidateName =  "${env.BRANCH_NAME}"
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