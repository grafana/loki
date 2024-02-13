import { buildCommands, parseImageMeta } from '../src/docker'

describe('push', () => {
  it('tags and pushes each architecture for each image', () => {
    const commands = buildCommands('grafana', [
      'fluent-bit-2.9.4-linux-amd64.tar',
      'fluentd-2.9.4-linux-amd64.tar',
      'logcli-2.9.4-linux-amd64.tar',
      'logcli-2.9.4-linux-arm.tar',
      'logcli-2.9.4-linux-arm64.tar',
      'logstash-2.9.4-linux-amd64.tar',
      'loki-2.9.4-linux-amd64.tar',
      'loki-2.9.4-linux-arm.tar',
      'loki-2.9.4-linux-arm64.tar',
      'loki-canary-2.9.4-linux-amd64.tar',
      'loki-canary-2.9.4-linux-arm.tar',
      'loki-canary-2.9.4-linux-arm64.tar',
      'loki-canary-boringcrypto-2.9.4-linux-amd64.tar',
      'loki-canary-boringcrypto-2.9.4-linux-arm.tar',
      'loki-canary-boringcrypto-2.9.4-linux-arm64.tar',
      'loki-operator-2.9.4-linux-amd64.tar',
      'promtail-2.9.4-linux-amd64.tar',
      'promtail-2.9.4-linux-arm.tar',
      'promtail-2.9.4-linux-arm64.tar',
      'querytee-2.9.4-linux-amd64.tar'
    ])

    expect(commands).toEqual([
      `docker load -i fluent-bit-2.9.4-linux-amd64.tar`,
      `docker push -a grafana/fluent-bit`,
      `docker manifest create grafana/fluent-bit:2.9.4 grafana/fluent-bit:2.9.4-amd64`,
      `docker manifest push grafana/fluent-bit:2.9.4`,

      `docker load -i fluentd-2.9.4-linux-amd64.tar`,
      `docker push -a grafana/fluentd`,
      `docker manifest create grafana/fluentd:2.9.4 grafana/fluentd:2.9.4-amd64`,
      `docker manifest push grafana/fluentd:2.9.4`,

      `docker load -i logcli-2.9.4-linux-amd64.tar`,
      `docker load -i logcli-2.9.4-linux-arm.tar`,
      `docker load -i logcli-2.9.4-linux-arm64.tar`,
      `docker push -a grafana/logcli`,
      `docker manifest create grafana/logcli:2.9.4 grafana/logcli:2.9.4-amd64 grafana/logcli:2.9.4-arm grafana/logcli:2.9.4-arm64`,
      `docker manifest push grafana/logcli:2.9.4`,

      `docker load -i logstash-2.9.4-linux-amd64.tar`,
      `docker push -a grafana/logstash`,
      `docker manifest create grafana/logstash:2.9.4 grafana/logstash:2.9.4-amd64`,
      `docker manifest push grafana/logstash:2.9.4`,

      `docker load -i loki-2.9.4-linux-amd64.tar`,
      `docker load -i loki-2.9.4-linux-arm.tar`,
      `docker load -i loki-2.9.4-linux-arm64.tar`,
      `docker push -a grafana/loki`,
      `docker manifest create grafana/loki:2.9.4 grafana/loki:2.9.4-amd64 grafana/loki:2.9.4-arm grafana/loki:2.9.4-arm64`,
      `docker manifest push grafana/loki:2.9.4`,

      `docker load -i loki-canary-2.9.4-linux-amd64.tar`,
      `docker load -i loki-canary-2.9.4-linux-arm.tar`,
      `docker load -i loki-canary-2.9.4-linux-arm64.tar`,
      `docker push -a grafana/loki-canary`,
      `docker manifest create grafana/loki-canary:2.9.4 grafana/loki-canary:2.9.4-amd64 grafana/loki-canary:2.9.4-arm grafana/loki-canary:2.9.4-arm64`,
      `docker manifest push grafana/loki-canary:2.9.4`,

      `docker load -i loki-canary-boringcrypto-2.9.4-linux-amd64.tar`,
      `docker load -i loki-canary-boringcrypto-2.9.4-linux-arm.tar`,
      `docker load -i loki-canary-boringcrypto-2.9.4-linux-arm64.tar`,
      `docker push -a grafana/loki-canary-boringcrypto`,
      `docker manifest create grafana/loki-canary-boringcrypto:2.9.4 grafana/loki-canary-boringcrypto:2.9.4-amd64 grafana/loki-canary-boringcrypto:2.9.4-arm grafana/loki-canary-boringcrypto:2.9.4-arm64`,
      `docker manifest push grafana/loki-canary-boringcrypto:2.9.4`,

      `docker load -i loki-operator-2.9.4-linux-amd64.tar`,
      `docker push -a grafana/loki-operator`,
      `docker manifest create grafana/loki-operator:2.9.4 grafana/loki-operator:2.9.4-amd64`,
      `docker manifest push grafana/loki-operator:2.9.4`,

      `docker load -i promtail-2.9.4-linux-amd64.tar`,
      `docker load -i promtail-2.9.4-linux-arm.tar`,
      `docker load -i promtail-2.9.4-linux-arm64.tar`,
      `docker push -a grafana/promtail`,
      `docker manifest create grafana/promtail:2.9.4 grafana/promtail:2.9.4-amd64 grafana/promtail:2.9.4-arm grafana/promtail:2.9.4-arm64`,
      `docker manifest push grafana/promtail:2.9.4`,

      `docker load -i querytee-2.9.4-linux-amd64.tar`,
      `docker push -a grafana/querytee`,
      `docker manifest create grafana/querytee:2.9.4 grafana/querytee:2.9.4-amd64`,
      `docker manifest push grafana/querytee:2.9.4`
    ])
  })
})

