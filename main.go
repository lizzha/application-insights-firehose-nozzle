package main

import (
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"

	"code.cloudfoundry.org/lager"
	"github.com/lizzha/application-insights-firehose-nozzle/ainozzle"
	"github.com/lizzha/application-insights-firehose-nozzle/caching"
	"github.com/lizzha/application-insights-firehose-nozzle/firehose"
	"github.com/cloudfoundry-community/go-cfclient"
	"gopkg.in/alecthomas/kingpin.v2"
)

const (
	firehoseSubscriptionID = "ai-nozzle"
	// lower limit for override

	version = "0.0.0"
)

// Required parameters
var (
	apiAddress        = kingpin.Flag("api-addr", "Api URL").OverrideDefaultFromEnvar("API_ADDR").Required().String()
	dopplerAddress    = kingpin.Flag("doppler-addr", "Traffic controller URL").OverrideDefaultFromEnvar("DOPPLER_ADDR").Required().String()
	cfUser            = kingpin.Flag("firehose-user", "CF user with admin and firehose access").OverrideDefaultFromEnvar("FIREHOSE_USER").Required().String()
	cfPassword        = kingpin.Flag("firehose-user-password", "Password of the CF user").OverrideDefaultFromEnvar("FIREHOSE_USER_PASSWORD").Required().String()
	skipSslValidation = kingpin.Flag("skip-ssl-validation", "Skip SSL validation").Default("false").OverrideDefaultFromEnvar("SKIP_SSL_VALIDATION").Bool()
	idleTimeout       = kingpin.Flag("idle-timeout", "Keep Alive duration for the firehose consumer").Default("25s").OverrideDefaultFromEnvar("IDLE_TIMEOUT").Duration()
	logLevel          = kingpin.Flag("log-level", "Log level: DEBUG, INFO, ERROR").Default("INFO").OverrideDefaultFromEnvar("LOG_LEVEL").String()
	instrumentKey     = kingpin.Flag("instrument-key", "InstrumentKey").OverrideDefaultFromEnvar("INSTRUMENT_KEY").Required().String()
)

func main() {
	kingpin.Version(version)
	kingpin.Parse()

	logger := lager.NewLogger("ai-nozzle")
	level := lager.INFO
	switch strings.ToUpper(*logLevel) {
	case "DEBUG":
		level = lager.DEBUG
	case "ERROR":
		level = lager.ERROR
	}
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, level))

	// enable thread dump
	threadDumpChan := registerGoRoutineDumpSignalChannel()
	defer close(threadDumpChan)
	go dumpGoRoutine(threadDumpChan)

	logger.Info("config", lager.Data{"SKIP_SSL_VALIDATION": *skipSslValidation})
	logger.Info("config", lager.Data{"IDLE_TIMEOUT": (*idleTimeout).String()})

	cfClientConfig := &cfclient.Config{
		ApiAddress:        *apiAddress,
		Username:          *cfUser,
		Password:          *cfPassword,
		SkipSslValidation: *skipSslValidation,
	}

	firehoseConfig := &firehose.FirehoseConfig{
		SubscriptionId:       firehoseSubscriptionID,
		TrafficControllerUrl: *dopplerAddress,
		IdleTimeout:          *idleTimeout,
	}

	firehoseClient := firehose.NewClient(cfClientConfig, firehoseConfig, logger)

	cachingClient := caching.NewCaching(cfClientConfig, logger)
	nozzle := ainozzle.NewAiNozzle(logger, firehoseClient, *instrumentKey, cachingClient)

	nozzle.Start()
}

func registerGoRoutineDumpSignalChannel() chan os.Signal {
	threadDumpChan := make(chan os.Signal, 1)
	signal.Notify(threadDumpChan, syscall.SIGUSR1)

	return threadDumpChan
}

func dumpGoRoutine(dumpChan chan os.Signal) {
	for range dumpChan {
		goRoutineProfiles := pprof.Lookup("goroutine")
		if goRoutineProfiles != nil {
			goRoutineProfiles.WriteTo(os.Stdout, 2)
		}
	}
}
