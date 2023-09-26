#!/bin/bash

# sed-wrap runs the appropriate sed command based on the
# underlying value of $OSTYPE
sed-wrap() {
  if [[ "${OSTYPE}" == "linux"* ]]; then
    # Linux
    sed -i "$1" "$2"
  else
    # macOS, BSD
    sed -i '' "$1" "$2"
  fi
}

echo
echo "Last 5 tags:"
git tag --sort=-taggerdate | head -n 5
echo

read -rp "Enter release version: " VERSION

if [[ ${VERSION} =~ ^v[0-9]+\.[0-9]+\.[0-9]+.*$ ]]; then
    echo "New Version: ${VERSION}"
else
    echo "Version must be in the format v0.1.0"
    exit 1
fi

LOKI_CURRENT=$(sed -n -e 's/^version: //p' production/helm/loki/Chart.yaml)
LOKI_SUGGESTED=$(tools/increment_version.sh -m "${LOKI_CURRENT}")
PROMTAIL_CURRENT=$(sed -n -e 's/^version: //p' production/helm/promtail/Chart.yaml)
PROMTAIL_SUGGESTED=$(tools/increment_version.sh -m "${PROMTAIL_CURRENT}")
LOKI_STACK_CURRENT=$(sed -n -e 's/^version: //p' production/helm/loki-stack/Chart.yaml)
LOKI_STACK_SUGGESTED=$(tools/increment_version.sh -m "${LOKI_STACK_CURRENT}")
echo
echo "Current Loki helm chart version: ${LOKI_CURRENT}"
read -rp "Enter new Loki helm chart version [${LOKI_SUGGESTED}]: " LOKI_VERSION
LOKI_VERSION=${LOKI_VERSION:-${LOKI_SUGGESTED}}
echo
echo "Current Promtail helm chart version: ${PROMTAIL_CURRENT}"
read -rp "Enter new Promtail helm chart version [${PROMTAIL_SUGGESTED}]: " PROMTAIL_VERSION
PROMTAIL_VERSION=${PROMTAIL_VERSION:-${PROMTAIL_SUGGESTED}}
echo
echo "Current Loki-Stack helm chart version: ${LOKI_STACK_CURRENT}"
read -rp "Enter new Loki-Stack helm chart version [${LOKI_STACK_SUGGESTED}]: " LOKI_STACK_VERSION
LOKI_STACK_VERSION=${LOKI_STACK_VERSION:-${LOKI_STACK_SUGGESTED}}
echo

echo "Creating Release"
echo "Release Version:       ${VERSION}"
echo "Loki Helm Chart:       ${LOKI_VERSION}"
echo "Promtail Helm Chart:   ${PROMTAIL_VERSION}"
echo "Loki-Stack Helm Chart: ${LOKI_STACK_VERSION}"
echo
read -rp "Is this correct? [y]: " CONTINUE
CONTINUE=${CONTINUE:-y}
echo

if [[ "${CONTINUE}" != "y" ]]; then
 exit 1
fi

echo "Updating helm and ksonnet image versions"
sed-wrap "s/.*promtail:.*/    promtail: 'grafana\/promtail:${VERSION}',/" production/ksonnet/promtail/config.libsonnet
sed-wrap "s/.*loki_canary:.*/    loki_canary: 'grafana\/loki-canary:${VERSION}',/" production/ksonnet/loki-canary/config.libsonnet
sed-wrap "s/.*loki:.*/    loki: 'grafana\/loki:${VERSION}',/" production/ksonnet/loki/images.libsonnet
sed-wrap "s/.*tag:.*/  tag: ${VERSION}/" production/helm/loki/values.yaml
sed-wrap "s/.*tag:.*/  tag: ${VERSION}/" production/helm/promtail/values.yaml

echo "Updating helm charts"
sed-wrap "s/^version:.*/version: ${LOKI_VERSION}/" production/helm/loki/Chart.yaml
sed-wrap "s/^version:.*/version: ${PROMTAIL_VERSION}/" production/helm/promtail/Chart.yaml
sed-wrap "s/^version:.*/version: ${LOKI_STACK_VERSION}/" production/helm/loki-stack/Chart.yaml

sed-wrap "s/^appVersion:.*/appVersion: ${VERSION}/" production/helm/loki/Chart.yaml
sed-wrap "s/^appVersion:.*/appVersion: ${VERSION}/" production/helm/promtail/Chart.yaml
sed-wrap "s/^appVersion:.*/appVersion: ${VERSION}/" production/helm/loki-stack/Chart.yaml

echo
echo "######################################################################################################"
echo
echo "Version numbers updated, create a new branch, commit and push"
echo
echo "######################################################################################################"

