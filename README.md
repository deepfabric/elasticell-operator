# Cell Operator


Cell Operator manages [elasticell](https://github.com/deepfabric/elasticell) clusters on [Kubernetes](https://kubernetes.io) and automates tasks related to operating a Cell cluster. It makes Cell a truly cloud-native kv-store.

> **Warning:** Currently, Cell Operator is work in progress [WIP] and is NOT ready for production. Use at your own risk.

## Features

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

## Acknowledgments

- Thanks [tidb-operator](https://github.com/pingcap/tidb-operator) for providing various member manager method.
