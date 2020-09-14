def notifySlack(String buildStatus = 'started') {
    buildStatus = buildStatus ?: 'built'

    def color

    if (buildStatus == 'started') {
        color = '#0092FD'
    } else if (buildStatus == 'built') {
        color = '#44AF69'
    } else if (buildStatus == 'failed') {
        color = '#FF5440'
    } else {
        color = '#FF9FA1'
    }

    def msg = "`loki` has ${buildStatus}:\n${env.BUILD_URL}\n`SHA: ${env.GIT_COMMIT}`"

    slackSend(color: color, message: msg)
}

def notifyGithub(String context, String desc, String status) {
    def repo = env.GIT_URL.replaceFirst(/^.*\/([^\/]+?).git$/, '$1')
    githubNotify(account: 'northflank', context: context, credentialsId: 'github', description: desc, repo: 'loki', sha: env.GIT_COMMIT, status: status)
}

pipeline {
    agent {
        kubernetes {
            label 'ci-loki'
            yaml """
kind: Pod
metadata:
  name: ci-loki
  namespace: northflank-system
  labels:
    app: ci-loki
  annotations:
      sidecar.istio.io/inject: "false"
      container.apparmor.security.beta.kubernetes.io/loki: unconfined
      container.seccomp.security.alpha.kubernetes.io/loki: unconfined

spec:
  containers:
  - name: loki
    image: moby/buildkit:rootless
    args:
    - --oci-worker-no-process-sandbox
    resources:
        limits:
            cpu: "8"
            memory: "16Gi"
        requests:
            cpu: "4"
            memory: "8Gi"
    imagePullPolicy: Always
    tty: true
    volumeMounts:
      - name: jenkins-docker-cfg
        mountPath: /home/user/.docker
  nodeSelector:
        fast: true
  volumes:
  - name: jenkins-docker-cfg
    projected:
      sources:
      - secret:
          name: northflank-regcred
          items:
            - key: .dockerconfigjson
              path: config.json
"""
        }
    }
    stages {
        stage("loki") {
            steps {
                notifyGithub('northflank-ci/loki', 'loki build started', 'PENDING')
                notifySlack('started')

                container(name: 'loki', shell: '/bin/sh') {
                    sh "buildctl build --frontend=dockerfile.v0 --local context=`pwd`/ --local dockerfile=`pwd`/cmd/loki/ --opt filename=Dockerfile.cross --output type=image,name=eu.gcr.io/northflank/northflank/ci/loki:${GIT_COMMIT},push=true"
                }
            }

            post {
                success {
                    notifyGithub('northflank-ci/loki','loki built successfully', 'SUCCESS')
                }
                failure {
                    notifyGithub('northflank-ci/loki','loki build failed', 'FAILURE')
                }
            }
        }
    }
    post {
        success {
            notifySlack('built')
        }
        failure {
            notifySlack('failed')
        }
    }
}
