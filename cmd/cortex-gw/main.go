package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/raintank/tsdb-gw/ingest/datadog"
	"github.com/raintank/tsdb-gw/persister/persist"

	"github.com/grafana/globalconf"
	"github.com/raintank/tsdb-gw/api"
	"github.com/raintank/tsdb-gw/ingest"
	"github.com/raintank/tsdb-gw/publish"
	cortexPublish "github.com/raintank/tsdb-gw/publish/cortex"
	"github.com/raintank/tsdb-gw/query/cortex"
	"github.com/raintank/tsdb-gw/util"
	log "github.com/sirupsen/logrus"
)

var (
	app             = "cortex-gw"
	GitHash         = "(none)"
	showVersion     = flag.Bool("version", false, "print version string")
	confFile        = flag.String("config", "/etc/gw/cortex-gw.ini", "configuration file path")
	authPlugin      = flag.String("api-auth-plugin", "grafana-instance", "auth plugin to use. (grafana-instance|file)")
	enforceRoles    = flag.Bool("enforce-roles", false, "enable role verification during authentication")
	forward3rdParty = flag.Bool("forward-3rdparty", false, "enable writing to cortex with non standard agents")
	writeURL        = flag.String("write-url", "http://localhost:9000", "cortex write address. use kubernetes:// for grpc")

	tracingEnabled = flag.Bool("tracing-enabled", false, "enable/disable distributed opentracing via jaeger")
	tracingAddr    = flag.String("tracing-addr", "localhost:6831", "address of the jaeger agent to send data to")
	metricsAddr    = flag.String("metrics-addr", ":8001", "http service address for the /metrics endpoint")

	persisterAddr    = flag.String("persister-addr", "http://localhost:9001/persist", "url of persister service")
	persisterEnabled = flag.Bool("persister-enabled", true, "enable the persister service")
)

func main() {
	flag.Parse()

	// Only try and parse the conf file if it exists
	path := ""
	if _, err := os.Stat(*confFile); err == nil {
		path = *confFile
	}

	conf, err := globalconf.NewWithOptions(&globalconf.Options{
		Filename:  path,
		EnvPrefix: "GW_",
	})
	if err != nil {
		log.Fatalf("error with configuration file: %s", err)
		os.Exit(1)
	}
	conf.ParseAll()

	if *showVersion {
		fmt.Printf("cortex-gw (built with %s, git hash %s)\n", runtime.Version(), GitHash)
		return
	}
	util.InitLogger()

	_, traceCloser, err := util.GetTracer(app, *tracingEnabled, *tracingAddr)
	if err != nil {
		log.Fatalf("Could not initialize jaeger tracer: %s", err.Error())
	}
	defer traceCloser.Close()

	if *persisterEnabled {
		persist.Init(*persisterAddr)
	}

	proxyURL := *writeURL

	var writeProxy *cortexPublish.CortexWriteProxy
	if strings.Contains(proxyURL, "kubernetes://") || strings.Contains(proxyURL, "dns://") {
		writeProxy, err = cortexPublish.NewCortexWriteProxy(cortexPublish.GRPCProxy, proxyURL)
	} else {
		writeProxy, err = cortexPublish.NewCortexWriteProxy(cortexPublish.HTTPProxy, proxyURL)
	}

	if err != nil {
		log.Fatalf("cannot initialise write proxy: %v", err)
	}

	if *forward3rdParty {
		publish.Init(cortexPublish.NewCortexPublisher(proxyURL))
	} else {
		publish.Init(nil)
	}

	if err := cortex.Init(); err != nil {
		log.Fatalf("could not initialize cortex proxy: %s", err.Error())
	}
	api := api.New(*authPlugin, app)
	initRoutes(api, writeProxy, *enforceRoles)

	ms := util.NewMetricsServer(*metricsAddr)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	log.Infof("Starting %v ...", app)
	done := make(chan struct{})
	inputs := []Stoppable{api.Start(), ms}
	go handleShutdown(done, interrupt, inputs)
	log.Infof("%v Started", app)
	<-done
}

// Stoppable represents things that can be stopped.
type Stoppable interface {
	Stop()
}

func handleShutdown(done chan struct{}, interrupt chan os.Signal, inputs []Stoppable) {
	<-interrupt
	log.Infoln("shutdown started.")
	var wg sync.WaitGroup
	for _, input := range inputs {
		wg.Add(1)
		go func(plugin Stoppable) {
			plugin.Stop()
			wg.Done()
		}(input)
	}

	complete := make(chan struct{})

	go func() {
		wg.Wait()
		close(complete)
	}()

	timer := time.NewTimer(time.Minute * 2)
	select {
	case <-timer.C:
		log.Errorln("shutdown taking too long, giving up waiting on plugins")
	case <-complete:
		log.Infof("shutdown complete")
	}
	close(done)
}

// InitRoutes initializes the routes.
func initRoutes(a *api.Api, writeProxy *cortexPublish.CortexWriteProxy, enforceRoles bool) {
	a.Router.Any("/api/prom/*", a.GenerateHandlers("read", enforceRoles, false, a.PromStats("cortex-read"), cortex.Proxy)...)
	a.Router.Any("/api/prom/push", a.GenerateHandlers("write", enforceRoles, false, a.PromStats("cortex-write"), writeProxy.Write)...)
	a.Router.Post("/datadog/api/v1/series", a.GenerateHandlers("write", enforceRoles, true, datadog.DataDogSeries)...)
	a.Router.Post("/datadog/api/v1/check_run", a.GenerateHandlers("write", enforceRoles, true, datadog.DataDogCheck)...)
	a.Router.Post("/datadog/intake", a.GenerateHandlers("write", enforceRoles, true, datadog.DataDogIntake)...)
	a.Router.Post("/opentsdb/api/put", a.GenerateHandlers("write", enforceRoles, false, ingest.OpenTSDBWrite)...)
	a.Router.Post("/metrics", a.GenerateHandlers("write", enforceRoles, false, ingest.Metrics)...)
}
