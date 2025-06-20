package p2p

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// SeedNode representa un nodo semilla en la red blockchain
type SeedNode struct {
	ID                  string
	Port                int
	nodeManager         *NodeManager
	config              *Config
	router              *mux.Router
	logger              *log.Logger
	mutex               sync.RWMutex
	healthCheckInterval time.Duration
	pruneInterval       time.Duration
	pruneAge            time.Duration
	storage             *DBStorage
	stopChan            chan struct{}
}

// NodeRegistrationRequest defines the expected request body for node registration
type NodeRegistrationRequest struct {
	Address  string   `json:"address"`
	NodeType NodeType `json:"nodeType"`
}

// NewSeedNode crea una nueva instancia de nodo semilla
func NewSeedNode(port int, logger *log.Logger) *SeedNode {
	nodeID := fmt.Sprintf("localhost:%d", port)

	// Configuración por defecto
	config := NewConfig("seed_config.json")

	seed := &SeedNode{
		ID:                  nodeID,
		Port:                port,
		router:              mux.NewRouter(),
		logger:              logger,
		config:              config,
		healthCheckInterval: 30 * time.Second,
		pruneInterval:       2 * time.Hour,
		pruneAge:            4 * time.Hour,
		stopChan:            make(chan struct{}),
	}

	seed.setupRoutes()
	return seed
}

// SetHealthCheckInterval establece el intervalo para verificaciones de salud
func (s *SeedNode) SetHealthCheckInterval(interval time.Duration) {
	s.healthCheckInterval = interval
}

// SetPruneSettings establece la configuración para eliminar nodos inactivos
func (s *SeedNode) SetPruneSettings(interval, age time.Duration) {
	s.pruneInterval = interval
	s.pruneAge = age
}

// setupRoutes configura las rutas API del nodo semilla
func (s *SeedNode) setupRoutes() {
	s.router.HandleFunc("/nodes", s.getNodesHandler).Methods("GET")
	s.router.HandleFunc("/register", s.registerNodeHandler).Methods("POST")
	s.router.HandleFunc("/ping", s.pingHandler).Methods("GET")
	s.router.HandleFunc("/status", s.statusHandler).Methods("GET")
	s.router.HandleFunc("/nodes/active", s.getActiveNodesHandler).Methods("GET")

	// Ruta para que otros nodos notifiquen sobre nuevos pares
	s.router.HandleFunc("/notify-new-peer", s.notifyNewPeerHandler).Methods("POST")

	// Nuevas rutas para administración
	s.router.HandleFunc("/admin/statistics", s.getStatisticsHandler).Methods("GET")
	s.router.HandleFunc("/admin/config", s.getConfigHandler).Methods("GET")
	s.router.HandleFunc("/admin/config", s.updateConfigHandler).Methods("POST")
}

// Start inicia el servidor HTTP del nodo semilla
func (s *SeedNode) Start() {
	s.logger.Printf("Nodo semilla iniciando en puerto %d", s.Port)

	// Inicializar nodos por defecto
	s.initializeDefaultNodes()

	// Iniciar monitoreo de nodos
	go s.startHealthCheck()

	// Iniciar rutina para eliminar nodos inactivos
	go s.startNodePruning()

	// Configurar timeouts para el servidor
	srv := &http.Server{
		Handler:      s.router,
		Addr:         fmt.Sprintf(":%d", s.Port),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	// Iniciar servidor HTTP
	s.logger.Printf("Servidor HTTP iniciado en puerto %d", s.Port)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatalf("Error iniciando el servidor HTTP: %v", err)
		}
	}()
}

// initializeDefaultNodes inicializa los nodos por defecto en la red
func (s *SeedNode) initializeDefaultNodes() {
	// Definir nodos iniciales por defecto
	defaultNodes := map[string]NodeType{
		"localhost:3000": SeedsNode,     // Nodo semilla principal
		"localhost:3001": ValidatorNode, // Nodo validador inicial
		"localhost:3002": RegularNode,   // Nodo completo inicial
		"localhost:3003": APINode,       // Nodo API inicial
	}

	s.logger.Printf("Inicializando nodos por defecto...")

	for address, nodeType := range defaultNodes {
		// Solo agregar si no existe ya
		if s.nodeManager.GetNodeDetails(address) == nil {
			s.nodeManager.AddNode(address, nodeType)
			s.logger.Printf("Nodo por defecto agregado: %s (Tipo: %s)", address, nodeType)
		} else {
			s.logger.Printf("Nodo por defecto ya existe: %s", address)
		}
	}

	s.logger.Printf("Inicialización de nodos por defecto completada")
}

