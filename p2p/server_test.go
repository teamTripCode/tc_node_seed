package p2p

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// Helper to create NodeManager for server tests (similar to node_manager_test.go)
func newNodeManagerForServerTest(t *testing.T) (*NodeManager, func()) {
	tempDir, err := ioutil.TempDir("", "server_test_nm_")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test_server_nodes.db")
	storage, err := NewDBStorage(dbPath, log.New(ioutil.Discard, "", 0))
	if err != nil {
		t.Fatalf("Failed to create DBStorage: %v", err)
	}

	config := NewConfig("")
	config.InitialNodes = []string{} // No initial nodes from config
	logger := log.New(ioutil.Discard, "", 0)

	nm := NewNodeManager(config, storage, logger)

	cleanup := func() {
		if err := storage.Close(); err != nil {
			t.Logf("Error closing server test storage: %v", err)
		}
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Error removing server test temp dir %s: %v", tempDir, err)
		}
	}
	return nm, cleanup
}

// Helper to setup SeedNode with a pre-populated NodeManager
func setupSeedNodeForTest(t *testing.T) (*SeedNode, *NodeManager, func()) {
	nm, nmCleanup := newNodeManagerForServerTest(t)

	// Pre-populate NodeManager
	// Active Nodes
	nm.AddNode("localhost:8001", ValidatorNode) // Active Validator
	nm.AddNode("localhost:8002", RegularNode)   // Active Regular
	nm.AddNode("localhost:8003", ValidatorNode) // Active Validator

	// Inactive Nodes - AddNode makes them active, so we manually update and save
	nm.AddNode("localhost:8004", RegularNode)   // To be Inactive Regular
	if node := nm.GetNodeDetails("localhost:8004"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8004"] = node // Update in memory
		nm.storage.SaveNode(node)         // Save change to DB
	}

	nm.AddNode("localhost:8005", ValidatorNode) // To be Inactive Validator
	if node := nm.GetNodeDetails("localhost:8005"); node != nil {
		node.IsResponding = false
		nm.nodes["localhost:8005"] = node // Update in memory
		nm.storage.SaveNode(node)         // Save change to DB
	}
	// Ensure LastSeen is distinct for sorting later if needed
	time.Sleep(1 * time.Millisecond)


	logger := log.New(ioutil.Discard, "", 0)
	config := NewConfig("") // Use default config for SeedNode itself

	// Need to pass the actual storage to SeedNode if it initializes its own NodeManager,
	// or ensure SeedNode uses the one we give it.
	// The NewSeedNode function initializes its own NodeManager.
	// We need to inject our pre-populated NodeManager.
	// Let's modify how SeedNode is created or use a setter if available.
	// For now, let's assume SeedNode has a way to set its nodeManager, or we construct it carefully.
	
	// SeedNode's NewSeedNode creates its own NodeManager.
	// We will replace it after creation for testing purposes.
	seedNode := NewSeedNode(9999, logger) // Port doesn't matter for handler tests using httptest
	seedNode.nodeManager = nm // Replace with our pre-populated manager
	seedNode.storage = nm.storage // Ensure it uses the same storage for consistency if it tries to save config etc.


	cleanup := func() {
		// nmCleanup will close storage and remove temp dir
		nmCleanup()
		// seedNode.Stop() // If Stop() has complex logic, call it. For now, it's mainly for storage.
	}

	return seedNode, nm, cleanup
}

// Helper to sort NodeStatus slices by address for consistent comparison
func sortNodes(nodes []*NodeStatus) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Address < nodes[j].Address
	})
}

