package p2p

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestConfig creates a default configuration for testing.
func newTestConfig() *Config {
	// In a real scenario, this might load from a test file or use default values.
	// For now, let's assume NewConfig can handle an empty path for defaults or creates a memory-only config.
	// If NewConfig actually tries to load "test_config.json", this needs to be created or mocked.
	// Based on current p2p/config.go, NewConfig("path") creates a new config and sets the path.
	// It doesn't auto-load. So, this should be fine for creating an in-memory config.
	cfg := NewConfig("test_config.json") // Path won't be used for saving/loading in these tests
	cfg.SetMaxNodes(10)                  // Default max nodes for tests
	return cfg
}

// newTestNodeManager creates a NodeManager with a test logger and config.
// storage is nil for these tests.
func newTestNodeManager(config *Config) *NodeManager {
	if config == nil {
		config = newTestConfig()
	}
	logger := log.New(os.Stdout, "TestNodeManager: ", log.LstdFlags)
	// DBStorage is nil as we are not testing storage interactions here
	return NewNodeManager(config, nil, logger)
}

func TestSendNodeUpdateNotification_Success(t *testing.T) {
	expectedNotification := NodeUpdateNotification{
		Address:  "1.2.3.4:8000",
		NodeType: FullNode,
	}
	var receivedNotification NodeUpdateNotification
	var requestReceived bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived = true
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/notify-new-peer" {
			t.Errorf("Expected path /notify-new-peer, got %s", r.URL.Path)
		}
		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type application/json, got %s", contentType)
		}

		err := json.NewDecoder(r.Body).Decode(&receivedNotification)
		if err != nil {
			t.Fatalf("Error decoding request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated) // Or http.StatusOK
	}))
	defer server.Close()

	nm := newTestNodeManager(nil)
	// The target address must be the mock server's address.
	// httptest.Server.URL is "http://127.0.0.1:XXXX", so we need to strip "http://"
	targetAddress := strings.TrimPrefix(server.URL, "http://")

	nm.sendNodeUpdateNotification(targetAddress, expectedNotification)

	if !requestReceived {
		t.Fatal("Mock server did not receive a request")
	}
	if receivedNotification.Address != expectedNotification.Address {
		t.Errorf("Expected address %s, got %s", expectedNotification.Address, receivedNotification.Address)
	}
	if receivedNotification.NodeType != expectedNotification.NodeType {
		t.Errorf("Expected node type %s, got %s", expectedNotification.NodeType, receivedNotification.NodeType)
	}
}

func TestSendNodeUpdateNotification_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	// Capture log output
	var logOutput bytes.Buffer
	logger := log.New(&logOutput, "TestServerError: ", log.LstdFlags)

	// Create a config and then the NodeManager with the custom logger
	config := newTestConfig()
	nm := NewNodeManager(config, nil, logger) // Use the custom logger

	notification := NodeUpdateNotification{Address: "1.2.3.4:8000", NodeType: FullNode}
	targetAddress := strings.TrimPrefix(server.URL, "http://")
	nm.sendNodeUpdateNotification(targetAddress, notification)

	// Check if the log contains an error message
	// This is a basic check. More sophisticated log checking might be needed in complex scenarios.
	if !strings.Contains(logOutput.String(), "Error al enviar notificación") {
		t.Errorf("Expected log output to contain 'Error al enviar notificación', got: %s", logOutput.String())
	}
}

