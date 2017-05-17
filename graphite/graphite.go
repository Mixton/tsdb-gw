package graphite

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/raintank/tsdb-gw/util"
)

var (
	GraphiteUrl  *url.URL
	WorldpingUrl *url.URL
	wpProxy      httputil.ReverseProxy
	gProxy       httputil.ReverseProxy
)

func Init(graphiteUrl, worldpingUrl string) error {
	var err error
	GraphiteUrl, err = url.Parse(graphiteUrl)
	if err != nil {
		return err
	}
	WorldpingUrl, err = url.Parse(worldpingUrl)
	return err
}

func Proxy(orgId int64, proxyPath string, request *http.Request) *httputil.ReverseProxy {

	// check if this is a special raintank_db requests then proxy to the worldping-api service.
	if proxyPath == "metrics/find" {
		query := request.FormValue("query")
		if strings.HasPrefix(query, "raintank_db") {
			wpProxy.Director = func(req *http.Request) {
				req.URL.Scheme = WorldpingUrl.Scheme
				req.URL.Host = WorldpingUrl.Host
				req.URL.Path = util.JoinUrlFragments(WorldpingUrl.Path, "/api/graphite/"+proxyPath)
			}
			return &wpProxy
		}
	}

	gProxy.Director = func(req *http.Request) {
		req.URL.Scheme = GraphiteUrl.Scheme
		req.URL.Host = GraphiteUrl.Host
		req.Header.Del("X-Org-Id")
		req.Header.Add("X-Org-Id", strconv.FormatInt(orgId, 10))
		req.URL.Path = util.JoinUrlFragments(GraphiteUrl.Path, proxyPath)
	}

	return &gProxy
}
