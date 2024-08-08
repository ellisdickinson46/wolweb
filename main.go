package main

import (
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/icefed/zlog"
	"github.com/ilyakaznacheev/cleanenv"
)

// Global variables
var (
	appVersion = "dev"
	appConfig  AppConfig
	appData    AppData
	args       Args

	//go:embed static
	staticFiles embed.FS
)

type Args struct {
	ConfigPath  string
	DevicesPath string
}

func main() {
	h := zlog.NewJSONHandler(&zlog.Config{
		HandlerOptions: slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
		TimeFormatter: func(buf []byte, t time.Time) []byte {
			return t.AppendFormat(buf, time.DateTime)
		},
		Development: true,
	})
	loggerInstance := zlog.New(h)
	zlog.SetDefault(loggerInstance)

	zlog.Infof("Starting Wake-on-Lan Web, Version \"%s\"", appVersion)

	processArgs()
	setWorkingDir()
	loadConfig()
	loadData()
	setupWebServer()
}

func setWorkingDir() {
	thisApp, err := os.Executable()
	if err != nil {
		zlog.Errorf("Error determining the directory. \"%s\"", err)
	}
	appPath := filepath.Dir(thisApp)
	os.Chdir(appPath)
	zlog.Debugf("Set working directory: %s", appPath)
}

func loadConfig() {
	err := cleanenv.ReadConfig(args.ConfigPath, &appConfig)
	if err != nil {
		zlog.Errorf("Error loading config.json file. \"%s\"", err)
	}
	zlog.Info("Application configuratrion loaded successfully")
}

func setupWebServer() {
	// Init HTTP Router - mux
	router := mux.NewRouter()

	// Define base path. Keep it empty when VDir is just "/" to avoid redirect loops
	// Add trailing slash if basePath is not empty
	basePath := ""
	if appConfig.VDir != "/" {
		basePath = appConfig.VDir
		router.HandleFunc(basePath, redirectToHomePage).Methods("GET")
	}

	// map directory to server static files
	var staticFS = fs.FS(staticFiles)
	staticFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	router.PathPrefix(basePath + "/static/").Handler(http.StripPrefix(basePath+"/static/", CacheControlWrapper(http.FileServer(http.FS(staticFS)))))

	// Define Home Route
	router.HandleFunc(basePath+"/", renderHomePage).Methods("GET")

	// Define Wakeup functions with a Device Name
	router.HandleFunc(basePath+"/wake/{deviceName}", wakeUpWithDeviceName).Methods("GET")
	router.HandleFunc(basePath+"/wake/{deviceName}/", wakeUpWithDeviceName).Methods("GET")

	// Define Data save Api function
	router.HandleFunc(basePath+"/data/save", saveData).Methods("POST")

	// Define Data get Api function
	router.HandleFunc(basePath+"/data/get", getData).Methods("GET")

	// Define health check function
	router.HandleFunc(basePath+"/health", checkHealth).Methods("GET")

	// Setup Webserver
	httpListen := net.ParseIP(appConfig.Host).String() + ":" + strconv.Itoa(appConfig.Port)
	zlog.Infof("Startup Webserver on \"%s\"", httpListen)

	srv := &http.Server{
		Handler: gziphandler.GzipHandler(handlers.RecoveryHandler(handlers.PrintRecoveryStack(true))(router)),
		Addr:    httpListen,
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	zlog.Error(srv.ListenAndServe().Error())
}

func CacheControlWrapper(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=31536000")
		h.ServeHTTP(w, r)
	})
}

func processArgs() {
	var configPath string
	var devicesPath string

	f := flag.NewFlagSet("wolweb", 1)

	f.StringVar(&configPath, "c", "config.json", "Path to configuration file")
	f.StringVar(&devicesPath, "d", "devices.json", "Path to devices file")

	f.Parse(os.Args[1:])

	configPath, err := filepath.Abs(configPath)
	if err != nil {
		zlog.Error(err.Error())
	}

	devicesPath, err = filepath.Abs(devicesPath)
	if err != nil {
		zlog.Error(err.Error())
	}

	args.ConfigPath = configPath
	args.DevicesPath = devicesPath
}
