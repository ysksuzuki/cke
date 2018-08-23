package cke

import (
	"github.com/coreos/etcd/etcdserver/etcdserverpb"
	"github.com/cybozu-go/log"
)

func etcdDecideToDo(c *Cluster, cs *ClusterStatus) Operator {
	// See docs/etcd.md

	var cpNodes []*Node
	for _, n := range c.Nodes {
		if n.ControlPlane {
			cpNodes = append(cpNodes, n)
		}
	}

	bootstrap := true
	for _, n := range c.Nodes {
		if cs.NodeStatuses[n.Address].Etcd.HasData {
			bootstrap = false
		}
	}
	if bootstrap {
		return EtcdBootOp(cpNodes, cs.Agents, c.Options.Etcd)
	}

	if len(cs.Etcd.Members) == 0 {
		log.Warn("No members of etcd cluster", nil)
		return nil
	}

	endpoints := make([]string, len(cpNodes))
	for i, n := range cpNodes {
		endpoints[i] = "http://" + n.Address + ":2379"
	}

	members := unhealthyNonClusterMember(c.Nodes, cs.Etcd)
	if len(members) > 0 {
		return EtcdRemoveMemberOp(endpoints, members)
	}
	nodes := unhealthyNonControlPlaneMember(c.Nodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdDestroyMemberOp(endpoints, nodes, cs.Agents, cs.Client, cs.Etcd.Members)
	}
	nodes = unstartedMemberControlPlane(cpNodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdAddMemberOp(endpoints, nodes, cs.Agents, cs.Client, c.Options.Etcd)
	}
	nodes = newMemberControlPlane(cpNodes, cs.Etcd)
	if len(nodes) > 0 {
		return EtcdAddMemberOp(endpoints, nodes, cs.Agents, cs.Client, c.Options.Etcd)
	}
	members = healthyNonClusterMember(c.Nodes, cs.Etcd)
	if len(members) > 0 {
		return EtcdRemoveMemberOp(endpoints, members)
	}
	nodes = runningNonControlPlaneMember(c.Nodes, cs.NodeStatuses)
	if len(nodes) > 0 {
		return EtcdDestroyMemberOp(endpoints, nodes, cs.Agents, cs.Client, cs.Etcd.Members)
	}
	nodes = outdatedControlPlaneMember(cpNodes, cs.NodeStatuses)
	if len(nodes) > 0 {
		return EtcdUpdateVersionOp(endpoints, cs.Client, nodes, cpNodes, cs.Agents, c.Options.Etcd)
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
	var targets []*Node
	for _, n := range nodes {
		if n.ControlPlane {
			continue
		}
		_, inMember := cs.Members[n.Address]
		health := cs.MemberHealth[n.Address]
		if health != EtcdNodeHealthy && inMember {
			targets = append(targets, n)
		}

	}
	return targets
}

func unstartedMemberControlPlane(cpNodes []*Node, cs EtcdClusterStatus) []*Node {
	var targets []*Node
	for _, n := range cpNodes {
		m, inMember := cs.Members[n.Address]
		if inMember && len(m.Name) == 0 {
			targets = append(targets, n)
		}
	}
	return targets
}

func newMemberControlPlane(cpNodes []*Node, cs EtcdClusterStatus) []*Node {
	var targets []*Node
	for _, n := range cpNodes {
		_, inMember := cs.Members[n.Address]
		if !inMember {
			targets = append(targets, n)
		}
	}
	return targets
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
	var targets []*Node
	for _, n := range allNodes {
		if n.ControlPlane {
			continue
		}
		st := statuses[n.Address]
		if st.Etcd.Running {
			targets = append(targets, n)
		}
	}
	return targets
}

func outdatedControlPlaneMember(allNodes []*Node, statuses map[string]*NodeStatus) []*Node {
	var targets []*Node
	for _, n := range allNodes {
		if !n.ControlPlane {
			continue
		}
		if EtcdImage != statuses[n.Address].Etcd.Image {
			targets = append(targets, n)
		}
	}
	return targets
}
