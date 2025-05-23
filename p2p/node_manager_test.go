package p2p

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// helper function to create a new NodeManager with a temporary DBStorage for testing
func newNodeManagerForTest(t *testing.T) (*NodeManager, func()) {
	tempDir, err := ioutil.TempDir("", "nodemanager_test_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "testnodes.db")
	storage, err := NewDBStorage(dbPath, log.New(ioutil.Discard, "", 0)) // Use discard logger for storage
	if err != nil {
		t.Fatalf("Failed to create DBStorage: %v", err)
	}

	config := NewConfig("") // Use default config
	config.InitialNodes = []string{} // Ensure no initial nodes for most tests
	logger := log.New(ioutil.Discard, "", 0) // Discard logger for NodeManager

	nm := NewNodeManager(config, storage, logger)

	cleanup := func() {
		if err := storage.Close(); err != nil {
			t.Logf("Error closing storage: %v", err)
		}
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Error removing temp dir %s: %v", tempDir, err)
		}
	}

	return nm, cleanup
}

func TestAddNode(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	addr1 := "localhost:8001"
	nodeType1 := ValidatorNode

	// Test adding a new node
	added := nm.AddNode(addr1, nodeType1)
	if !added {
		t.Errorf("AddNode failed to add a new node %s", addr1)
	}

	node1Details := nm.GetNodeDetails(addr1)
	if node1Details == nil {
		t.Fatalf("Node %s not found after adding", addr1)
	}
	if node1Details.NodeType != nodeType1 {
		t.Errorf("Expected NodeType %s for node %s, got %s", nodeType1, addr1, node1Details.NodeType)
	}
	initialLastSeen := node1Details.LastSeen

	// Test adding the same node again (should update LastSeen and NodeType)
	time.Sleep(10 * time.Millisecond) // Ensure LastSeen can change
	updatedNodeType := RegularNode
	addedAgain := nm.AddNode(addr1, updatedNodeType)
	if addedAgain { // AddNode returns false if node already exists
		t.Errorf("AddNode returned true when adding an existing node %s", addr1)
	}

	node1DetailsUpdated := nm.GetNodeDetails(addr1)
	if node1DetailsUpdated == nil {
		t.Fatalf("Node %s not found after re-adding", addr1)
	}
	if node1DetailsUpdated.NodeType != updatedNodeType {
		t.Errorf("Expected updated NodeType %s for node %s, got %s", updatedNodeType, addr1, node1DetailsUpdated.NodeType)
	}
	if !node1DetailsUpdated.LastSeen.After(initialLastSeen) {
		t.Errorf("Expected LastSeen to be updated for node %s, old: %v, new: %v", addr1, initialLastSeen, node1DetailsUpdated.LastSeen)
	}
}

func TestGetNodesByType(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	nm.AddNode("localhost:8001", ValidatorNode)
	nm.AddNode("localhost:8002", RegularNode)
	nm.AddNode("localhost:8003", ValidatorNode)
	nm.AddNode("localhost:8004", RegularNode)
	nm.AddNode("localhost:8005", ValidatorNode)

	validatorNodes := nm.GetNodesByType(ValidatorNode)
	if len(validatorNodes) != 3 {
		t.Errorf("Expected 3 validator nodes, got %d", len(validatorNodes))
	}
	for _, node := range validatorNodes {
		if node.NodeType != ValidatorNode {
			t.Errorf("GetNodesByType(ValidatorNode) returned node with type %s", node.NodeType)
		}
	}

	regularNodes := nm.GetNodesByType(RegularNode)
	if len(regularNodes) != 2 {
		t.Errorf("Expected 2 regular nodes, got %d", len(regularNodes))
	}
	for _, node := range regularNodes {
		if node.NodeType != RegularNode {
			t.Errorf("GetNodesByType(RegularNode) returned node with type %s", node.NodeType)
		}
	}

	// Test with a type that has no nodes (inventing a type for test)
	type UnknownNodeType NodeType
	unknownNodes := nm.GetNodesByType("unknown")
	if len(unknownNodes) != 0 {
		t.Errorf("Expected 0 nodes for unknown type, got %d", len(unknownNodes))
	}
}

