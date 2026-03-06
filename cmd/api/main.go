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
	"os"
	"strings"
	"time"

	"github.com/mcstatus-io/mcutil/v4/formatting"
	"github.com/mcstatus-io/mcutil/v4/options"
	"github.com/mcstatus-io/mcutil/v4/response"
	"github.com/mcstatus-io/mcutil/v4/status"
	"github.com/mcstatus-io/mcutil/v4/util"
)

const responseTTL = time.Minute

type apiStatusResponse struct {
	Online      bool                `json:"online"`
	Host        string              `json:"host"`
	Port        uint16              `json:"port"`
	IPAddress   string              `json:"ip_address"`
	EULABlocked bool                `json:"eula_blocked"`
	RetrievedAt int64               `json:"retrieved_at"`
	ExpiresAt   int64               `json:"expires_at"`
	SRVRecord   *response.SRVRecord `json:"srv_record"`
	Version     apiVersion          `json:"version"`
	Players     apiPlayers          `json:"players"`
	MOTD        apiMOTD             `json:"motd"`
	Icon        *string             `json:"icon,omitempty"`
	Mods        []apiMod            `json:"mods"`
	Software    any                 `json:"software"`
	Plugins     []apiPlugin         `json:"plugins"`
	Error       string              `json:"error,omitempty"`
}

type apiVersion struct {
	NameRaw   string `json:"name_raw"`
	NameClean string `json:"name_clean"`
	NameHTML  string `json:"name_html"`
	Protocol  int64  `json:"protocol"`
}

type apiPlayers struct {
	Online int64       `json:"online"`
	Max    int64       `json:"max"`
	List   []apiPlayer `json:"list"`
}

type apiPlayer struct {
	UUID      string `json:"uuid"`
	NameRaw   string `json:"name_raw"`
	NameClean string `json:"name_clean"`
	NameHTML  string `json:"name_html"`
}

type apiMOTD struct {
	Raw   string `json:"raw"`
	Clean string `json:"clean"`
	HTML  string `json:"html"`
}

type apiMod struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type apiPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
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
	timeout     time.Duration
	includeIcon bool
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	timeout := flag.Duration("timeout", 5*time.Second, "Lookup timeout")

	flag.Parse()

	if *timeout <= 0 {
		log.Fatal("timeout must be > 0")
	}

	includeIcon := parseBoolEnv("icon")

	server := &http.Server{
		Addr:              *listen,
		Handler:           &apiHandler{timeout: *timeout, includeIcon: includeIcon},
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf(
		"mc-status api listening on %s (GET /{server_type}/{server_ip}:{server_port}), include_icon=%t",
		*listen,
		includeIcon,
	)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse("only GET is supported"))

		return
	}

	serverType, address, ok := parseRequestPath(r.URL.Path)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse("route must be /{server_type}/{server_ip}:{server_port}"))

		return
	}

	host, port, err := parseTarget(serverType, address)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	switch serverType {
	case "java":
		javaStatus, err := fetchJavaStatus(ctx, host, port, h.timeout)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, offlineResponse(host, port, err))

			return
		}

		writeJSON(w, http.StatusOK, mapJavaResponse(host, port, javaStatus, h.includeIcon))
	case "bedrock":
		bedrockStatus, err := fetchBedrockStatus(ctx, host, port, h.timeout)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, offlineResponse(host, port, err))

			return
		}

		writeJSON(w, http.StatusOK, mapBedrockResponse(host, port, bedrockStatus))
	default:
		writeJSON(w, http.StatusBadRequest, errorResponse("server_type must be one of: java, bedrock"))
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

func mapJavaResponse(host string, port uint16, result *response.StatusModern, includeIcon bool) apiStatusResponse {
	playerList := make([]apiPlayer, 0, len(result.Players.Sample))
	for _, v := range result.Players.Sample {
		playerList = append(playerList, apiPlayer{
			UUID:      v.ID,
			NameRaw:   v.Name.Raw,
			NameClean: v.Name.Clean,
			NameHTML:  v.Name.HTML,
		})
	}

	response := newBaseResponse(host, port)
	response.Online = true
	response.SRVRecord = result.SRVRecord
	response.Version = apiVersion{
		NameRaw:   result.Version.Name.Raw,
		NameClean: result.Version.Name.Clean,
		NameHTML:  result.Version.Name.HTML,
		Protocol:  result.Version.Protocol,
	}
	response.Players = apiPlayers{
		Online: valueOrZero(result.Players.Online),
		Max:    valueOrZero(result.Players.Max),
		List:   playerList,
	}
	response.MOTD = mapMOTD(&result.MOTD)
	response.Mods = mapMods(result.Mods)

	if includeIcon {
		response.Icon = result.Favicon
	}

	return response
}

func mapBedrockResponse(host string, port uint16, result *response.StatusBedrock) apiStatusResponse {
	versionInfo := parseFormattingString(stringValueOrEmpty(result.Version))

	response := newBaseResponse(host, port)
	response.Online = true
	response.Version = apiVersion{
		NameRaw:   versionInfo.Raw,
		NameClean: versionInfo.Clean,
		NameHTML:  versionInfo.HTML,
		Protocol:  valueOrZero(result.ProtocolVersion),
	}
	response.Players = apiPlayers{
		Online: valueOrZero(result.OnlinePlayers),
		Max:    valueOrZero(result.MaxPlayers),
		List:   make([]apiPlayer, 0),
	}

	response.MOTD = mapMOTD(result.MOTD)

	return response
}

func offlineResponse(host string, port uint16, err error) apiStatusResponse {
	response := newBaseResponse(host, port)
	response.Online = false
	response.Error = err.Error()

	return response
}

func errorResponse(message string) apiStatusResponse {
	response := newBaseResponse("", 0)
	response.Error = message

	return response
}

func mapMOTD(result *formatting.Result) apiMOTD {
	if result == nil {
		return apiMOTD{}
	}

	return apiMOTD{
		Raw:   result.Raw,
		Clean: result.Clean,
		HTML:  result.HTML,
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

func parseFormattingString(raw string) formatting.Result {
	if len(raw) == 0 {
		return formatting.Result{}
	}

	parsed, err := formatting.Parse(raw)
	if err != nil || parsed == nil {
		return formatting.Result{
			Raw:   raw,
			Clean: raw,
			HTML:  raw,
		}
	}

	return *parsed
}

func mapMods(modInfo *response.ModInfo) []apiMod {
	if modInfo == nil || len(modInfo.List) == 0 {
		return make([]apiMod, 0)
	}

	mods := make([]apiMod, 0, len(modInfo.List))

	for _, mod := range modInfo.List {
		mods = append(mods, apiMod{
			ID:      mod.ID,
			Version: mod.Version,
		})
	}

	return mods
}

func parseBoolEnv(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))

	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func newBaseResponse(host string, port uint16) apiStatusResponse {
	retrievedAt := time.Now().UnixMilli()

	return apiStatusResponse{
		Host:        host,
		Port:        port,
		IPAddress:   host,
		EULABlocked: false,
		RetrievedAt: retrievedAt,
		ExpiresAt:   retrievedAt + responseTTL.Milliseconds(),
		Version:     apiVersion{},
		Players: apiPlayers{
			List: make([]apiPlayer, 0),
		},
		MOTD:     apiMOTD{},
		Mods:     make([]apiMod, 0),
		Software: nil,
		Plugins:  make([]apiPlugin, 0),
	}
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}
