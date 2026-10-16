package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

const healthTimeout = 3 * time.Second

// healthURL — адрес проверки для LINGUASTACK_HTTP_ADDR. Сервер, слушающий
// все интерфейсы, проверяется через loopback.
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("адрес %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/health", nil
}

// probeHealth — проверка для HEALTHCHECK контейнера: в distroless-образе
// нет curl, поэтому её выполняет сам бинарник (api -healthcheck).
func probeHealth(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("сервер не отвечает: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health: HTTP %d", resp.StatusCode)
	}
	return nil
}
