package httpapi

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bbmeme05/device-ingress/internal/batcher"
	"github.com/bbmeme05/device-ingress/internal/openrtb"
	"github.com/bytedance/sonic"
)

type Config struct {
	MaxCompressedBodyBytes int64
	MaxDecodedBodyBytes    int64
}

type Metrics struct {
	Requests          atomic.Uint64
	InvalidJSON       atomic.Uint64
	InvalidDevice     atomic.Uint64
	InvalidAndroidIFA atomic.Uint64
	Encoded           atomic.Uint64
	DroppedQueueFull  atomic.Uint64
}

type Server struct {
	config  Config
	batch   *batcher.Batcher
	metrics Metrics
	gzip    sync.Pool
}

func New(config Config, batch *batcher.Batcher) *Server {
	server := &Server{config: config, batch: batch}
	server.gzip.New = func() any {
		writer, err := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		if err != nil {
			panic(err)
		}
		return writer
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/openrtb/device", s.ingest)
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("GET /metrics", s.metricsHandler)
	return mux
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) ingest(writer http.ResponseWriter, request *http.Request) {
	s.metrics.Requests.Add(1)

	body, err := s.readBody(writer, request)
	if err != nil {
		s.metrics.InvalidJSON.Add(1)
		sampledWarning(s.metrics.InvalidJSON.Load(), "invalid OpenRTB request body", "error", err)
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	var bidRequest openrtb.BidRequest
	if err := sonic.Unmarshal(body, &bidRequest); err != nil {
		s.metrics.InvalidJSON.Add(1)
		sampledWarning(s.metrics.InvalidJSON.Load(), "invalid OpenRTB JSON", "error", err)
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	record, err := bidRequest.ToDeviceRecord()
	if err != nil {
		s.metrics.InvalidDevice.Add(1)
		if errors.Is(err, openrtb.ErrMissingIFA) {
			s.metrics.InvalidAndroidIFA.Add(1)
			sampledWarning(s.metrics.InvalidAndroidIFA.Load(), "dropped Android request without a valid IFA", "request_id", bidRequest.ID)
		} else {
			sampledWarning(s.metrics.InvalidDevice.Load(), "dropped invalid OpenRTB device", "reason", err, "request_id", bidRequest.ID)
		}
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	payload, err := s.encode(record)
	if err != nil {
		s.metrics.InvalidDevice.Add(1)
		sampledWarning(s.metrics.InvalidDevice.Load(), "failed to encode device record", "error", err, "request_id", bidRequest.ID)
		writer.WriteHeader(http.StatusNoContent)
		return
	}

	s.metrics.Encoded.Add(1)
	if err := s.batch.Enqueue(payload); err != nil {
		s.metrics.DroppedQueueFull.Add(1)
		sampledWarning(s.metrics.DroppedQueueFull.Load(), "device ingress queue full; request dropped", "request_id", bidRequest.ID)
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) readBody(writer http.ResponseWriter, request *http.Request) ([]byte, error) {
	contentType := request.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "application/json") && !strings.HasPrefix(strings.ToLower(contentType), "application/openrtb+json") {
		return nil, fmt.Errorf("unsupported content type")
	}

	compressed, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, s.config.MaxCompressedBodyBytes))
	if err != nil {
		return nil, err
	}

	contentEncoding := strings.TrimSpace(request.Header.Get("Content-Encoding"))
	if contentEncoding != "" && !strings.EqualFold(contentEncoding, "gzip") && !strings.EqualFold(contentEncoding, "identity") {
		return nil, fmt.Errorf("unsupported content encoding")
	}

	reader := io.Reader(bytes.NewReader(compressed))
	if strings.EqualFold(contentEncoding, "gzip") || isGzip(compressed) {
		gzipReader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("open gzip body: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	body, err := io.ReadAll(io.LimitReader(reader, s.config.MaxDecodedBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > s.config.MaxDecodedBodyBytes {
		return nil, fmt.Errorf("decoded body exceeds limit")
	}
	return body, nil
}

func isGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func (s *Server) encode(record openrtb.DeviceRecord) ([]byte, error) {
	jsonData, err := sonic.Marshal(record)
	if err != nil {
		return nil, err
	}

	buffer := bytes.NewBuffer(make([]byte, 0, len(jsonData)/2+64))
	writer := s.gzip.Get().(*gzip.Writer)
	writer.Reset(buffer)
	if _, err := writer.Write(jsonData); err != nil {
		s.gzip.Put(writer)
		return nil, err
	}
	if err := writer.Close(); err != nil {
		s.gzip.Put(writer)
		return nil, err
	}
	s.gzip.Put(writer)
	return buffer.Bytes(), nil
}

func (s *Server) metricsHandler(writer http.ResponseWriter, _ *http.Request) {
	batchMetrics := s.batch.Metrics()
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(writer, "device_ingress_requests_total %d\n", s.metrics.Requests.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_invalid_json_total %d\n", s.metrics.InvalidJSON.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_invalid_device_total %d\n", s.metrics.InvalidDevice.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_invalid_android_ifa_total %d\n", s.metrics.InvalidAndroidIFA.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_encoded_total %d\n", s.metrics.Encoded.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_dropped_queue_full_total %d\n", s.metrics.DroppedQueueFull.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_queue_enqueued_total %d\n", batchMetrics.Queued.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_queue_dropped_total %d\n", batchMetrics.DroppedFull.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_queue_pushed_total %d\n", batchMetrics.Pushed.Load())
	_, _ = fmt.Fprintf(writer, "device_ingress_queue_push_failures_total %d\n", batchMetrics.PushFailures.Load())
}

func sampledWarning(count uint64, message string, args ...any) {
	if count <= 10 || count%1000 == 0 {
		slog.Warn(message, args...)
	}
}

func (s *Server) ShutdownTimeout() time.Duration {
	return 10 * time.Second
}
