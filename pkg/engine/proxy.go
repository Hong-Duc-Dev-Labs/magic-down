package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// NormalizeProxyMode returns a canonical proxy mode ("system", "none", "http", "socks5").
func NormalizeProxyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ProxyModeNone:
		return ProxyModeNone
	case ProxyModeHTTP:
		return ProxyModeHTTP
	case ProxyModeSOCKS5:
		return ProxyModeSOCKS5
	default:
		return ProxyModeSystem
	}
}

// BuildHTTPClient constructs an *http.Client configured according to ProxyConfig.
// Client.Timeout is left at 0 for long downloads; transport dial/TLS timeouts apply.
func BuildHTTPClient(cfg ProxyConfig) (*http.Client, error) {
	mode := NormalizeProxyMode(cfg.Mode)
	baseDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		DialContext:           baseDialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	switch mode {
	case ProxyModeNone:
		tr.Proxy = nil

	case ProxyModeSystem:
		tr.Proxy = systemProxyFunc

	case ProxyModeHTTP:
		proxyURL, err := buildExplicitProxyURL("http", cfg)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(proxyURL)

	case ProxyModeSOCKS5:
		host := strings.TrimSpace(cfg.Host)
		if host == "" {
			return nil, errors.New("SOCKS5 proxy host cannot be empty")
		}
		if cfg.Port <= 0 || cfg.Port > 65535 {
			return nil, fmt.Errorf("invalid SOCKS5 proxy port: %d", cfg.Port)
		}
		var auth *xproxy.Auth
		if cfg.Username != "" || cfg.Password != "" {
			auth = &xproxy.Auth{
				User:     cfg.Username,
				Password: cfg.Password,
			}
		}
		addr := net.JoinHostPort(host, strconv.Itoa(cfg.Port))
		dialer, err := xproxy.SOCKS5("tcp", addr, auth, baseDialer)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
		}
		tr.Proxy = nil
		if cd, ok := dialer.(xproxy.ContextDialer); ok {
			tr.DialContext = cd.DialContext
		} else {
			tr.DialContext = func(ctx context.Context, network, targetAddr string) (net.Conn, error) {
				return dialer.Dial(network, targetAddr)
			}
		}
	}

	return &http.Client{
		Transport: tr,
		Timeout:   0,
	}, nil
}

func buildExplicitProxyURL(defaultScheme string, cfg ProxyConfig) (*url.URL, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, errors.New("proxy host cannot be empty")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid proxy port: %d", cfg.Port)
	}
	scheme := defaultScheme
	if strings.HasPrefix(strings.ToLower(host), "https://") {
		scheme = "https"
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "HTTPS://")
	} else if strings.HasPrefix(strings.ToLower(host), "http://") {
		scheme = "http"
		host = strings.TrimPrefix(host, "http://")
		host = strings.TrimPrefix(host, "HTTP://")
	}
	host = strings.TrimSuffix(host, "/")

	u := &url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(host, strconv.Itoa(cfg.Port)),
	}
	if cfg.Username != "" || cfg.Password != "" {
		u.User = url.UserPassword(cfg.Username, cfg.Password)
	}
	return u, nil
}

func systemProxyFunc(req *http.Request) (*url.URL, error) {
	if u, err := http.ProxyFromEnvironment(req); err != nil || u != nil {
		return u, err
	}
	return detectPlatformSystemProxy(req)
}

// TestProxyConnection verifies that a ProxyConfig can reach targetURL (defaults
// to a lightweight 204 endpoint when empty).
func TestProxyConnection(ctx context.Context, cfg ProxyConfig, targetURL string) error {
	if strings.TrimSpace(targetURL) == "" {
		targetURL = "https://www.google.com/generate_204"
	}
	client, err := BuildHTTPClient(cfg)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(testCtx, http.MethodHead, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "MagicDown/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("proxy test returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// parseWindowsProxyServer parses WinINet ProxyServer registry values such as:
//   - "127.0.0.1:8080"
//   - "http=127.0.0.1:8080;https=127.0.0.1:8443"
//   - "socks=127.0.0.1:1080"
func parseWindowsProxyServer(proxyServer, proxyOverride string, req *http.Request) (*url.URL, error) {
	raw := strings.TrimSpace(proxyServer)
	if raw == "" || req == nil || req.URL == nil {
		return nil, nil
	}
	if matchesWindowsProxyBypass(req.URL.Hostname(), proxyOverride) {
		return nil, nil
	}

	reqScheme := strings.ToLower(req.URL.Scheme)
	var selected string
	if strings.Contains(raw, "=") {
		entries := strings.Split(raw, ";")
		byScheme := make(map[string]string, len(entries))
		for _, entry := range entries {
			kv := strings.SplitN(strings.TrimSpace(entry), "=", 2)
			if len(kv) == 2 {
				byScheme[strings.ToLower(strings.TrimSpace(kv[0]))] = strings.TrimSpace(kv[1])
			}
		}
		if v := byScheme[reqScheme]; v != "" {
			selected = v
		} else if v := byScheme["http"]; v != "" {
			selected = v
		} else if v := byScheme["socks"]; v != "" {
			selected = "socks5://" + strings.TrimPrefix(v, "socks5://")
		}
	} else {
		selected = raw
	}

	if selected == "" {
		return nil, nil
	}
	if !strings.Contains(selected, "://") {
		selected = "http://" + selected
	}
	return url.Parse(selected)
}

func matchesWindowsProxyBypass(host, proxyOverride string) bool {
	if host == "" || strings.TrimSpace(proxyOverride) == "" {
		return false
	}
	h := strings.ToLower(host)
	for _, entry := range strings.Split(proxyOverride, ";") {
		p := strings.ToLower(strings.TrimSpace(entry))
		if p == "" {
			continue
		}
		if p == "<local>" {
			if !strings.Contains(h, ".") || h == "localhost" || h == "127.0.0.1" || h == "::1" {
				return true
			}
			continue
		}
		if p == h {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(h, p[1:]) {
			return true
		}
		if strings.HasSuffix(p, "*") && strings.HasPrefix(h, strings.TrimSuffix(p, "*")) {
			return true
		}
	}
	return false
}
