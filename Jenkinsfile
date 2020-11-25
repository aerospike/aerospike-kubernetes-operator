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
        stage('Build') {
             options {
                lock('gopath-k8s-operator')
            }

            steps {
                sh 'mkdir -p $GO_REPO_ROOT'
                sh 'ln -sf ${WORKSPACE} ${GO_REPO}'

                dir("${env.GO_REPO}") {
                    sh "operator-sdk build ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                    sh "docker push ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                }
            }
        }

       stage('Test') {
             options {
                lock('gke-k8s-cluster')
            }

            steps {
                dir("${env.GO_REPO}") {
                    sh "./test/e2e/test.sh ${OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}"
                }
            }
        }
    }                                                                                                    }