// Stop detiene todas las rutinas del nodo semilla
func (s *SeedNode) Stop() {
	close(s.stopChan)

	// Guardar estado antes de salir
	if err := s.nodeManager.SaveState(); err != nil {
		s.logger.Printf("Error guardando estado del gestor de nodos: %v", err)
	}

	// Cerrar almacenamiento
	if s.storage != nil {
		if err := s.storage.Close(); err != nil {
			s.logger.Printf("Error cerrando almacenamiento: %v", err)
		}
	}
}

// getNodesHandler devuelve la lista de nodos conocidos, con filtrado opcional
func (s *SeedNode) getNodesHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de lista de nodos desde %s, URI: %s", r.RemoteAddr, r.RequestURI)

	query := r.URL.Query()
	nodeTypeFilter := query.Get("type")
	requesterType := query.Get("requesterType")

	var nodesToReturn []*NodeStatus

	if NodeType(requesterType) == ValidatorNode {
		s.logger.Printf("Solicitud de un validador, devolviendo solo validadores activos.")
		nodesToReturn = s.nodeManager.GetActiveNodesByType(ValidatorNode)
	} else if nodeTypeFilter != "" {
		// Validate nodeTypeFilter
		validNodeType := false
		switch NodeType(nodeTypeFilter) {
		case ValidatorNode, RegularNode, APINode, SeedsNode:
			validNodeType = true
		}

		if validNodeType {
			s.logger.Printf("Filtrando nodos por tipo: %s", nodeTypeFilter)
			nodesToReturn = s.nodeManager.GetNodesByType(NodeType(nodeTypeFilter))
		} else {
			s.logger.Printf("Tipo de nodo inválido en el filtro: %s. Devolviendo lista vacía.", nodeTypeFilter)
			nodesToReturn = make([]*NodeStatus, 0)
		}
	} else {
		s.logger.Printf("Devolviendo todos los nodos.")
		nodesToReturn = s.nodeManager.GetAllNodes()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodesToReturn); err != nil {
		s.logger.Printf("Error codificando nodos a JSON: %v", err)
		http.Error(w, "Error procesando la solicitud", http.StatusInternalServerError)
	}
}

// getActiveNodesHandler devuelve solo los nodos activos, con filtrado opcional
func (s *SeedNode) getActiveNodesHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de lista de nodos activos desde %s, URI: %s", r.RemoteAddr, r.RequestURI)

	query := r.URL.Query()
	nodeTypeFilter := query.Get("type")
	requesterType := query.Get("requesterType")

	var activeNodesToReturn []*NodeStatus

	if NodeType(requesterType) == ValidatorNode {
		s.logger.Printf("Solicitud de un validador (para activos), devolviendo solo validadores activos.")
		activeNodesToReturn = s.nodeManager.GetActiveNodesByType(ValidatorNode)
	} else if nodeTypeFilter != "" {
		// Validate nodeTypeFilter
		if NodeType(nodeTypeFilter) != ValidatorNode && NodeType(nodeTypeFilter) != RegularNode &&
			NodeType(nodeTypeFilter) != APINode && NodeType(nodeTypeFilter) != SeedsNode {
			s.logger.Printf("Tipo de nodo inválido para filtro activo: %s", nodeTypeFilter)
			http.Error(w, "Tipo de nodo inválido para filtro", http.StatusBadRequest)
			return
		}
		s.logger.Printf("Filtrando nodos activos por tipo: %s", nodeTypeFilter)
		activeNodesToReturn = s.nodeManager.GetActiveNodesByType(NodeType(nodeTypeFilter))
	} else {
		s.logger.Printf("Devolviendo todos los nodos activos.")
		activeNodesToReturn = s.nodeManager.GetActiveNodes()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(activeNodesToReturn); err != nil {
		s.logger.Printf("Error codificando nodos activos a JSON: %v", err)
		http.Error(w, "Error procesando la solicitud", http.StatusInternalServerError)
	}
}

