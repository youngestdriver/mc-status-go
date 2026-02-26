package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mcstatus-io/mcutil/v4/formatting"
	"github.com/mcstatus-io/mcutil/v4/options"
	"github.com/mcstatus-io/mcutil/v4/response"
	"github.com/mcstatus-io/mcutil/v4/status"
	"github.com/mcstatus-io/mcutil/v4/util"
)

type apiStatusResponse struct {
	Online   bool       `json:"online"`
	Hostname string     `json:"hostname"`
	Version  apiVersion `json:"version"`
	Players  apiPlayers `json:"players"`
	MOTD     apiMOTD    `json:"motd"`
	Error    string     `json:"error,omitempty"`
}

type apiVersion struct {
	Name     string `json:"name"`
	Protocol int64  `json:"protocol"`
}

type apiPlayers struct {
	Online int64       `json:"online"`
	Max    int64       `json:"max"`
	List   []apiPlayer `json:"list"`
}

type apiPlayer struct {
	Name      string `json:"name"`
	NameClean string `json:"name_clean"`
}

type apiMOTD struct {
	Clean string `json:"clean"`
	Raw   string `json:"raw"`
}

type javaStatusFetcher func(context.Context, string, uint16, time.Duration) (*response.StatusModern, error)
type bedrockStatusFetcher func(context.Context, string, uint16, time.Duration) (*response.StatusBedrock, error)

var (
	fetchJavaStatus    javaStatusFetcher    = realFetchJavaStatus
	fetchBedrockStatus bedrockStatusFetcher = realFetchBedrockStatus
)

func realFetchJavaStatus(ctx context.Context, host string, port uint16, timeout time.Duration) (*response.StatusModern, error) {
	return status.Modern(ctx, host, port, options.StatusModern{
		EnableSRV:       true,
		Timeout:         timeout,
		ProtocolVersion: -1,
		Ping:            true,
		Debug:           false,
	})
}

func realFetchBedrockStatus(ctx context.Context, host string, port uint16, timeout time.Duration) (*response.StatusBedrock, error) {
	return status.Bedrock(ctx, host, port, options.StatusBedrock{
		Timeout:    timeout,
		ClientGUID: rand.Int63(),
	})
}

type apiHandler struct {
	timeout time.Duration
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	timeout := flag.Duration("timeout", 5*time.Second, "Lookup timeout")

	flag.Parse()

	if *timeout <= 0 {
		log.Fatal("timeout must be > 0")
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           &apiHandler{timeout: *timeout},
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("mc-status api listening on %s (GET /{server_type}/{server_ip}:{server_port})", *listen)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, apiStatusResponse{
			Error: "only GET is supported",
		})

		return
	}

	serverType, address, ok := parseRequestPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, apiStatusResponse{
			Error: "route must be /{server_type}/{server_ip}:{server_port}",
		})

		return
	}

	host, port, err := parseTarget(serverType, address)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiStatusResponse{
			Error: err.Error(),
		})

		return
	}

	hostname := fmt.Sprintf("%s:%d", host, port)
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	switch serverType {
	case "java":
		javaStatus, err := fetchJavaStatus(ctx, host, port, h.timeout)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, offlineResponse(hostname, err))

			return
		}

		writeJSON(w, http.StatusOK, mapJavaResponse(hostname, javaStatus))
	case "bedrock":
		bedrockStatus, err := fetchBedrockStatus(ctx, host, port, h.timeout)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, offlineResponse(hostname, err))

			return
		}

		writeJSON(w, http.StatusOK, mapBedrockResponse(hostname, bedrockStatus))
	default:
		writeJSON(w, http.StatusBadRequest, apiStatusResponse{
			Error: "server_type must be one of: java, bedrock",
		})
	}
}

func parseRequestPath(path string) (string, string, bool) {
	trimmed := strings.Trim(path, "/")
	if len(trimmed) == 0 {
		return "", "", false
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", "", false
	}

	serverType := strings.ToLower(strings.TrimSpace(parts[0]))
	if len(serverType) == 0 {
		return "", "", false
	}

	address, err := url.PathUnescape(parts[1])
	if err != nil {
		return "", "", false
	}

	address = strings.TrimSpace(address)
	if len(address) == 0 {
		return "", "", false
	}

	return serverType, address, true
}

func parseTarget(serverType string, address string) (string, uint16, error) {
	defaultPort, err := defaultPortForType(serverType)
	if err != nil {
		return "", 0, err
	}

	host, port, err := util.ParseAddress(address)
	if err != nil {
		return "", 0, fmt.Errorf("invalid server target %q", address)
	}

	if len(host) == 0 {
		return "", 0, fmt.Errorf("invalid server target %q", address)
	}

	if port == nil {
		return host, defaultPort, nil
	}

	return host, *port, nil
}

func defaultPortForType(serverType string) (uint16, error) {
	switch serverType {
	case "java":
		return util.DefaultJavaPort, nil
	case "bedrock":
		return util.DefaultBedrockPort, nil
	default:
		return 0, fmt.Errorf("server_type must be one of: java, bedrock")
	}
}

func mapJavaResponse(hostname string, result *response.StatusModern) apiStatusResponse {
	playerList := make([]apiPlayer, 0, len(result.Players.Sample))
	for _, v := range result.Players.Sample {
		playerList = append(playerList, apiPlayer{
			Name:      v.Name.Raw,
			NameClean: v.Name.Clean,
		})
	}

	return apiStatusResponse{
		Online:   true,
		Hostname: hostname,
		Version: apiVersion{
			Name:     result.Version.Name.Clean,
			Protocol: result.Version.Protocol,
		},
		Players: apiPlayers{
			Online: valueOrZero(result.Players.Online),
			Max:    valueOrZero(result.Players.Max),
			List:   playerList,
		},
		MOTD: apiMOTD{
			Clean: result.MOTD.Clean,
			Raw:   result.MOTD.Raw,
		},
	}
}

func mapBedrockResponse(hostname string, result *response.StatusBedrock) apiStatusResponse {
	return apiStatusResponse{
		Online:   true,
		Hostname: hostname,
		Version: apiVersion{
			Name:     stringValueOrEmpty(result.Version),
			Protocol: valueOrZero(result.ProtocolVersion),
		},
		Players: apiPlayers{
			Online: valueOrZero(result.OnlinePlayers),
			Max:    valueOrZero(result.MaxPlayers),
			List:   make([]apiPlayer, 0),
		},
		MOTD: mapMOTD(result.MOTD),
	}
}

func offlineResponse(hostname string, err error) apiStatusResponse {
	return apiStatusResponse{
		Online:   false,
		Hostname: hostname,
		Version:  apiVersion{},
		Players: apiPlayers{
			List: make([]apiPlayer, 0),
		},
		MOTD:  apiMOTD{},
		Error: err.Error(),
	}
}

func mapMOTD(result *formatting.Result) apiMOTD {
	if result == nil {
		return apiMOTD{}
	}

	return apiMOTD{
		Clean: result.Clean,
		Raw:   result.Raw,
	}
}

func valueOrZero[T ~int64](v *T) T {
	if v == nil {
		return 0
	}

	return *v
}

func stringValueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}

	return *v
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}
