package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHealthURL(t *testing.T) {
	cases := map[string]string{
		":8000":          "http://127.0.0.1:8000/api/health",
		"0.0.0.0:8000":   "http://127.0.0.1:8000/api/health",
		"[::]:8000":      "http://127.0.0.1:8000/api/health",
		"127.0.0.1:8077": "http://127.0.0.1:8077/api/health",
		"[::1]:9000":     "http://[::1]:9000/api/health",
	}
	for addr, want := range cases {
		got, err := healthURL(addr)
		require.NoError(t, err, addr)
		require.Equal(t, want, got, addr)
	}
	_, err := healthURL("8000")
	require.Error(t, err)
}

func TestProbeHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer ok.Close()
	require.NoError(t, probeHealth(t.Context(), ok.URL))

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer broken.Close()
	err := probeHealth(t.Context(), broken.URL)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "503"), err.Error())

	require.Error(t, probeHealth(t.Context(), "http://127.0.0.1:1/api/health"))
}
