package cortex

import (
	"reflect"
	"testing"

	schema "github.com/grafana/metrictank/schema"
	"github.com/prometheus/prometheus/prompb"
)

func Test_packageMetrics(t *testing.T) {
	tests := []struct {
		name    string
		metrics []*schema.MetricData
		want    writeRequest
		wantErr bool
	}{
		{
			name: "basic metric",
			metrics: []*schema.MetricData{
				{Name: "example_metric"},
			},
			want: writeRequest{
				Request: prompb.WriteRequest{
					Timeseries: []*prompb.TimeSeries{
						{
							Labels: []*prompb.Label{
								{Name: "__name__", Value: "example_metric"},
							},
							Samples: []prompb.Sample{
								{Value: 0, Timestamp: 0},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "graphite metric",
			metrics: []*schema.MetricData{
				{Name: "example.metric"},
			},
			want: writeRequest{
				Request: prompb.WriteRequest{
					Timeseries: []*prompb.TimeSeries{
						{
							Labels: []*prompb.Label{
								{Name: "__name__", Value: "example_metric"},
							},
							Samples: []prompb.Sample{
								{Value: 0, Timestamp: 0},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "tagged metric",
			metrics: []*schema.MetricData{
				{
					Name: "example_metric",
					Tags: []string{"example=tag"},
				},
			},
			want: writeRequest{
				Request: prompb.WriteRequest{
					Timeseries: []*prompb.TimeSeries{
						{
							Labels: []*prompb.Label{
								{Name: "__name__", Value: "example_metric"},
								{Name: "example", Value: "tag"},
							},
							Samples: []prompb.Sample{
								{Value: 0, Timestamp: 0},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "bad tagged metric",
			metrics: []*schema.MetricData{
				{
					Name: "example_metric",
					Tags: []string{"example="},
				},
			},
			want: writeRequest{
				Request: prompb.WriteRequest{
					Timeseries: []*prompb.TimeSeries{
						{
							Labels: []*prompb.Label{
								{Name: "__name__", Value: "example_metric"},
							},
							Samples: []prompb.Sample{
								{Value: 0, Timestamp: 0},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "tagged metric",
			metrics: []*schema.MetricData{
				{
					Name: "example_metric",
					Tags: []string{"example1=tag", "example2=tag"},
				},
			},
			want: writeRequest{
				Request: prompb.WriteRequest{
					Timeseries: []*prompb.TimeSeries{
						{
							Labels: []*prompb.Label{
								{Name: "__name__", Value: "example_metric"},
								{Name: "example1", Value: "tag"},
								{Name: "example2", Value: "tag"},
							},
							Samples: []prompb.Sample{
								{Value: 0, Timestamp: 0},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "no metrics",
			metrics: []*schema.MetricData{},
			want:    writeRequest{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := packageMetrics(tt.metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("packageMetrics() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("packageMetrics() = %v, want %v", got, tt.want)
			}
		})
	}
}
