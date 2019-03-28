#!/bin/sh

# This script is used to start proxy containers in kubernetes cluster

# Use DownwardAPIVolumeFiles to store informations of the cluster:
# https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/#the-downward-api
#
#   runmode="normal/debug"
#
set -uo pipefail
ANNOTATIONS="/etc/podinfo/annotations"

if [[ ! -f "${ANNOTATIONS}" ]]
then
    echo "${ANNOTATIONS} does't exist, exiting."
    exit 1
fi
source ${ANNOTATIONS} 2>/dev/null
runmode=${runmode:-normal}
if [[ X${runmode} == Xdebug ]]
then
    echo "entering debug mode."
    tail -f /dev/null
fi

# the general form of variable PEER_SERVICE_NAME is: "<clusterName>-pd-peer"
cluster_name=${CLUSTER_NAME}
discovery_url="${cluster_name}-discovery.${NAMESPACE}.svc:10261"

ARGS="
--cfg=/etc/proxy/proxy-config.json
"

until result=$(wget -qO- -T 3 http://${discovery_url}/proxy-config 2>/dev/null); do
    echo "waiting for discovery service returns start args ..."
    sleep $((RANDOM % 5))
done
echo ${resutl} > /etc/proxy/proxy-config.json

echo "start cell-proxy ..."
cat /etc/proxy/proxy-config.json
exec /redis-proxy ${ARGS}
