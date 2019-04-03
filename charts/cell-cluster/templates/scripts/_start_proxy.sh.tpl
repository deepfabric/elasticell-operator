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
hostip=`hostname -i | tr "\n" " " | sed "s/ //g"`
encoded_pod_ip=`echo ${hostip} | tr "\n" " " | sed "s/ //g" | base64`

# the general form of variable PEER_SERVICE_NAME is: "<clusterName>-pd-peer"
discovery_url="${CLUSTER_NAME}-discovery.${NAMESPACE}.svc:10261"

until result=$(wget -qO- -T 3 http://${discovery_url}/proxy-config/${encoded_pod_ip} 2>/dev/null); do
    echo "waiting for discovery service returns start args ..."
    sleep $((RANDOM % 5))
done
echo ${result} > /var/lib/redis-proxy/proxy-config.json

ARGS="\
-log-level=debug \
--cfg=/var/lib/redis-proxy/proxy-config.json \
"

echo "start cell-proxy ..."
cat /etc/proxy/proxy-config.json
exec /usr/local/bin/redis-proxy ${ARGS}
