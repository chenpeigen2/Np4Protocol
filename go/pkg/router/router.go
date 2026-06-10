package router

import (
	"math/rand"
	"sync"
)

// Node represents a network node with an ID and address.
type Node struct {
	ID   string
	Addr string
}

// Router manages a set of network nodes and provides random selection.
type Router struct {
	nodes map[string]*Node
	mu    sync.RWMutex
}

// NewRouter creates a new Router instance.
func NewRouter() *Router {
	return &Router{
		nodes: make(map[string]*Node),
	}
}

// AddNode adds a node to the router, keyed by its ID.
func (r *Router) AddNode(node *Node) {
	r.mu.Lock()
	r.nodes[node.ID] = node
	r.mu.Unlock()
}

// RemoveNode removes a node from the router by ID.
func (r *Router) RemoveNode(id string) {
	r.mu.Lock()
	delete(r.nodes, id)
	r.mu.Unlock()
}

// GetNode retrieves a node by ID. Returns the node and whether it was found.
func (r *Router) GetNode(id string) (*Node, bool) {
	r.mu.RLock()
	node, ok := r.nodes[id]
	r.mu.RUnlock()
	return node, ok
}

// NodeCount returns the number of nodes currently in the router.
func (r *Router) NodeCount() int {
	r.mu.RLock()
	count := len(r.nodes)
	r.mu.RUnlock()
	return count
}

// SelectRandomNodes returns up to count randomly selected nodes.
// If count exceeds the total number of nodes, all nodes are returned.
func (r *Router) SelectRandomNodes(count int) []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		all = append(all, node)
	}

	if count > len(all) {
		count = len(all)
	}

	rand.Shuffle(len(all), func(i, j int) {
		all[i], all[j] = all[j], all[i]
	})

	return all[:count]
}
