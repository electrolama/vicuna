package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

//go:embed web/*
var webAssets embed.FS

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	configPath := flag.String("config", "", "JSON configuration path (default: discover vicuna.json)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("vicuna %s\n", version)
		return
	}
	config, loadedConfigPath, err := loadDeploymentConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if loadedConfigPath != "" {
		log.Printf("loaded configuration from %s", loadedConfigPath)
	}

	static, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatal(err)
	}

	hub := newHub()
	manager := newSerialManager(hub)
	api := newAPIServer(manager, hub, http.FS(static), config)
	server := &http.Server{
		Addr:              *listen,
		Handler:           api.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       75 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdown
		manager.Disconnect()
		_ = server.Close()
	}()

	log.Printf("vicuna %s listening at http://%s", version, displayAddress(*listen))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func displayAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
