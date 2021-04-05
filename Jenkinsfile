pipeline {
    agent any
    tools {
        go 'go-1.13.5'
    }

    environment {
        GOPATH="/var/lib/jenkins/go"
        GO_REPO_ROOT="${env.GOPATH}/src/github.com"
        GO_REPO="${env.GO_REPO_ROOT}/aerospike-kubernetes-operator"
        DOCKER_REGISTRY=""
        OPERATOR_NAME = "aerospike-kubernetes-operator"
        OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME = "${env.DOCKER_REGISTRY}aerospike/${env.OPERATOR_NAME}:candidate-${env.BRANCH_NAME}"
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
                            sh "rsync -aK ${env.WORKSPACE}/../../aerospike-kubernetes-operator-resources/secrets/ deploy/secrets"
                            // Changing directory again otherwise operator generates binary with the symlink name.
                            sh "cd ${GO_REPO} && operator-sdk build ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                            sh "docker push ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                        }
                    }
                }

               stage('Test') {
                    steps {
                        dir("${env.GO_REPO}") {
                            sh "./test/e2e/test.sh ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                        }
                    }
                }
            }
        }
    }

    post {
        always {
            junit testResults: '**/build/test-results/**/*.xml', keepLongStdio: true
        }
    }
}
