package ainozzle

import (
	//"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/lizzha/application-insights-firehose-nozzle/caching"
	"github.com/lizzha/application-insights-firehose-nozzle/firehose"
	"github.com/lizzha/application-insights-firehose-nozzle/messages"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/jjjordanmsft/ApplicationInsights-Go/appinsights"
)

type AiNozzle struct {
	logger          lager.Logger
	errChan         <-chan error
	msgChan         <-chan *events.Envelope
	signalChan      chan os.Signal
	telemetryClient appinsights.TelemetryClient
	firehoseClient  firehose.Client
	cachingClient   caching.CachingClient
}

func NewAiNozzle(logger lager.Logger, firehoseClient firehose.Client, instrumentKey string, caching caching.CachingClient) *AiNozzle {
	return &AiNozzle{
		logger:          logger,
		errChan:         make(<-chan error),
		msgChan:         make(<-chan *events.Envelope),
		signalChan:      make(chan os.Signal, 2),
		telemetryClient: appinsights.NewTelemetryClient(instrumentKey),
		firehoseClient:  firehoseClient,
		cachingClient:   caching,
	}
}

func (o *AiNozzle) Start() error {
	o.cachingClient.Initialize()

	// setup for termination signal from CF
	signal.Notify(o.signalChan, syscall.SIGTERM, syscall.SIGINT)

	o.msgChan, o.errChan = o.firehoseClient.Connect()

	err := o.routeEvents()
	return err
}

func (o *AiNozzle) routeEvents() error {
	for {
		// loop over message and signal channel
		select {
		case s := <-o.signalChan:
			o.logger.Info("exiting", lager.Data{"signal caught": s.String()})
			if err := o.firehoseClient.CloseConsumer(); err != nil {
				o.logger.Error("error closing consumer", err)
			}
			os.Exit(1)
		case msg := <-o.msgChan:
			switch msg.GetEventType() {
			case events.Envelope_LogMessage:
				message := messages.NewLogMessage(msg, o.cachingClient)
				if strings.Contains(message.SourceType, "RTR") {
					if m, err := ParseRtr(message.Message); err == nil {
						name := m.method + " " + m.path
						url := m.xForwardedProto + "://" + m.host + m.path

						telem := appinsights.NewRequestTelemetry(name, m.method, url, m.timestamp, m.responseTime, m.statusCode, m.isSuccess)
						context := telem.Context()

						context.User().SetUserAgent(m.userAgent)

						context.Location().SetIp(m.xForwardedFor)
						context.Operation().SetName(name)

						telem.SetProperty("request_bytes_received", m.requestBytesReceived)
						telem.SetProperty("body_bytes_sent", m.bodyBytesSent)
						telem.SetProperty("referer", m.referer)
						telem.SetProperty("remote_addr", m.remoteAddr)
						telem.SetProperty("dest_ip_port", m.destIpAndPort)
						telem.SetProperty("vcap_request_id", m.vcapRequestId)
						telem.SetProperty("app_id", m.appId)
						telem.SetProperty("app_index", m.appIndex)
						telem.SetProperty("app_name", message.ApplicationName)

						o.telemetryClient.Track(telem)
					} else {
						o.logger.Error("Error parsing RTR message", err)
					}
				} else {
					var severity appinsights.SeverityLevel
					if message.MessageType == "ERR" {
						severity = appinsights.Error
					} else {
						severity = appinsights.Information
					}
					telem := appinsights.NewTraceTelemetry(message.Message, severity)

					telem.SetProperty("app_id", message.AppID)
					telem.SetProperty("app_name", message.ApplicationName)
					telem.SetProperty("source_type", message.SourceType)
					telem.SetProperty("source_instance", message.SourceInstance)

					o.telemetryClient.Track(telem)
				}
			default:
				continue
			}
			// When the number of one type of events reaches the max per batch, trigger the post immediately
		case err := <-o.errChan:
			o.logger.Error("Error while reading from the firehose", err)

			if strings.Contains(err.Error(), "close 1008 (policy violation)") {
				o.logger.Error("Disconnected because nozzle couldn't keep up. Please try scaling up the nozzle.", nil)
			}

			o.logger.Error("Closing connection with traffic controller", nil)
			o.firehoseClient.CloseConsumer()
			return err
		}
	}
}

