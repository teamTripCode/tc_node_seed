package p2p

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// MockNodeManager is a mock implementation of NodeManager for testing server handlers.
type MockNodeManager struct {
	mutex          sync.RWMutex
	nodes          map[string]*NodeStatus
	addNodeCalled  bool
	addNodeAddr    string
	addNodeTyp     NodeType
	addNodeReturn  bool // What AddNode should return
	addNodeCount   int
	logger         *log.Logger
	config         *Config // Add a config field
	storage        *DBStorage // Add a storage field
}

func NewMockNodeManager(logger *log.Logger, config *Config) *MockNodeManager {
	return &MockNodeManager{
		nodes:  make(map[string]*NodeStatus),
		logger: logger,
		config: config, // Store the config
	}
}

// Methods to satisfy the implicit interface NodeManager provides to SeedNode
// We only need to fully mock methods used by the handlers being tested.

func (m *MockNodeManager) AddNode(address string, nodeType NodeType) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.addNodeCalled = true
	m.addNodeAddr = address
	m.addNodeTyp = nodeType
	m.addNodeCount++

	// Store the node if it's a new one, or update if existing (simplified)
	if _, exists := m.nodes[address]; !exists && m.addNodeReturn {
		m.nodes[address] = &NodeStatus{Address: address, NodeType: nodeType, IsResponding: true, LastSeen: time.Now()}
	} else if exists { // if it exists, AddNode would update it and return false by default
        // no specific action needed here for mock, just return configured value
	}
	return m.addNodeReturn
}

func (m *MockNodeManager) GetNodeDetails(address string) *NodeStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if node, exists := m.nodes[address]; exists {
		cpy := *node // return a copy
		return &cpy
	}
	return nil
}

// GetAllNodes returns all nodes. Used by statusHandler, getNodesHandler.
func (m *MockNodeManager) GetAllNodes() []*NodeStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	nodes := make([]*NodeStatus, 0, len(m.nodes))
	for _, node := range m.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetActiveNodes returns active nodes. Used by statusHandler, getActiveNodesHandler.
func (m *MockNodeManager) GetActiveNodes() []*NodeStatus {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	activeNodes := make([]*NodeStatus, 0)
	for _, node := range m.nodes {
		if node.IsResponding {
			activeNodes = append(activeNodes, node)
		}
	}
	return activeNodes
}

// Reset can be used to clear mock state between tests
func (m *MockNodeManager) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.addNodeCalled = false
	m.addNodeAddr = ""
	m.addNodeTyp = ""
	m.addNodeCount = 0
	m.nodes = make(map[string]*NodeStatus)
}

// --- Unused methods by notifyNewPeerHandler, but part of NodeManager ---
// We add them as stubs to make MockNodeManager more generally usable if SeedNode calls them.
func (m *MockNodeManager) loadNodesFromStorage() {}
func (m *MockNodeManager) GetNodesByType(nodeType NodeType) []*NodeStatus { return nil }
func (m *MockNodeManager) GetActiveNodesByType(nodeType NodeType) []*NodeStatus { return nil }
func (m *MockNodeManager) CheckNodeStatus(address string) bool { return false }
func (m *MockNodeManager) StartHealthCheck(interval time.Duration) {}
func (m *MockNodeManager) CheckAllNodes() {}
func (m *MockNodeManager) PruneNodes(maxAge time.Duration) int { return 0 }
func (m *MockNodeManager) SetNodeVersion(address string, version string) bool { return false }
func (m *MockNodeManager) SaveState() error { return nil }
// sendNodeUpdateNotification is not called by server handlers, it's internal to NodeManager
// func (m *MockNodeManager) sendNodeUpdateNotification(targetNodeAddress string, newNodeInfo NodeUpdateNotification) {}


// Helper to create a SeedNode with a MockNodeManager for testing
func newTestSeedNode(mockNM *MockNodeManager) *SeedNode {
	logger := log.New(os.Stdout, "TestSeedNode: ", log.LstdFlags)
	// Create a minimal config for the SeedNode.
	// The SeedNode constructor (NewSeedNode) creates its own config.
	// To inject a mock manager, we construct SeedNode manually.

	// Ensure mockNM has a config if it needs one for methods SeedNode might call
	if mockNM.config == nil {
		mockNM.config = NewConfig("mock_nm_config.json") // or some test default
		mockNM.config.SetMaxNodes(5) // example
	}

	sn := &SeedNode{
		ID:          "test-seed",
		Port:        8080, // Dummy port
		nodeManager: mockNM, // This is the key part for injecting the mock
		logger:      logger,
		router:      mux.NewRouter(), // Critical: router must be initialized
		config:      mockNM.config, // Use config from mockNM or a new test one
		stopChan:    make(chan struct{}),
	}
	// We need to explicitly call setupRoutes for the test seed node
	sn.setupRoutes()
	return sn
}


