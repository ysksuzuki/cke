Cluster configuration
=====================

CKE deploys and maintains a Kubernetes cluster and an etcd cluster solely for
the Kubernetes cluster.  The configurations of the clusters can be defined by
a YAML or JSON object with these fields:

- [Node](#node)
- [Taint](#taint)
- [Reboot](#reboot)
- [Options](#options)
  - [ServiceParams](#serviceparams)
  - [Mount](#mount)
  - [EtcdParams](#etcdparams)
  - [APIServerParams](#apiserverparams)
  - [ProxyParams](#proxyparams)
  - [KubeletParams](#kubeletparams)
  - [SchedulerParams](#schedulerparams)

| Name                  | Required | Type      | Description                                                      |
| --------------------- | -------- | --------- | ---------------------------------------------------------------- |
| `name`                | true     | string    | The k8s cluster name.                                            |
| `nodes`               | true     | array     | `Node` list.                                                     |
| `taint_control_plane` | false    | bool      | If true, taint contorl plane nodes.                              |
| `service_subnet`      | true     | string    | CIDR subnet for k8s `Service`.                                   |
| `dns_servers`         | false    | array     | List of upstream DNS server IP addresses.                        |
| `dns_service`         | false    | string    | Upstream DNS service name with namespace as `namespace/service`. |
| `reboot`              | false    | `Reboot`  | See [Reboot](#reboot).                                           |
| `options`             | false    | `Options` | See [Options](#options).                                         |

* Upstream DNS servers can be specified one of the following ways:
    * List server IP addresses in `dns_servers`.
    * Specify Kubernetes `Service` name in `dns_service` (e.g. `"kube-system/dns"`).  
      The service type must be `ClusterIP`.

Node
----

A `Node` has these fields:

| Name            | Required | Type      | Description                                                    |
| --------------- | -------- | --------- | -------------------------------------------------------------- |
| `address`       | true     | string    | IP address of the node.                                        |
| `hostname`      | false    | string    | Override the real hostname of the node in k8s.                 |
| `user`          | true     | string    | SSH user name.                                                 |
| `control_plane` | false    | bool      | If true, the node will be used for k8s control plane and etcd. |
| `annotations`   | false    | object    | Node annotations.                                              |
| `labels`        | false    | object    | Node labels.                                                   |
| `taints`        | false    | `[]Taint` | Node taints.                                                   |

`annotations`, `labels`, and `taints` are added or updated, but not removed.
This is because other applications may edit their own annotations, labels, or taints.

Note that annotations, labels, and taints whose names contain `cke.cybozu.com/` or start with `node-role.kubernetes.io/` are reserved for CKE internal usage, therefore should not be used.

Taint
-----

| Name     | Required | Type   | Description                                      |
| -------- | -------- | ------ | ------------------------------------------------ |
| `key`    | true     | string | The taint key to be applied to a node.           |
| `value`  | false    | string | The taint value corresponding to the taint key.  |
| `effect` | true     | string | `NoSchedule`, `PreferNoSchedule` or `NoExecute`. |

Reboot
------

| Name                       | Required | Type                             | Description                                                            |
| -------------------------- | -------- | -------------------------------- | ---------------------------------------------------------------------- |
| `command`                  | true     | array                            | A command to reboot and wait for a node to get back.  List of strings. |
| `eviction_timeout_seconds` | false    | *int                             | Deadline for eviction. Must be positive. Default is nil.               |
| `command_timeout_seconds`  | false    | *int                             | Deadline for rebooting. Zero means infinity. Default is nil.           |
| `protected_namespaces`     | false    | [`LabelSelector`][LabelSelector] | A label selector to protect namespaces.                                |

`command` is the command (1) to reboot the node and (2) to wait for the boot-up of the node.
CKE sends a [Node data object](cluster.md#node) serialized into JSON to its standard input.
The command should return zero if reboot has completed.
If reboot failed or timeout exceeded, the command should return non-zero.

If `eviction_timeout_seconds` is nil, 10 minutes is used as the default.

If `command_timeout_seconds` is nil or zero, no deadline is set.

CKE tries to delete Pods in the `protected_namespaces` gracefully with the Kubernetes eviction API.
If any of the Pods cannot be deleted, it aborts the operation.

The Pods in the non-protected namespaces are also tried to be deleted gracefully with the Kubernetes eviction API, but they would be simply deleted if eviction is denied.

If `protected_namespaces` is not given, all namespaces are protected.

Options
-------

`Option` is a set of optional parameters for k8s components.

| Name                      | Required | Type              | Description                             |
| ------------------------- | -------- | ----------------- | --------------------------------------- |
| `etcd`                    | false    | `EtcdParams`      | Extra arguments for etcd.               |
| `etcd-rivers`             | false    | `ServiceParams`   | Extra arguments for EtcdRivers.         |
| `rivers`                  | false    | `ServiceParams`   | Extra arguments for Rivers.             |
| `kube-api`                | false    | `APIServerParams` | Extra arguments for API server.         |
| `kube-controller-manager` | false    | `ServiceParams`   | Extra arguments for controller manager. |
| `kube-scheduler`          | false    | `SchedulerParams` | Extra arguments for scheduler.          |
| `kube-proxy`              | false    | `ProxyParams`     | Extra arguments for kube-proxy.         |
| `kubelet`                 | false    | `KubeletParams`   | Extra arguments for kubelet.            |

### ServiceParams

| Name          | Required | Type   | Description                                     |
| ------------- | -------- | ------ | ----------------------------------------------- |
| `extra_args`  | false    | array  | Extra command-line arguments.  List of strings. |
| `extra_binds` | false    | array  | Extra bind mounts.  List of `Mount`.            |
| `extra_env`   | false    | object | Extra environment variables.                    |

### Mount

| Name            | Required | Type   | Description                                       |
| --------------- | -------- | ------ | ------------------------------------------------- |
| `source`        | true     | string | Path in a host to a directory or a file.          |
| `destination`   | true     | string | Path in the container filesystem.                 |
| `read_only`     | false    | bool   | True to mount the directory or file as read-only. |
| `propagation`   | false    | string | Whether mounts can be propagated to replicas.     |
| `selinux_label` | false    | string | Relabel the SELinux label of the host directory.  |

`selinux-label`:

- "z":  The mount content is shared among multiple containers.
- "Z":  The mount content is private and unshared.
- This label should not be specified to system directories.

### EtcdParams

| Name          | Required | Type   | Description                                       |
| ------------- | -------- | ------ | ------------------------------------------------- |
| `volume_name` | false    | string | Docker volume name for data. Default: `etcd-cke`. |
| `extra_args`  | false    | array  | Extra command-line arguments.  List of strings.   |
| `extra_binds` | false    | array  | Extra bind mounts.  List of `Mount`.              |
| `extra_env`   | false    | object | Extra environment variables.                      |

### APIServerParams

| Name                | Required | Type   | Description                                              |
| ------------------- | -------- | ------ | -------------------------------------------------------- |
| `audit_log_enabled` | false    | bool   | If true, audit log will be logged to the specified path. |
| `audit_log_policy`  | false    | string | Audit policy configuration in yaml format.               |
| `audit_log_path`    | false    | string | Audit log output path. Default is standard output.       |
| `extra_args`        | false    | array  | Extra command-line arguments.  List of strings.          |
| `extra_binds`       | false    | array  | Extra bind mounts.  List of `Mount`.                     |
| `extra_env`         | false    | object | Extra environment variables.                             |

### ProxyParams

| Name          | Required | Type                               | Description                                     |
| ------------- | -------- | ---------------------------------- | ----------------------------------------------- |
| `config`      | false    | `*v1alpha1.KubeProxyConfiguration` | See below.                                      |
| `disable`     | false    | bool                               | If true, CKE will skip to install kube-proxy.   |
| `extra_args`  | false    | array                              | Extra command-line arguments.  List of strings. |
| `extra_binds` | false    | array                              | Extra bind mounts.  List of `Mount`.            |
| `extra_env`   | false    | object                             | Extra environment variables.                    |

`config` must be a partial [`v1alpha1.KubeProxyConfiguration`](https://pkg.go.dev/k8s.io/kube-proxy@v0.20.6/config/v1alpha1#KubeProxyConfiguration).
Fields in the below table have default values:

| Name                                                    | Value                                          |
| ------------------------------------------------------- | ---------------------------------------------- |
| `HostnameOverride`                                      | Host name or address if the host name is empty |
| `MetricsBindAddress`                                    | `0.0.0.0`                                      |
| `KubeProxyConntrackConfiguration.TCPEstablishedTimeout` | `24h`                                          |
| `KubeProxyConntrackConfiguration.TCPCloseWaitTimeout`   | `1h`                                           |

`ClientConnection.Kubeconfig` is managed by CKE and are not configurable.
Changing `KubeProxyConfiguration.Mode` requires full node restarts.

### KubeletParams

| Name            | Required | Type                            | Description                                                             |
| --------------- | -------- | ------------------------------- | ----------------------------------------------------------------------- |
| `boot_taints`   | false    | `[]Taint`                       | Bootstrap node taints.                                                  |
| `cni_conf_file` | false    | `CNIConfFile`                   | CNI configuration file.                                                 |
| `config`        | false    | `*v1beta1.KubeletConfiguration` | See below.                                                              |
| `cri_endpoint`  | false    | string                          | Path of the runtime socket. Default: `/run/containerd/containerd.sock`. |
| `extra_args`    | false    | array                           | Extra command-line arguments.  List of strings.                         |
| `extra_binds`   | false    | array                           | Extra bind mounts.  List of `Mount`.                                    |
| `extra_env`     | false    | object                          | Extra environment variables.                                            |

#### Boot taints

Taints in `boot_taints` are added to a Node in the following cases:

- when that Node is registered with Kubernetes by `kubelet`, or
- when `kubelet` on that Node is being booted while the Node resource is already registered.

Those taints can be removed manually when they are no longer needed.
Note that the second case happens when the physical node is rebooted without resource manipulation.
If you want to add taints only at Node registration, use kubelet's `--register-with-taints` options in `extra_args`.

#### KubeletConfiguration

`config` must be a partial [`v1beta1.KubeletConfiguration`](https://pkg.go.dev/k8s.io/kubelet@v0.20.6/config/v1beta1#KubeletConfiguration).

Fields that are described as _This field should not be updated without a full node reboot._ won't be updated on the running node for safety.  Such fields include `CgroupDriver` or `QOSReserved`.

Fields in the below table have default values:

| Name                    | Value             |
| ----------------------- | ----------------- |
| `ClusterDomain`         | `cluster.local`   |
| `RuntimeRequestTimeout` | `15m`             |
| `HealthzBindAddress`    | `0.0.0.0`         |
| `VolumePluginDir`       | `/opt/volume/bin` |

`TLSCertFile`, `TLSPrivateKeyFile`, `Authentication`, `Authorization`, and `ClusterDNS` are managed by CKE and are not configurable.

#### CNIConfFile

CNI configuration file specified by `cni_conf_file` will be put in `/etc/cni/net.d` directory
on all nodes.  The file is created only when `kubelet` starts on the node; it will *not* be
updated later on.

| Name      | Required | Type   | Description                 |
| --------- | -------- | ------ | --------------------------- |
| `name`    | true     | string | file name                   |
| `content` | true     | string | file content in JSON format |

`name` is the filename of CNI configuration file.
It should end with either `.conf` or `.conflist`.

### SchedulerParams

| Name          | Required | Type                                  | Description                                     |
| ------------- | -------- | ------------------------------------- | ----------------------------------------------- |
| `config`      | false    | `*v1beta1.KubeSchedulerConfiguration` | See below.                                      |
| `extra_args`  | false    | array                                 | Extra command-line arguments.  List of strings. |
| `extra_binds` | false    | array                                 | Extra bind mounts.  List of `Mount`.            |
| `extra_env`   | false    | object                                | Extra environment variables.                    |

`config` must be a partial [`v1beta1.KubeSchedulerConfiguration`](https://pkg.go.dev/k8s.io/kube-scheduler@v0.20.6/config/v1beta1#KubeSchedulerConfiguration).

Fields in `config` may have default values.  Some fields are overwritten by CKE.
Please see the source code for more details.

[CRI]: https://github.com/kubernetes/kubernetes/blob/242a97307b34076d5d8f5bbeb154fa4d97c9ef1d/docs/devel/container-runtime-interface.md
[log rotation for CRI runtime]: https://github.com/kubernetes/kubernetes/issues/58823
[LabelSelector]: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
