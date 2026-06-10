package router

import (
	"fmt"
	"testing"
)

func TestRouterAddRemoveNode(t *testing.T) {
	router := NewRouter()

	node1 := &Node{ID: "node1", Addr: "192.168.1.1:8080"}
	node2 := &Node{ID: "node2", Addr: "192.168.1.2:8080"}

	router.AddNode(node1)
	router.AddNode(node2)

	if router.NodeCount() != 2 {
		t.Errorf("expected 2 nodes, got %d", router.NodeCount())
	}

	router.RemoveNode("node1")

	if router.NodeCount() != 1 {
		t.Errorf("expected 1 node after removal, got %d", router.NodeCount())
	}
}

func TestRouterSelectRandomNodes(t *testing.T) {
	router := NewRouter()

	for i := 0; i < 10; i++ {
		router.AddNode(&Node{
			ID:   fmt.Sprintf("node%d", i),
			Addr: fmt.Sprintf("192.168.1.%d:8080", i),
		})
	}

	selected := router.SelectRandomNodes(3)
	if len(selected) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(selected))
	}

	seen := make(map[string]bool)
	for _, node := range selected {
		if seen[node.ID] {
			t.Error("duplicate node selected")
		}
		seen[node.ID] = true
	}
}
