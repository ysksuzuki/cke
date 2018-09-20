package cke

import (
	"path/filepath"
	"strings"
)

const (
	kubeAPIServerContainerName         = "kube-apiserver"
	kubeControllerManagerContainerName = "kube-controller-manager"
	kubeProxyContainerName             = "kube-proxy"
	kubeSchedulerContainerName         = "kube-scheduler"
	kubeletContainerName               = "kubelet"
	pauseContainerName                 = "pause"
	riversContainerName                = "rivers"

	rbacRoleName        = "system:kube-apiserver-to-kubelet"
	rbacRoleBindingName = "system:kube-apiserver"
)

var (
	// admissionPlugins is the recommended list of admission plugins.
	// https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#is-there-a-recommended-set-of-admission-controllers-to-use
	admissionPlugins = []string{
		"NamespaceLifecycle",
		"LimitRanger",
		"ServiceAccount",
		"DefaultStorageClass",
		"DefaultTolerationSeconds",
		"MutatingAdmissionWebhook",
		"ValidatingAdmissionWebhook",
		"ResourceQuota",
	}
)

type riversBootOp struct {
	nodes     []*Node
	upstreams []*Node
	params    ServiceParams
	step      int
}

type riversRestartOp struct {
	nodes     []*Node
	upstreams []*Node
	params    ServiceParams

	pulled bool
}

type apiServerBootOp struct {
	cps   []*Node
	nodes []*Node

	serviceSubnet string
	params        ServiceParams

	step      int
	makeFiles *makeFilesCommand
}

type controllerManagerBootOp struct {
	nodes []*Node

	cluster       string
	serviceSubnet string
	params        ServiceParams

	step      int
	makeFiles *makeFilesCommand
}

type schedulerBootOp struct {
	nodes []*Node

	cluster string
	params  ServiceParams

	step      int
	makeFiles *makeFilesCommand
}

type apiServerRestartOp struct {
	cps   []*Node
	nodes []*Node

	serviceSubnet string
	params        ServiceParams

	pulled bool
}

type controllerManagerRestartOp struct {
	nodes []*Node

	cluster       string
	serviceSubnet string
	params        ServiceParams

	pulled bool
}

type schedulerRestartOp struct {
	nodes []*Node

	cluster string
	params  ServiceParams

	pulled bool
}

type containerStopOp struct {
	nodes    []*Node
	name     string
	executed bool
}

type kubeProxyBootOp struct {
	nodes []*Node

	cluster string
	params  ServiceParams

	step      int
	makeFiles *makeFilesCommand
}

type kubeletBootOp struct {
	nodes []*Node

	cluster   string
	podSubnet string
	params    KubeletParams

	step      int
	makeFiles *makeFilesCommand
}

type kubeRBACRoleInstallOp struct {
	apiserver     *Node
	roleExists    bool
	bindingExists bool
}

// RiversBootOp returns an Operator to bootstrap rivers.
func RiversBootOp(nodes, upstreams []*Node, params ServiceParams) Operator {
	return &riversBootOp{
		nodes:     nodes,
		upstreams: upstreams,
		params:    params,
	}
}

func (o *riversBootOp) Name() string {
	return "rivers-bootstrap"
}

func (o *riversBootOp) NextCommand() Commander {
	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.nodes, ToolsImage}
	case 1:
		o.step++
		return makeDirsCommand{o.nodes, []string{"/var/log/rivers"}}
	case 2:
		o.step++
		return runContainerCommand{
			nodes:  o.nodes,
			name:   riversContainerName,
			img:    ToolsImage,
			params: RiversParams(o.upstreams),
			extra:  o.params,
		}
	default:
		return nil
	}
}

// RiversRestartOp returns an Operator to restart rivers.
func RiversRestartOp(nodes, upstreams []*Node, params ServiceParams) Operator {
	return &riversRestartOp{
		nodes:     nodes,
		upstreams: upstreams,
		params:    params,
	}
}

func (o *riversRestartOp) Name() string {
	return "rivers-restart"
}

func (o *riversRestartOp) NextCommand() Commander {
	if !o.pulled {
		o.pulled = true
		return imagePullCommand{o.nodes, ToolsImage}
	}

	return restartContainerCommand{
		nodes:  o.nodes,
		name:   riversContainerName,
		img:    ToolsImage,
		params: RiversParams(o.upstreams),
		extra:  o.params,
	}
}

