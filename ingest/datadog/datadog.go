package datadog

import (
	"encoding/json"
	"fmt"

	"github.com/raintank/tsdb-gw/api/models"
	"github.com/raintank/tsdb-gw/ingest"
	"github.com/raintank/tsdb-gw/publish"
	log "github.com/sirupsen/logrus"
	schema "github.com/grafana/metrictank/schema"
)

// DataDogSeriesPayload struct to unmarshal datadog agent json
type DataDogSeriesPayload struct {
	Series []struct {
		Name   string       `json:"metric"`
		Points [][2]float64 `json:"points"`
		Tags   []string     `json:"tags"`
		Host   string       `json:"host"`
		Mtype  string       `json:"type"`
		Device string       `json:"device,omitempty"`
	} `json:"series"`
}

func DataDogSeries(ctx *models.Context) {
	if ctx.Req.Request.Body == nil {
		ctx.JSON(400, "no data included in request.")
		return
	}
	defer ctx.Req.Request.Body.Close()

	data, err := decodeJSON(ctx.Req.Request.Body, ctx.Req.Request.Header.Get("Content-Encoding") == "deflate")
	if err != nil {
		ctx.JSON(400, fmt.Sprintf("unable to decode request, reason: %v", err))
		return
	}

	var series DataDogSeriesPayload
	err = json.Unmarshal(data, &series)
	if err != nil {
		ctx.JSON(400, fmt.Sprintf("unable to unmarshal request, reason: %v", err))
		return
	}

	buf := make([]*schema.MetricData, 0)
	defer func(buf []*schema.MetricData) {
		for _, m := range buf {
			ingest.MetricPool.Put(m)
		}
	}(buf)

	for _, ts := range series.Series {
		tagSet := createTagSet(ts.Host, ts.Device, ts.Tags)
		for _, point := range ts.Points {
			md := ingest.MetricPool.Get()
			*md = schema.MetricData{
				Name:     ts.Name,
				Interval: 0,
				Value:    point[1],
				Unit:     "unknown",
				Time:     int64(point[0]),
				Mtype:    ts.Mtype,
				Tags:     tagSet,
				OrgId:    ctx.ID,
			}
			md.SetId()
			buf = append(buf, md)
		}
	}
	err = publish.Publish(buf)

	if err != nil {
		log.Errorf("failed to publish datadog series metrics. %s", err)
		ctx.JSON(500, err)
		return
	}
	ctx.JSON(200, "ok")
	return
}

// DataDogSeriesPayload struct to unmarshal datadog agent json
type DataDogCheckPayload []struct {
	Check     string   `json:"check"`
	Host      string   `json:"host_name"`
	Timestamp int64    `json:"timestamp"`
	Status    float64  `json:"status"`
	Message   string   `json:"message"`
	Tags      []string `json:"tags"`
}

func DataDogCheck(ctx *models.Context) {
	if ctx.Req.Request.Body == nil {
		ctx.JSON(400, "no data included in request.")
		return
	}
	defer ctx.Req.Request.Body.Close()

	data, err := decodeJSON(ctx.Req.Request.Body, ctx.Req.Request.Header.Get("Content-Encoding") == "deflate")
	if err != nil {
		ctx.JSON(400, fmt.Sprintf("unable to decode request, reason: %v", err))
	}

	var checks DataDogCheckPayload

	err = json.Unmarshal(data, &checks)
	if err != nil {
		ctx.JSON(400, fmt.Sprintf("unable to unmarshal request, reason: %v", err))
	}

	buf := make([]*schema.MetricData, 0)
	defer func(buf []*schema.MetricData) {
		for _, m := range buf {
			ingest.MetricPool.Put(m)
		}
	}(buf)

	for _, check := range checks {
		tagSet := createTagSet(check.Host, "", check.Tags)
		md := ingest.MetricPool.Get()
		*md = schema.MetricData{
			Name:     check.Check,
			Interval: 0,
			Value:    check.Status,
			Unit:     "unknown",
			Time:     check.Timestamp,
			Mtype:    "gauge",
			Tags:     tagSet,
			OrgId:    ctx.ID,
		}
		md.SetId()
		buf = append(buf, md)
	}

	err = publish.Publish(buf)

	if err != nil {
		log.Errorf("failed to publish datadog metrics. %s", err)
		ctx.JSON(500, err)
		return
	}

	ctx.JSON(200, "ok")
	return
}
