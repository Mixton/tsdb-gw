package ingest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestConfigureRateLimits(t *testing.T) {
	tests := []struct {
		name             string
		limitStr         string
		wantErr          bool
		expectedLimiters map[int]int // org id -> limit
	}{
		{
			name:             "simple single org",
			limitStr:         "22:1000",
			wantErr:          false,
			expectedLimiters: map[int]int{22: 1000},
		},
		{
			name:             "simple multi org",
			limitStr:         "22:1000;23:10;100:1000",
			wantErr:          false,
			expectedLimiters: map[int]int{22: 1000, 23: 10, 100: 1000},
		},
		{
			name:             "invalid format",
			limitStr:         "22:1000;23:10:100:1000",
			wantErr:          true,
			expectedLimiters: map[int]int{},
		},
		{
			name:             "invalid another format",
			limitStr:         "22:1000;23;10;100:1000",
			wantErr:          true,
			expectedLimiters: map[int]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ConfigureRateLimits(tt.limitStr); (err != nil) != tt.wantErr {
				t.Errorf("ConfigureRateLimits() error = %v, wantErr %v", err, tt.wantErr)
			}
			for orgId, expectedLimit := range tt.expectedLimiters {
				if limiter, ok := rateLimiters[orgId]; !ok {
					t.Fatalf("Expected limit for org %d, but there was none", orgId)
				} else {
					if limiter.Limit() != rate.Limit(expectedLimit) {
						t.Fatalf("Expected org %d to have limit %d, but it had %d", orgId, expectedLimit, int(limiter.Limit()))
					}
				}
			}
		})
	}
}

// TestLimitingRate is a bit racy, but since it tests a rate limiter I can't think of a better way to do it
func TestLimitingRate(t *testing.T) {
	tests := []struct {
		name                          string
		limitStr                      string
		wantErr                       bool
		orgId                         int
		requestRate                   int    // how many requests we receive per second
		datapointsPerRequest          int    // how many datapoints are in each request
		testTime                      int    // number of seconds to run the test
		expectedIngestedDatapointsMin uint32 // min number of datapoints that should get ingested
		expectedIngestedDatapointsMax uint32 // max number of datapoints that should get ingested
		expectedRejectedRequestsMin   uint32 // min number of requests that should get rejected
		expectedRejectedRequestsMax   uint32 // max number of requests that should get rejected
	}{
		{
			name:                 "1000 datapoints/sec, 100 reqs/sec, 100 datapoints/req",
			limitStr:             "1:1000",
			wantErr:              false,
			orgId:                1,
			requestRate:          100,
			datapointsPerRequest: 100,
			testTime:             10,

			// - we're receiving 100 requests per second, with 100 datapoints per request, we permit 1000 datapoints per second, we're testing for 10 seconds
			// - roughly 10000 datapoints should get ingested in total
			// - in total 1000 requests should be made, out of which around 100 (10000/100) should get accepted, around 900 should get rejected
			expectedIngestedDatapointsMin: 9000,
			expectedIngestedDatapointsMax: 11000,
			expectedRejectedRequestsMin:   800,
			expectedRejectedRequestsMax:   1000,
		}, {
			name:                 "100 datapoints/sec, 10 reqs/sec, 50 datapoints/req",
			limitStr:             "1:100",
			wantErr:              false,
			orgId:                1,
			requestRate:          10,
			datapointsPerRequest: 50,
			testTime:             10,

			// - we're receiving 10 requests per second, with 50 datapoints per request, we permit 100 datapoints per second, we're testing for 10 seconds
			// - roughly 1000 datapoints should get ingested in total
			// - in total 100 requests should be made, out of which around 20 (1000/50) should get accepted, around 80 should get rejected
			expectedIngestedDatapointsMin: 900,
			expectedIngestedDatapointsMax: 1100,
			expectedRejectedRequestsMin:   70,
			expectedRejectedRequestsMax:   90,
		}, {
			name:                 "333 datapoints/sec, 7 reqs/sec, 123 datapoints/req",
			limitStr:             "1:333",
			wantErr:              false,
			orgId:                1,
			requestRate:          7,
			datapointsPerRequest: 123,
			testTime:             10,

			// - we're receiving 7 requests per second, with 123 datapoints per request, we permit 333 datapoints per second, we're testing for 10 seconds
			// - roughly 3333 datapoints should get ingested in total
			// - in total 70 requests should be made, out of which around 27 (3333/123) should get accepted, around 43 should get rejected
			expectedIngestedDatapointsMin: 3300,
			expectedIngestedDatapointsMax: 3600,
			expectedRejectedRequestsMin:   35,
			expectedRejectedRequestsMax:   50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ConfigureRateLimits(tt.limitStr); (err != nil) != tt.wantErr {
				t.Errorf("ConfigureRateLimits() error = %v, wantErr %v", err, tt.wantErr)
			}

			done := time.NewTimer(time.Duration(tt.testTime) * time.Second)
			requestCh := make(chan int, 1000)

			go func() {
				ticker := time.NewTicker(time.Second / time.Duration(tt.requestRate))
				defer ticker.Stop()
				defer close(requestCh)

				for range ticker.C {
					select {
					case <-done.C:
						return
					case requestCh <- tt.datapointsPerRequest:
					default:
					}
				}
			}()

			var ingestedDatapoints, rejectedRequests uint32

			for datapoints := range requestCh {
				go func(datapoints int) {
					ctx := context.Background()
					if IsRateBudgetAvailable(ctx, tt.orgId) {
						rateLimit(ctx, tt.orgId, datapoints)
						atomic.AddUint32(&ingestedDatapoints, uint32(datapoints))
					} else {
						atomic.AddUint32(&rejectedRequests, 1)
					}
				}(datapoints)
			}

			if ingestedDatapoints < tt.expectedIngestedDatapointsMin || ingestedDatapoints > tt.expectedIngestedDatapointsMax {
				t.Fatalf("ingested datapoints is outside expected range. Expected %d - %d, Got %d", tt.expectedIngestedDatapointsMin, tt.expectedIngestedDatapointsMax, ingestedDatapoints)
			}

			if rejectedRequests < tt.expectedRejectedRequestsMin || rejectedRequests > tt.expectedRejectedRequestsMax {
				t.Fatalf("rejected requests is outside expected range. Expected %d - %d, Got %d", tt.expectedRejectedRequestsMin, tt.expectedRejectedRequestsMax, rejectedRequests)
			}
		})
	}
}