// RiversParams returns a ServiceParams for rivers
func RiversParams(upstreams []*Node) ServiceParams {
	var ups []string
	for _, n := range upstreams {
		ups = append(ups, n.Address+":6443")
	}
	args := []string{
		"rivers",
		"--upstreams=" + strings.Join(ups, ","),
		"--listen=" + "127.0.0.1:16443",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/var/log/rivers", "/var/log/rivers", false, "", LabelShared},
		},
	}
}

// APIServerBootOp returns an Operator to bootstrap kube-apiserver
func APIServerBootOp(cps, nodes []*Node, serviceSubnet string, params ServiceParams) Operator {
	return &apiServerBootOp{
		cps:           cps,
		nodes:         nodes,
		serviceSubnet: serviceSubnet,
		params:        params,
		makeFiles:     &makeFilesCommand{nodes: nodes},
	}
}

func (o *apiServerBootOp) Name() string {
	return "kube-apiserver-bootstrap"
}

func (o *apiServerBootOp) NextCommand() Commander {
	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.nodes, HyperkubeImage}
	case 1:
		o.step++
		dirs := []string{
			"/var/log/kubernetes/apiserver",
		}
		return makeDirsCommand{o.nodes, dirs}
	case 2:
		o.step++
		return prepareAPIServerFilesCommand{o.makeFiles}
	case 3:
		o.step++
		return o.makeFiles
	case 4:
		o.step++
		opts := []string{
			"--mount", "type=tmpfs,dst=/run/kubernetes",
		}
		paramsMap := make(map[string]ServiceParams)
		for _, n := range o.nodes {
			paramsMap[n.Address] = APIServerParams(o.cps, n.Address, o.serviceSubnet)
		}
		return runContainerCommand{
			nodes:     o.nodes,
			name:      kubeAPIServerContainerName,
			img:       HyperkubeImage,
			opts:      opts,
			paramsMap: paramsMap,
			extra:     o.params,
		}
	default:
		return nil
	}
}

// ControllerManagerBootOp returns an Operator to bootstrap kube-controller-manager
func ControllerManagerBootOp(nodes []*Node, cluster string, serviceSubnet string, params ServiceParams) Operator {
	return &controllerManagerBootOp{
		nodes:         nodes,
		cluster:       cluster,
		serviceSubnet: serviceSubnet,
		params:        params,
		makeFiles:     &makeFilesCommand{nodes: nodes},
	}
}

func (o *controllerManagerBootOp) Name() string {
	return "kube-controller-manager-bootstrap"
}

func (o *controllerManagerBootOp) NextCommand() Commander {
	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.nodes, HyperkubeImage}
	case 1:
		o.step++
		dirs := []string{
			"/var/log/kubernetes/controller-manager",
		}
		return makeDirsCommand{o.nodes, dirs}
	case 2:
		o.step++
		return prepareControllerManagerFilesCommand{o.cluster, o.makeFiles}
	case 3:
		o.step++
		return o.makeFiles
	case 4:
		o.step++
		return runContainerCommand{
			nodes:  o.nodes,
			name:   kubeControllerManagerContainerName,
			img:    HyperkubeImage,
			params: ControllerManagerParams(o.cluster, o.serviceSubnet),
			extra:  o.params,
		}
	default:
		return nil
	}
}

// SchedulerBootOp returns an Operator to bootstrap kube-scheduler
func SchedulerBootOp(nodes []*Node, cluster string, params ServiceParams) Operator {
	return &schedulerBootOp{
		nodes:     nodes,
		cluster:   cluster,
		params:    params,
		makeFiles: &makeFilesCommand{nodes: nodes},
	}
}

func (o *schedulerBootOp) Name() string {
	return "kube-scheduler-bootstrap"
}

func (o *schedulerBootOp) NextCommand() Commander {
	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.nodes, HyperkubeImage}
	case 1:
		o.step++
		dirs := []string{
			"/var/log/kubernetes/scheduler",
		}
		return makeDirsCommand{o.nodes, dirs}
	case 2:
		o.step++
		return prepareSchedulerFilesCommand{o.cluster, o.makeFiles}
	case 3:
		o.step++
		return o.makeFiles
	case 4:
		o.step++
		return runContainerCommand{
			nodes:  o.nodes,
			name:   kubeSchedulerContainerName,
			img:    HyperkubeImage,
			params: SchedulerParams(),
			extra:  o.params,
		}
	default:
		return nil
	}
}

