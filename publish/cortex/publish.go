package cortex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/prompb"
	schema "github.com/grafana/metrictank/schema"
	"github.com/raintank/tsdb-gw/publish"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context/ctxhttp"
)

var (
	writeBPoolSize  = flag.Int("bpool-size", 100, "max number of byte buffers in the cortex write buffer pool")
	writeBPoolWidth = flag.Int("bpool-width", 1024, "capacity of byte array provided by cortex write buffer pool")

	errBadTag    = errors.New("unable to parse tags")
	errNoMetrics = errors.New("no metrics provided in write request")

	droppedSamplesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cortex_gw",
			Subsystem: "publisher",
			Name:      "dropped_samples_total",
			Help:      "Total number of samples which were dropped.",
		},
		[]string{},
	)
	succeededSamplesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "cortex_gw",
			Subsystem: "publisher",
			Name:      "succeeded_samples_total",
			Help:      "Total number of samples successfully sent.",
		},
		[]string{},
	)
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex_gw",
		Subsystem: "publisher",
		Name:      "publish_duration_seconds",
		Help:      "Time (in seconds) spent publishing metrics to cortex.",
		Buckets:   prometheus.ExponentialBuckets(.05, 2, 10),
	}, []string{"status"})
)

const maxErrMsgLen = 256

type cortexPublisher struct {
	url     *url.URL
	client  *http.Client
	timeout time.Duration
}

// NewCortexPublisher creates a new cortex publisher.
func NewCortexPublisher(writeURL string) publish.Publisher {
	cortexURL, err := url.Parse(writeURL)
	if err != nil {
		panic(err)
	}

	return &cortexPublisher{
		url: cortexURL,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        20000,
				MaxIdleConnsPerHost: 1000,
				DisableKeepAlives:   false,
				DisableCompression:  true,
				IdleConnTimeout:     5 * time.Minute,
			},
		},
		timeout: time.Second * 60,
	}
}

func (c *cortexPublisher) Publish(metrics []*schema.MetricData) error {
	start := time.Now()
	req, err := packageMetrics(metrics)
	if err != nil {
		log.Debugf("unable to package metrics, %v", err)
		droppedSamplesTotal.WithLabelValues().Add(float64(len(metrics)))
		return err
	}

	succeededSamplesTotal.WithLabelValues().Add(float64(len(metrics)))

	err = c.Write(req)
	took := time.Since(start)
	if err != nil {
		requestDuration.WithLabelValues("failed").Observe(took.Seconds())
		return err
	}

	requestDuration.WithLabelValues("succeeded").Observe(took.Seconds())
	return nil
}

func (c *cortexPublisher) Type() string {
	return "cortex"
}

// Write sends a batch of samples to the HTTP endpoint.
func (c *cortexPublisher) Write(req writeRequest) error {
	data, err := proto.Marshal(&req.Request)
	if err != nil {
		return err
	}

	compressed := snappy.Encode(nil, data)
	httpReq, err := http.NewRequest("POST", c.url.String()+"/api/prom/push", bytes.NewReader(compressed))
	if err != nil {
		return err
	}
	httpReq.Header.Add("Content-Encoding", "snappy")
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
	httpReq.Header.Set("X-Scope-OrgID", strconv.Itoa(req.orgID))

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	httpResp, err := ctxhttp.Do(ctx, c.client, httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(httpResp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s: %s", httpResp.Status, line)
	}

	return err
}

type writeRequest struct {
	Request prompb.WriteRequest
	orgID   int
}

func packageMetrics(metrics []*schema.MetricData) (writeRequest, error) {
	if len(metrics) < 1 {
		return writeRequest{}, errNoMetrics
	}

	req := prompb.WriteRequest{
		Timeseries: make([]*prompb.TimeSeries, 0, len(metrics)),
	}
	for _, m := range metrics {
		labels := make([]*prompb.Label, 0, len(m.Tags)+1)
		labels = append(labels,
			&prompb.Label{
				Name:  "__name__",
				Value: strings.Replace(m.Name, ".", "_", -1),
			},
		)
		for _, tag := range m.Tags {
			tv := strings.SplitN(tag, "=", 2)
			if len(tv) < 2 || tv[0] == "" || tv[1] == "" {
				log.Warnf("tag: '%v' is not able to be encoded", tv)
				continue
			}
			labels = append(labels, &prompb.Label{
				Name:  tv[0],
				Value: tv[1],
			})
		}
		req.Timeseries = append(req.Timeseries, &prompb.TimeSeries{
			Labels: labels,
			Samples: []prompb.Sample{
				{
					Value:     m.Value,
					Timestamp: m.Time * 1000,
				},
			},
		})
	}

	return writeRequest{
		Request: req,
		orgID:   metrics[0].OrgId,
	}, nil
}
