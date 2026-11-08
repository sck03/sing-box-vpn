package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func rejectHTTPRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func directClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp4", address)
	}, TLSHandshakeTimeout: 8 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, ForceAttemptHTTP2: true, DisableCompression: true}, CheckRedirect: rejectHTTPRedirect}
}

func tunnelClient(active tunnel) *http.Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, _, err := dialWorker(ctx, active.socket(), "tcp", address)
		return conn, err
	}, TLSHandshakeTimeout: 8 * time.Second, IdleConnTimeout: 90 * time.Second, MaxIdleConns: 2, MaxIdleConnsPerHost: 1, ForceAttemptHTTP2: true, DisableCompression: true}
	return &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: rejectHTTPRedirect}
}

func openHTTP(ctx context.Context, client *http.Client, url string, limit int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("invalid HTTP request")
	}
	req.Header.Set("User-Agent", "sing-box-vpn/"+version)
	req.Header.Set("Accept-Encoding", "identity")
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("HTTP lookup unavailable")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("HTTP response %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		res.Body.Close()
		return nil, errors.New("HTTP response too large")
	}
	return res.Body, nil
}

func readHTTP(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	body, err := openHTTP(ctx, client, url, limit)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return readHTTPBody(body, limit)
}

func readHTTPBody(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("HTTP response too large")
	}
	return data, nil
}

func readNodeSource(ctx context.Context, client *http.Client, url string) ([]Node, error) {
	body, err := openHTTP(ctx, client, url, maxNodeSource)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return parseNodes(body)
}