// ContainerStopOp returns an Operator to stop container
func ContainerStopOp(nodes []*Node, name string) Operator {
	return &containerStopOp{
		nodes: nodes,
		name:  name,
	}
}

func (o *containerStopOp) Name() string {
	return "container-stop"
}

func (o *containerStopOp) NextCommand() Commander {
	if o.executed {
		return nil
	}
	o.executed = true
	return stopContainersCommand{o.nodes, o.name}
}

// APIServerRestartOp returns an Operator to restart kube-apiserver
func APIServerRestartOp(cps, nodes []*Node, serviceSubnet string, params ServiceParams) Operator {
	return &apiServerRestartOp{
		cps:           cps,
		nodes:         nodes,
		serviceSubnet: serviceSubnet,
		params:        params,
	}
}

func (o *apiServerRestartOp) Name() string {
	return "kube-apiserver-restart"
}

func (o *apiServerRestartOp) NextCommand() Commander {
	if !o.pulled {
		o.pulled = true
		return imagePullCommand{o.nodes, HyperkubeImage}
	}

	if len(o.nodes) == 0 {
		return nil
	}

	// API server should be restarted one by one.
	node := o.nodes[0]
	o.nodes = o.nodes[1:]
	opts := []string{
		"--mount", "type=tmpfs,dst=/run/kubernetes",
	}
	return runContainerCommand{
		nodes:   []*Node{node},
		name:    kubeAPIServerContainerName,
		img:     HyperkubeImage,
		opts:    opts,
		params:  APIServerParams(o.cps, node.Address, o.serviceSubnet),
		extra:   o.params,
		restart: true,
	}
}

// ControllerManagerRestartOp returns an Operator to restart kube-controller-manager
func ControllerManagerRestartOp(nodes []*Node, cluster, serviceSubnet string, params ServiceParams) Operator {
	return &controllerManagerRestartOp{
		nodes:         nodes,
		cluster:       cluster,
		serviceSubnet: serviceSubnet,
		params:        params,
	}
}

func (o *controllerManagerRestartOp) Name() string {
	return "kube-controller-manager-restart"
}

func (o *controllerManagerRestartOp) NextCommand() Commander {
	if !o.pulled {
		o.pulled = true
		return imagePullCommand{o.nodes, HyperkubeImage}
	}

	return runContainerCommand{
		nodes:   o.nodes,
		name:    kubeControllerManagerContainerName,
		img:     HyperkubeImage,
		params:  ControllerManagerParams(o.cluster, o.serviceSubnet),
		extra:   o.params,
		restart: true,
	}
}

// SchedulerRestartOp returns an Operator to restart kube-scheduler
func SchedulerRestartOp(nodes []*Node, cluster string, params ServiceParams) Operator {
	return &schedulerRestartOp{
		nodes:   nodes,
		cluster: cluster,
		params:  params,
	}
}

func (o *schedulerRestartOp) Name() string {
	return "kube-scheduler-restart"
}

func (o *schedulerRestartOp) NextCommand() Commander {
	if !o.pulled {
		o.pulled = true
		return imagePullCommand{o.nodes, HyperkubeImage}
	}

	return runContainerCommand{
		nodes:   o.nodes,
		name:    kubeSchedulerContainerName,
		img:     HyperkubeImage,
		params:  SchedulerParams(),
		extra:   o.params,
		restart: true,
	}
}

