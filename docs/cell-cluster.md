# Cell Cluster Operation Guide

Cell Operator can manage multiple clusters in the same Kubernetes cluster. Clusters are qualified by `namespace` and `clusterName`.For now, only one cell cluster can be deployed in one kubernetes namespace.

The default `clusterName` is `demo` which is defined in charts/cell-cluster/values.yaml. The following variables will be used in the rest of the document:

```shell
$ releaseName="cell-cluster"
$ namespace="cell"
$ clusterName="demo" # Make sure this is the same as variable defined in charts/cell-cluster/values.yaml
```

> **Note:** The rest of the document will use `values.yaml` to reference `charts/cell-cluster/values.yaml`

## Deploy Cell cluster

After Cell Operator and Helm are deployed correctly, Cell cluster can be deployed using following command:

```shell
$ helm install charts/cell-cluster --name=${releaseName} --namespace=${namespace}
$ kubectl get po -n ${namespace} -l app.kubernetes.io/name=cell-operator
```

The resource limits should be equal or bigger than the resource requests, it is suggested to set limit and request equal to get [`Guaranteed` QoS]( https://kubernetes.io/docs/tasks/configure-pod-container/quality-service-pod/#create-a-pod-that-gets-assigned-a-qos-class-of-guaranteed).

For other settings, the variables in `values.yaml` are self-explanatory with comments. You can modify them according to your need before installing the charts.

## Access Cell cluster

By default cellcluster's redis service is is not exposed outside kubernetes cluster. You can modify it to NodePort which will expose the port on k8s host IP.  Or modify it to [`LoadBalancer`](https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer) if the underlining Kubernetes supports this kind of service.

```shell
$ kubectl get svc -n ${namespace} # check the available services
```

* Access inside of the Kubernetes cluster

    When your application is deployed in the same Kubernetes cluster, you can access Cell via domain name `demo-cell.proxy.svc` with port `6379`. Here `demo` is the `clusterName` which can be modified in `values.yaml`. And the latter `cell` is the namespace you specified when using `helm install` to deploy Cell cluster.

* Access outside of the Kubernetes cluster

    * Using kubectl portforward

        ```shell
        $ kubectl port-forward -n ${namespace} svc/${clusterName}-proxy 6379:6379 &>/tmp/portforward-proxy.log
        $ redis-cli
        ```


## Scale Cell cluster

Cell Operator supports both horizontal and vertical scaling, but there are some caveats for storage vertical scaling.

* Kubernetes is v1.11 or later, please reference [the official blog](https://kubernetes.io/blog/2018/07/12/resizing-persistent-volumes-using-kubernetes/)
* Backend storage class supports resizing. (Currently only a limited of network storage class supports resizing)

When using local persistent volumes, even CPU and memory vertical scaling can cause problems because there may be not enough resources on the node.

Due to the above reasons, it's recommended to do horizontal scaling other than vertical scaling when workload increases.

### Horizontal scaling

To scale in/out Cell cluster, just modify the `replicas` of PD, Store and Proxy in `values.yaml` file. And then run the following command:

```shell
$ helm upgrade ${releaseName} charts/cell-cluster
```

**Caveat:**	

```
1. PD can't scale after cell cluster initialize.
2. Store can only scale out but scale in.
3. Proxy can scale out or scale in.        
```

### Vertical scaling

To scale up/down Cell cluster, modify the cpu/memory/storage limits and requests of PD, Store and Proxy in `values.yaml` file. And then run the same command as above.

## Upgrade Cell cluster

Upgrade Cell cluster is similar to scale Cell cluster, but by changing `image` of PD, Store and Proxy to different image versions in `values.yaml`. And then run the following command:

```shell
$ helm upgrade ${releaseName} charts/cell-cluster
```

**Caveat:**

​	You can roll upgrade Proxy online. You can upgrade PD/Store when there is no IO through put.

## Destroy Cell cluster

To destroy Cell cluster, run the following command:

```shell
$ helm delete ${releaseName} --purge
```

The above command only delete the running pods, the data is persistent. If you do not need the data anymore, you can delete pvcs and pvs manually.
