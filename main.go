package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"tripcodechain_seed_node/p2p"
)

func main() {
	// Configurar logger
	logger := log.New(os.Stdout, "[SEED] ", log.LstdFlags)

	// Parseando flags de línea de comandos
	port := flag.Int("port", 3000, "Puerto en el que escuchará el nodo semilla")
	flag.Parse()

	logger.Printf("Iniciando nodo semilla en puerto %d", *port)

	// Crear instancia de nodo semilla
	seed := p2p.NewSeedNode(*port, logger)

	// Iniciar el nodo semilla
	seed.Start()
	logger.Println("Nodo semilla iniciado correctamente")

	// Esperar señal para terminar
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	logger.Println("Cerrando nodo semilla...")
}