// APIServerParams returns built-in ServiceParams form kube-apiserver
func APIServerParams(controlPlanes []*Node, advertiseAddress, serviceSubnet string) ServiceParams {
	var etcdServers []string
	for _, n := range controlPlanes {
		etcdServers = append(etcdServers, "https://"+n.Address+":2379")
	}

	args := []string{
		"apiserver",
		"--allow-privileged",
		"--etcd-servers=" + strings.Join(etcdServers, ","),
		"--etcd-cafile=" + K8sPKIPath("etcd-ca.crt"),
		"--etcd-certfile=" + K8sPKIPath("apiserver-etcd-client.crt"),
		"--etcd-keyfile=" + K8sPKIPath("apiserver-etcd-client.key"),

		"--bind-address=0.0.0.0",
		"--insecure-port=0",
		"--client-ca-file=" + K8sPKIPath("ca.crt"),
		"--tls-cert-file=" + K8sPKIPath("apiserver.crt"),
		"--tls-private-key-file=" + K8sPKIPath("apiserver.key"),
		"--kubelet-certificate-authority=" + K8sPKIPath("ca.crt"),
		"--kubelet-client-certificate=" + K8sPKIPath("apiserver.crt"),
		"--kubelet-client-key=" + K8sPKIPath("apiserver.key"),
		"--kubelet-https=true",

		"--enable-admission-plugins=" + strings.Join(admissionPlugins, ","),

		// for service accounts
		"--service-account-key-file=" + K8sPKIPath("service-account.crt"),
		"--service-account-lookup",

		"--authorization-mode=Node,RBAC",

		"--advertise-address=" + advertiseAddress,
		"--service-cluster-ip-range=" + serviceSubnet,
		"--audit-log-path=/var/log/kubernetes/apiserver/audit.log",
		"--log-dir=/var/log/kubernetes/apiserver/",
		"--logtostderr=false",
		"--machine-id-file=/etc/machine-id",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/etc/hostname", "/etc/machine-id", true, "", ""},
			{"/var/log/kubernetes/apiserver", "/var/log/kubernetes/apiserver", false, "", LabelPrivate},
			{"/etc/kubernetes", "/etc/kubernetes", true, "", LabelShared},
		},
	}
}

// ControllerManagerParams returns a ServiceParams for kube-controller-manager
func ControllerManagerParams(clusterName, serviceSubnet string) ServiceParams {
	args := []string{
		"controller-manager",
		"--cluster-name=" + clusterName,
		"--service-cluster-ip-range=" + serviceSubnet,
		"--kubeconfig=/etc/kubernetes/controller-manager/kubeconfig",
		"--log-dir=/var/log/kubernetes/controller-manager",
		"--logtostderr=false",

		// ToDo: cluster signing
		// https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster/#a-note-to-cluster-administrators
		// https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping/
		//    Create an intermediate CA under cke/ca-kubernetes?
		//    or just an certficate/key pair?
		// "--cluster-signing-cert-file=..."
		// "--cluster-signing-key-file=..."

		// for service accounts
		"--root-ca-file=" + K8sPKIPath("ca.crt"),
		"--service-account-private-key-file=" + K8sPKIPath("service-account.key"),
		"--use-service-account-credentials=true",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/etc/hostname", "/etc/machine-id", true, "", ""},
			{"/etc/kubernetes", "/etc/kubernetes", true, "", LabelShared},
			{"/var/log/kubernetes/controller-manager", "/var/log/kubernetes/controller-manager", false, "", LabelPrivate},
		},
	}
}

// SchedulerParams return a ServiceParams form kube-scheduler
func SchedulerParams() ServiceParams {
	args := []string{
		"scheduler",
		"--kubeconfig=/etc/kubernetes/scheduler/kubeconfig",
		"--log-dir=/var/log/kubernetes/scheduler",
		"--logtostderr=false",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/etc/hostname", "/etc/machine-id", true, "", ""},
			{"/etc/kubernetes", "/etc/kubernetes", true, "", LabelShared},
			{"/var/log/kubernetes/scheduler", "/var/log/kubernetes/scheduler", false, "", LabelPrivate},
		},
	}
}

// KubeProxyBootOp returns an Operator to boot kube-proxy.
func KubeProxyBootOp(nodes []*Node, cluster string, params ServiceParams) Operator {
	return &kubeProxyBootOp{
		nodes:     nodes,
		cluster:   cluster,
		params:    params,
		makeFiles: &makeFilesCommand{nodes: nodes},
	}
}

func (o *kubeProxyBootOp) Name() string {
	return "kube-proxy-bootstrap"
}

func (o *kubeProxyBootOp) NextCommand() Commander {
	switch o.step {
	case 0:
		return imagePullCommand{o.nodes, HyperkubeImage}
	case 1:
		dirs := []string{
			"/var/log/kubernetes/proxy",
		}
		return makeDirsCommand{o.nodes, dirs}
	case 2:
		o.step++
		return prepareProxyFilesCommand{o.cluster, o.makeFiles}
	case 3:
		o.step++
		return o.makeFiles
	case 4:
		opts := []string{
			"--tmpfs=/run",
			"--privileged",
		}
		return runContainerCommand{
			nodes:  o.nodes,
			name:   kubeProxyContainerName,
			img:    HyperkubeImage,
			opts:   opts,
			params: ProxyParams(),
			extra:  o.params,
		}
	default:
		return nil
	}
}

