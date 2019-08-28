package ingest

import (
	"context"
	"fmt"
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

// TestLimitingRate is a bit racy, but since it tests a rate limiter I can't think of a better way to do it.
// In this test we're testing the limiter with a defined number of requests per time and a defined rate limit,
// then we check if the expected number of requests has been accepted / rejected. But even if theoretically
// we know how many requests should get accepted / rejected, it can always be that due to how the processes
// get scheduled a few more / less get accepted / rejected.
// That's why the test is checking for minimums and maximums of a range, instead of checking for an exact number.
// note: this mimics what happens during an ingestion request:
// * first call IsRateBudgetAvailable to see if the request would be rejected
// * then rateLimit - assuming the "request" was not rejected to see if a batch of points would be rejected
func TestLimitingRate(t *testing.T) {
	tests := []struct {
		name                          string
		limit                         int
		orgId                         int
		requestRate                   int    // how many requests we issue per second
		datapointsPerRequest          int    // how many datapoints to mimic per request
		testTime                      int    // number of seconds to run the test
		expectedIngestedDatapointsMin uint32 // min number of datapoints that should get ingested
		expectedIngestedDatapointsMax uint32 // max number of datapoints that should get ingested
		expectedRejectedRequestsMin   uint32 // min number of requests that should get rejected
		expectedRejectedRequestsMax   uint32 // max number of requests that should get rejected
	}{
		{
			name:                 "100 reqs/sec, 100 datapoints/req with 1 kHz limit",
			limit:                1000,
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
			name:                 "10 reqs/sec, 50 datapoints/req with 100 Hz limit",
			limit:                100,
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
			name:                 "7 reqs/sec, 123 datapoints/req with 333 Hz limit",
			limit:                333,
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
		if err := ConfigureRateLimits(fmt.Sprintf("%d:%d", tt.orgId, tt.limit)); err != nil {
			t.Errorf("%s: ConfigureRateLimits() got error = %v", tt.name, err)
		}

		requestCh := make(chan int, 1000)
		ctx, cancel := context.WithCancel(context.Background())

		go func() {

			done := time.NewTimer(time.Duration(tt.testTime) * time.Second)
			ticker := time.NewTicker(time.Second / time.Duration(tt.requestRate))

			defer ticker.Stop()
			defer close(requestCh)

			for range ticker.C {
				select {
				case <-done.C:
					cancel()
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
				if IsRateBudgetAvailable(ctx, tt.orgId) {
					err := rateLimit(ctx, tt.orgId, datapoints)
					if err != nil {
						if err == ErrRequestExceedsBurst {
							// TODO: shouldn't we have a test case that triggers this case?
						} else if err == context.Canceled {
							// that's not unexpected, because we're canceling the context once testTime has passed
						} else {
							t.Fatalf("%s: rateLimit returned unexpected error: %v", tt.name, err)
						}
					} else {
						atomic.AddUint32(&ingestedDatapoints, uint32(datapoints))
					}
				} else {
					atomic.AddUint32(&rejectedRequests, 1)
				}
			}(datapoints)
		}

		wg.Wait()

		if atomic.LoadUint32(&ingestedDatapoints) < tt.expectedIngestedDatapointsMin || atomic.LoadUint32(&ingestedDatapoints) > tt.expectedIngestedDatapointsMax {
			t.Fatalf("%s: ingested datapoints expected in range %d - %d, Got %d", tt.name, tt.expectedIngestedDatapointsMin, tt.expectedIngestedDatapointsMax, atomic.LoadUint32(&ingestedDatapoints))
		}

		if atomic.LoadUint32(&rejectedRequests) < tt.expectedRejectedRequestsMin || atomic.LoadUint32(&rejectedRequests) > tt.expectedRejectedRequestsMax {
			t.Fatalf("%s: rejected requests expected in range %d - %d, Got %d", tt.name, tt.expectedRejectedRequestsMin, tt.expectedRejectedRequestsMax, atomic.LoadUint32(&rejectedRequests))
		}
	}
}
