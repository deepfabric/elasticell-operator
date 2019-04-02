#!/bin/sh

# This script is used to start pd containers in kubernetes cluster

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

# the general form of variable PEER_SERVICE_NAME is: "<clusterName>-pd"
cluster_name=`echo ${SERVICE_NAME} | sed 's/-pd//'`
hostip=`hostname -i | tr "\n" " " | sed "s/ //g"`
domain="${HOSTNAME}.${SERVICE_NAME}.${NAMESPACE}"
pd_domain="${SERVICE_NAME}.${NAMESPACE}.svc"

discovery_url="${cluster_name}-discovery.${NAMESPACE}.svc:10261"
encoded_domain_url=`echo ${domain}  | tr "\n" " " | sed "s/ //g" | base64`
encoded_pod_ip=`echo ${hostip} | tr "\n" " " | sed "s/ //g" | base64`
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

    if nslookup ${pd_domain} 2>/dev/null
    then
        echo "nslookup domain ${pd_domain} success"
        break
    else
        echo "nslookup domain ${pd_domain} failed" >&2
    fi

done


ARGS="--data=/var/lib/pd/data \
-log-level=debug \
--name=${HOSTNAME} \
--addr-rpc=${hostip}:20800 \
--urls-client=http://${hostip}:2379 \
--urls-peer=http://${hostip}:2380 \
--initial-cluster=\
"

until result=$(wget -qO- -T 3 http://${discovery_url}/new/${encoded_domain_url}/${encoded_pod_ip} 2>/dev/null); do
    echo "waiting for discovery service returns start args ..."
    sleep $((RANDOM % 5))
done
ARGS="${ARGS}${result}"

echo "starting pd-server ..."
sleep $((RANDOM % 10))
echo "/usr/local/bin/pd ${ARGS}"
exec /usr/local/bin/pd ${ARGS}
