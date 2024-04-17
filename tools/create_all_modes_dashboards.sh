#!/usr/bin/env bash
# Run this from the root directory of Loki

set -eo pipefail

WORKDIR=production/all-modes-loki-mixin-compiled
SED=sed
# Mac users should install gsed using homebrew: brew install gnu-sed
if command -v gsed &> /dev/null
then
   SED=gsed
fi

# Clean up working dir
rm -rf $WORKDIR
mkdir -p $WORKDIR

# Copy over production/loki-mixin-compiled to working dir
cp -r production/loki-mixin-compiled/* $WORKDIR

# Run replaces
$SED -i 's!job=~\\"($namespace)/distributor\\"!job=~\\"($namespace)/(distributor|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/ingester\.\*\\"!job=~\\"($namespace)/(ingester\.\*|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"$namespace/ingester\.\*\\"!job=~\\"$namespace/(ingester\.\*|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/ingester\.\+\\"!job=~\\"($namespace)/(ingester\.\+|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/ingester-zone\.\*\\"!job=~\\"($namespace)/(ingester-zone\.\*|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/ingester\\"!job=~\\"($namespace)/(ingester|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"$namespace/ingester\\"!job=~\\"$namespace/(ingester|(loki|enterprise-logs)-write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/query-frontend\\"!job=~\\"($namespace)/(query-frontend|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"$namespace/compactor\\"!job=~\\"($namespace)/(compactor|(loki|enterprise-logs)-backend|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/compactor\\"!job=~\\"($namespace)/(compactor|(loki|enterprise-logs)-backend|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/querier\\"!job=~\\"($namespace)/(querier|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/query-scheduler\\"!job=~\\"($namespace)/(query-scheduler|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/index-gateway\\"!job=~\\"($namespace)/(index-gateway|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/ruler\\"!job=~\\"($namespace)/(ruler|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!job=~\\"($namespace)/(querier|index-gateway)\\"!job=~\\"($namespace)/(querier|index-gateway|(loki|enterprise-logs)-read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json

$SED -i 's!pod=~\\"querier.*\\"!pod=~\\"(querier.*|(loki|enterprise-logs)-read.*|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!pod=~\\"distributor.*\\"!pod=~\\"(distributor.*|(loki|enterprise-logs)-write.*|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!pod=~\\"ingester.*\\"!pod=~\\"(ingester.*|(loki|enterprise-logs)-write.*|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json

$SED -i 's!container=~\\"distributor\\"!container=~\\"(distributor|write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=\\"ingester\\"!container=~\\"(ingester|write|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=\\"compactor\\"!container=~\\"(compactor|backend|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=\\"querier\\"!container=~\\"(querier|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=~\\"querier\\"!container=~\\"(querier|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=~\\"query-frontend\\"!container=~\\"(query-frontend|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=~\\"query-scheduler\\"!container=~\\"(query-scheduler|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=\\"index-gateway\\"!container=~\\"(index-gateway|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
$SED -i 's!container=~\\"ruler\\"!container=~\\"(ruler|(loki|read|loki-single-binary)\\"!g' $WORKDIR/dashboards/*.json
