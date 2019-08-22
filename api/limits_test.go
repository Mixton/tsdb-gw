package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/metrictank/schema"
	"github.com/raintank/tsdb-gw/api/models"
	"github.com/raintank/tsdb-gw/ingest"
	"github.com/raintank/tsdb-gw/publish"
	"gopkg.in/macaron.v1"
)

func TestRateLimitHander(t *testing.T) {
	publish.Init(nil)
	m := macaron.New()
	m.Use(macaron.Renderer())
	setOrg := func(ctx *models.Context) {
		ctx.User.ID = 1
	}
	m.Router.Post("/metrics", GetContextHandler(), setOrg, IngestRateLimiter(), ingest.Metrics)
	ts := httptest.NewServer(m)
	defer ts.Close()

	ingest.ConfigureRateLimits("1:100")

	// send two requests so we hit our limit
	for i := 0; i < 2; i++ {
		err := sendData(t, ts.URL, 50)
		if err != nil {
			t.Fatalf("got err: %s", err)
		}
	}
	// send another request, this should get rejected as we have
	// already exceeded our limit
	err := sendData(t, ts.URL, 50)
	if err == nil {
		t.Fatalf("expected to be rate limited but weren't")
	}
	// sleep so our rate drops to 0
	time.Sleep(time.Second)

	// send a request that almost hits the limit, but not quite
	err = sendData(t, ts.URL, 98)
	if err != nil {
		t.Fatalf("got err: %s", err)
	}

	// send a request that is almost the full limit.  This should get blocked
	// for close to 1 second before being processed.
	pre := time.Now()
	err = sendData(t, ts.URL, 99)
	if err != nil {
		t.Fatalf("expected to be rate limited but weren't")
	}
	if time.Since(pre) < time.Millisecond*500 {
		t.Fatalf("expected request to be delayed due to limit")
	}
}

func sendData(t *testing.T, url string, numPoints int) error {
	metrics := make([]*schema.MetricData, numPoints)
	for i := 0; i < numPoints; i++ {
		metrics[i] = &schema.MetricData{
			Name:     fmt.Sprintf("foo.blah.%d", i),
			Time:     time.Now().Unix(),
			Interval: 10,
		}
	}
	b, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	buf := bytes.NewBuffer(b)
	res, err := http.Post(url+"/metrics", "application/json", buf)

	if err != nil {
		return err
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}

	t.Logf("%d: %s", res.StatusCode, body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("%d %s", res.StatusCode, body)
	}
	return nil
}
