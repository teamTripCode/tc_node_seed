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
	NodeType     NodeType  `json:"nodeType,omitempty"`
}

// NodeManager gestiona los nodos conocidos y su estado
type NodeManager struct {
	nodes   map[string]*NodeStatus
	config  *Config
	storage *DBStorage
	mutex   sync.RWMutex
	logger  *log.Logger
}

// NewNodeManager crea un nuevo gestor de nodos
func NewNodeManager(config *Config, storage *DBStorage, logger *log.Logger) *NodeManager {
	nm := &NodeManager{
		nodes:   make(map[string]*NodeStatus),
		config:  config,
		storage: storage,
		mutex:   sync.RWMutex{},
		logger:  logger,
	}

	// Cargar nodos desde la base de datos
	if storage != nil {
		nm.loadNodesFromStorage()
	}

	// Añadir nodos iniciales de la configuración si no existen ya
	for _, node := range config.GetInitialNodes() {
		nm.AddNode(node)
	}

	return nm
}

// loadNodesFromStorage carga todos los nodos desde el almacenamiento
func (nm *NodeManager) loadNodesFromStorage() {
	if nm.storage == nil {
		nm.logger.Println("No hay almacenamiento configurado, no se cargarán nodos")
		return
	}

	nodes, err := nm.storage.LoadAllNodes()
	if err != nil {
		nm.logger.Printf("Error cargando nodos desde almacenamiento: %v", err)
		return
	}

	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	for addr, node := range nodes {
		nm.nodes[addr] = node
		nm.logger.Printf("Nodo cargado desde almacenamiento: %s (última vez visto: %s)",
			addr, node.LastSeen.Format(time.RFC3339))
	}

	nm.logger.Printf("Cargados %d nodos desde almacenamiento", len(nodes))
}

// AddNode añade un nuevo nodo a la lista de nodos conocidos
// o actualiza su tipo si ya existe.
func (nm *NodeManager) AddNode(address string, nodeType NodeType) bool {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	// Verificar si ya existe
	if node, exists := nm.nodes[address]; exists {
		// Actualizar última vez visto y tipo de nodo
		node.LastSeen = time.Now()
		node.NodeType = nodeType // Update node type
		// Potentially add logic here if node type changes, e.g. re-validate or log
		nm.logger.Printf("Nodo %s ya existe, actualizado LastSeen y NodeType a %s", address, nodeType)

		// Persistir la actualización
		if nm.storage != nil {
			go nm.storage.SaveNode(node) // node already includes NodeType
		}

		// Consider returning true if updating is also a "successful" operation for the caller
		// For now, stick to false as per original logic for "already exists"
		return false
	}

	// Verificar límite máximo de nodos
	if len(nm.nodes) >= nm.config.GetMaxNodes() {
		nm.logger.Printf("Límite de nodos alcanzado (%d), no se puede añadir más", nm.config.GetMaxNodes())
		return false
	}

	// Añadir nuevo nodo
	newNode := &NodeStatus{
		Address:      address,
		LastSeen:     time.Now(),
		IsResponding: true, // Assume new nodes are responding initially
		NodeType:     nodeType, // Set node type
	}

	nm.nodes[address] = newNode
	nm.logger.Printf("Nodo añadido: %s, Tipo: %s", address, nodeType)

	// Persistir el nuevo nodo
	if nm.storage != nil {
		go nm.storage.SaveNode(newNode)
	}

	return true
}

// GetActiveNodes devuelve una lista de nodos activos
func (nm *NodeManager) GetActiveNodes() []*NodeStatus { // Return type changed
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0)
	for _, status := range nm.nodes { // Iterate over values (NodeStatus)
		if status.IsResponding {
			nodes = append(nodes, status)
		}
	}
	return nodes
}

// GetAllNodes devuelve todos los nodos conocidos
func (nm *NodeManager) GetAllNodes() []*NodeStatus { // Return type changed
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0, len(nm.nodes))
	for _, node := range nm.nodes { // Iterate over values (NodeStatus)
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNodesByType devuelve nodos de un tipo específico.
func (nm *NodeManager) GetNodesByType(nodeType NodeType) []*NodeStatus {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0)
	for _, status := range nm.nodes {
		if status.NodeType == nodeType {
			// Create a copy to avoid external modifications if necessary,
			// or return pointers directly if struct fields are not modified elsewhere.
			// For simplicity, returning direct pointers here.
			nodes = append(nodes, status)
		}
	}
	return nodes
}

