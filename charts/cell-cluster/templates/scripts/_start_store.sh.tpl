#!/bin/sh

# This script is used to start store containers in kubernetes cluster

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

store_domain="${HOSTNAME}.${PEER_SERVICE_NAME}.${NAMESPACE}.svc"
elapseTime=0
period=1
threshold=30
while true; do
    sleep ${period}
    elapseTime=$(( elapseTime+period ))

    if [[ ${elapseTime} -ge ${threshold} ]]
    then
        echo "waiting for pd cluster ready timeout" >&2
        exit 1
    fi

    if nslookup ${store_domain} 2>/dev/null
    then
        echo "nslookup store_domain ${store_domain} success"
        break
    else
        echo "nslookup store_domain ${store_domain} failed" >&2
    fi

done


discovery_url="${CLUSTER_NAME}-discovery.${NAMESPACE}.svc:10261"

until result=$(wget -qO- -T 3 http://${discovery_url}/store-config 2>/dev/null); do
    echo "waiting for discovery service returns start args ..."
    sleep $((RANDOM % 5))
done

ARGS="--data=/var/lib/store/data \
-log-level=debug \
--addr=${store_domain}:10800 \
--addr-cli=${store_domain}:6370 \
--zone=zone-1 --rack=rack-1 \
--interval-heartbeat-store=5 \
--interval-heartbeat-cell=2 \
--pd=${result}\
"

echo "starting cell ..."
echo "/usr/local/bin/cell ${ARGS}"
exec /usr/local/bin/cell ${ARGS}
