package cke

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/etcd/etcdserver/api/etcdhttp"
	"github.com/coreos/etcd/etcdserver/etcdserverpb"
)

const (
	defaultEtcdVolumeName = "etcd-cke"
	etcdContainerName     = "etcd"
)

func etcdVolumeName(e EtcdParams) string {
	if len(e.VolumeName) == 0 {
		return defaultEtcdVolumeName
	}
	return e.VolumeName
}

func addressInURLs(address string, urls []string) (bool, error) {
	for _, urlStr := range urls {
		u, err := url.Parse(urlStr)
		if err != nil {
			return false, err
		}
		h, _, err := net.SplitHostPort(u.Host)
		if err != nil {
			return false, err
		}
		if h == address {
			return true, nil
		}
	}
	return false, nil
}

func etcdGuessMemberName(m *etcdserverpb.Member) (string, error) {
	if len(m.Name) > 0 {
		return m.Name, nil
	}

	if len(m.PeerURLs) == 0 {
		return "", errors.New("empty PeerURLs")
	}

	u, err := url.Parse(m.PeerURLs[0])
	if err != nil {
		return "", err
	}
	h, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		return "", err
	}
	return h, nil
}

type etcdBootOp struct {
	endpoints []string
	nodes     []*Node
	params    EtcdParams
	step      int
	cpIndex   int
}

// EtcdBootOp returns an Operator to bootstrap etcd cluster.
func EtcdBootOp(endpoints []string, nodes []*Node, params EtcdParams) Operator {
	return &etcdBootOp{
		endpoints: endpoints,
		nodes:     nodes,
		params:    params,
		step:      0,
		cpIndex:   0,
	}
}

func (o *etcdBootOp) Name() string {
	return "etcd-bootstrap"
}

func (o *etcdBootOp) NextCommand() Commander {
	volname := etcdVolumeName(o.params)
	extra := o.params.ServiceParams

	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{o.nodes, "etcd"}
	case 1:
		o.step++
		return issueEtcdCertificatesCommand{o.nodes}
	case 2:
		o.step++
		return volumeCreateCommand{o.nodes, volname}
	case 3:
		node := o.nodes[o.cpIndex]

		o.cpIndex++
		if o.cpIndex == len(o.nodes) {
			o.step++
		}
		opts := []string{
			"--mount",
			"type=volume,src=" + volname + ",dst=/var/lib/etcd",
			"--volume=/etc/etcd/pki:/etc/etcd/pki:ro",
		}
		var initialCluster []string
		for _, n := range o.nodes {
			initialCluster = append(initialCluster, n.Address+"=https://"+n.Address+":2380")
		}
		return runContainerCommand{[]*Node{node}, etcdContainerName, opts, etcdBuiltInParams(node, initialCluster, "new"), extra}
	case 4:
		o.step++
		return waitEtcdSyncCommand{o.endpoints, 0}
	default:
		return nil
	}
}

func etcdBuiltInParams(node *Node, initialCluster []string, state string) ServiceParams {
	// NOTE: "--initial-*" flags and its value must be joined with '=' to
	// compare paramter on detecting outdated parameters to restart it.
	args := []string{
		"--name=" + node.Address,
		"--listen-peer-urls=https://0.0.0.0:2380",
		"--listen-client-urls=https://0.0.0.0:2379",
		"--initial-advertise-peer-urls=https://" + node.Address + ":2380",
		"--advertise-client-urls=https://" + node.Address + ":2379",
		"--cert-file=/etc/etcd/pki/server.crt",
		"--key-file=/etc/etcd/pki/server.key",
		"--client-cert-auth=true",
		"--trusted-ca-file=/etc/etcd/pki/ca-client.crt",
		"--peer-cert-file=/etc/etcd/pki/peer.crt",
		"--peer-key-file=/etc/etcd/pki/peer.key",
		"--peer-client-cert-auth=true",
		"--peer-trusted-ca-file=/etc/etcd/pki/ca-peer.crt",
		"--initial-cluster=" + strings.Join(initialCluster, ","),
		"--initial-cluster-token=cke",
		"--initial-cluster-state=" + state,
		"--enable-v2=false",
		"--enable-pprof=true",
		"--auto-compaction-mode=periodic",
		"--auto-compaction-retention=24",
	}
	params := ServiceParams{
		ExtraArguments: args,
	}

	return params
}

