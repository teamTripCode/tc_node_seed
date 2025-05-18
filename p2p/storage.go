package p2p

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// DBStorage handles the persistence of node data using LevelDB
type DBStorage struct {
	db     *leveldb.DB
	path   string
	logger *log.Logger
}

// NewDBStorage creates a new LevelDB storage instance
func NewDBStorage(path string, logger *log.Logger) (*DBStorage, error) {
	options := &opt.Options{
		CompactionTableSize: 2 * opt.MiB,
		WriteBuffer:         4 * opt.MiB,
	}

	db, err := leveldb.OpenFile(path, options)
	if err != nil {
		return nil, fmt.Errorf("cannot open LevelDB at %s: %v", path, err)
	}

	return &DBStorage{
		db:     db,
		path:   path,
		logger: logger,
	}, nil
}

// Close closes the database connection
func (s *DBStorage) Close() error {
	return s.db.Close()
}

// SaveNode saves a node status to the database
func (s *DBStorage) SaveNode(node *NodeStatus) error {
	key := []byte("node:" + node.Address)

	// Serialize the node status
	value, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("error serializing node %s: %v", node.Address, err)
	}

	// Save to database
	if err := s.db.Put(key, value, nil); err != nil {
		return fmt.Errorf("error saving node %s to database: %v", node.Address, err)
	}

	s.logger.Printf("Node %s saved to database", node.Address)
	return nil
}

// LoadNode loads a node status from the database
func (s *DBStorage) LoadNode(address string) (*NodeStatus, error) {
	key := []byte("node:" + address)

	// Get from database
	data, err := s.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil // Node not found
		}
		return nil, fmt.Errorf("error loading node %s from database: %v", address, err)
	}

	// Deserialize the node status
	var node NodeStatus
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("error deserializing node %s: %v", address, err)
	}

	return &node, nil
}

// DeleteNode removes a node from the database
func (s *DBStorage) DeleteNode(address string) error {
	key := []byte("node:" + address)

	// Delete from database
	if err := s.db.Delete(key, nil); err != nil {
		return fmt.Errorf("error deleting node %s from database: %v", address, err)
	}

	s.logger.Printf("Node %s deleted from database", address)
	return nil
}

// LoadAllNodes loads all nodes from the database
func (s *DBStorage) LoadAllNodes() (map[string]*NodeStatus, error) {
	nodes := make(map[string]*NodeStatus)

	// Iterate through database entries with prefix "node:"
	iter := s.db.NewIterator(util.BytesPrefix([]byte("node:")), nil)
	defer iter.Release()

	for iter.Next() {
		// Get key and value
		key := iter.Key()
		value := iter.Value()

		// Extract address from key
		address := string(key)[5:] // Remove "node:" prefix

		// Deserialize node status
		var node NodeStatus
		if err := json.Unmarshal(value, &node); err != nil {
			s.logger.Printf("Error deserializing node %s: %v", address, err)
			continue
		}

		nodes[address] = &node
	}

	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("error iterating through nodes: %v", err)
	}

	s.logger.Printf("Loaded %d nodes from database", len(nodes))
	return nodes, nil
}

// SaveConfig saves the configuration to the database
func (s *DBStorage) SaveConfig(config *Config) error {
	key := []byte("config")

	// Serialize the config
	value, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("error serializing config: %v", err)
	}

	// Save to database
	if err := s.db.Put(key, value, nil); err != nil {
		return fmt.Errorf("error saving config to database: %v", err)
	}

	s.logger.Printf("Configuration saved to database")
	return nil
}

// LoadConfig loads the configuration from the database
func (s *DBStorage) LoadConfig() (*Config, error) {
	key := []byte("config")

	// Get from database
	data, err := s.db.Get(key, nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			return nil, nil // Config not found
		}
		return nil, fmt.Errorf("error loading config from database: %v", err)
	}

	// Deserialize the config
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("error deserializing config: %v", err)
	}

	return &config, nil
}

// StoreStatistic stores a statistic in the database
func (s *DBStorage) StoreStatistic(name string, value interface{}) error {
	key := []byte("stat:" + name + ":" + time.Now().Format(time.RFC3339))

	// Serialize the value
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("error serializing statistic %s: %v", name, err)
	}

	// Save to database
	if err := s.db.Put(key, data, nil); err != nil {
		return fmt.Errorf("error saving statistic %s to database: %v", name, err)
	}

	return nil
}