// GetActiveNodesByType devuelve nodos activos de un tipo específico.
func (nm *NodeManager) GetActiveNodesByType(nodeType NodeType) []*NodeStatus {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0)
	for _, status := range nm.nodes {
		if status.IsResponding && status.NodeType == nodeType {
			nodes = append(nodes, status)
		}
	}
	return nodes
}

// CheckNodeStatus verifica si un nodo está activo
func (nm *NodeManager) CheckNodeStatus(address string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + address + "/ping")

	isResponding := false

	if err != nil {
		nm.mutex.Lock()
		if node, exists := nm.nodes[address]; exists {
			node.IsResponding = false

			// Persistir el cambio de estado
			if nm.storage != nil {
				go nm.storage.SaveNode(node)
			}
		}
		nm.mutex.Unlock()
	} else {
		defer resp.Body.Close()
		isResponding = resp.StatusCode == http.StatusOK

		nm.mutex.Lock()
		if node, exists := nm.nodes[address]; exists {
			node.LastSeen = time.Now()
			node.IsResponding = isResponding

			// Persistir el cambio de estado
			if nm.storage != nil {
				go nm.storage.SaveNode(node)
			}
		}
		nm.mutex.Unlock()
	}

	return isResponding
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
	nodes := nm.GetAllNodes() // This now returns []*NodeStatus
	nm.logger.Printf("Verificando estado de %d nodos", len(nodes))

	for _, nodeStatus := range nodes { // Iterate over NodeStatus objects
		isActive := nm.CheckNodeStatus(nodeStatus.Address) // Pass address to CheckNodeStatus
		nm.logger.Printf("Nodo %s (Tipo: %s) está %s", nodeStatus.Address, nodeStatus.NodeType, statusText(isActive))
	}

	// Registrar estadística
	if nm.storage != nil {
		stats := map[string]int{
			"total":  len(nodes),
			"active": len(nm.GetActiveNodes()),
		}

		if err := nm.storage.StoreStatistic("node_health_check", stats); err != nil {
			nm.logger.Printf("Error guardando estadísticas: %v", err)
		}
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
			// Eliminar del almacenamiento
			if nm.storage != nil {
				if err := nm.storage.DeleteNode(addr); err != nil {
					nm.logger.Printf("Error eliminando nodo %s del almacenamiento: %v", addr, err)
				}
			}

			// Eliminar de la memoria
			delete(nm.nodes, addr)
			pruned++
			nm.logger.Printf("Nodo eliminado por inactividad: %s", addr)
		}
	}

	return pruned
}

// SetNodeVersion establece la versión de un nodo
func (nm *NodeManager) SetNodeVersion(address string, version string) bool {
	nm.mutex.Lock()
	defer nm.mutex.Unlock()

	if node, exists := nm.nodes[address]; exists {
		node.Version = version

		// Persistir el cambio
		if nm.storage != nil {
			go nm.storage.SaveNode(node)
		}

		return true
	}

	return false
}

// GetNodeDetails devuelve información detallada de un nodo
func (nm *NodeManager) GetNodeDetails(address string) *NodeStatus {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	if node, exists := nm.nodes[address]; exists {
		// Crear una copia para evitar modificaciones externas
		copy := *node
		return &copy
	}

	return nil
}

// SaveState guarda el estado actual del gestor de nodos
func (nm *NodeManager) SaveState() error {
	if nm.storage == nil {
		nm.logger.Println("No hay almacenamiento configurado")
		return nil
	}

	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	// Guardar todos los nodos
	for _, node := range nm.nodes {
		if err := nm.storage.SaveNode(node); err != nil {
			return err
		}
	}

	// Guardar configuración
	return nm.storage.SaveConfig(nm.config)
}

// statusText convierte un estado booleano a texto
func statusText(active bool) string {
	if active {
		return "activo"
	}
	return "inactivo"
}
