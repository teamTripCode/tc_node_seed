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
	ID          string
	Port        int
	nodeManager *NodeManager
	config      *Config
	router      *mux.Router
	logger      *log.Logger
	mutex       sync.RWMutex
}

// NewSeedNode crea una nueva instancia de nodo semilla
func NewSeedNode(port int, logger *log.Logger) *SeedNode {
	nodeID := fmt.Sprintf("localhost:%d", port)

	// Configuración por defecto
	config := NewConfig("seed_config.json")

	seed := &SeedNode{
		ID:     nodeID,
		Port:   port,
		router: mux.NewRouter(),
		logger: logger,
		config: config,
	}

	// Inicializar gestor de nodos
	dbStorage, err := NewDBStorage("nodes.db", logger) // Ajusta el nombre del archivo si es necesario
	if err != nil {
		logger.Fatalf("Error inicializando DBStorage: %v", err)
	}
	seed.nodeManager = NewNodeManager(config, dbStorage, logger)

	// Añadir este nodo a la lista
	seed.nodeManager.AddNode(nodeID)

	seed.setupRoutes()
	return seed
}

// setupRoutes configura las rutas API del nodo semilla
func (s *SeedNode) setupRoutes() {
	s.router.HandleFunc("/nodes", s.getNodesHandler).Methods("GET")
	s.router.HandleFunc("/register", s.registerNodeHandler).Methods("POST")
	s.router.HandleFunc("/ping", s.pingHandler).Methods("GET")
	s.router.HandleFunc("/status", s.statusHandler).Methods("GET")
	s.router.HandleFunc("/nodes/active", s.getActiveNodesHandler).Methods("GET")
}

// Start inicia el servidor HTTP del nodo semilla
func (s *SeedNode) Start() {
	s.logger.Printf("Nodo semilla iniciando en puerto %d", s.Port)

	// Iniciar monitoreo de nodos
	s.nodeManager.StartHealthCheck(30 * time.Second)

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
	if err := srv.ListenAndServe(); err != nil {
		s.logger.Fatalf("Error iniciando el servidor HTTP: %v", err)
	}
}

// getNodesHandler devuelve la lista de todos los nodos conocidos
func (s *SeedNode) getNodesHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de lista de nodos desde %s", r.RemoteAddr)

	nodes := s.nodeManager.GetAllNodes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// getActiveNodesHandler devuelve solo los nodos activos
func (s *SeedNode) getActiveNodesHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Recibida solicitud de lista de nodos activos desde %s", r.RemoteAddr)

	nodes := s.nodeManager.GetActiveNodes()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// registerNodeHandler registra un nuevo nodo en la red
func (s *SeedNode) registerNodeHandler(w http.ResponseWriter, r *http.Request) {
	var node string
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		s.logger.Printf("Error decodificando solicitud de registro: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.nodeManager.AddNode(node) {
		w.WriteHeader(http.StatusCreated)
		s.logger.Printf("Nodo registrado con éxito: %s", node)
	} else {
		w.WriteHeader(http.StatusOK)
		s.logger.Printf("Nodo ya estaba registrado o límite alcanzado: %s", node)
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
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// startNodePruning inicia el proceso de eliminación de nodos inactivos
func (s *SeedNode) startNodePruning() {
	ticker := time.NewTicker(2 * time.Hour)

	for {
		<-ticker.C
		pruned := s.nodeManager.PruneNodes(4 * time.Hour)
		s.logger.Printf("Eliminados %d nodos inactivos", pruned)

		// Guardar configuración actualizada
		if err := s.config.Save(); err != nil {
			s.logger.Printf("Error guardando configuración: %v", err)
		}
	}
}

// GetKnownNodes devuelve una copia de la lista de nodos conocidos
func (s *SeedNode) GetKnownNodes() []string {
	return s.nodeManager.GetAllNodes()
}
