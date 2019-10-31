package api

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"

	"github.com/raintank/tsdb-gw/api/models"
	"github.com/raintank/tsdb-gw/auth"
	"golang.org/x/net/http/httpguts"
)

func upgradeType(h http.Header) string {
	if !httpguts.HeaderValuesContainsToken(h["Connection"], "Upgrade") {
		return ""
	}
	return strings.ToLower(h.Get("Upgrade"))
}

func createWSBackendServer(t *testing.T) *httptest.Server {
	// Creates a test server with Websocket handler
	return httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verifying whether right Websocket headers are set
		if upgradeType(r.Header) != "websocket" {
			t.Error("unexpected backend request")
			http.Error(w, "unexpected request", 400)
			return
		}

		c, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer c.Close()

		// Upgrading connection to Websocket
		io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: upgrade\r\nUpgrade: WebSocket\r\n\r\n")

		// Reading messages from client
		bs := bufio.NewScanner(c)
		if !bs.Scan() {
			t.Errorf("backend failed to read line from client: %v", bs.Err())
			return
		}
		fmt.Fprintf(c, "backend got %q\n", bs.Text())
	}))

}

// ReverseProxy in Go uses Hijack() method to handle Websocket connection requests.
// Any type that satisfies http.Hijacker interface has that method.
// This function is to test whether all the middlewares or other pieces of codes that manipulate http.ResponseWriter which
// eventually gets passed to ServeHTTP method of ReverseProxy satisfies http.Hijacker interface.
// If any newly added middleware does not satisfy http.Hijacker interface, this test would fail.
func TestReverseProxyWebSocketWithHijacker(t *testing.T) {
	*addr = ":0"

	// Creating a backend server for proxying WS requests to it
	backendServer := createWSBackendServer(t)
	backendServer.Start()
	defer backendServer.Close()

	// Creating a proxy server for proxying requests to backend server created previously
	backURL, _ := url.Parse(backendServer.URL)
	rproxy := httputil.NewSingleHostReverseProxy(backURL)

	a := New("grafana-instance", "test-ws")
	a.Router.Any("/ws", a.GenerateHandlers("read", false, false, false, a.PromStats("read"), func(c *models.Context) {
		rproxy.ServeHTTP(c.Resp, c.Req.Request)
	})...)

	frontendProxy := httptest.Server{
		Listener: a.l,
		Config:   &http.Server{Handler: a.Router},
	}
	frontendProxy.Start()
	defer frontendProxy.Close()

	// Sending WS request to proxy server and verifying whether it works fine
	req, _ := http.NewRequest("GET", frontendProxy.URL+"/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.SetBasicAuth("1", auth.AdminKey)

	c := frontendProxy.Client()
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != 101 {
		t.Fatalf("status = %v; want 101", res.Status)
	}

	rwc, ok := res.Body.(io.ReadWriteCloser)
	if !ok {
		t.Fatalf("response body is of type %T; does not implement ReadWriteCloser", res.Body)
	}
	defer rwc.Close()

	io.WriteString(rwc, "Hello\n")
	bs := bufio.NewScanner(rwc)
	if !bs.Scan() {
		t.Fatalf("Scan: %v", bs.Err())
	}

	got := bs.Text()
	want := `backend got "Hello"`
	if got != want {
		t.Errorf("got %#q, want %#q", got, want)
	}
}

// This is an example type which does not satisfy http.Hijacker interface since it does not implement Hijack function
type ResponseWriterWithoutHijacker struct {
	http.ResponseWriter
}

func (rw *ResponseWriterWithoutHijacker) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

// This test is just for showing how ReverseProxy would fail when ResponseWriter does not satisfy http.Hijacker interface
func TestReverseProxyWebSocketWithoutHijacker(t *testing.T) {
	*addr = ":0"

	backendServer := createWSBackendServer(t)
	backendServer.Start()
	defer backendServer.Close()

	backURL, _ := url.Parse(backendServer.URL)
	rproxy := httputil.NewSingleHostReverseProxy(backURL)

	frontendProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rproxy.ServeHTTP(&ResponseWriterWithoutHijacker{w}, r)
	}))

	defer frontendProxy.Close()

	req, _ := http.NewRequest("GET", frontendProxy.URL+"/ws", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.SetBasicAuth("1", auth.AdminKey)

	c := frontendProxy.Client()
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	if res.StatusCode != 502 {
		t.Fatalf("status = %v; want 502", res.Status)
	}
}
