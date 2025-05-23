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

	// Inicializar gestor de nodos
	dbStorage, err := NewDBStorage("nodes.db", logger) // Ajusta el nombre del archivo si es necesario
	if err != nil {
		logger.Fatalf("Error inicializando DBStorage: %v", err)
	}
	seed.storage = dbStorage
	seed.nodeManager = NewNodeManager(config, dbStorage, logger)

	// Añadir este nodo a la lista
	seed.nodeManager.AddNode(nodeID)

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

	// Nuevas rutas para administración
	s.router.HandleFunc("/admin/statistics", s.getStatisticsHandler).Methods("GET")
	s.router.HandleFunc("/admin/config", s.getConfigHandler).Methods("GET")
	s.router.HandleFunc("/admin/config", s.updateConfigHandler).Methods("POST")
}

// Start inicia el servidor HTTP del nodo semilla
func (s *SeedNode) Start() {
	s.logger.Printf("Nodo semilla iniciando en puerto %d", s.Port)

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
	requesterType := query.Get("requesterType") // New parameter to identify requester

	var nodesToReturn []*NodeStatus // Uses the new return type from NodeManager

	if NodeType(requesterType) == ValidatorNode {
		s.logger.Printf("Solicitud de un validador, devolviendo solo validadores activos.")
		nodesToReturn = s.nodeManager.GetActiveNodesByType(ValidatorNode)
	} else if nodeTypeFilter != "" {
		// Validate nodeTypeFilter if necessary
		validNodeType := false
		switch NodeType(nodeTypeFilter) {
		case ValidatorNode, RegularNode:
			validNodeType = true
		}

		if validNodeType {
			s.logger.Printf("Filtrando nodos por tipo: %s", nodeTypeFilter)
			nodesToReturn = s.nodeManager.GetNodesByType(NodeType(nodeTypeFilter))
		} else {
			s.logger.Printf("Tipo de nodo inválido en el filtro: %s. Devolviendo lista vacía.", nodeTypeFilter)
			nodesToReturn = make([]*NodeStatus, 0) // Return empty list for invalid type
			// Alternatively, return http.StatusBadRequest
			// http.Error(w, fmt.Sprintf("Tipo de nodo inválido: %s", nodeTypeFilter), http.StatusBadRequest)
			// return
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
	requesterType := query.Get("requesterType") // New parameter to identify requester

	var activeNodesToReturn []*NodeStatus // Uses the new return type

	if NodeType(requesterType) == ValidatorNode {
		s.logger.Printf("Solicitud de un validador (para activos), devolviendo solo validadores activos.")
		activeNodesToReturn = s.nodeManager.GetActiveNodesByType(ValidatorNode)
	} else if nodeTypeFilter != "" {
		// Validate nodeTypeFilter
		if NodeType(nodeTypeFilter) != ValidatorNode && NodeType(nodeTypeFilter) != RegularNode {
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

// NodeRegistrationRequest defines the expected request body for node registration
type NodeRegistrationRequest struct {
	Address  string   `json:"address"`
	NodeType NodeType `json:"nodeType"`
}

// registerNodeHandler registra un nuevo nodo en la red
func (s *SeedNode) registerNodeHandler(w http.ResponseWriter, r *http.Request) {
	var req NodeRegistrationRequest // Use the new struct
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Printf("Error decodificando solicitud de registro: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Basic validation for NodeType (optional, but good practice)
	if req.NodeType != ValidatorNode && req.NodeType != RegularNode {
		s.logger.Printf("Tipo de nodo inválido: %s", req.NodeType)
		http.Error(w, "Tipo de nodo inválido. Debe ser 'validator' o 'regular'.", http.StatusBadRequest)
		return
	}

	// NOTE: s.nodeManager.AddNode will need to be updated to accept NodeType
	if s.nodeManager.AddNode(req.Address, req.NodeType) { // Pass NodeType here
		w.WriteHeader(http.StatusCreated)
		s.logger.Printf("Nodo registrado con éxito: %s, Tipo: %s", req.Address, req.NodeType)

		// Registrar estadística (consider adding nodeType to stats)
		if s.storage != nil {
			stats := map[string]interface{}{
				"event":    "node_registered",
				"node":     req.Address,
				"nodeType": req.NodeType, // Add nodeType to stats
				"time":     time.Now().Format(time.RFC3339),
			}

			if err := s.storage.StoreStatistic("node_registration", stats); err != nil {
				s.logger.Printf("Error guardando estadística de registro: %v", err)
			}
		}
	} else {
		w.WriteHeader(http.StatusOK) // Or perhaps a different status if it was already registered but type is different?
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

	// TODO: Implementar recuperación de estadísticas desde la base de datos
	// Esta es una implementación básica por ahora
	stats := map[string]interface{}{
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
func (s *SeedNode) GetKnownNodes() []*NodeStatus { // Updated return type
	return s.nodeManager.GetAllNodes()
}

// GetActiveNodes devuelve una copia de la lista de nodos activos
func (s *SeedNode) GetActiveNodes() []*NodeStatus { // Updated return type
	return s.nodeManager.GetActiveNodes()
}

// SetStorage sets the database storage for the seed node
func (s *SeedNode) SetStorage(storage *DBStorage) {
	s.storage = storage
	// Initialize node manager with the provided storage
	s.nodeManager = NewNodeManager(s.config, storage, s.logger)

	// Add this node to the list
	s.nodeManager.AddNode(s.ID)
}
