package ingest

import (
	"context"
	"sync"
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
		if err := ConfigureRateLimits(tt.limitStr); (err != nil) != tt.wantErr {
			t.Errorf("%s: ConfigureRateLimits() error = %v, wantErr %v", tt.name, err, tt.wantErr)
		}
		for orgId, expectedLimit := range tt.expectedLimiters {
			if limiter, ok := rateLimiters[orgId]; !ok {
				t.Fatalf("%s: Expected limit for org %d, but there was none", tt.name, orgId)
			} else {
				if limiter.Limit() != rate.Limit(expectedLimit) {
					t.Fatalf("%s: Expected org %d to have limit %d, but it had %d", tt.name, orgId, expectedLimit, int(limiter.Limit()))
				}
			}
		}
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
			expectedIngestedDatapointsMin: 7500,
			expectedIngestedDatapointsMax: 12500,
			expectedRejectedRequestsMin:   875,
			expectedRejectedRequestsMax:   925,
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
			expectedIngestedDatapointsMin: 500,
			expectedIngestedDatapointsMax: 1500,
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
			expectedIngestedDatapointsMin: 2460,
			expectedIngestedDatapointsMax: 4305,
			expectedRejectedRequestsMin:   35,
			expectedRejectedRequestsMax:   50,
		},
	}

	for _, tt := range tests {
		if err := ConfigureRateLimits(tt.limitStr); (err != nil) != tt.wantErr {
			t.Errorf("%s: ConfigureRateLimits() error = %v, wantErr %v", tt.name, err, tt.wantErr)
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

		wg := sync.WaitGroup{}
		for datapoints := range requestCh {
			wg.Add(1)
			go func(datapoints int) {
				defer wg.Done()
				ctx := context.Background()
				if IsRateBudgetAvailable(ctx, tt.orgId) {
					rateLimit(ctx, tt.orgId, datapoints)
					atomic.AddUint32(&ingestedDatapoints, uint32(datapoints))
				} else {
					atomic.AddUint32(&rejectedRequests, 1)
				}
			}(datapoints)
		}

		wg.Wait()

		if atomic.LoadUint32(&ingestedDatapoints) < tt.expectedIngestedDatapointsMin || atomic.LoadUint32(&ingestedDatapoints) > tt.expectedIngestedDatapointsMax {
			t.Fatalf("%s: ingested datapoints is outside expected range. Expected %d - %d, Got %d", tt.name, tt.expectedIngestedDatapointsMin, tt.expectedIngestedDatapointsMax, atomic.LoadUint32(&ingestedDatapoints))
		}

		if atomic.LoadUint32(&rejectedRequests) < tt.expectedRejectedRequestsMin || atomic.LoadUint32(&rejectedRequests) > tt.expectedRejectedRequestsMax {
			t.Fatalf("%s: rejected requests is outside expected range. Expected %d - %d, Got %d", tt.name, tt.expectedRejectedRequestsMin, tt.expectedRejectedRequestsMax, atomic.LoadUint32(&rejectedRequests))
		}
	}
}