func (o *kubeWorkerBootOp) NextCommand() Commander {
	var opts []string

	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.kubelets, ToolsImage}
	case 1:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		dirs := []string{
			cniBinDir,
			cniConfDir,
			cniVarDir,
			"/var/log/kubernetes/kubelet",
			"/var/log/pods",
			"/var/log/containers",
			"/opt/volume/bin",
			"/var/log/kubernetes/proxy",
		}
		return makeDirsCommand{o.kubelets, dirs}
	case 2:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return makeFilesCommand{o.kubelets, cniBridgeConfig(o.podSubnet),
			filepath.Join(cniConfDir, "98-bridge.conf")}
	case 3:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return runContainerCommand{nodes: o.kubelets, name: "install-cni", img: ToolsImage, opts: opts,
			params: ServiceParams{
				ExtraArguments: []string{"/usr/local/cke-tools/bin/install-cni"},
				ExtraBinds: []Mount{
					{Source: cniBinDir, Destination: "/host/bin", ReadOnly: false},
					{Source: cniConfDir, Destination: "/host/net.d", ReadOnly: false},
				},
			},
		}
	case 4:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		return imagePullCommand{o.proxies, HyperkubeImage}
	case 5:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return makeKubeletKubeconfigCommand{o.kubelets, o.cluster, o.options.Kubelet}
	case 6:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		return makeProxyKubeconfigCommand{o.proxies, o.cluster}
	case 7:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return volumeCreateCommand{o.kubelets, "dockershim"}
	case 8:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		opts = []string{
			"--pid=host",
			"--mount=type=volume,src=dockershim,dst=/var/lib/dockershim",
			"--privileged",
		}
		params := make(map[string]ServiceParams)
		for _, n := range o.kubelets {
			params[n.Address] = KubeletServiceParams(n)
		}
		return runContainerParamsCommand{o.kubelets, kubeletContainerName, HyperkubeImage,
			opts, params, o.options.Kubelet.ServiceParams}
	case 9:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		opts = []string{
			"--tmpfs=/run",
			"--privileged",
		}
		return runContainerCommand{o.proxies, kubeProxyContainerName, HyperkubeImage,
			opts, ProxyParams(), o.options.Proxy}
	default:
		return nil
	}
}

// KubeWorkerRestartOp returns an Operator to restart kubernetes workers
func KubeWorkerRestartOp(cps, rivers, kubelets, proxies []*Node, cluster string, options Options) Operator {
	return &kubeWorkerRestartOp{
		cps:      cps,
		cluster:  cluster,
		rivers:   rivers,
		kubelets: kubelets,
		proxies:  proxies,
		options:  options,
	}
}

func (o *kubeWorkerRestartOp) Name() string {
	return "worker-restart"
}

func (o *kubeWorkerRestartOp) NextCommand() Commander {
	var opts []string

	switch o.step {
	case 0:
		o.step++
		if len(o.rivers) == 0 {
			return o.NextCommand()
		}
		return imagePullCommand{o.rivers, ToolsImage}
	case 1:
		o.step++
		if len(o.proxies)+len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return imagePullCommand{o.proxies, HyperkubeImage}
	case 2:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return makeKubeletKubeconfigCommand{o.kubelets, o.cluster, o.options.Kubelet}
	case 3:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		return makeProxyKubeconfigCommand{o.proxies, o.cluster}
	case 4:
		o.step++
		if len(o.rivers) == 0 {
			return o.NextCommand()
		}
		return killContainersCommand{o.rivers, riversContainerName}
	case 5:
		o.step++
		if len(o.rivers) == 0 {
			return o.NextCommand()
		}
		return runContainerCommand{o.rivers, riversContainerName, ToolsImage,
			opts, RiversParams(o.cps), o.options.Rivers}
	case 6:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		return stopContainersCommand{o.kubelets, kubeletContainerName}
	case 7:
		o.step++
		if len(o.kubelets) == 0 {
			return o.NextCommand()
		}
		opts = []string{
			"--pid=host",
			"--mount=type=volume,src=dockershim,dst=/var/lib/dockershim",
			"--privileged",
		}
		params := make(map[string]ServiceParams)
		for _, n := range o.kubelets {
			params[n.Address] = KubeletServiceParams(n)
		}
		return runContainerParamsCommand{o.kubelets, kubeletContainerName, HyperkubeImage,
			opts, params, o.options.Kubelet.ServiceParams}
	case 8:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		return stopContainersCommand{o.proxies, kubeProxyContainerName}
	case 9:
		o.step++
		if len(o.proxies) == 0 {
			return o.NextCommand()
		}
		opts = []string{
			"--tmpfs=/run",
			"--privileged",
		}
		return runContainerCommand{o.proxies, kubeProxyContainerName, HyperkubeImage,
			opts, ProxyParams(), o.options.Proxy}
	default:
		return nil
	}
}

