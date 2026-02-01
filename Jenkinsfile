pipeline {
    agent any

    environment {
        gitLabel = VersionNumber([
            projectStartDate: '2023-01-01',
            versionNumberString: "${params.gitLabel}",
            worstResultForIncrement: 'SUCCESS'
        ])
    }

    stages {
        stage('Unit Test') {
            steps {
                script {
                    currentBuild.displayName = "${params.gitLabel}"
                }
                echo 'Skipped...'
            }
        }
        stage('Docker Build') {
            steps {
                sh "docker build --build-arg debug_mode=--no-dev -t rocinantesystems/go-apcups-nis:latest ."
            }
        }
        stage('E2E Test (local)') {
            steps {
                echo 'Skipped...'
            }
        }
        stage('Docker:latest') {
            steps {
                sh "docker push rocinantesystems/go-apcups-nis:latest"
            }
        }
        stage('Push to stage') {
            steps {
                echo 'Skipped...'
                // sh './redeploy-rancher-stage.sh'
            }
        }
        stage('E2E Test (stage)') {
            steps {
                echo 'Skipped...'
            }
        }
        stage('Docker:tag') {
            steps {
                sh "docker tag rocinantesystems/go-apcups-nis:latest rocinantesystems/go-apcups-nis:${params.gitLabel}"
                sh "docker push rocinantesystems/go-apcups-nis:${params.gitLabel}"
                sh "docker rmi rocinantesystems/go-apcups-nis:${params.gitLabel}"
                sh "docker rmi rocinantesystems/go-apcups-nis:latest"
            }
        }
        stage('Push to production') {
            steps {
                echo 'Skipped...'
            }
        }
        stage('E2E Test (production)') {
            steps {
                echo 'Skipped...'
            }
        }
    }
}