func TestNotifyNewPeerHandler_Success_NewNode(t *testing.T) {
	mockNM := NewMockNodeManager(log.New(os.Stdout, "MockNM: ", log.LstdFlags), NewConfig("test_config.json"))
	seedNode := newTestSeedNode(mockNM) // router is setup inside here

	mockNM.addNodeReturn = true // Simulate new node successfully added

	notificationPayload := NodeUpdateNotification{
		Address:  "10.0.0.1:8001",
		NodeType: FullNode,
	}
	payloadBytes, _ := json.Marshal(notificationPayload)
	req := httptest.NewRequest(http.MethodPost, "/notify-new-peer", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	// Use the SeedNode's router to serve the HTTP request
	seedNode.router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusCreated)
	}

	mockNM.mutex.RLock()
	if !mockNM.addNodeCalled {
		t.Error("expected NodeManager.AddNode to be called, but it wasn't")
	}
	if mockNM.addNodeAddr != notificationPayload.Address {
		t.Errorf("NodeManager.AddNode called with wrong address: got %s want %s", mockNM.addNodeAddr, notificationPayload.Address)
	}
	if mockNM.addNodeTyp != notificationPayload.NodeType {
		t.Errorf("NodeManager.AddNode called with wrong type: got %s want %s", mockNM.addNodeTyp, notificationPayload.NodeType)
	}
	mockNM.mutex.RUnlock()

	expectedResponseFragment := "registrado/actualizado exitosamente"
	if !strings.Contains(rr.Body.String(), expectedResponseFragment) {
		t.Errorf("handler response body = %s; want to contain = %s", rr.Body.String(), expectedResponseFragment)
	}
}

func TestNotifyNewPeerHandler_Success_ExistingNode(t *testing.T) {
	mockNM := NewMockNodeManager(log.New(os.Stdout, "MockNM: ", log.LstdFlags), NewConfig("test_config.json"))
	seedNode := newTestSeedNode(mockNM)

	mockNM.addNodeReturn = false // Simulate existing node (or limit reached)

	notificationPayload := NodeUpdateNotification{
		Address:  "10.0.0.2:8002",
		NodeType: ValidatorNode,
	}
	payloadBytes, _ := json.Marshal(notificationPayload)
	req := httptest.NewRequest(http.MethodPost, "/notify-new-peer", bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	seedNode.router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	mockNM.mutex.RLock()
	if !mockNM.addNodeCalled {
		t.Error("expected NodeManager.AddNode to be called, but it wasn't")
	}
	if mockNM.addNodeAddr != notificationPayload.Address {
		t.Errorf("NodeManager.AddNode called with wrong address: got %s want %s", mockNM.addNodeAddr, notificationPayload.Address)
	}
	if mockNM.addNodeTyp != notificationPayload.NodeType {
		t.Errorf("NodeManager.AddNode called with wrong type: got %s want %s", mockNM.addNodeTyp, notificationPayload.NodeType)
	}
	mockNM.mutex.RUnlock()

	expectedResponseFragment := "procesado (ya existía o límite alcanzado)"
	if !strings.Contains(rr.Body.String(), expectedResponseFragment) {
		t.Errorf("handler response body = %s; want to contain = %s", rr.Body.String(), expectedResponseFragment)
	}
}

func TestNotifyNewPeerHandler_BadRequests(t *testing.T) {
	mockNM := NewMockNodeManager(log.New(os.Stdout, "MockNM: ", log.LstdFlags), NewConfig("test_config.json"))
	seedNode := newTestSeedNode(mockNM)

	tests := []struct {
		name             string
		payload          string
		expectedStatus   int
		expectAddNodeCall bool
		expectedErrorMsg string
	}{
		{
			name:             "Invalid JSON",
			payload:          `{"address": "1.2.3.4:8000", "nodeType": "full"`, // Malformed
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Cuerpo de la solicitud inválido",
		},
		{
			name:             "Missing Address",
			payload:          `{"nodeType": "full"}`,
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Dirección de nodo faltante",
		},
		{
			name:             "Missing NodeType",
			payload:          `{"address": "1.2.3.4:8000"}`,
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Tipo de nodo faltante",
		},
		{
			name:             "Invalid NodeType",
			payload:          `{"address": "1.2.3.4:8000", "nodeType": "invalidtype"}`,
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Tipo de nodo inválido: invalidtype",
		},
		{
			name:             "Empty Address",
			payload:          `{"address": "", "nodeType": "full"}`,
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Dirección de nodo faltante",
		},
		{
			name:             "Empty NodeType",
			payload:          `{"address": "1.2.3.4:8000", "nodeType": ""}`,
			expectedStatus:   http.StatusBadRequest,
			expectAddNodeCall: false,
			expectedErrorMsg: "Tipo de nodo faltante",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockNM.Reset() // Reset mock before each test case

			req := httptest.NewRequest(http.MethodPost, "/notify-new-peer", strings.NewReader(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			seedNode.router.ServeHTTP(rr, req)

			if status := rr.Code; status != tc.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v. Body: %s", status, tc.expectedStatus, rr.Body.String())
			}

			if tc.expectAddNodeCall && !mockNM.addNodeCalled {
				t.Error("expected NodeManager.AddNode to be called, but it wasn't")
			}
			if !tc.expectAddNodeCall && mockNM.addNodeCalled {
				t.Error("expected NodeManager.AddNode NOT to be called, but it was")
			}

			if tc.expectedErrorMsg != "" && !strings.Contains(rr.Body.String(), tc.expectedErrorMsg) {
				t.Errorf("response body '%s' does not contain expected error message '%s'", rr.Body.String(), tc.expectedErrorMsg)
			}
		})
	}
}
