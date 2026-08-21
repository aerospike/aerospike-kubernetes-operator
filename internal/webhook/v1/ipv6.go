package v1

import (
	"context"
	"net"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list

const (
	// defaultNodeIPv6CacheTTL bounds how stale a cached IPv6 capability answer can be.
	// Node addressing is a property of the cluster network, so it changes only when the
	// cluster is rebuilt or a differently addressed node pool joins. A few minutes keeps
	// admission off the API server while still picking such a change up without a restart.
	defaultNodeIPv6CacheTTL = 5 * time.Minute

	// nodeListPageSize keeps a cache refresh from materialising every node of a large
	// cluster at once. The scan stops at the first IPv6-capable node, so single-stack
	// IPv6 clusters answer from the first page.
	nodeListPageSize = 100
)

// nodeIPv6Prober reports whether the Kubernetes cluster has at least one IPv6-capable
// node, caching the answer because it is consulted on every AerospikeCluster admission.
type nodeIPv6Prober struct {
	expiry time.Time
	reader client.Reader
	ttl    time.Duration
	mu     sync.Mutex
	cached bool
}

func newNodeIPv6Prober(reader client.Reader, ttl time.Duration) *nodeIPv6Prober {
	if ttl <= 0 {
		ttl = defaultNodeIPv6CacheTTL
	}

	return &nodeIPv6Prober{
		reader: reader,
		ttl:    ttl,
	}
}

// IPv6Capable reports whether any node carries an IPv6 InternalIP address.
// The lock is held across the refresh so that concurrent admissions collapse into a
// single node listing rather than each issuing its own.
func (p *nodeIPv6Prober) IPv6Capable(ctx context.Context) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if time.Now().Before(p.expiry) {
		return p.cached, nil
	}

	capable, err := clusterHasIPv6Node(ctx, p.reader)
	if err != nil {
		return false, err
	}

	p.cached = capable
	p.expiry = time.Now().Add(p.ttl)

	return capable, nil
}

// clusterHasIPv6Node reports whether any node reports an IPv6 InternalIP. This mirrors
// the address selection the init container performs at runtime, so admission agrees with
// the addresses the pods actually end up advertising.
func clusterHasIPv6Node(ctx context.Context, reader client.Reader) (bool, error) {
	listOpts := []client.ListOption{client.Limit(nodeListPageSize)}

	for {
		nodes := &v1.NodeList{}
		if err := reader.List(ctx, nodes, listOpts...); err != nil {
			return false, err
		}

		for idx := range nodes.Items {
			if nodeHasIPv6InternalIP(&nodes.Items[idx]) {
				return true, nil
			}
		}

		if nodes.Continue == "" {
			return false, nil
		}

		listOpts = []client.ListOption{client.Limit(nodeListPageSize), client.Continue(nodes.Continue)}
	}
}

func nodeHasIPv6InternalIP(node *v1.Node) bool {
	for idx := range node.Status.Addresses {
		address := &node.Status.Addresses[idx]
		if address.Type != v1.NodeInternalIP {
			continue
		}

		if ip := net.ParseIP(address.Address); ip != nil && ip.To4() == nil {
			return true
		}
	}

	return false
}
