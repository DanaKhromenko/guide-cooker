@Library('testPipelineLibrary') _

def jenkinsAgent      = 'dockerized'

def ownerEmail        = 'danadiadius@gmail.com'
def ownerSlackChannel = '#test-guide-builds'

def mainBranch        = 'main'

def projectName       = env.JOB_NAME.split('/')[0].toLowerCase()
def testImageName     = "${projectName}-test/${env.BRANCH_NAME}".toLowerCase()

pipeline {
    agent { node { label jenkinsAgent } }

    environment {
        GH_TOKEN              = credentials('test-github-jenkins-bot')
        GH_CREDENTIAL         = "${GH_TOKEN_PSW}"
        TEST_SERVERLESS  = credentials('test-aws-credentials')
        AWS_ACCESS_KEY_ID     = "${TEST_SERVERLESS_USR}"
        AWS_SECRET_ACCESS_KEY = "${TEST_SERVERLESS_PSW}"
        INSTANCE_NAME         = "${BRANCH_NAME}-${BUILD_ID}".toLowerCase()
        OWNER                 = "${ownerEmail}"
    }

    options {
        ansiColor('xterm')
        buildDiscarder(logRotator(daysToKeepStr: '90', numToKeepStr: '10'))
        disableConcurrentBuilds()
        timeout(time: 1, unit: 'HOURS')
        timestamps()
    }

    stages {
        stage('Build Application') {
            steps {
                // Note: Commented out because this is a test project, and all resource names are fictitious.
                // sh 'make build'
                // sh 'make test'
                // sh 'make vet'
                // sh 'make package'
                sh 'make dummy'
            }
        }
        stage('Publish Results') {
            steps {
                // Note: Commented out because this is a test project, and all resource names are fictitious.
                // publishCoverage adapters: [coberturaAdapter('coverage/coverage.xml')], sourceFileResolver: sourceFiles('NEVER_STORE')
                // publishHTML([allowMissing: false, alwaysLinkToLastBuild: false, keepAll: false, reportDir: 'coverage', reportFiles: 'coverage.html', reportName: 'CoverageHTML', reportTitles: ''])
                sh 'make dummy'
            }
        }
        stage('Test Application') {
            steps {
                // Note: Commented out because this is a test project, and all resource names are fictitious.
                // sh 'make deploy'
                // sh 'sleep 60' // Note: some AWS resources require additional time after creation.
                // runServerlessFunctionalTestPython imageName: testImageName, extraDockerArgs: '-e AWS_ACCESS_KEY_ID -e AWS_SECRET_ACCESS_KEY'
                sh 'make dummy'
            }
            post {
                always {
                    // Note: Commented out because this is a test project, and all resource names are fictitious.
                    // sh 'make undeploy'
                    sh 'make dummy'
                }
            }
        }
        stage('Publish Application') {
            when {
                branch mainBranch
            }
            steps {
                // Note: Commented out because this is a test project, and all resource names are fictitious.
                // sh 'make publish'
                sh 'make dummy'
            }
        }
    }

    post {
        always {
            // Note: Commented out because this is a test project, and all resource names are fictitious.
            // sh 'make fix-permissions'
            // notifySlack(ownerSlackChannel)
            sh 'make dummy'
        }
        cleanup {
            // Note: Commented out because this is a test project, and all resource names are fictitious.
            // cleanWs()
            sh 'make dummy'
        }
    }
}
