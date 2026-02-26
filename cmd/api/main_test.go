package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mcstatus-io/mcutil/v4/formatting"
	"github.com/mcstatus-io/mcutil/v4/response"
)

func TestServeHTTPJavaSuccess(t *testing.T) {
	originalJava := fetchJavaStatus
	originalBedrock := fetchBedrockStatus
	defer func() {
		fetchJavaStatus = originalJava
		fetchBedrockStatus = originalBedrock
	}()

	online := int64(2)
	max := int64(20)

	fetchJavaStatus = func(_ context.Context, host string, port uint16, timeout time.Duration) (*response.StatusModern, error) {
		if host != "play.example.com" {
			t.Fatalf("unexpected host: %s", host)
		}
		if port != 25565 {
			t.Fatalf("unexpected port: %d", port)
		}
		if timeout != 5*time.Second {
			t.Fatalf("unexpected timeout: %v", timeout)
		}

		return &response.StatusModern{
			Version: response.Version{
				Name: formatting.Result{
					Raw:   "1.20.1",
					Clean: "1.20.1",
				},
				Protocol: 763,
			},
			Players: response.Players{
				Online: &online,
				Max:    &max,
				Sample: []response.SamplePlayer{
					{
						ID: "1",
						Name: formatting.Result{
							Raw:   "\u00a7aPlayer1",
							Clean: "Player1",
						},
					},
				},
			},
			MOTD: formatting.Result{
				Raw:   "\u00a7aWelcome to the server",
				Clean: "Welcome to the server",
			},
		}, nil
	}

	fetchBedrockStatus = func(context.Context, string, uint16, time.Duration) (*response.StatusBedrock, error) {
		t.Fatal("bedrock fetcher should not be called")

		return nil, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/java/play.example.com:25565", nil)
	rec := httptest.NewRecorder()
	handler := &apiHandler{timeout: 5 * time.Second}

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}

	var got apiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !got.Online {
		t.Fatalf("expected online=true")
	}

	if got.Hostname != "play.example.com:25565" {
		t.Fatalf("unexpected hostname: %s", got.Hostname)
	}

	if got.Version.Name != "1.20.1" || got.Version.Protocol != 763 {
		t.Fatalf("unexpected version: %+v", got.Version)
	}

	if got.Players.Online != 2 || got.Players.Max != 20 {
		t.Fatalf("unexpected players count: %+v", got.Players)
	}

	if len(got.Players.List) != 1 {
		t.Fatalf("unexpected sample list length: %d", len(got.Players.List))
	}

	if got.Players.List[0].Name != "\u00a7aPlayer1" || got.Players.List[0].NameClean != "Player1" {
		t.Fatalf("unexpected sample player: %+v", got.Players.List[0])
	}

	if got.MOTD.Clean != "Welcome to the server" || got.MOTD.Raw != "\u00a7aWelcome to the server" {
		t.Fatalf("unexpected motd: %+v", got.MOTD)
	}
}

func TestServeHTTPBedrockSuccess(t *testing.T) {
	originalJava := fetchJavaStatus
	originalBedrock := fetchBedrockStatus
	defer func() {
		fetchJavaStatus = originalJava
		fetchBedrockStatus = originalBedrock
	}()

	protocol := int64(766)
	online := int64(4)
	max := int64(30)
	version := "1.20.80"

	fetchJavaStatus = func(context.Context, string, uint16, time.Duration) (*response.StatusModern, error) {
		t.Fatal("java fetcher should not be called")

		return nil, nil
	}

	fetchBedrockStatus = func(_ context.Context, host string, port uint16, timeout time.Duration) (*response.StatusBedrock, error) {
		if host != "bedrock.example.com" {
			t.Fatalf("unexpected host: %s", host)
		}
		if port != 19132 {
			t.Fatalf("unexpected default bedrock port: %d", port)
		}
		if timeout != 3*time.Second {
			t.Fatalf("unexpected timeout: %v", timeout)
		}

		motd, err := formatting.Parse("\u00a7bBedrock MOTD")
		if err != nil {
			t.Fatalf("failed to parse motd: %v", err)
		}

		return &response.StatusBedrock{
			ProtocolVersion: &protocol,
			OnlinePlayers:   &online,
			MaxPlayers:      &max,
			Version:         &version,
			MOTD:            motd,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/bedrock/bedrock.example.com", nil)
	rec := httptest.NewRecorder()
	handler := &apiHandler{timeout: 3 * time.Second}

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}

	var got apiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !got.Online {
		t.Fatalf("expected online=true")
	}

	if got.Hostname != "bedrock.example.com:19132" {
		t.Fatalf("unexpected hostname: %s", got.Hostname)
	}

	if got.Version.Name != "1.20.80" || got.Version.Protocol != 766 {
		t.Fatalf("unexpected version: %+v", got.Version)
	}

	if got.Players.Online != 4 || got.Players.Max != 30 {
		t.Fatalf("unexpected players count: %+v", got.Players)
	}

	if len(got.Players.List) != 0 {
		t.Fatalf("unexpected player list for bedrock: %+v", got.Players.List)
	}

	if got.MOTD.Clean != "Bedrock MOTD" || got.MOTD.Raw != "\u00a7bBedrock MOTD" {
		t.Fatalf("unexpected motd: %+v", got.MOTD)
	}
}

func TestServeHTTPUnknownServerType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/foo/play.example.com:25565", nil)
	rec := httptest.NewRecorder()

	(&apiHandler{timeout: 5 * time.Second}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}

	var got apiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got.Error == "" {
		t.Fatalf("expected error message for unknown server type")
	}
}

func TestServeHTTPUpstreamError(t *testing.T) {
	originalJava := fetchJavaStatus
	defer func() {
		fetchJavaStatus = originalJava
	}()

	fetchJavaStatus = func(context.Context, string, uint16, time.Duration) (*response.StatusModern, error) {
		return nil, errors.New("dial timeout")
	}

	req := httptest.NewRequest(http.MethodGet, "/java/play.example.com:25565", nil)
	rec := httptest.NewRecorder()

	(&apiHandler{timeout: 5 * time.Second}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}

	var got apiStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if got.Online {
		t.Fatalf("expected online=false for upstream error")
	}

	if got.Error == "" {
		t.Fatalf("expected upstream error message")
	}
}
