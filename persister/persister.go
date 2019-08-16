package persister

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	schema "github.com/grafana/metrictank/schema"
	"github.com/raintank/tsdb-gw/ingest/datadog/payloads"
	"github.com/raintank/tsdb-gw/metrics_client"
	"github.com/raintank/tsdb-gw/persister/storage"
	log "github.com/sirupsen/logrus"
)

// Persister ingests payloads that are serialized into metrics and repeatedly
// sent to a metrics endpoint in the schema.MetricData format
type Persister struct {
	*sync.Mutex
	metrics       map[string][]*schema.MetricData
	client        *metrics_client.Client
	store         *storage.Client
	interval      int
	maxBufferSize int
}

// Config contains the configuration require to
// create a Persister
type Config struct {
	interval            int
	maxBufferSize       int
	MetricsClientConfig metrics_client.Config
	StorageClientConfig storage.Config
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.interval, "persister-interval", 60, "seconds between sending persisted metrics")
	f.IntVar(&cfg.maxBufferSize, "persister-buffer-size", 1000, "max size of metrics payload send to metrics gateway")
	cfg.MetricsClientConfig.RegisterFlags(f)
	cfg.StorageClientConfig.RegisterFlags(f)
}

// New constructs a new persister
func New(cfg *Config) (*Persister, error) {
	client, err := metrics_client.New(cfg.MetricsClientConfig)
	if err != nil {
		return nil, err
	}

	store, err := storage.New(context.Background(), cfg.StorageClientConfig)
	if err != nil {
		return nil, err
	}

	return &Persister{
		&sync.Mutex{},
		map[string][]*schema.MetricData{},
		client,
		store,
		cfg.interval,
		cfg.maxBufferSize,
	}, nil
}

func (p *Persister) load() error {
	payloads, err := p.store.Retrieve()
	log.Infof("loaded %v payloads to persist from storage", len(payloads))

	if err != nil {
		return err
	}

	for k, v := range payloads {
		err := p.Persist(k, v.GeneratePersistentMetrics())
		if err != nil {
			return err
		}
	}

	return nil
}

// PersistHandler handles requests with payloads meant to be persisted
func (p *Persister) PersistHandler(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		defer r.Body.Close()
		data, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Errorf("unable to read request body. %s", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var payload payloads.PersistPayload
		err = json.Unmarshal(data, &payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("unable to unmarshal request, reason: %v", err)))
			return
		}

		rowKey := strconv.Itoa(payload.OrgID) + ":" + payload.Hostname

		err = p.store.Store(rowKey, payload)
		if err != nil {
			log.Errorf("failed to persist metricData. %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var info payloads.DataDogIntakePayload
		err = json.Unmarshal(payload.Raw, &info)
		info.OrgID = payload.OrgID

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("unable to unmarshal request, reason: %v", err)))
			return
		}

		err = p.Persist(rowKey, info.GeneratePersistentMetrics())
		if err != nil {
			log.Errorf("failed to persist metricData. %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte("no metrics to persists"))
}

// RemoveRowsRequest contains the information required to remove
// metrics from the persister
type RemoveRowsRequest struct {
	RowKeys []string `json:"rowkeys"`
}

// RemoveRowsHandler handles requests to remove payloads from the persister
func (p *Persister) RemoveRowsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		body, err := ioutil.ReadAll(r.Body)

		if err != nil {
			log.Errorf("unable to read request body. %s", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		removeRequest := RemoveRowsRequest{}
		err = json.Unmarshal(body, &removeRequest)
		if err != nil {
			log.Errorf("unable to unmarshal request. %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		err = p.store.Remove(removeRequest.RowKeys)
		if err != nil {
			log.Errorf("failed to remove metrics: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		p.Lock()
		log.Infof("removing metrics from persister metrics map")
		for _, r := range removeRequest.RowKeys {
			log.Debugf("removing %v", r)
			delete(p.metrics, r)
		}
		p.Unlock()

		if err != nil {
			log.Errorf("failed to remove metrics: %s", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Write([]byte("ok"))
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte("no metrics to remove"))
}

// IndexHandler serves up the metrics currently being persisted
func (p *Persister) IndexHandler(w http.ResponseWriter, r *http.Request) {
	p.Lock()
	data, err := json.Marshal(p.metrics)
	p.Unlock()
	if err != nil {
		log.Errorf("unable to marshal metrics index: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(data)
	return
}

// Persist add metrics to memory map for persisting
func (p *Persister) Persist(rowKey string, metrics []*schema.MetricData) error {
	log.Infof("persisting %v metrics", len(metrics))
	p.Lock()
	p.metrics[rowKey] = metrics
	p.Unlock()
	return nil
}

// Push schedules sending metrics to the gateway
func (p *Persister) Push(quit chan struct{}) {
	err := p.load()
	if err != nil {
		return
	}
	ticker := time.NewTicker(time.Duration(p.interval) * time.Second)
	for {
		select {
		case <-ticker.C:
			err := p.Send()
			if err != nil {
				log.Errorf("unable to send metrics; %v", err)
			}
		case <-quit:
			ticker.Stop()
			return
		}
	}
}

// Send packages and sends metrics to the gateway
func (p *Persister) Send() error {
	now := time.Now().Unix()
	p.Lock()
	metrics := []*schema.MetricData{}
	for rowKey, metricRow := range p.metrics {
		if len(metricRow) < 1 {
			log.Warningf("empty metric row %v", rowKey)
			continue
		}
		for _, metric := range metricRow {
			metric.Time = now
			metric.Interval = p.interval
			metrics = append(metrics, metric)
			if len(metrics) >= p.maxBufferSize {
				err := p.client.Push(metrics)
				if err != nil {
					return err
				}
				metrics = []*schema.MetricData{}
			}
		}
	}
	p.Unlock()

	if len(metrics) > 0 {
		return p.client.Push(metrics)
	}

	return nil
}