// TestGetNodesHandler tests the /nodes endpoint
func TestGetNodesHandler(t *testing.T) {
	seedNode, nm, cleanup := setupSeedNodeForTest(t)
	defer cleanup()

	server := httptest.NewServer(seedNode.router)
	defer server.Close()

	// Expected nodes (all nodes from NodeManager)
	expectedAllNodes := nm.GetAllNodes()
	sortNodes(expectedAllNodes)

	// Active Validator nodes
	expectedActiveValidators := nm.GetActiveNodesByType(ValidatorNode)
	sortNodes(expectedActiveValidators)
	
	// All Validator nodes
	expectedAllValidators := nm.GetNodesByType(ValidatorNode)
	sortNodes(expectedAllValidators)

	// All Regular nodes
	expectedAllRegulars := nm.GetNodesByType(RegularNode)
	sortNodes(expectedAllRegulars)


	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		expectedNodes  []*NodeStatus
	}{
		{
			name:           "all nodes (no params)",
			path:           "/nodes",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedAllNodes,
		},
		{
			name:           "filter by type=validator",
			path:           "/nodes?type=validator",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedAllValidators,
		},
		{
			name:           "filter by type=regular",
			path:           "/nodes?type=regular",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedAllRegulars,
		},
		{
			name:           "filter by type=invalidtype",
			path:           "/nodes?type=invalidtype",
			expectedStatus: http.StatusOK, // Handler returns empty list for invalid type
			expectedNodes:  []*NodeStatus{},
		},
		{
			name:           "filter by requesterType=validator",
			path:           "/nodes?requesterType=validator",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedActiveValidators, // Expects only ACTIVE validators
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := http.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, res.StatusCode)
			}

			body, err := ioutil.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("Failed to read response body: %v", err)
			}

			var actualNodes []*NodeStatus
			if err := json.Unmarshal(body, &actualNodes); err != nil {
				t.Fatalf("Failed to unmarshal JSON response: %v. Body: %s", err, string(body))
			}
			sortNodes(actualNodes)

			if !reflect.DeepEqual(actualNodes, tc.expectedNodes) {
				t.Errorf("Expected nodes:\n%v\nGot nodes:\n%v", tc.expectedNodes, actualNodes)
				// For detailed diff:
				// for i := range actualNodes {
				//  if i < len(tc.expectedNodes) && !reflect.DeepEqual(actualNodes[i], tc.expectedNodes[i]) {
				//      t.Logf("Diff at index %d: Expected %v, Got %v", i, tc.expectedNodes[i], actualNodes[i])
				//  }
				// }
			}
		})
	}
}

// TestGetActiveNodesHandler tests the /nodes/active endpoint
func TestGetActiveNodesHandler(t *testing.T) {
	seedNode, nm, cleanup := setupSeedNodeForTest(t)
	defer cleanup()

	server := httptest.NewServer(seedNode.router)
	defer server.Close()

	// Expected nodes
	expectedAllActive := nm.GetActiveNodes()
	sortNodes(expectedAllActive)

	expectedActiveValidators := nm.GetActiveNodesByType(ValidatorNode)
	sortNodes(expectedActiveValidators)

	expectedActiveRegulars := nm.GetActiveNodesByType(RegularNode)
	sortNodes(expectedActiveRegulars)

	testCases := []struct {
		name           string
		path           string
		expectedStatus int
		expectedNodes  []*NodeStatus // nil if status is not OK
	}{
		{
			name:           "all active (no params)",
			path:           "/nodes/active",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedAllActive,
		},
		{
			name:           "active filtered by type=validator",
			path:           "/nodes/active?type=validator",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedActiveValidators,
		},
		{
			name:           "active filtered by type=regular",
			path:           "/nodes/active?type=regular",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedActiveRegulars,
		},
		{
			name:           "active filtered by type=invalidtype",
			path:           "/nodes/active?type=invalidtype",
			expectedStatus: http.StatusBadRequest, // Handler returns error for invalid type
			expectedNodes:  nil,                   // No body content expected for error
		},
		{
			name:           "active filtered by requesterType=validator",
			path:           "/nodes/active?requesterType=validator",
			expectedStatus: http.StatusOK,
			expectedNodes:  expectedActiveValidators, // Expects only ACTIVE validators
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := http.Get(server.URL + tc.path)
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, res.StatusCode)
			}

			if tc.expectedNodes != nil { // Only try to parse body if we expect a success
				body, err := ioutil.ReadAll(res.Body)
				if err != nil {
					t.Fatalf("Failed to read response body: %v", err)
				}

				var actualNodes []*NodeStatus
				if err := json.Unmarshal(body, &actualNodes); err != nil {
					t.Fatalf("Failed to unmarshal JSON response: %v. Body: %s", err, string(body))
				}
				sortNodes(actualNodes)

				if !reflect.DeepEqual(actualNodes, tc.expectedNodes) {
					t.Errorf("Expected nodes:\n%v\nGot nodes:\n%v", tc.expectedNodes, actualNodes)
				}
			}
		})
	}
}

// TestMain for setup/teardown if needed across package tests
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
