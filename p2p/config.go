package p2p

import (
	"encoding/json"
	"os"
	"sync"
)

// Config representa la configuración del nodo semilla
type Config struct {
	InitialNodes []string `json:"initialNodes"` // Lista inicial de nodos conocidos
	MaxNodes     int      `json:"maxNodes"`     // Número máximo de nodos a almacenar
	ConfigFile   string   `json:"configFile"`   // Ruta al archivo de configuración
	mutex        sync.RWMutex
}

// NewConfig crea una nueva configuración con valores por defecto
func NewConfig(configFile string) *Config {
	config := &Config{
		InitialNodes: []string{},
		MaxNodes:     100, // Valor por defecto
		ConfigFile:   configFile,
	}

	// Cargar desde archivo si existe
	if _, err := os.Stat(configFile); err == nil {
		config.Load()
	}

	return config
}

// Load carga la configuración desde un archivo
func (c *Config) Load() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	file, err := os.Open(c.ConfigFile)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	return decoder.Decode(c)
}

// Save guarda la configuración en un archivo
func (c *Config) Save() error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	file, err := os.Create(c.ConfigFile)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

// GetInitialNodes devuelve una copia de los nodos iniciales
func (c *Config) GetInitialNodes() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	nodes := make([]string, len(c.InitialNodes))
	copy(nodes, c.InitialNodes)
	return nodes
}

// SetInitialNodes establece los nodos iniciales
func (c *Config) SetInitialNodes(nodes []string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.InitialNodes = make([]string, len(nodes))
	copy(c.InitialNodes, nodes)
}

// GetMaxNodes devuelve el número máximo de nodos
func (c *Config) GetMaxNodes() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.MaxNodes
}

// SetMaxNodes establece el número máximo de nodos
func (c *Config) SetMaxNodes(max int) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.MaxNodes = max
}