// KubeRBACRoleInstallOp returns an Operator to install ClusterRole and binding for RBAC.
func KubeRBACRoleInstallOp(apiserver *Node, roleExists bool) Operator {
	return &kubeRBACRoleInstallOp{
		apiserver:  apiserver,
		roleExists: roleExists,
	}
}

func (o *kubeRBACRoleInstallOp) Name() string {
	return "install-rbac-role"
}

func (o *kubeRBACRoleInstallOp) NextCommand() Commander {
	switch {
	case !o.roleExists:
		o.roleExists = true
		return makeRBACRoleCommand{o.apiserver}
	case !o.bindingExists:
		o.bindingExists = true
		return makeRBACRoleBindingCommand{o.apiserver}
	}
	return nil
}

// ProxyParams returns a ServiceParams form kube-proxy
func ProxyParams() ServiceParams {
	args := []string{
		"proxy",
		"--proxy-mode=ipvs",
		"--kubeconfig=/etc/kubernetes/proxy/kubeconfig",
		"--log-dir=/var/log/kubernetes/proxy",
		"--logtostderr=false",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/etc/hostname", "/etc/machine-id", true, "", ""},
			{"/etc/kubernetes", "/etc/kubernetes", true, "", LabelShared},
			{"/lib/modules", "/lib/modules", true, "", ""},
			{"/var/log/kubernetes/proxy", "/var/log/kubernetes/proxy", false, "", LabelPrivate},
		},
	}
}

// KubeletServiceParams returns a ServiceParams for kubelet
func KubeletServiceParams(n *Node) ServiceParams {
	args := []string{
		"kubelet",
		"--config=/etc/kubernetes/kubelet/config.yml",
		"--kubeconfig=/etc/kubernetes/kubelet/kubeconfig",
		"--allow-privileged=true",
		"--hostname-override=" + n.Nodename(),
		"--pod-infra-container-image=" + PauseImage.Name(),
		"--log-dir=/var/log/kubernetes/kubelet",
		"--logtostderr=false",
		"--network-plugin=cni",
		"--volume-plugin-dir=/opt/volume/bin",
	}
	return ServiceParams{
		ExtraArguments: args,
		ExtraBinds: []Mount{
			{"/etc/hostname", "/etc/machine-id", true, "", ""},
			{"/etc/kubernetes", "/etc/kubernetes", true, "", LabelShared},
			{"/var/lib/kubelet", "/var/lib/kubelet", false, PropagationShared, LabelShared},
			// TODO: /var/lib/docker is used by cAdvisor.
			// cAdvisor will be removed from kubelet. Then remove this bind mount.
			{"/var/lib/docker", "/var/lib/docker", false, "", LabelPrivate},
			{"/opt/volume/bin", "/opt/volume/bin", false, PropagationShared, LabelShared},
			{"/var/log/pods", "/var/log/pods", false, "", LabelShared},
			{"/var/log/containers", "/var/log/containers", false, "", LabelShared},
			{"/var/log/kubernetes/kubelet", "/var/log/kubernetes/kubelet", false, "", LabelPrivate},
			{"/run", "/run", false, "", ""},
			{"/sys", "/sys", true, "", ""},
			{"/dev", "/dev", false, "", ""},
			{cniBinDir, cniBinDir, true, "", LabelShared},
			{cniConfDir, cniConfDir, true, "", LabelShared},
			{cniVarDir, cniVarDir, false, "", LabelShared},
		},
	}
}
