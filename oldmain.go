package main

// import (
// 	"flag"
// 	"log"
// 	"os"
// 	"os/signal"
// 	"path/filepath"
// 	"syscall"
// 	"time"

// 	"tripcodechain_seed_node/p2p"
// )

// func main() {
// 	// Configure logger
// 	logger := log.New(os.Stdout, "[SEED] ", log.LstdFlags)

// 	// Parse command-line flags
// 	port := flag.Int("port", 3000, "Port on which the seed node will listen")
// 	dbPath := flag.String("db", "nodes.db", "Path to the LevelDB database")
// 	configPath := flag.String("config", "seed_config.json", "Path to the configuration file")
// 	dataDir := flag.String("datadir", "", "Directory for database and configuration files")
// 	checkInterval := flag.Duration("check", 30*time.Second, "Interval for node health checks")
// 	pruneInterval := flag.Duration("prune", 2*time.Hour, "Interval for pruning inactive nodes")
// 	pruneAge := flag.Duration("pruneage", 4*time.Hour, "Age after which inactive nodes are pruned")
// 	flag.Parse()

// 	// If data directory is specified, adjust paths
// 	if *dataDir != "" {
// 		// Create data directory if it doesn't exist
// 		if err := os.MkdirAll(*dataDir, 0755); err != nil {
// 			logger.Fatalf("Failed to create data directory: %v", err)
// 		}

// 		*dbPath = filepath.Join(*dataDir, *dbPath)
// 		*configPath = filepath.Join(*dataDir, *configPath)
// 	}

// 	logger.Printf("Starting seed node on port %d", *port)
// 	logger.Printf("Using database: %s", *dbPath)
// 	logger.Printf("Using config file: %s", *configPath)

// 	// Initialize the database storage
// 	storage, err := p2p.NewDBStorage(*dbPath, logger)
// 	if err != nil {
// 		logger.Fatalf("Failed to initialize database storage: %v", err)
// 	}
// 	defer storage.Close()
// 	logger.Println("LevelDB storage initialized successfully")

// 	// Create a new seed node using the provided constructor
// 	seed := p2p.NewSeedNode(*port, logger)

// 	// Set the storage for the seed node
// 	seed.SetStorage(storage)

// 	// Set custom intervals if specified
// 	if *checkInterval > 0 {
// 		seed.SetHealthCheckInterval(*checkInterval)
// 	}

// 	if *pruneInterval > 0 && *pruneAge > 0 {
// 		seed.SetPruneSettings(*pruneInterval, *pruneAge)
// 	}

// 	// Start the seed node
// 	seed.Start()
// 	logger.Println("Seed node started successfully")

// 	// Record startup statistic
// 	nodes, _ := storage.LoadAllNodes()
// 	if err := storage.StoreStatistic("node_startup", map[string]any{
// 		"time":       time.Now().Format(time.RFC3339),
// 		"port":       *port,
// 		"totalNodes": len(nodes),
// 	}); err != nil {
// 		logger.Printf("Error storing startup statistic: %v", err)
// 	}

// 	// Wait for termination signal
// 	c := make(chan os.Signal, 1)
// 	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
// 	<-c

// 	logger.Println("Shutting down seed node...")
// 	seed.Stop()

// 	// Record shutdown statistic
// 	nodeStats, err := storage.LoadAllNodes()
// 	activeNodes := 0
// 	for _, node := range nodeStats {
// 		if node.IsResponding {
// 			activeNodes++
// 		}
// 	}

// 	if err := storage.StoreStatistic("node_shutdown", map[string]interface{}{
// 		"time":        time.Now().Format(time.RFC3339),
// 		"uptime":      time.Since(time.Now()).String(),
// 		"totalNodes":  len(nodeStats),
// 		"activeNodes": activeNodes,
// 	}); err != nil {
// 		logger.Printf("Error storing shutdown statistic: %v", err)
// 	}
// }
