# Cell Operator


Cell Operator manages [elasticell](https://github.com/deepfabric/elasticell) clusters on [Kubernetes](https://kubernetes.io) and automates tasks which is corresponding to operate a Cell cluster,  such as deploy,  provision,  scale,  upgrade. It makes elasticell a truly cloud-native distributed data store.

> **Caveat:** Currently, Cell-Operator must be  used under some constraints, we will fix it in the future:
>
> 1. PD's replica nums can't be changed after cluster deploy.
>
> 2. Only scale-out is supproted by store,  but scale-in.
>
> 3. Rolling upgrade proxy is safely and will not interupt online service. However,  the upgrade of store and pd may cause some read/write out of time because of raft learship change.
>
> 4. Only one cell-cluster can be deployed into a k8s namespace.
>
>    

## Features

- __Atuomtic deployment wiht Kubernetes package manager Helm__

​      helm install cell-operator --name=cell-operator --namespace=cell-admin

​      helm install ./cell-cluster -n cell --namespace=cell

​      kubectl -n cell get all:

![image-20190408140814719](/Users/load/code/go/src/github.com/deepfabric/elasticell-operator/cell-cluster-in-k8s.png)

- __Safely scaling the Cell cluster__

    Cell Operator empowers elasticell with horizontal scalability on the cloud.

    Now cell's scalabily is under some constraints: 

        1. PD can't scale after cell cluster initialize.
        2. Store can only scale out but scale in.
        3. Proxy can scale out or scale in.        

- __Rolling upgrade the Cell cluster__

    You can roll upgrade Pd/Proxy online.
    You can upgrade Store when there is no IO through put.

- __Kubernetes package manager support__

    By embracing Kubernetes package manager [Helm](https://helm.sh), users can easily deploy Elasticell clusters with only one command.



If you are already familiar with [Kubernetes](https://kubernetes.io), 
the following docs can be helpful for managing Cell clusters with Cell Operator

- [Cell Operator Setup](.docs/cell-operator.md)
- [Cell Cluster Operation Guide](.docs/cell-cluster.md)

## Acknowledgments

- Thanks [tidb-operator](https://github.com/pingcap/tidb-operator) for providing various member manager method.
