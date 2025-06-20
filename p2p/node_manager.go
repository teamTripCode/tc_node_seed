package p2p

import (
	"bytes"
	"encoding/json"
	"io"
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
// Notifica a otros nodos del mismo tipo sobre el nuevo nodo.
func (nm *NodeManager) AddNode(address string, nodeType NodeType) bool {
	nm.mutex.Lock()
	// Verificar si ya existe
	if node, exists := nm.nodes[address]; exists {
		// Actualizar última vez visto y tipo de nodo
		node.LastSeen = time.Now()
		node.NodeType = nodeType // Update node type
		nm.logger.Printf("Nodo %s ya existe, actualizado LastSeen y NodeType a %s", address, nodeType)

		// Persistir la actualización
		if nm.storage != nil {
			go nm.storage.SaveNode(node)
		}
		nm.mutex.Unlock() // Unlock before returning false
		return false
	}

	// Verificar límite máximo de nodos
	if len(nm.nodes) >= nm.config.GetMaxNodes() {
		nm.logger.Printf("Límite de nodos alcanzado (%d), no se puede añadir más", nm.config.GetMaxNodes())
		nm.mutex.Unlock() // Unlock before returning false
		return false
	}

	// Añadir nuevo nodo
	newNode := &NodeStatus{
		Address:      address,
		LastSeen:     time.Now(),
		IsResponding: true,     // Assume new nodes are responding initially
		NodeType:     nodeType, // Set node type
	}

	nm.nodes[address] = newNode
	nm.logger.Printf("Nodo añadido: %s, Tipo: %s", address, nodeType)

	// Persistir el nuevo nodo
	if nm.storage != nil {
		go nm.storage.SaveNode(newNode)
	}

	// Unlock mutex before potentially long-running operations (notifications)
	nm.mutex.Unlock()

	// Notificar a otros nodos del mismo tipo
	// Copiamos newNodeData para evitar problemas de concurrencia si newNode cambia.
	newNodeData := NodeUpdateNotification{
		Address:  newNode.Address,
		NodeType: newNode.NodeType,
	}

	// GetActiveNodesByType también necesita RLock, así que no puede estar bajo el Lock anterior.
	// Es importante llamar a GetActiveNodesByType después de desbloquear el mutex,
	// para evitar un deadlock, ya que GetActiveNodesByType también intenta adquirir un RLock.
	// Se obtiene la lista de nodos ANTES de iterar para evitar problemas si la lista cambia.
	// No notificamos al nodo recién añadido sobre sí mismo.
	peersToNotify := make([]*NodeStatus, 0)
	nm.mutex.RLock() // Read Lock para leer la lista de nodos de forma segura
	for _, peer := range nm.nodes {
		if peer.IsResponding && peer.NodeType == newNode.NodeType && peer.Address != newNode.Address {
			peersToNotify = append(peersToNotify, peer)
		}
	}
	nm.mutex.RUnlock()


	if len(peersToNotify) > 0 {
		nm.logger.Printf("Notificando a %d peers del tipo %s sobre el nuevo nodo %s", len(peersToNotify), newNode.NodeType, newNode.Address)
		for _, peer := range peersToNotify {
			// Hacemos una copia de peer.Address para evitar problemas de concurrencia si el peer cambia en el map.
			peerAddress := peer.Address
			go nm.sendNodeUpdateNotification(peerAddress, newNodeData)
		}
	}

	return true
}

// GetActiveNodes devuelve una lista de nodos activos
func (nm *NodeManager) GetActiveNodes() []*NodeStatus {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0)
	for _, status := range nm.nodes {
		if status.IsResponding {
			nodes = append(nodes, status)
		}
	}
	return nodes
}

// GetAllNodes devuelve todos los nodos conocidos
func (nm *NodeManager) GetAllNodes() []*NodeStatus {
	nm.mutex.RLock()
	defer nm.mutex.RUnlock()

	nodes := make([]*NodeStatus, 0, len(nm.nodes))
	for _, node := range nm.nodes {
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
	nodes := nm.GetAllNodes()
	nm.logger.Printf("Verificando estado de %d nodos", len(nodes))

	for _, nodeStatus := range nodes {
		isActive := nm.CheckNodeStatus(nodeStatus.Address)
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

// sendNodeUpdateNotification envía una notificación a un nodo específico sobre un nuevo par.
func (nm *NodeManager) sendNodeUpdateNotification(targetNodeAddress string, newNodeInfo NodeUpdateNotification) {
	nm.logger.Printf("Intentando notificar a %s sobre el nuevo nodo %s (Tipo: %s)", targetNodeAddress, newNodeInfo.Address, newNodeInfo.NodeType)

	// Construir la URL del endpoint de notificación en el nodo destino
	// TODO: Hacer que "/notify-new-peer" sea configurable si es necesario
	notificationURL := "http://" + targetNodeAddress + "/notify-new-peer"

	// Convertir la información del nuevo nodo a JSON
	payload, err := json.Marshal(newNodeInfo)
	if err != nil {
		nm.logger.Printf("Error al convertir a JSON la información del nuevo nodo para %s: %v", targetNodeAddress, err)
		return
	}

	// Crear cliente HTTP con timeout
	client := &http.Client{
		Timeout: 5 * time.Second, // Timeout para la solicitud
	}

	// Realizar la solicitud POST
	resp, err := client.Post(notificationURL, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		nm.logger.Printf("Error al enviar notificación a %s: %v", targetNodeAddress, err)
		// Aquí se podría implementar una lógica de reintento si fuera necesario
		return
	}
	defer resp.Body.Close()

	// Verificar el código de estado de la respuesta
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		nm.logger.Printf("Notificación enviada con éxito a %s. Estado: %s", targetNodeAddress, resp.Status)
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		nm.logger.Printf("Error al enviar notificación a %s. Estado: %s, Respuesta: %s", targetNodeAddress, resp.Status, string(bodyBytes))
	}
}

// statusText convierte un estado booleano a texto
func statusText(active bool) string {
	if active {
		return "activo"
	}
	return "inactivo"
}