type RtrMessage struct {
	host                 string
	timestamp            time.Time
	method               string
	path                 string
	protocol             string
	statusCode           string
	requestBytesReceived string
	bodyBytesSent        string
	referer              string
	userAgent            string
	remoteAddr           string
	destIpAndPort        string
	xForwardedFor        string
	xForwardedProto      string
	vcapRequestId        string
	responseTime         time.Duration
	appId                string
	appIndex             string
	isSuccess            bool
}

func ParseRtr(log string) (msg RtrMessage, err error) {
	strs := strings.Split(log, "\"")
	if l := len(strs); l < 20 {
		return msg, fmt.Errorf("Error parsing RTR message: %s", log)
	} else {
		for i := 0; i < l; i++ {
			strs[i] = strings.TrimSpace(strs[i])
		}
	}

	// parse host and timestamp
	s := strings.Split(strs[0], " ")
	msg.host = s[0]
	if len(s) < 3 {
		return msg, fmt.Errorf("Error parsing timestamp: %s", s)
	}
	timeStr := s[2][1 : len(s[2])-1]
	if msg.timestamp, err = time.Parse("2006-01-02T15:04:05.999-0700", timeStr); err != nil {
		return msg, fmt.Errorf("Error parsing timestamp: %s", err.Error())
	}

	// parse method, path and protocol
	s = strings.Split(strs[1], " ")
	msg.method = s[0]
	if len(s) < 3 {
		return msg, fmt.Errorf("Error parsing path and protocol: %s", s)
	}
	msg.path = s[1]
	msg.protocol = s[2]

	// parse status code, request bytes received, and body bytes sent
	s = strings.Split(strs[2], " ")
	msg.statusCode = s[0]
	if n, err := strconv.Atoi(s[0]); err == nil {
		msg.isSuccess = (n < 400)
	} else {
		return msg, fmt.Errorf("Error parsing success: %s", s)
	}
	if len(s) < 3 {
		return msg, fmt.Errorf("Error parsing bytes received and sent: %s", s)
	}
	msg.requestBytesReceived = s[1]
	msg.bodyBytesSent = s[2]

	// parse referer
	msg.referer = strs[3]

	// parse user agent
	msg.userAgent = strs[5]

	// parse remote addr
	msg.remoteAddr = strs[7]

	// parse dest ip and port
	msg.destIpAndPort = strs[9]

	// parse x_forwarded_for
	if strings.Contains(strs[10], "x_forwarded_for") {
		msg.xForwardedFor = strings.Split(strs[11], ",")[0]
	} else {
		return msg, fmt.Errorf("Error parsing x_forwarded_for: %s", strs[10])
	}

	// parse x_forwarded_proto
	if strs[13] == "http" {
		msg.xForwardedProto = "http"
	} else if strs[13] == "https" {
		msg.xForwardedProto = "https"
	} else {
		return msg, fmt.Errorf("Error parsing x_forwarded_proto: %s", strs[13])
	}

	// parse vcap_request_id
	if strings.Contains(strs[14], "vcap_request_id") {
		msg.vcapRequestId = strs[15]
	} else {
		return msg, fmt.Errorf("Error parsing vcap_request_id: %s", strs[14])
	}

	// parse response time and app id
	s = strings.Split(strs[16], " ")
	if strings.Contains(s[0], "response_time:") {
		timeStr = strings.Split(s[0], ":")[1]
		if responseTime, err := strconv.ParseFloat(timeStr, 64); err != nil {
			return msg, fmt.Errorf("Error parsing response time: %s", err.Error())
		} else {
			msg.responseTime = time.Duration(responseTime*1000000000) * time.Nanosecond
		}
	}
	if len(s) > 1 && strings.Contains(s[1], "app_id") {
		msg.appId = strs[17]
	} else {
		return msg, fmt.Errorf("Error parsing app id: %s", strs[16])
	}

	// parse app index
	if strings.Contains(strs[18], "app_index") {
		msg.appIndex = strs[19]
	} else {
		return msg, fmt.Errorf("Error parsing app index: %s", strs[18])
	}
	return msg, nil
}
