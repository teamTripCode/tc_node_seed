package p2p

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// NodeStatus representa el estado de un nodo conocido
type NodeStatus struct {
	Address      string    `json:"address"`
	LastSeen     time.Time `json:"lastSeen"`
	IsResponding bool      `json:"isResponding"`
	Version      string    `json:"version,omitempty"`
}

// NodeManager gestiona los nodos conocidos y su estado
type NodeManager struct {
	nodes  map[string]*NodeStatus
	config *Config
	mutex  sync.RWMutex
	logger *log.Logger
}

// NewNodeManager crea un nuevo gestor de nodos
func NewNodeManager(config *Config, logger *log.Logger) *NodeManager {
	nm := &NodeManager{
		nodes:  make(map[string]*NodeStatus),
		config: config,
		mutex:  sync.RWMutex{},
		logger: logger,
	}

	// Añadir nodos iniciales de la configuración
	for _, node := range config.GetInitialNodes() {
		nm.AddNode(node)
	}

	return nm
}

// AddNode añade un nuevo nodo a la lista de nodos conocidos
func (nm *NodeManager) AddNode(address string) bool {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	// Verificar si ya existe
	if _, exists := nm.nodes[address]; exists {
		// Actualizar última vez visto
		nm.nodes[address].LastSeen = time.Now()
		return false
	}

	// Verificar límite máximo de nodos
	if len(nm.nodes) >= nm.config.GetMaxNodes() {
		nm.logger.Printf("Límite de nodos alcanzado (%d), no se puede añadir más", nm.config.GetMaxNodes())
		return false
	}

	// Añadir nuevo nodo
	nm.nodes[address] = &NodeStatus{
		Address:      address,
		LastSeen:     time.Now(),
		IsResponding: true,
	}

	nm.logger.Printf("Nodo añadido: %s", address)
	return true
}

// GetActiveNodes devuelve una lista de nodos activos
func (nm *NodeManager) GetActiveNodes() []string {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]string, 0)
	for addr, status := range nm.nodes {
		if status.IsResponding {
			nodes = append(nodes, addr)
		}
	}

	return nodes
}

// GetAllNodes devuelve todos los nodos conocidos
func (nm *NodeManager) GetAllNodes() []string {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]string, 0, len(nm.nodes))
	for addr := range nm.nodes {
		nodes = append(nodes, addr)
	}

	return nodes
}

// CheckNodeStatus verifica si un nodo está activo
func (nm *NodeManager) CheckNodeStatus(address string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + address + "/ping")

	if err != nil {
		nm.mutex.Lock()
		if node, exists := nm.nodes[address]; exists {
			node.IsResponding = false
		}
		nm.mutex.Unlock()
		return false
	}

	defer resp.Body.Close()

	nm.mutex.Lock()
	if node, exists := nm.nodes[address]; exists {
		node.LastSeen = time.Now()
		node.IsResponding = resp.StatusCode == http.StatusOK
	}
	nm.mutex.Unlock()

	return resp.StatusCode == http.StatusOK
}

// StartHealthCheck inicia verificaciones periódicas de salud de los nodos
func (nm *NodeManager) StartHealthCheck(interval time.Duration) {
	ticker := time.NewTicker(interval)

	go func() {
		for {
			<-ticker.C
			nm.CheckAllNodes()
		}
	}()
}

// CheckAllNodes verifica el estado de todos los nodos conocidos
func (nm *NodeManager) CheckAllNodes() {
	nodes := nm.GetAllNodes()
	nm.logger.Printf("Verificando estado de %d nodos", len(nodes))

	for _, node := range nodes {
		isActive := nm.CheckNodeStatus(node)
		nm.logger.Printf("Nodo %s está %s", node, statusText(isActive))
	}
}

// PruneNodes elimina nodos que no han respondido en un tiempo
func (nm *NodeManager) PruneNodes(maxAge time.Duration) int {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	now := time.Now()
	pruned := 0

	for addr, status := range nm.nodes {
		if !status.IsResponding && now.Sub(status.LastSeen) > maxAge {
			delete(nm.nodes, addr)
			pruned++
			nm.logger.Printf("Nodo eliminado por inactividad: %s", addr)
		}
	}

	return pruned
}

// statusText convierte un estado booleano a texto
func statusText(active bool) string {
	if active {
		return "activo"
	}
	return "inactivo"
}
