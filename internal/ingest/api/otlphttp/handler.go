package otlphttp

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"

	"github.com/Talif787/prism/internal/ingest/app"
	"github.com/Talif787/prism/internal/ingest/domain"
	"github.com/Talif787/prism/internal/platform/httpx"
)

// Handler serves the OTLP/HTTP endpoints.
type Handler struct {
	pipeline     *app.Pipeline
	maxBodyBytes int64
	logger       *slog.Logger
}

func NewHandler(p *app.Pipeline, maxBodyBytes int64, logger *slog.Logger) *Handler {
	return &Handler{pipeline: p, maxBodyBytes: maxBodyBytes, logger: logger}
}

// Register mounts the standard OTLP/HTTP routes. These paths match the OTel
// exporter defaults, so an SDK pointed at this host with no path override works.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/metrics", h.handleMetrics)
	mux.HandleFunc("POST /v1/traces", h.handleTraces)
	mux.HandleFunc("POST /v1/logs", h.handleLogs)
}

type decodeFunc func(body []byte, contentType string) (decoded, error)

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, domain.SignalMetrics, decodeMetrics, &colmetricspb.ExportMetricsServiceResponse{})
}

func (h *Handler) handleTraces(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, domain.SignalTraces, decodeTraces, &coltracepb.ExportTraceServiceResponse{})
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	h.handle(w, r, domain.SignalLogs, decodeLogs, &collogspb.ExportLogsServiceResponse{})
}

func (h *Handler) handle(w http.ResponseWriter, r *http.Request, signal domain.Signal, decode decodeFunc, resp proto.Message) {
	apiKey, ok := extractAPIKey(r)
	if !ok {
		writeStatus(w, r, http.StatusUnauthorized, "unauthorized", "missing API key")
		return
	}

	contentType := r.Header.Get("Content-Type")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxBodyBytes))
	if err != nil {
		writeStatus(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds limit")
		return
	}

	dec, err := decode(body, contentType)
	if err != nil {
		if errors.Is(err, domain.ErrEmptyPayload) {
			writeStatus(w, r, http.StatusBadRequest, "empty_payload", "request body is empty")
			return
		}
		writeStatus(w, r, http.StatusBadRequest, "malformed_payload", "could not decode OTLP payload")
		return
	}

	result, err := h.pipeline.Ingest(r.Context(), app.Input{
		APIKey:       apiKey,
		Signal:       signal,
		Payload:      dec.payload,
		PointCount:   dec.pointCount,
		Fingerprints: dec.fingerprints,
		ReceivedAt:   time.Now().UTC(),
	})
	if err != nil {
		h.writePipelineError(w, r, err)
		return
	}

	_ = result
	writeExportResponse(w, r, contentType, resp)
}

func (h *Handler) writePipelineError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, app.ErrUnauthorized):
		writeStatus(w, r, http.StatusUnauthorized, "unauthorized", "invalid API key")
	case errors.Is(err, app.ErrForbidden):
		writeStatus(w, r, http.StatusForbidden, "forbidden", "key lacks ingest scope")
	case errors.Is(err, app.ErrRateLimited):
		w.Header().Set("Retry-After", "1")
		writeStatus(w, r, http.StatusTooManyRequests, "rate_limited", "ingestion rate limit exceeded")
	case errors.Is(err, app.ErrCardinality):
		writeStatus(w, r, http.StatusTooManyRequests, "cardinality_exceeded", "series cardinality budget exceeded")
	default:
		h.logger.ErrorContext(r.Context(), "ingest pipeline error", slog.Any("error", err))
		writeStatus(w, r, http.StatusServiceUnavailable, "unavailable", "unable to accept telemetry")
	}
}

// extractAPIKey accepts the key via Authorization: Bearer or the X-Prism-Key
// header, both settable through OTEL_EXPORTER_OTLP_HEADERS.
func extractAPIKey(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(h, prefix) {
			token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
			if token != "" {
				return token, true
			}
		}
	}
	if k := strings.TrimSpace(r.Header.Get("X-Prism-Key")); k != "" {
		return k, true
	}
	return "", false
}

// writeExportResponse returns an empty OTLP success response in the client's format.
func writeExportResponse(w http.ResponseWriter, r *http.Request, contentType string, resp proto.Message) {
	if normalizeContentType(contentType) == contentTypeJSON {
		w.Header().Set("Content-Type", contentTypeJSON)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
		return
	}
	out, err := proto.Marshal(resp)
	if err != nil {
		writeStatus(w, r, http.StatusInternalServerError, "internal_error", "")
		return
	}
	w.Header().Set("Content-Type", contentTypeProtobuf)
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func writeStatus(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	httpx.WriteProblem(w, r, status, code, http.StatusText(status), detail)
}