describe('parseImage', () => {
  it('parse the image file name', () => {
    const twoNineFour = '2.9.4'
    for (const img of [
      {
        file: 'fluent-bit-2.9.4-linux-amd64.tar',
        expected: {
          image: 'fluent-bit',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'fluentd-2.9.4-linux-amd64.tar',
        expected: {
          image: 'fluentd',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'logcli-2.9.4-linux-amd64.tar',
        expected: {
          image: 'logcli',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'logcli-2.9.4-linux-arm.tar',
        expected: {
          image: 'logcli',
          version: twoNineFour,
          platform: 'linux/arm'
        }
      },
      {
        file: 'logcli-2.9.4-linux-arm64.tar',
        expected: {
          image: 'logcli',
          version: twoNineFour,
          platform: 'linux/arm64'
        }
      },
      {
        file: 'logstash-2.9.4-linux-amd64.tar',
        expected: {
          image: 'logstash',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'loki-2.9.4-linux-amd64.tar',
        expected: {
          image: 'loki',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'loki-2.9.4-linux-arm.tar',
        expected: {
          image: 'loki',
          version: twoNineFour,
          platform: 'linux/arm'
        }
      },
      {
        file: 'loki-2.9.4-linux-arm64.tar',
        expected: {
          image: 'loki',
          version: twoNineFour,
          platform: 'linux/arm64'
        }
      },
      {
        file: 'loki-canary-2.9.4-linux-amd64.tar',
        expected: {
          image: 'loki-canary',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'loki-canary-2.9.4-linux-arm.tar',
        expected: {
          image: 'loki-canary',
          version: twoNineFour,
          platform: 'linux/arm'
        }
      },
      {
        file: 'loki-canary-2.9.4-linux-arm64.tar',
        expected: {
          image: 'loki-canary',
          version: twoNineFour,
          platform: 'linux/arm64'
        }
      },
      {
        file: 'loki-canary-boringcrypto-2.9.4-linux-amd64.tar',
        expected: {
          image: 'loki-canary-boringcrypto',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'loki-canary-boringcrypto-2.9.4-linux-arm.tar',
        expected: {
          image: 'loki-canary-boringcrypto',
          version: twoNineFour,
          platform: 'linux/arm'
        }
      },
      {
        file: 'loki-canary-boringcrypto-2.9.4-linux-arm64.tar',
        expected: {
          image: 'loki-canary-boringcrypto',
          version: twoNineFour,
          platform: 'linux/arm64'
        }
      },
      {
        file: 'loki-operator-2.9.4-linux-amd64.tar',
        expected: {
          image: 'loki-operator',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'promtail-2.9.4-linux-amd64.tar',
        expected: {
          image: 'promtail',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'promtail-2.9.4-linux-arm.tar',
        expected: {
          image: 'promtail',
          version: twoNineFour,
          platform: 'linux/arm'
        }
      },
      {
        file: 'promtail-2.9.4-linux-arm64.tar',
        expected: {
          image: 'promtail',
          version: twoNineFour,
          platform: 'linux/arm64'
        }
      },
      {
        file: 'querytee-2.9.4-linux-amd64.tar',
        expected: {
          image: 'querytee',
          version: twoNineFour,
          platform: 'linux/amd64'
        }
      },
      {
        file: 'loki-2.9.4.alpha.1-linux-amd64.tar',
        expected: {
          image: 'loki',
          version: '2.9.4.alpha.1',
          platform: 'linux/amd64'
        }
      }
    ]) {
      const image = parseImageMeta(img.file)
      expect(image?.image).toEqual(img.expected.image)
      expect(image?.version.toString()).toEqual(img.expected.version)
      expect(image?.platform).toEqual(img.expected.platform)
    }
  })
})
