package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

func MultiRegistry() {
	// Parse command-line flags
	seedNode := flag.String("seed", "localhost:3000", "Seed node address")
	nodesRange := flag.String("nodes", "", "Range of node ports to register (e.g., 3001-3005) or comma-separated list (e.g., 3001,3002,3005)")
	baseHost := flag.String("host", "localhost", "Host name for the nodes")
	flag.Parse()

	if *nodesRange == "" {
		log.Fatal("Please provide node ports to register with -nodes flag (e.g., -nodes 3001-3005 or -nodes 3001,3002,3005)")
	}

	logger := log.New(log.Writer(), "[REGISTER] ", log.LstdFlags)
	logger.Printf("Registering nodes with seed node %s", *seedNode)

	// Parse the nodes range
	var ports []int
	if strings.Contains(*nodesRange, "-") {
		// Range format (e.g., 3001-3005)
		parts := strings.Split(*nodesRange, "-")
		if len(parts) != 2 {
			logger.Fatalf("Invalid range format. Expected format: start-end (e.g., 3001-3005)")
		}

		start, err := strconv.Atoi(parts[0])
		if err != nil {
			logger.Fatalf("Invalid start port: %v", err)
		}

		end, err := strconv.Atoi(parts[1])
		if err != nil {
			logger.Fatalf("Invalid end port: %v", err)
		}

		for port := start; port <= end; port++ {
			ports = append(ports, port)
		}
	} else if strings.Contains(*nodesRange, ",") {
		// List format (e.g., 3001,3002,3005)
		for _, portStr := range strings.Split(*nodesRange, ",") {
			port, err := strconv.Atoi(strings.TrimSpace(portStr))
			if err != nil {
				logger.Fatalf("Invalid port %s: %v", portStr, err)
			}
			ports = append(ports, port)
		}
	} else {
		// Single port
		port, err := strconv.Atoi(*nodesRange)
		if err != nil {
			logger.Fatalf("Invalid port %s: %v", *nodesRange, err)
		}
		ports = append(ports, port)
	}

	logger.Printf("Attempting to register %d nodes with ports: %v", len(ports), ports)

	// Check if seed node is available first
	client := &http.Client{Timeout: 3 * time.Second}
	pingURL := fmt.Sprintf("http://%s/ping", *seedNode)
	resp, err := client.Get(pingURL)
	if err != nil {
		logger.Fatalf("Failed to connect to seed node at %s: %v", *seedNode, err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Fatalf("Seed node returned non-OK status: %d", resp.StatusCode)
	}
	logger.Printf("Seed node is available")

	// Register all nodes in parallel
	var wg sync.WaitGroup
	nodesChan := make(chan string, len(ports))

	for _, port := range ports {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()
			nodeAddr := fmt.Sprintf("%s:%d", *baseHost, port)

			// Register the node
			nodeJSON, err := json.Marshal(nodeAddr)
			if err != nil {
				logger.Printf("Error marshaling node address %s: %v", nodeAddr, err)
				return
			}

			registerURL := fmt.Sprintf("http://%s/register", *seedNode)
			resp, err := client.Post(registerURL, "application/json", bytes.NewBuffer(nodeJSON))
			if err != nil {
				logger.Printf("Failed to register node %s: %v", nodeAddr, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusCreated {
				logger.Printf("Node %s registered successfully with status %d", nodeAddr, resp.StatusCode)
				nodesChan <- nodeAddr // Track successfully registered nodes
			} else if resp.StatusCode == http.StatusOK {
				logger.Printf("Node %s was already registered or limit reached with status %d", nodeAddr, resp.StatusCode)
				nodesChan <- nodeAddr // Also track these nodes
			} else {
				logger.Printf("Unexpected response code for node %s: %d", nodeAddr, resp.StatusCode)
			}
		}(port)
	}

	// Wait for all registration attempts to complete
	wg.Wait()
	close(nodesChan)

	// Collect registered nodes
	registeredNodes := make([]string, 0, len(ports))
	for node := range nodesChan {
		registeredNodes = append(registeredNodes, node)
	}

	// Verify nodes are in the list
	logger.Printf("Verifying registration for %d nodes...", len(registeredNodes))
	nodesURL := fmt.Sprintf("http://%s/nodes", *seedNode)
	resp, err = client.Get(nodesURL)
	if err != nil {
		logger.Fatalf("Failed to get nodes list: %v", err)
	}
	defer resp.Body.Close()

	var nodes []string
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil {
		logger.Fatalf("Error decoding nodes list: %v", err)
	}

	logger.Printf("Current nodes in seed node (%d total):", len(nodes))
	for _, node := range nodes {
		logger.Printf("- %s", node)
	}

	// Check how many of our registered nodes are in the list
	missingNodes := 0
	for _, registeredNode := range registeredNodes {
		found := slices.Contains(nodes, registeredNode)
		if !found {
			logger.Printf("Warning: Node %s was registered but NOT found in the nodes list", registeredNode)
			missingNodes++
		}
	}

	if missingNodes > 0 {
		logger.Printf("Warning: %d of %d registered nodes were not found in the nodes list", missingNodes, len(registeredNodes))
	} else {
		logger.Printf("Success! All %d registered nodes were found in the seed node's list", len(registeredNodes))
	}
}