// EtcdAddMemberOp returns an Operator to add member to etcd cluster.
func EtcdAddMemberOp(endpoints []string, targetNodes []*Node, params EtcdParams) Operator {
	return &etcdAddMemberOp{
		endpoints:   endpoints,
		targetNodes: targetNodes,
		params:      params,
		step:        0,
		nodeIndex:   0,
	}
}

type etcdAddMemberOp struct {
	endpoints   []string
	targetNodes []*Node
	params      EtcdParams
	step        int
	nodeIndex   int
}

func (o *etcdAddMemberOp) Name() string {
	return "etcd-add-member"
}

func (o *etcdAddMemberOp) NextCommand() Commander {
	volname := etcdVolumeName(o.params)
	extra := o.params.ServiceParams

	if o.nodeIndex >= len(o.targetNodes) {
		return nil
	}

	node := o.targetNodes[o.nodeIndex]

	switch o.step {
	case 0:
		o.step++
		return imagePullCommand{[]*Node{node}, "etcd"}
	case 1:
		o.step++
		return stopContainerCommand{node, etcdContainerName}
	case 2:
		o.step++
		return volumeRemoveCommand{[]*Node{node}, volname}
	case 3:
		o.step++
		return volumeCreateCommand{[]*Node{node}, volname}
	case 4:
		o.step++
		return issueEtcdCertificatesCommand{[]*Node{node}}
	case 5:
		o.step++
		opts := []string{
			"--mount",
			"type=volume,src=" + volname + ",dst=/var/lib/etcd",
			"--volume=/etc/etcd/pki:/etc/etcd/pki:ro",
		}
		return addEtcdMemberCommand{o.endpoints, node, opts, extra}
	case 6:
		o.step = 0
		o.nodeIndex++
		endpoints := []string{"https://" + node.Address + ":2379"}
		return waitEtcdSyncCommand{endpoints, 0}
	}
	return nil
}

type addEtcdMemberCommand struct {
	endpoints []string
	node      *Node
	opts      []string
	extra     ServiceParams
}

func (c addEtcdMemberCommand) Run(ctx context.Context, inf Infrastructure) error {
	cli, err := inf.NewEtcdClient(c.endpoints)
	if err != nil {
		return err
	}
	defer cli.Close()

	resp, err := cli.MemberList(ctx)
	if err != nil {
		return err
	}
	members := resp.Members

	inMember := false
	for _, m := range members {
		inMember, err = addressInURLs(c.node.Address, m.PeerURLs)
		if err != nil {
			return err
		}
		if inMember {
			break
		}
	}

	if !inMember {
		resp, err := cli.MemberAdd(ctx, []string{fmt.Sprintf("https://%s:2380", c.node.Address)})
		if err != nil {
			return err
		}
		members = resp.Members
	}
	// gofail: var etcdAfterMemberAdd struct{}
	ce := Docker(inf.Agent(c.node.Address))
	ss, err := ce.Inspect(etcdContainerName)
	if err != nil {
		return err
	}
	if ss.Running {
		return nil
	}

	var initialCluster []string
	for _, m := range members {
		for _, u := range m.PeerURLs {
			if len(m.Name) == 0 {
				initialCluster = append(initialCluster, c.node.Address+"="+u)
			} else {
				initialCluster = append(initialCluster, m.Name+"="+u)
			}
		}
	}

	return ce.RunSystem(etcdContainerName, c.opts, etcdBuiltInParams(c.node, initialCluster, "existing"), c.extra)
}

func (c addEtcdMemberCommand) Command() Command {
	return Command{
		Name:   "add-etcd-member",
		Target: c.node.Address,
	}
}

type waitEtcdSyncCommand struct {
	endpoints  []string
	redundancy int
}

