package main

import (
	"flag"
	"log"
	"runtime"

	"github.com/numbleroot/pluto/config"
	"github.com/numbleroot/pluto/server"
)

// Functions

func main() {

	// Set CPUs usable by pluto to all available.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Parse command-line flag that defines a config path.
	configFlag := flag.String("config", "config.toml", "Provide path to configuration file in TOML syntax.")
	flag.Parse()

	// Read configuration from file.
	Config, err := config.LoadConfig(*configFlag)
	if err != nil {
		log.Fatal(err)
	}

	// Load environment from .env file.
	// Env, err := config.LoadEnv()
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// Initialize a server instance.
	Server := server.InitServer(Config)
	defer Server.Socket.Close()

	// Loop on incoming requests.
	Server.RunServer(Config.IMAP.Greeting)
}
