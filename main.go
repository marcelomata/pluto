package main

import (
	"flag"
	stdlog "log"
	"os"
	"runtime"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/numbleroot/pluto/auth"
	"github.com/numbleroot/pluto/config"
	"github.com/numbleroot/pluto/imap"
)

// Functions

// initAuthenticator of the correct implementation specified in the config
// to be used in the imap.Distributor.
func initAuthenticator(config *config.Config) (imap.PlainAuthenticator, error) {

	switch config.Distributor.AuthAdapter {
	case "AuthPostgres":
		// Connect to PostgreSQL database.
		return auth.NewPostgresAuthenticator(
			config.Distributor.AuthPostgres.IP,
			config.Distributor.AuthPostgres.Port,
			config.Distributor.AuthPostgres.Database,
			config.Distributor.AuthPostgres.User,
			config.Distributor.AuthPostgres.Password,
			config.Distributor.AuthPostgres.UseTLS,
		)
	default: // AuthFile
		// Open authentication file and read user information.
		return auth.NewFile(
			config.Distributor.AuthFile.File,
			config.Distributor.AuthFile.Separator,
		)
	}
}

func main() {

	var err error

	// Set CPUs usable by pluto to all available.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Parse command-line flag that defines a config path.
	configFlag := flag.String("config", "config.toml", "Provide path to configuration file in TOML syntax.")
	distributorFlag := flag.Bool("distributor", false, "Append this flag to indicate that this process should take the role of the distributor.")
	workerFlag := flag.String("worker", "", "If this process is intended to run as one of the IMAP worker nodes, specify which of the ones defined in your config file this should be.")
	failoverFlag := flag.Bool("failover", false, "Add this flag to a worker node in order to operate this node as a passthrough-failover node for specified crashed worker node.")
	storageFlag := flag.Bool("storage", false, "Append this flag to indicate that this process should take the role of the storage node.")
	loglevelFlag := flag.String("loglevel", "debug", "This flag sets the default logging level.")
	flag.Parse()

	logger := initLogger(*loglevelFlag)

	// Read configuration from file.
	conf, err := config.LoadConfig(*configFlag)
	if err != nil {
		level.Error(logger).Log("msg", "failed to load the config", "err", err)
		os.Exit(1)
	}

	// Initialize and run a node of the pluto
	// system based on passed command line flag.
	if *distributorFlag {

		authenticator, err := initAuthenticator(conf)
		if err != nil {
			stdlog.Fatal(err)
		}

		// Initialize distributor.
		distr, err := imap.InitDistributor(logger, conf, authenticator)
		if err != nil {
			stdlog.Fatal(err)
		}
		defer distr.Socket.Close()

		// Loop on incoming requests.
		err = distr.Run()
		if err != nil {
			stdlog.Fatal(err)
		}

	} else if *workerFlag != "" {

		if *failoverFlag {

			// Initialize a failover worker node.
			failWorker, err := imap.InitFailoverWorker(conf, *workerFlag)
			if err != nil {
				stdlog.Fatal(err)
			}
			defer failWorker.MailSocket.Close()

			// Loop on incoming requests to pass on.
			err = failWorker.RunFailover()
			if err != nil {
				stdlog.Fatal(err)
			}
		} else {

			// Initialize a normally operating worker.
			worker, err := imap.InitWorker(conf, *workerFlag)
			if err != nil {
				stdlog.Fatal(err)
			}
			defer worker.MailSocket.Close()
			defer worker.SyncSocket.Close()

			// Loop on incoming requests.
			err = worker.Run()
			if err != nil {
				stdlog.Fatal(err)
			}
		}

	} else if *storageFlag {

		// Initialize storage.
		storage, err := imap.InitStorage(conf)
		if err != nil {
			stdlog.Fatal(err)
		}
		defer storage.MailSocket.Close()
		defer storage.SyncSocket.Close()

		// Loop on incoming requests.
		err = storage.Run()
		if err != nil {
			stdlog.Fatal(err)
		}

	} else {

		// If no flags were specified, print usage
		// and return with failure value.
		flag.Usage()
		os.Exit(1)

	}
}

func initLogger(loglevel string) log.Logger {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))

	switch strings.ToLower(loglevel) {

	case "info":
		level.NewFilter(logger, level.AllowInfo())
	case "warn":
		level.NewFilter(logger, level.AllowWarn())
	case "error":
		level.NewFilter(logger, level.AllowError())
	default:
		level.NewFilter(logger, level.AllowDebug())
	}

	return logger
}