func (c waitEtcdSyncCommand) Run(ctx context.Context, inf Infrastructure) error {
	for i := 0; i < 3; i++ {
		count := 0
		for _, ep := range c.endpoints {
			u := ep + "/health"
			req, err := http.NewRequest("GET", u, nil)
			if err != nil {
				continue
			}
			req = req.WithContext(ctx)
			resp, err := inf.HTTPClient().Do(req)
			if err != nil {
				continue
			}
			health := new(etcdhttp.Health)
			err = json.NewDecoder(resp.Body).Decode(health)
			resp.Body.Close()
			if err != nil || health.Health != "true" {
				continue
			}
			count++
		}
		if count >= int(len(c.endpoints)/2)+1+c.redundancy {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return errors.New("etcd sync timeout")
}

func (c waitEtcdSyncCommand) Command() Command {
	return Command{
		Name:   "wait-etcd-sync",
		Target: strings.Join(c.endpoints, ","),
	}
}

type removeEtcdMemberCommand struct {
	endpoints []string
	ids       []uint64
}

func (c removeEtcdMemberCommand) Run(ctx context.Context, inf Infrastructure) error {
	cli, err := inf.NewEtcdClient(c.endpoints)
	if err != nil {
		return err
	}
	defer cli.Close()

	for _, id := range c.ids {
		_, err := cli.MemberRemove(ctx, id)
		if err != nil {
			return err
		}
	}
	// gofail: var etcdAfterMemberRemove struct{}
	return nil
}

func (c removeEtcdMemberCommand) Command() Command {
	idStrs := make([]string, len(c.ids))
	for i, id := range c.ids {
		idStrs[i] = strconv.FormatUint(id, 10)
	}
	return Command{
		Name:   "remove-etcd-member",
		Target: strings.Join(idStrs, ","),
	}
}

// EtcdWaitClusterOp returns an Operator to wait until etcd cluster becomes healthy
func EtcdWaitClusterOp(endpoints []string) Operator {
	return &etcdWaitClusterOp{
		endpoints: endpoints,
	}
}

type etcdWaitClusterOp struct {
	endpoints []string
	executed  bool
}

func (o *etcdWaitClusterOp) Name() string {
	return "etcd-wait-cluster"
}

func (o *etcdWaitClusterOp) NextCommand() Commander {
	if o.executed {
		return nil
	}
	o.executed = true

	return waitEtcdSyncCommand{o.endpoints, 0}
}

// EtcdRemoveMemberOp returns an Operator to remove member from etcd cluster.
func EtcdRemoveMemberOp(endpoints []string, targets map[string]*etcdserverpb.Member) Operator {
	return &etcdRemoveMemberOp{
		endpoints: endpoints,
		targets:   targets,
	}
}

type etcdRemoveMemberOp struct {
	endpoints []string
	targets   map[string]*etcdserverpb.Member
	executed  bool
}

func (o *etcdRemoveMemberOp) Name() string {
	return "etcd-remove-member"
}

func (o *etcdRemoveMemberOp) NextCommand() Commander {
	if o.executed {
		return nil
	}
	o.executed = true

	var ids []uint64
	for _, v := range o.targets {
		ids = append(ids, v.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return removeEtcdMemberCommand{o.endpoints, ids}
}

// EtcdDestroyMemberOp create new etcdDestroyMemberOp instance
func EtcdDestroyMemberOp(endpoints []string, targets []*Node, members map[string]*etcdserverpb.Member) Operator {
	return &etcdDestroyMemberOp{
		endpoints: endpoints,
		targets:   targets,
		members:   members,
	}
}

type etcdDestroyMemberOp struct {
	endpoints []string
	targets   []*Node
	members   map[string]*etcdserverpb.Member
	params    EtcdParams
	step      int
	nodeIndex int
}

func (o *etcdDestroyMemberOp) Name() string {
	return "etcd-destroy-member"
}

func (o *etcdDestroyMemberOp) NextCommand() Commander {
	if o.nodeIndex >= len(o.targets) {
		return nil
	}

	node := o.targets[o.nodeIndex]
	volname := etcdVolumeName(o.params)

	switch o.step {
	case 0:
		o.step++
		var ids []uint64
		if m, ok := o.members[node.Address]; ok {
			ids = []uint64{m.ID}
		}
		return removeEtcdMemberCommand{o.endpoints, ids}
	case 1:
		o.step++
		return waitEtcdSyncCommand{o.endpoints, 0}
	case 2:
		o.step++
		return stopContainerCommand{node, etcdContainerName}
	case 3:
		o.step = 0
		o.nodeIndex++
		return volumeRemoveCommand{[]*Node{node}, volname}
	}
	return nil
}

// EtcdUpdateVersionOp create new etcdUpdateVersionOp instance
func EtcdUpdateVersionOp(endpoints []string, targets []*Node, cpNodes []*Node, params EtcdParams) Operator {
	return &etcdUpdateVersionOp{
		endpoints: endpoints,
		targets:   targets,
		cpNodes:   cpNodes,
		params:    params,
	}
}

type etcdUpdateVersionOp struct {
	endpoints []string
	targets   []*Node
	cpNodes   []*Node
	params    EtcdParams
	step      int
	nodeIndex int
}

func (o *etcdUpdateVersionOp) Name() string {
	return "etcd-update-version"
}

func (o *etcdUpdateVersionOp) NextCommand() Commander {
	if o.nodeIndex >= len(o.targets) {
		return nil
	}

	volname := etcdVolumeName(o.params)
	extra := o.params.ServiceParams

	switch o.step {
	case 0:
		o.step++
		return waitEtcdSyncCommand{o.endpoints, 1}
	case 1:
		o.step++
		return imagePullCommand{[]*Node{o.targets[o.nodeIndex]}, "etcd"}
	case 2:
		o.step++
		target := o.targets[o.nodeIndex]
		return stopContainerCommand{target, etcdContainerName}
	case 3:
		o.step = 0
		target := o.targets[o.nodeIndex]
		opts := []string{
			"--mount",
			"type=volume,src=" + volname + ",dst=/var/lib/etcd",
			"--volume=/etc/etcd/pki:/etc/etcd/pki:ro",
		}
		var initialCluster []string
		for _, n := range o.cpNodes {
			initialCluster = append(initialCluster, n.Address+"=https://"+n.Address+":2380")
		}
		o.nodeIndex++
		return runContainerCommand{[]*Node{target}, etcdContainerName, opts, etcdBuiltInParams(target, initialCluster, "new"), extra}
	}
	return nil
}

// EtcdRestartOp create new etcdRestartOp instance
func EtcdRestartOp(endpoints []string, targets []*Node, cpNodes []*Node, params EtcdParams) Operator {
	return &etcdRestartOp{
		endpoints: endpoints,
		targets:   targets,
		cpNodes:   cpNodes,
		params:    params,
	}
}

type etcdRestartOp struct {
	endpoints []string
	targets   []*Node
	cpNodes   []*Node
	params    EtcdParams
	step      int
	nodeIndex int
}

func (o *etcdRestartOp) Name() string {
	return "etcd-restart"
}

func (o *etcdRestartOp) NextCommand() Commander {
	if o.nodeIndex >= len(o.targets) {
		return nil
	}

	volname := etcdVolumeName(o.params)
	extra := o.params.ServiceParams

	switch o.step {
	case 0:
		o.step++
		return waitEtcdSyncCommand{o.endpoints, 1}
	case 1:
		o.step++
		target := o.targets[o.nodeIndex]
		return stopContainerCommand{target, etcdContainerName}
	case 2:
		o.step = 0
		target := o.targets[o.nodeIndex]
		opts := []string{
			"--mount",
			"type=volume,src=" + volname + ",dst=/var/lib/etcd",
			"--volume=/etc/etcd/pki:/etc/etcd/pki:ro",
		}
		var initialCluster []string
		for _, n := range o.cpNodes {
			initialCluster = append(initialCluster, n.Address+"=https://"+n.Address+":2380")
		}
		o.nodeIndex++
		return runContainerCommand{[]*Node{target}, etcdContainerName, opts, etcdBuiltInParams(target, initialCluster, "new"), extra}
	}
	return nil
}
