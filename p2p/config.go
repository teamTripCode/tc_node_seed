package p2p

import (
	"encoding/json"
	"os"
	"sync"
)

// Config representa la configuración del nodo semilla
type Config struct {
	InitialNodes        []string     `json:"initialNodes"`        // Lista inicial de nodos conocidos
	MaxNodes            int          `json:"maxNodes"`            // Número máximo de nodos a almacenar
	ConfigFile          string       `json:"configFile"`          // Ruta al archivo de configuración
	DBPath              string       `json:"dbPath"`              // Ruta al directorio de la base de datos
	NodePruneInterval   int          `json:"nodePruneInterval"`   // Intervalo para eliminar nodos inactivos (en segundos)
	NodePruneAge        int          `json:"nodePruneAge"`        // Tiempo de inactividad para considerar un nodo para eliminación (en segundos)
	HealthCheckInterval int          `json:"healthCheckInterval"` // Intervalo para verificaciones de salud (en segundos)
	mutex               sync.RWMutex `json:"-"`                   // No serializar
}

// NewConfig crea una nueva configuración con valores por defecto
func NewConfig(configFile string) *Config {
	config := &Config{
		InitialNodes:        []string{},
		MaxNodes:            100, // Valor por defecto
		ConfigFile:          configFile,
		DBPath:              "data/leveldb", // Directorio por defecto para LevelDB
		NodePruneInterval:   7200,           // 2 horas en segundos
		NodePruneAge:        14400,          // 4 horas en segundos
		HealthCheckInterval: 30,             // 30 segundos
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

	// Asegurar que el directorio existe
	dir := dirPath(c.ConfigFile)
	if dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

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

// GetDBPath devuelve la ruta a la base de datos
func (c *Config) GetDBPath() string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.DBPath
}

// SetDBPath establece la ruta a la base de datos
func (c *Config) SetDBPath(path string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.DBPath = path
}

// GetNodePruneInterval devuelve el intervalo para eliminar nodos inactivos
func (c *Config) GetNodePruneInterval() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.NodePruneInterval
}

// GetNodePruneAge devuelve el tiempo de inactividad para considerar un nodo para eliminación
func (c *Config) GetNodePruneAge() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.NodePruneAge
}

// GetHealthCheckInterval devuelve el intervalo para verificaciones de salud
func (c *Config) GetHealthCheckInterval() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.HealthCheckInterval
}

// dirPath extrae el directorio de una ruta de archivo
func dirPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}
