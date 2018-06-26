# ci-vm-operator

This controller manages the lifecycle of virtual machines in Google Compute Engine. A request for a new virtual machine can be
made by POSTing a new `VirtualMachine`:

```yaml
apiVersion: ci.openshift.io/v1alpha1
kind: VirtualMachine
metadata:
  name: my-virtual-machine
spec:
  machineType: n1-standard-1
  bootDisk:
    imageFamily: compute/v1/projects/centos-cloud/global/images/family/centos-6
    sizeGb: 25
    type: pd-standard
```

The controller will instantiate the virtual machine in GCE and expose connection details by creating a secret in the namespace of
the `VirtualMachine` object with the same name. A machine-local SSH key pair will be exposed in the `id_rsa` and `id_rsa.pub`
fields, while connection information for use with `ssh` will be exposed in the `ssh_config` field:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-virtual-machine
  ownerReferences:
  - apiVersion: v1alpha1
    kind: virtualmachine.ci.openshift.io
    name: my-virtual-machine
type: Opaque
data:
  id_rsa: LS0tLS1CRUdJTiBSU0EgUFJJVk...
  id_rsa.pub: c3NoLXJzYSBBQUFBQjNOem...
  ssh_config: SG9zdCBza3V6bmV0cy10ZX...
```

Deleting the `VirtualMachine` object will trigger deletion of the virtual machine in GCE. A finalizer is used to ensure that all
resources in GCE are cleaned up before the record of the `VirtualMachine` is removed from `etcd`.

## Deployment

Deployment of these components requires `system:admin` level control, as it includes the creation of cluster-level resources like
the `VirtualMachine` `CustomResourceDefinition`. To deploy, run:

```sh
CI_VM_OPERATOR_GCE_CREDENTIALS_JSON=/path/to/credentials.json make deploy
```

On creation, a mutating admission controller adds the finalizer to each `VirtualMachine` object; on updates a validating admission
controller ensures that the spec is not changed. In order for these to function, the API server must be set up to enable dynamic
admission control through webhooks. In `master-config.yaml`, set:

```yaml
admissionConfig:
  pluginConfig:
    MutatingAdmissionWebhook:
      configuration:
        apiVersion: v1
        kind: DefaultAdmissionConfig
    ValidatingAdmissionWebhook:
      configuration:
        apiVersion: v1
        kind: DefaultAdmissionConfig
```