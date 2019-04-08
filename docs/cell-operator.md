# Cell Operator Setup

## Requirements

Before deploying the Cell Operator, make sure the following requirements are satisfied:

* Kubernetes v1.11 or later
* [DNS addons](https://kubernetes.io/docs/tasks/access-application-cluster/configure-dns-cluster/)
* [PersistentVolume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
* [RBAC](https://kubernetes.io/docs/admin/authorization/rbac) enabled 
* [Helm](https://helm.sh) v2.12.2 or later

## Kubernetes

Cell Operator uses [PersistentVolume](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) to persist Cell cluster data (including the pd's data, store's data), so the Kubernetes must provide at least one kind of persistent volume. To achieve better performance, local SSD disk persistent volume is recommended. You can follow [this step](#local-persistent-volume) to auto provisioning local persistent volumes.

The Kubernetes cluster is suggested to enable [RBAC](https://kubernetes.io/docs/admin/authorization/rbac). Otherwise you may want to set `rbac.create` to `false` in the values.yaml of both cell-operator and cell-cluster charts.

## Helm

You can follow Helm official [documentation](https://helm.sh) to install Helm in your Kubernetes cluster. The following instructions are listed here for quick reference:

1. Install helm client

    ```
    $ curl https://raw.githubusercontent.com/kubernetes/helm/master/scripts/get | bash
    ```

    Or if you use macOS, you can use homebrew to install Helm by `brew install kubernetes-helm`

2. Install helm server

    ```shell
    $ kubectl apply -f https://raw.githubusercontent.com/deepfabric/elasticell-operator/master/manifests/tiller-rbac.yaml
    $ helm init --service-account=tiller --upgrade
    $ kubectl get po -n kube-system -l name=tiller # make sure tiller pod is running
    ```

    If `RBAC` is not enabled for the Kubernetes cluster, then `helm init --upgrade` should be enough.

## Local Persistent Volume

Local disks are recommended to be formatted as ext4 filesystem. The local persistent volume directory must be a mount point:  e.g.  [bind mount](https://unix.stackexchange.com/questions/198590/what-is-a-bind-mount):

### Bind mount

If your bare metal k8s cluster is deployed and privosioned by rancher,  target path of the bind mount  must be the subdirectory of  "/var/lib/rancher/k8s-local-storage", because of rancher's constraints. The disadvantages of bind mount for store: all the volumes has the size of the whole disk and there is no quota and isolation of bind mount volumes. If your data directory is `/mnt/k8s-local-storage`, you can create a bind mount with the following commands:

```shell
$ sudo mkdir -p /mnt/k8s-local-storage/vol1
$ sudo mkdir -p /var/lib/rancher/k8s-local-storage/vol1
$ sudo mount -o bind /mnt/k8s-local-storage/vol1 /var/lib/rancher/k8s-local-storage/vol1
```

Use this command to confirm the mount point exist:

```shell
$ mount | grep /var/lib/rancher/k8s-local-storage/vol1
```

After mounting all data path on Kubernetes nodes, you can deploy [local-volume-provisioner](https://github.com/kubernetes-sigs/sig-storage-local-static-provisioner) to automatically provision the mounted path as Local PersistentVolumes.

```shell
$ kubectl apply -f https://raw.githubusercontent.com/deepfabric/elasticell-operator/master/manifests/local-volume-provisioner.yaml
$ kubectl get po -n kube-system -l app=local-volume-provisioner
$ kubectl get pv | grep local-storage
```

## Install Cell Operator

Cell Operator uses [CRD](https://kubernetes.io/docs/tasks/access-kubernetes-api/custom-resources/custom-resource-definitions/) to extend Kubernetes, so to use Cell Operator, you should first create `CellCluster` custom resource kind. This is a one-time job, namely you can only need to do this once in your Kubernetes cluster.

```shell
$ kubectl apply -f https://raw.githubusercontent.com/deepfabric/elasticell-operator/master/manifests/crd.yaml
$ kubectl get crd cellclusters.deepfabric.com
```

After the `CellCluster` custom resource is created, you can install Cell Operator in your Kubernetes cluster.

Uncomment the `scheduler.kubeSchedulerImage` in `values.yaml`, set it to the same as your kubernetes cluster version.

```shell
$ git clone https://github.com/deepfabric/elasticell-operator.git
$ cd cell-operator
$ helm install charts/cell-operator --name=cell-operator --namespace=cell-admin
$ kubectl get po -n cell-admin -l app.kubernetes.io/name=cell-operator
```

## Custom Cell Operator

Customizing is done by modifying `charts/cell-operator/values.yaml`. The rest of the document will use `values.yaml` to reference `charts/cell-operator/values.yaml`

Cell Operator contains two components:

* cell-controller-manager
* cell-scheduler

This two components are stateless, so they are deployed via `Deployment`. You can customize the `replicas` and resource limits/requests as you wish in the values.yaml.

After editing values.yaml, run the following command to apply the modification:

```shell
$ helm upgrade cell-operator charts/cell-operator
```

## Upgrade Cell Operator

Upgrading Cell Operator itself is similar to customize Cell Operator, modify the image version in values.yaml and then run `helm upgrade`:

```shell
$ helm upgrade cell-operator charts/cell-operator
```

When a new version of cell-operator comes out, simply update the `operatorImage` in values.yaml and run the above command should be enough. 