func TestAddNode_NotificationDispatch(t *testing.T) {
	nmLogger := log.New(os.Stdout, "TestAddNodeDispatch: ", log.LstdFlags)
	nmConfig := newTestConfig()
	nm := NewNodeManager(nmConfig, nil, nmLogger)

	newNode := NodeUpdateNotification{Address: "10.0.0.1:8000", NodeType: FullNode}

	// Map to track if a server was called and what it received
	serverHit := make(map[string]bool)
	receivedData := make(map[string]NodeUpdateNotification)
	var serverMutex sync.Mutex // To protect shared maps

	handlerFunc := func(serverAddr string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			serverMutex.Lock()
			defer serverMutex.Unlock()

			serverHit[serverAddr] = true
			var notification NodeUpdateNotification
			if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				t.Logf("Error decoding at mock server %s: %v", serverAddr, err)
				return
			}
			receivedData[serverAddr] = notification
			w.WriteHeader(http.StatusOK)
		}
	}

	// Peer 1: Same type, should be notified
	peer1Addr := "127.0.0.1:5001"
	server1 := httptest.NewServer(handlerFunc(peer1Addr))
	defer server1.Close()
	nm.AddNode(strings.TrimPrefix(server1.URL, "http://"), FullNode) // AddNode expects address without http://

	// Peer 2: Different type, should NOT be notified
	peer2Addr := "127.0.0.1:5002"
	server2 := httptest.NewServer(handlerFunc(peer2Addr))
	defer server2.Close()
	nm.AddNode(strings.TrimPrefix(server2.URL, "http://"), ValidatorNode)

	// Peer 3: Same type, but marked as not responding, should NOT be notified
	peer3Addr := "127.0.0.1:5003"
	server3 := httptest.NewServer(handlerFunc(peer3Addr))
	defer server3.Close()
	nm.AddNode(strings.TrimPrefix(server3.URL, "http://"), FullNode)
	// Manually mark as not responding (NodeManager's internal state)
	nm.mutex.Lock()
	if node, ok := nm.nodes[strings.TrimPrefix(server3.URL, "http://")]; ok {
		node.IsResponding = false
	}
	nm.mutex.Unlock()

	// Peer 4: Same type, should be notified
	peer4Addr := "127.0.0.1:5004"
	server4 := httptest.NewServer(handlerFunc(peer4Addr))
	defer server4.Close()
	nm.AddNode(strings.TrimPrefix(server4.URL, "http://"), FullNode)

	// Add the new node that should trigger notifications
	nm.AddNode(newNode.Address, newNode.NodeType)

	// Wait for async notifications to be processed.
	// This is a common challenge in testing async operations.
	// A more robust solution might involve channels or other synchronization primitives
	// if the sendNodeUpdateNotification provided such mechanisms.
	time.Sleep(100 * time.Millisecond) // Adjust as necessary

	serverMutex.Lock()
	defer serverMutex.Unlock()

	// Check Peer 1 (should be hit)
	if !serverHit[strings.TrimPrefix(server1.URL, "http://")] {
		t.Errorf("Peer 1 (%s) of same type was not notified", peer1Addr)
	} else {
		if rcv, ok := receivedData[strings.TrimPrefix(server1.URL, "http://")]; ok {
			if rcv.Address != newNode.Address || rcv.NodeType != newNode.NodeType {
				t.Errorf("Peer 1 received wrong data: %+v, expected %+v", rcv, newNode)
			}
		} else {
			t.Errorf("Peer 1 hit but no data recorded")
		}
	}

	// Check Peer 2 (should NOT be hit)
	if serverHit[strings.TrimPrefix(server2.URL, "http://")] {
		t.Errorf("Peer 2 (%s) of different type was notified", peer2Addr)
	}

	// Check Peer 3 (should NOT be hit because not responding)
	if serverHit[strings.TrimPrefix(server3.URL, "http://")] {
		t.Errorf("Peer 3 (%s) (not responding) was notified", peer3Addr)
	}

	// Check Peer 4 (should be hit)
	if !serverHit[strings.TrimPrefix(server4.URL, "http://")] {
		t.Errorf("Peer 4 (%s) of same type was not notified", peer4Addr)
	} else {
		if rcv, ok := receivedData[strings.TrimPrefix(server4.URL, "http://")]; ok {
			if rcv.Address != newNode.Address || rcv.NodeType != newNode.NodeType {
				t.Errorf("Peer 4 received wrong data: %+v, expected %+v", rcv, newNode)
			}
		} else {
			t.Errorf("Peer 4 hit but no data recorded")
		}
	}

	// Also, the new node itself should not be in the serverHit map as a target of notification
	if serverHit[newNode.Address] {
		t.Errorf("The new node %s was notified about itself", newNode.Address)
	}
}

// TODO: Add more tests for NodeManager if necessary, e.g. edge cases for AddNode, etc.
// For now, focusing on notification aspects.
