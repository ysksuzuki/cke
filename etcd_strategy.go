package cke

import (
	"sort"
	"strings"

	"github.com/coreos/etcd/etcdserver/etcdserverpb"
)

func etcdDecideToDo(c *Cluster, cs *ClusterStatus) Operator {
	// See docs/etcd.md

	cpNodes := controlPlanes(c.Nodes)
	endpoints := make([]string, len(cpNodes))
	for i, n := range cpNodes {
		endpoints[i] = "http://" + n.Address + ":2379"
	}

	bootstrap := true
	for _, n := range c.Nodes {
		if cs.NodeStatuses[n.Address].Etcd.HasData {
			bootstrap = false
		}
	}
	if bootstrap {
		return EtcdBootOp(endpoints, cpNodes, c.Options.Etcd)
	}

	members := unhealthyNonClusterMember(c.Nodes, cs.Etcd)
	if len(members) > 0 {
		return EtcdRemoveMemberOp(endpoints, members)
	}
	nodes := unhealthyNonControlPlaneMember(c.Nodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdDestroyMemberOp(endpoints, nodes, cs.Etcd.Members)
	}
	nodes = unstartedMemberControlPlane(cpNodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdAddMemberOp(endpoints, nodes, c.Options.Etcd)
	}
	if !etcdClusterIsHealthy(cs.Etcd) {
		return EtcdWaitMemberOp(endpoints)
	}
	nodes = newMemberControlPlane(cpNodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdAddMemberOp(endpoints, nodes, c.Options.Etcd)
	}
	members = healthyNonClusterMember(c.Nodes, cs.Etcd)
	if len(members) > 0 {
		return EtcdRemoveMemberOp(endpoints, members)
	}
	nodes = runningNonControlPlaneMember(c.Nodes, cs.NodeStatuses)
	if len(nodes) > 0 {
		return EtcdDestroyMemberOp(endpoints, nodes, cs.Etcd.Members)
	}
	nodes = outdatedEtcdImageMember(cpNodes, cs.NodeStatuses)
	if len(nodes) > 0 {
		return EtcdUpdateVersionOp(endpoints, nodes, cpNodes, c.Options.Etcd)
	}
	nodes = outdatedEtcdParamsMember(cpNodes, c.Options.Etcd.ServiceParams, cs.NodeStatuses)
	if len(nodes) > 0 {
		return EtcdRestartOp(endpoints, nodes, cpNodes, c.Options.Etcd)
	}

	return nil
}

func unhealthyNonClusterMember(allNodes []*Node, cs EtcdClusterStatus) map[string]*etcdserverpb.Member {
	mem := make(map[string]*etcdserverpb.Member)
	for k, v := range cs.Members {
		mem[k] = v
	}
	for _, n := range allNodes {
		delete(mem, n.Address)
	}
	for k := range mem {
		if cs.MemberHealth[k] == EtcdNodeHealthy {
			delete(mem, k)
		}
	}
	return mem
}

func unhealthyNonControlPlaneMember(nodes []*Node, cs EtcdClusterStatus) []*Node {
	return filterNodes(nodes, func(n *Node) bool {
		if n.ControlPlane {
			return false
		}
		_, inMember := cs.Members[n.Address]
		health := cs.MemberHealth[n.Address]
		return health != EtcdNodeHealthy && inMember
	})
}

func unstartedMemberControlPlane(cpNodes []*Node, cs EtcdClusterStatus) []*Node {
	return filterNodes(cpNodes, func(n *Node) bool {
		m, inMember := cs.Members[n.Address]
		return inMember && len(m.Name) == 0
	})
}

func newMemberControlPlane(cpNodes []*Node, cs EtcdClusterStatus) []*Node {
	return filterNodes(cpNodes, func(n *Node) bool {
		_, inMember := cs.Members[n.Address]
		return !inMember
	})
}

func healthyNonClusterMember(allNodes []*Node, cs EtcdClusterStatus) map[string]*etcdserverpb.Member {
	mem := make(map[string]*etcdserverpb.Member)
	for k, v := range cs.Members {
		mem[k] = v
	}
	for _, n := range allNodes {
		delete(mem, n.Address)
	}
	for k := range mem {
		if cs.MemberHealth[k] != EtcdNodeHealthy {
			delete(mem, k)
		}
	}
	return mem
}

func runningNonControlPlaneMember(allNodes []*Node, statuses map[string]*NodeStatus) []*Node {
	return filterNodes(allNodes, func(n *Node) bool {
		return !n.ControlPlane && statuses[n.Address].Etcd.Running
	})
}

func etcdClusterIsHealthy(cs EtcdClusterStatus) bool {
	for _, s := range cs.MemberHealth {
		if s == EtcdNodeHealthy {
			return true
		}
	}
	return false
}

func outdatedEtcdImageMember(nodes []*Node, statuses map[string]*NodeStatus) []*Node {
	return filterNodes(nodes, func(n *Node) bool {
		return EtcdImage != statuses[n.Address].Etcd.Image
	})
}

func outdatedEtcdParamsMember(nodes []*Node, extra ServiceParams, statuses map[string]*NodeStatus) []*Node {
	return filterNodes(nodes, func(n *Node) bool {
		newBuiltIn := etcdBuiltInParams(n, []string{}, "new")
		newExtra := extra

		currentBuiltin := statuses[n.Address].Etcd.BuiltInParams
		currentExtra := statuses[n.Address].Etcd.ExtraParams

		// NOTE ignore parameters starting with "--initial-" prefix.
		// There options are used only on starting etcd process at first time.
		eqArgs := func(s1, s2 []string) bool {
			var sorted1, sorted2 []string
			for _, s := range s1 {
				if !strings.HasPrefix(s, "--initial-") {
					sorted1 = append(sorted1, s)
				}
			}
			for _, s := range s2 {
				if !strings.HasPrefix(s, "--initial-") {
					sorted2 = append(sorted2, s)
				}
			}

			sort.Strings(sorted1)
			sort.Strings(sorted2)
			return compareStrings(sorted1, sorted2)
		}

		if !eqArgs(newBuiltIn.ExtraArguments, currentBuiltin.ExtraArguments) ||
			!eqArgs(newExtra.ExtraArguments, currentExtra.ExtraArguments) {
			return true
		}
		if !compareMounts(newBuiltIn.ExtraBinds, currentBuiltin.ExtraBinds) ||
			!compareMounts(newExtra.ExtraBinds, currentBuiltin.ExtraBinds) ||
			!compareStringMap(newBuiltIn.ExtraEnvvar, currentBuiltin.ExtraEnvvar) ||
			!compareStringMap(newExtra.ExtraEnvvar, currentBuiltin.ExtraEnvvar) {
			return true
		}

		return false
	})
}
