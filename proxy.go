package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// The sandbox lets a command talk to this proxy and to nothing else outside the machine. The proxy passes a
// connection on only when its host is allowed, and remembers every host it turned away so the run can name them.

type Proxy struct {
	allow func(host string) bool
	ln    net.Listener
	srv   *http.Server

	mu      sync.Mutex
	blocked map[string]int
}

func newProxy(allow func(host string) bool) *Proxy {
	return &Proxy{allow: allow, blocked: map[string]int{}}
}

// start serves on the listener until stop.
func (p *Proxy) start(ln net.Listener) {
	p.ln = ln
	p.srv = &http.Server{Handler: http.HandlerFunc(p.handle), ReadHeaderTimeout: 30 * time.Second}
	go p.srv.Serve(ln)
}

func (p *Proxy) stop() {
	if p.srv != nil {
		p.srv.Close()
	}
}

func (p *Proxy) port() int {
	if a, ok := p.ln.Addr().(*net.TCPAddr); ok {
		return a.Port
	}
	return 0
}

// blockedHosts returns "host ×n" for every host turned away, most attempts first.
func (p *Proxy) blockedHosts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	hosts := make([]string, 0, len(p.blocked))
	for h := range p.blocked {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool {
		if p.blocked[hosts[i]] != p.blocked[hosts[j]] {
			return p.blocked[hosts[i]] > p.blocked[hosts[j]]
		}
		return hosts[i] < hosts[j]
	})
	return hosts
}

func (p *Proxy) permitted(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host != "" && (isLocalHost(host) || p.allow(host)) {
		return true
	}
	p.mu.Lock()
	p.blocked[host]++
	p.mu.Unlock()
	return false
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if !r.URL.IsAbs() || r.URL.Host == "" {
		http.Error(w, "keymoat: this is a proxy", http.StatusBadRequest)
		return
	}
	if !p.permitted(r.URL.Host) {
		http.Error(w, fmt.Sprintf("keymoat: %s is not an allowed host", r.URL.Hostname()), http.StatusForbidden)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range []string{"Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate"} {
		out.Header.Del(h)
	}
	resp, err := directTransport.RoundTrip(out)
	if err != nil {
		http.Error(w, "keymoat: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// The proxy itself never goes through a proxy from the environment: that would be a way around the list.
var directTransport = &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext, ResponseHeaderTimeout: 60 * time.Second}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	if !p.permitted(r.Host) {
		http.Error(w, fmt.Sprintf("keymoat: %s is not an allowed host", r.Host), http.StatusForbidden)
		return
	}
	upstream, err := net.DialTimeout("tcp", r.Host, 15*time.Second)
	if err != nil {
		http.Error(w, "keymoat: "+err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "keymoat: cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	client.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	go func() {
		// Bytes the client sent right behind its CONNECT are already in the buffer.
		io.Copy(upstream, buf)
		closeWrite(upstream)
	}()
	io.Copy(client, upstream)
	closeWrite(client)
	client.Close()
	upstream.Close()
}

func closeWrite(c net.Conn) {
	if t, ok := c.(interface{ CloseWrite() error }); ok {
		t.CloseWrite()
	}
}

// proxyEnv is what points the common tools at the proxy.
func proxyEnv(port int) []string {
	u := fmt.Sprintf("http://127.0.0.1:%d", port)
	none := "localhost,127.0.0.1,::1"
	return []string{
		"HTTP_PROXY=" + u, "HTTPS_PROXY=" + u, "http_proxy=" + u, "https_proxy=" + u, "ALL_PROXY=" + u, "all_proxy=" + u,
		"NO_PROXY=" + none, "no_proxy=" + none,
		"npm_config_proxy=" + u, "npm_config_https_proxy=" + u, "npm_config_noproxy=" + none,
		"YARN_HTTP_PROXY=" + u, "YARN_HTTPS_PROXY=" + u,
		// Node's own fetch only honours a proxy from the environment when told to.
		"NODE_USE_ENV_PROXY=1",
	}
}