// registerNodeHandler registra un nuevo nodo en la red
func (s *SeedNode) registerNodeHandler(w http.ResponseWriter, r *http.Request) {
	var req NodeRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Printf("Error decodificando solicitud de registro: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Basic validation for NodeType
	if req.NodeType != ValidatorNode && req.NodeType != RegularNode &&
		req.NodeType != APINode && req.NodeType != SeedsNode {
		s.logger.Printf("Tipo de nodo inválido: %s", req.NodeType)
		http.Error(w, "Tipo de nodo inválido. Debe ser 'validator', 'regular', 'api' o 'seed'.", http.StatusBadRequest)
		return
	}

	if s.nodeManager.AddNode(req.Address, req.NodeType) {
		w.WriteHeader(http.StatusCreated)
		s.logger.Printf("Nodo registrado con éxito: %s, Tipo: %s", req.Address, req.NodeType)

		// Registrar estadística
		if s.storage != nil {
			stats := map[string]interface{}{
				"event":    "node_registered",
				"node":     req.Address,
				"nodeType": req.NodeType,
				"time":     time.Now().Format(time.RFC3339),
			}

			if err := s.storage.StoreStatistic("node_registration", stats); err != nil {
				s.logger.Printf("Error guardando estadística de registro: %v", err)
			}
		}
	} else {
		w.WriteHeader(http.StatusOK)
		s.logger.Printf("Nodo ya estaba registrado o límite alcanzado: %s", req.Address)
	}
}

// pingHandler responde a solicitudes de ping
func (s *SeedNode) pingHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Nodo semilla %s está activo", s.ID)
	s.logger.Printf("Ping recibido desde %s", r.RemoteAddr)
}

