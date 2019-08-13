package cortex

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"github.com/oxtoacart/bpool"
	"github.com/raintank/tsdb-gw/api/models"
	"github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/user"
)

type ProxyType int

const (
	HTTPProxy ProxyType = iota
	GRPCProxy
)

func newHTTPWriteProxy(writeURL string) (*httputil.ReverseProxy, error) {
	cortexURL, err := url.Parse(writeURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse cortex write url '%s': %v", writeURL, err)
	}

	// Separate Proxy for Writes, add BufferPool for perf reasons if needed
	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = cortexURL.Scheme
			req.URL.Host = cortexURL.Host
		},
		BufferPool: bpool.NewBytePool(*writeBPoolSize, *writeBPoolWidth),

		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:          10000,
			MaxIdleConnsPerHost:   1000, // see https://github.com/golang/go/issues/13801
			IdleConnTimeout:       90 * time.Second,
			DisableKeepAlives:     true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}, nil
}

func newGRPCWriteProxy(writeURL string) (*server.Client, error) {
	httpGrpcClient, err := server.NewClient(writeURL)
	if err != nil {
		return nil, fmt.Errorf("unable to create grpc client: %v", err)
	}

	return httpGrpcClient, nil
}

type CortexWriteProxy struct {
	handler http.Handler
}

// NewCortexWriteProxy initializes the cortex reverse proxies.
func NewCortexWriteProxy(ptype ProxyType, writeURL string) (cwp *CortexWriteProxy, err error) {
	cwp = &CortexWriteProxy{}

	switch ptype {
	case HTTPProxy:
		cwp.handler, err = newHTTPWriteProxy(writeURL)

	case GRPCProxy:
		cwp.handler, err = newGRPCWriteProxy(writeURL)
	}

	return
}

// Write adds the required headers and forwards the write request.
func (cwp *CortexWriteProxy) Write(c *models.Context) {
	id := strconv.Itoa(c.User.ID)
	req := c.Req.Request
	req.Header.Set("X-Scope-OrgID", id)
	req = req.WithContext(user.InjectOrgID(req.Context(), id))

	cwp.handler.ServeHTTP(c.Resp, req)
}
