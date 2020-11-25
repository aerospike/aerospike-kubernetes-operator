pipeline {
    agent any
    tools {
        go 'go-1.13.4'
    }

    environment {
        GOPATH = "${env.WORKSPACE}/"
        PATH = "${env.PATH}:${env.WORKSPACE}/bin:/usr/local/go/bin"
        GOOS = "linux"
        GOARCH = "amd64"
        CGO_ENABLED = 0
        DOCKER_REGISTRY="192.168.200.205:5000"
        OPERATOR_NAME = "aerospike-kubernetes-operator"
        OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME = "${env.DOCKER_REGISTRY}/aerospike/${env.OPERATOR_NAME}:candidate-${env.BRANCH_NAME}"
    }

    stages {
        stage('Compile') {
            steps {
                sh 'operator-sdk build ${env.OPERATOR_CONTAINER_IMAGE_CANDIDATE_NAME}'
            }
        }
    }

    post {
        cleanup {
            cleanWs()
        }
    }

}