// statusHandler devuelve el estado del nodo semilla
func (s *SeedNode) statusHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de estado desde %s", r.RemoteAddr)

	status := map[string]interface{}{
		"id":          s.ID,
		"totalNodes":  len(s.nodeManager.GetAllNodes()),
		"activeNodes": len(s.nodeManager.GetActiveNodes()),
		"maxNodes":    s.config.GetMaxNodes(),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"uptime":      time.Since(time.Now()).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// getStatisticsHandler devuelve estadísticas almacenadas
func (s *SeedNode) getStatisticsHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de estadísticas desde %s", r.RemoteAddr)

	stats := map[string]any{
		"totalNodes":      len(s.nodeManager.GetAllNodes()),
		"activeNodes":     len(s.nodeManager.GetActiveNodes()),
		"nodesRatio":      float64(len(s.nodeManager.GetActiveNodes())) / float64(len(s.nodeManager.GetAllNodes())),
		"lastHealthCheck": time.Now().Add(-1 * time.Minute).Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// getConfigHandler devuelve la configuración actual
func (s *SeedNode) getConfigHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de configuración desde %s", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.config)
}

// updateConfigHandler actualiza la configuración
func (s *SeedNode) updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de actualización de configuración desde %s", r.RemoteAddr)

	var config struct {
		MaxNodes     *int      `json:"maxNodes,omitempty"`
		InitialNodes *[]string `json:"initialNodes,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, "Error decodificando configuración", http.StatusBadRequest)
		return
	}

	// Actualizar solo los campos proporcionados
	if config.MaxNodes != nil {
		s.config.SetMaxNodes(*config.MaxNodes)
	}

	if config.InitialNodes != nil {
		s.config.SetInitialNodes(*config.InitialNodes)
	}

	// Guardar configuración actualizada
	if err := s.config.Save(); err != nil {
		s.logger.Printf("Error guardando configuración: %v", err)
		http.Error(w, "Error guardando configuración", http.StatusInternalServerError)
		return
	}

	// Guardar en la base de datos
	if s.storage != nil {
		if err := s.storage.SaveConfig(s.config); err != nil {
			s.logger.Printf("Error guardando configuración en la base de datos: %v", err)
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Configuración actualizada correctamente")
}

// notifyNewPeerHandler maneja las notificaciones de nuevos nodos recibidas de otros pares.
func (s *SeedNode) notifyNewPeerHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida notificación de nuevo par desde %s", r.RemoteAddr)

	var notification NodeUpdateNotification
	if err := json.NewDecoder(r.Body).Decode(&notification); err != nil {
		s.logger.Printf("Error decodificando notificación de nuevo par: %v", err)
		http.Error(w, "Cuerpo de la solicitud inválido: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validación básica
	if notification.Address == "" {
		s.logger.Printf("Dirección de nodo faltante en la notificación de nuevo par.")
		http.Error(w, "Dirección de nodo faltante", http.StatusBadRequest)
		return
	}
	if notification.NodeType == "" { // Asumiendo que NodeType no puede ser vacío. Ajustar si es necesario.
		s.logger.Printf("Tipo de nodo faltante en la notificación de nuevo par para %s.", notification.Address)
		http.Error(w, "Tipo de nodo faltante", http.StatusBadRequest)
		return
	}
    // Validar NodeType
    switch notification.NodeType {
    case ValidatorNode, RegularNode, APINode, SeedsNode, FullNode: // Incluyendo FullNode por si acaso
        // Tipo de nodo válido
    default:
        s.logger.Printf("Tipo de nodo inválido '%s' en la notificación de nuevo par para %s.", notification.NodeType, notification.Address)
        http.Error(w, fmt.Sprintf("Tipo de nodo inválido: %s", notification.NodeType), http.StatusBadRequest)
        return
    }


	s.logger.Printf("Procesando notificación para añadir nodo: %s, Tipo: %s", notification.Address, notification.NodeType)

	// Añadir el nodo al gestor de nodos local.
	// AddNode se encarga de la lógica de si es nuevo o una actualización.
	// También maneja el límite de nodos.
	if s.nodeManager.AddNode(notification.Address, notification.NodeType) {
		// El nodo fue añadido exitosamente (era nuevo)
		s.logger.Printf("Nodo %s (Tipo: %s) añadido exitosamente a través de notificación.", notification.Address, notification.NodeType)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, "Nodo %s registrado/actualizado exitosamente.", notification.Address)
	} else {
		// El nodo ya existía (actualizado) o no se pudo añadir (límite alcanzado)
		// AddNode ya loguea estos casos.
		// Devolver OK implica que la solicitud fue procesada, incluso si el nodo ya existía.
		s.logger.Printf("Nodo %s (Tipo: %s) ya existía o no se pudo añadir (límite alcanzado) a través de notificación.", notification.Address, notification.NodeType)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Nodo %s procesado (ya existía o límite alcanzado).", notification.Address)

	}
}

// startHealthCheck inicia el monitoreo periódico de nodos
func (s *SeedNode) startHealthCheck() {
	ticker := time.NewTicker(s.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.nodeManager.CheckAllNodes()
		case <-s.stopChan:
			return
		}
	}
}

// startNodePruning inicia el proceso de eliminación de nodos inactivos
func (s *SeedNode) startNodePruning() {
	ticker := time.NewTicker(s.pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pruned := s.nodeManager.PruneNodes(s.pruneAge)
			s.logger.Printf("Eliminados %d nodos inactivos", pruned)

			// Guardar configuración actualizada
			if err := s.config.Save(); err != nil {
				s.logger.Printf("Error guardando configuración: %v", err)
			}

			// Guardar en la base de datos
			if s.storage != nil {
				if err := s.storage.SaveConfig(s.config); err != nil {
					s.logger.Printf("Error guardando configuración en la base de datos: %v", err)
				}
			}
		case <-s.stopChan:
			return
		}
	}
}

// GetKnownNodes devuelve una copia de la lista de nodos conocidos
func (s *SeedNode) GetKnownNodes() []*NodeStatus {
	return s.nodeManager.GetAllNodes()
}

// GetActiveNodes devuelve una copia de la lista de nodos activos
func (s *SeedNode) GetActiveNodes() []*NodeStatus {
	return s.nodeManager.GetActiveNodes()
}

// SetStorage sets the database storage for the seed node
func (s *SeedNode) SetStorage(storage *DBStorage) {
	s.storage = storage
	// Initialize node manager with the provided storage
	s.nodeManager = NewNodeManager(s.config, storage, s.logger)
}
