package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"tripcodechain_seed_node/p2p"
)

func main() {
	// Configure logger
	logger := log.New(os.Stdout, "[SEED] ", log.LstdFlags)

	// Parse command-line flags
	port := flag.Int("port", 3000, "Port on which the seed node will listen")
	dbPath := flag.String("db", "nodes.db", "Path to the LevelDB database")
	configPath := flag.String("config", "seed_config.json", "Path to the configuration file")
	dataDir := flag.String("datadir", "", "Directory for database and configuration files")
	checkInterval := flag.Duration("check", 30*time.Second, "Interval for node health checks")
	pruneInterval := flag.Duration("prune", 2*time.Hour, "Interval for pruning inactive nodes")
	pruneAge := flag.Duration("pruneage", 4*time.Hour, "Age after which inactive nodes are pruned")
	flag.Parse()

	// If data directory is specified, adjust paths
	if *dataDir != "" {
		// Create data directory if it doesn't exist
		if err := os.MkdirAll(*dataDir, 0755); err != nil {
			logger.Fatalf("Failed to create data directory: %v", err)
		}

		*dbPath = filepath.Join(*dataDir, *dbPath)
		*configPath = filepath.Join(*dataDir, *configPath)
	}

	logger.Printf("Starting seed node on port %d", *port)
	logger.Printf("Using database: %s", *dbPath)
	logger.Printf("Using config file: %s", *configPath)

	// Initialize the database storage
	storage, err := p2p.NewDBStorage(*dbPath, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize database storage: %v", err)
	}
	defer storage.Close()
	logger.Println("LevelDB storage initialized successfully")

	// Load or create configuration
	config := p2p.NewConfig(*configPath)

	// Try to load existing config from database
	dbConfig, err := storage.LoadConfig()
	if err != nil {
		logger.Printf("Warning: Could not load configuration from database: %v", err)
	} else if dbConfig != nil {
		logger.Println("Configuration loaded from database")
		config = dbConfig
	}

	// Create node manager
	nodeManager := p2p.NewNodeManager(config, storage, logger)

	// Create seed node instance
	seed := &p2p.SeedNode{
		ID:          "localhost:" + strconv.Itoa(*port),
		Port:        *port,
		router:      nil, // Will be initialized in Start()
		logger:      logger,
		config:      config,
		nodeManager: nodeManager,
	}

	// Set custom intervals if specified
	if *checkInterval > 0 {
		seed.SetHealthCheckInterval(*checkInterval)
	}

	if *pruneInterval > 0 && *pruneAge > 0 {
		seed.SetPruneSettings(*pruneInterval, *pruneAge)
	}

	// Start the seed node
	seed.Start()
	logger.Println("Seed node started successfully")

	// Periodically save the configuration to both file and database
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			logger.Println("Saving configuration...")

			// Save to file
			if err := config.Save(); err != nil {
				logger.Printf("Error saving configuration to file: %v", err)
			}

			// Save to database
			if err := storage.SaveConfig(config); err != nil {
				logger.Printf("Error saving configuration to database: %v", err)
			}
		}
	}()

	// Record startup statistic
	if err := storage.StoreStatistic("node_startup", map[string]interface{}{
		"time":       time.Now().Format(time.RFC3339),
		"port":       *port,
		"totalNodes": len(nodeManager.GetAllNodes()),
	}); err != nil {
		logger.Printf("Error storing startup statistic: %v", err)
	}

	// Wait for termination signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	logger.Println("Shutting down seed node...")

	// Save final state before exit
	if err := nodeManager.SaveState(); err != nil {
		logger.Printf("Error saving node manager state: %v", err)
	}

	// Record shutdown statistic
	if err := storage.StoreStatistic("node_shutdown", map[string]interface{}{
		"time":        time.Now().Format(time.RFC3339),
		"uptime":      time.Since(time.Now()).String(),
		"totalNodes":  len(nodeManager.GetAllNodes()),
		"activeNodes": len(nodeManager.GetActiveNodes()),
	}); err != nil {
		logger.Printf("Error storing shutdown statistic: %v", err)
	}
}