func TestGetActiveNodesByType(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	// Add nodes, some active, some not (by default they are active)
	nm.AddNode("localhost:8001", ValidatorNode) // Active Validator
	nm.AddNode("localhost:8002", RegularNode)   // Active Regular
	nm.AddNode("localhost:8003", ValidatorNode) // Active Validator
	nm.AddNode("localhost:8004", RegularNode)   // Active Regular

	// Manually set some nodes to inactive
	if node := nm.GetNodeDetails("localhost:8002"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8002"] = node // Directly update for test simplicity
	}
	if node := nm.GetNodeDetails("localhost:8003"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8003"] = node // Directly update for test simplicity
	}


	activeValidators := nm.GetActiveNodesByType(ValidatorNode)
	if len(activeValidators) != 1 {
		t.Errorf("Expected 1 active validator node, got %d. Nodes: %+v", len(activeValidators), activeValidators)
	} else if activeValidators[0].Address != "localhost:8001" {
		t.Errorf("Expected active validator localhost:8001, got %s", activeValidators[0].Address)
	}


	activeRegulars := nm.GetActiveNodesByType(RegularNode)
	if len(activeRegulars) != 1 {
		t.Errorf("Expected 1 active regular node, got %d. Nodes: %+v", len(activeRegulars), activeRegulars)
	} else if activeRegulars[0].Address != "localhost:8004" {
		t.Errorf("Expected active regular localhost:8004, got %s", activeRegulars[0].Address)
	}

	// Test with a type that has no active nodes
	nm.AddNode("localhost:8005", ValidatorNode)
	if node := nm.GetNodeDetails("localhost:8005"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8005"] = node
	}
	activeValidatorsAfterInactive := nm.GetActiveNodesByType(ValidatorNode)
	if len(activeValidatorsAfterInactive) != 1 { // Still expecting the first one
		t.Errorf("Expected 1 active validator node after adding an inactive one, got %d", len(activeValidatorsAfterInactive))
	}
}

func TestGetAllNodesReturnType(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	nm.AddNode("localhost:8001", ValidatorNode)
	nm.AddNode("localhost:8002", RegularNode)

	allNodes := nm.GetAllNodes()
	if len(allNodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(allNodes))
	}
	if _, ok := interface{}(allNodes).([]*NodeStatus); !ok {
		t.Errorf("GetAllNodes did not return []*NodeStatus, got %T", allNodes)
	}
	// Check if addresses are present (simple check)
	found8001 := false
	found8002 := false
	for _, node := range allNodes {
		if node.Address == "localhost:8001" {
			found8001 = true
		}
		if node.Address == "localhost:8002" {
			found8002 = true
		}
	}
	if !found8001 || !found8002 {
		t.Errorf("GetAllNodes didn't return all added nodes. 8001_found=%v, 8002_found=%v", found8001, found8002)
	}
}

func TestGetActiveNodesReturnType(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	nm.AddNode("localhost:8001", ValidatorNode) // Active
	nm.AddNode("localhost:8002", RegularNode)   // Active

	if node := nm.GetNodeDetails("localhost:8002"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8002"] = node // Directly update for test simplicity
	}

	activeNodes := nm.GetActiveNodes()
	if len(activeNodes) != 1 {
		t.Errorf("Expected 1 active node, got %d", len(activeNodes))
	}
	if _, ok := interface{}(activeNodes).([]*NodeStatus); !ok {
		t.Errorf("GetActiveNodes did not return []*NodeStatus, got %T", activeNodes)
	}
	if activeNodes[0].Address != "localhost:8001" {
		t.Errorf("Expected active node localhost:8001, got %s", activeNodes[0].Address)
	}
}

func TestPersistence(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	node1 := &NodeStatus{Address: "localhost:9001", NodeType: ValidatorNode, IsResponding: true, LastSeen: time.Now()}
	node2 := &NodeStatus{Address: "localhost:9002", NodeType: RegularNode, IsResponding: false, LastSeen: time.Now()}

	nm.AddNode(node1.Address, node1.NodeType)
	// For IsResponding and LastSeen, we'd need to update them after adding or use SetNodeStatus if available
	// For simplicity, we'll retrieve and update, or rely on AddNode's defaults and then check.
	// AddNode sets IsResponding to true by default.
	
	// Update node1 details after adding
	nm.mutex.Lock() // Lock for direct map modification
	nm.nodes[node1.Address].IsResponding = node1.IsResponding 
	nm.nodes[node1.Address].LastSeen = node1.LastSeen
	nm.mutex.Unlock()
	nm.storage.SaveNode(nm.nodes[node1.Address])


	nm.AddNode(node2.Address, node2.NodeType)
	nm.mutex.Lock()
	nm.nodes[node2.Address].IsResponding = node2.IsResponding // Set to false for test
	nm.nodes[node2.Address].LastSeen = node2.LastSeen
	nm.mutex.Unlock()
	nm.storage.SaveNode(nm.nodes[node2.Address])


	// Clear in-memory nodes (not the storage)
	nm.nodes = make(map[string]*NodeStatus)
	if len(nm.GetAllNodes()) != 0 {
		t.Fatal("In-memory nodes not cleared before loading")
	}

	// Load from storage
	nm.loadNodesFromStorage()

	loadedNodes := nm.GetAllNodes()
	if len(loadedNodes) != 2 {
		t.Fatalf("Expected 2 nodes to be loaded from storage, got %d", len(loadedNodes))
	}

	expectedNodes := map[string]*NodeStatus{
		node1.Address: node1,
		node2.Address: node2,
	}

	for _, loaded := range loadedNodes {
		expected, ok := expectedNodes[loaded.Address]
		if !ok {
			t.Errorf("Loaded unexpected node: %s", loaded.Address)
			continue
		}
		// Comparing time.Time can be tricky due to monotonic clock. Truncate or use Before/After.
		// For this test, we will compare the NodeType and IsResponding.
		if loaded.NodeType != expected.NodeType {
			t.Errorf("For node %s, expected NodeType %s, got %s", loaded.Address, expected.NodeType, loaded.NodeType)
		}
		if loaded.IsResponding != expected.IsResponding {
			t.Errorf("For node %s, expected IsResponding %t, got %t", loaded.Address, expected.IsResponding, loaded.IsResponding)
		}
        // Comparing reflect.DeepEqual for the whole struct might fail due to LastSeen precision
        // So we check fields that matter for persistence.
	}
}

