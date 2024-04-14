# application-aware-quota
Empower users to customize and manage resource quotas per component in Kubernetes, utilizing the scheduling gates feature.


### Deploy it on your cluster

Deploying the AAQ controller is straightforward.

  ```
  $ export VERSION=$(curl -s https://api.github.com/repos/kubevirt/application-aware-quota/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  $ kubectl create -f https://github.com/kubevirt/application-aware-quota/releases/download/$VERSION/aaq-operator.yaml
  $ kubectl create -f https://github.com/kubevirt/application-aware-quota/releases/download/$VERSION/aaq-cr.yaml
  ```


### Deploy it with our CI system

AAQ includes a self-contained development and test environment.  We use Docker to build, and we provide a simple way to get a test cluster up and running on your laptop. The development tools include a version of kubectl that you can use to communicate with the cluster. A wrapper script to communicate with the cluster can be invoked using ./cluster-up/kubectl.sh.

```bash
$ mkdir $GOPATH/src/kubevirt.io && cd $GOPATH/src/kubevirt.io
$ git clone https://github.com/kubevirt/application-aware-quota && cd application-aware-quota
$ make cluster-up
$ make cluster-sync
$ ./cluster-up/kubectl.sh .....
```
For development on external cluster (not provisioned by our AAQ),
check out the [external provider](cluster-sync/external/README.md).

