#!/bin/sh

# This script is used to start tikv containers in kubernetes cluster

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

ARGS="--data=/var/lib/cell/data \
--log-file=/var/lib/cell/cell.log \
--addr=${HOSTNAME}:10800 \
--addr-cli=:6370 \
--zone=zone-1 --rack=rack-1 \
--interval-heartbeat-store=5 \
--interval-heartbeat-cell=2 \
--pd=${CLUSTER_NAME}-pd:2379
"

echo "starting cell ..."
echo "/usr/local/bin/cell ${ARGS}"
exec /usr/local/bin/cell ${ARGS}