// A more robust way to compare NodeStatus slices if order doesn't matter
func compareNodeStatusSlices(t *testing.T, got, want []*NodeStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("Slice lengths differ: got %d, want %d", len(got), len(want))
		return
	}
	// Create maps for easier comparison if order doesn't matter
	gotMap := make(map[string]*NodeStatus)
	for _, node := range got {
		gotMap[node.Address] = node
	}
	for _, nodeWant := range want {
		nodeGot, ok := gotMap[nodeWant.Address]
		if !ok {
			t.Errorf("Wanted node %s not found in got slice", nodeWant.Address)
			continue
		}
		// Using reflect.DeepEqual for individual nodes, but be careful with time.Time
		// For now, let's assume NodeType is the most critical for type-based getters
		if nodeGot.NodeType != nodeWant.NodeType {
			t.Errorf("Node %s: got NodeType %s, want %s", nodeGot.Address, nodeGot.NodeType, nodeWant.NodeType)
		}
        if nodeGot.IsResponding != nodeWant.IsResponding {
             t.Errorf("Node %s: got IsResponding %v, want %v", nodeGot.Address, nodeGot.IsResponding, nodeWant.IsResponding)
        }
	}
}

func TestMain(m *testing.M) {
	// Setup can go here if needed for all tests, e.g., cleaning up old test DBs
	// For now, individual test cleanup is used.
	exitCode := m.Run()
	os.Exit(exitCode)
}

// Example of how one might improve TestGetActiveNodesByType with compareNodeStatusSlices
// This is illustrative and not directly part of the initial file creation.
func TestGetActiveNodesByType_Improved(t *testing.T) {
	nm, cleanup := newNodeManagerForTest(t)
	defer cleanup()

	// Setup nodes
	n1 := &NodeStatus{Address: "localhost:8001", NodeType: ValidatorNode, IsResponding: true}
	n2 := &NodeStatus{Address: "localhost:8002", NodeType: RegularNode, IsResponding: true}
	n3 := &NodeStatus{Address: "localhost:8003", NodeType: ValidatorNode, IsResponding: false} // inactive
	n4 := &NodeStatus{Address: "localhost:8004", NodeType: RegularNode, IsResponding: false} // inactive
	n5 := &NodeStatus{Address: "localhost:8005", NodeType: ValidatorNode, IsResponding: true}


	nm.AddNode(n1.Address, n1.NodeType)
	nm.nodes[n1.Address].IsResponding = n1.IsResponding

	nm.AddNode(n2.Address, n2.NodeType)
	nm.nodes[n2.Address].IsResponding = n2.IsResponding

	nm.AddNode(n3.Address, n3.NodeType)
	nm.nodes[n3.Address].IsResponding = n3.IsResponding

	nm.AddNode(n4.Address, n4.NodeType)
	nm.nodes[n4.Address].IsResponding = n4.IsResponding
	
	nm.AddNode(n5.Address, n5.NodeType)
	nm.nodes[n5.Address].IsResponding = n5.IsResponding


	// Test for active validators
	gotActiveValidators := nm.GetActiveNodesByType(ValidatorNode)
	wantActiveValidators := []*NodeStatus{n1, n5} // Define expected slice
	compareNodeStatusSlices(t, gotActiveValidators, wantActiveValidators)

	// Test for active regulars
	gotActiveRegulars := nm.GetActiveNodesByType(RegularNode)
	wantActiveRegulars := []*NodeStatus{n2}
	compareNodeStatusSlices(t, gotActiveRegulars, wantActiveRegulars)
}
