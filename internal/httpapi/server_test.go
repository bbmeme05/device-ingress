package httpapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bbmeme05/device-ingress/internal/batcher"
	"github.com/bbmeme05/device-ingress/internal/openrtb"
	"github.com/bytedance/sonic"
)

type fakePublisher struct {
	batches chan [][]byte
}

func (f *fakePublisher) PushBatch(_ context.Context, payloads [][]byte) error {
	copyPayloads := make([][]byte, len(payloads))
	for index, payload := range payloads {
		copyPayloads[index] = append([]byte(nil), payload...)
	}
	f.batches <- copyPayloads
	return nil
}

func TestIngestReturnsNoContentAndQueuesValidDevice(t *testing.T) {
	publisher := &fakePublisher{batches: make(chan [][]byte, 1)}
	batch := batcher.New(publisher, batcher.Config{
		Workers: 1, QueueDepth: 8, BatchSize: 1, BatchWait: time.Millisecond, PushTimeout: time.Second,
	})
	defer batch.Close()

	server := New(Config{MaxCompressedBodyBytes: 1 << 20, MaxDecodedBodyBytes: 1 << 20}, batch)
	request := httptest.NewRequest(http.MethodPost, "/v1/openrtb/device", bytes.NewBufferString(validAndroidRequest))
	request.Header.Set("Content-Type", "application/openrtb+json")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("response = %d %q, want 204 with empty body", response.Code, response.Body.String())
	}

	select {
	case payloads := <-publisher.batches:
		if len(payloads) != 1 {
			t.Fatalf("pushed %d payloads, want 1", len(payloads))
		}
		reader, err := gzip.NewReader(bytes.NewReader(payloads[0]))
		if err != nil {
			t.Fatalf("gzip.NewReader() error = %v", err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read gzip data: %v", err)
		}
		var record openrtb.DeviceRecord
		if err := sonic.Unmarshal(data, &record); err != nil {
			t.Fatalf("decode device record: %v", err)
		}
		if record.AppID != "com.viber.voip" || record.OS != "android" {
			t.Fatalf("unexpected record: %#v", record)
		}
	case <-time.After(time.Second):
		t.Fatal("device record was not pushed")
	}
}

func TestIngestAcceptsGzipBodyWithAndWithoutHeader(t *testing.T) {
	for _, testCase := range []struct {
		name             string
		setContentHeader bool
	}{
		{name: "content encoding gzip", setContentHeader: true},
		{name: "gzip magic bytes only", setContentHeader: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			publisher := &fakePublisher{batches: make(chan [][]byte, 1)}
			batch := batcher.New(publisher, batcher.Config{
				Workers: 1, QueueDepth: 8, BatchSize: 1, BatchWait: time.Millisecond, PushTimeout: time.Second,
			})
			defer batch.Close()

			server := New(Config{MaxCompressedBodyBytes: 1 << 20, MaxDecodedBodyBytes: 1 << 20}, batch)
			request := httptest.NewRequest(http.MethodPost, "/v1/openrtb/device", bytes.NewReader(gzipRequestBody(t, validAndroidRequest)))
			request.Header.Set("Content-Type", "application/json")
			if testCase.setContentHeader {
				request.Header.Set("Content-Encoding", "gzip")
			}
			response := httptest.NewRecorder()

			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("response = %d %q, want 204 with empty body", response.Code, response.Body.String())
			}
			select {
			case <-publisher.batches:
			case <-time.After(time.Second):
				t.Fatal("gzip request was not queued")
			}
		})
	}
}

func TestIngestReturnsNoContentForMissingAndroidIFA(t *testing.T) {
	publisher := &fakePublisher{batches: make(chan [][]byte, 1)}
	batch := batcher.New(publisher, batcher.Config{
		Workers: 1, QueueDepth: 8, BatchSize: 1, BatchWait: time.Millisecond, PushTimeout: time.Second,
	})
	defer batch.Close()

	server := New(Config{MaxCompressedBodyBytes: 1 << 20, MaxDecodedBodyBytes: 1 << 20}, batch)
	request := httptest.NewRequest(http.MethodPost, "/v1/openrtb/device", bytes.NewBufferString(`{"id":"missing-ifa","app":{"bundle":"com.example.app"},"imp":[{"id":"1"}],"device":{"os":"android","ip":"203.0.113.10","ua":"Mozilla/5.0"}}`))
	request.Header.Set("Authorization", "Bearer test-token")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("response = %d %q, want 204 with empty body", response.Code, response.Body.String())
	}
	if got := server.metrics.InvalidAndroidIFA.Load(); got != 1 {
		t.Fatalf("invalid IFA count = %d, want 1", got)
	}
	select {
	case <-publisher.batches:
		t.Fatal("invalid Android request should not be sent downstream")
	case <-time.After(20 * time.Millisecond):
	}
}

const validAndroidRequest = `{
  "id":"request-1",
  "clientId":2501,
  "supplyId":98,
  "app":{"bundle":"com.viber.voip","cat":["IAB19"],"publisher":{"id":"98"}},
  "source":{"tid":"transaction-1"},
  "imp":[{"id":"1","tagid":"banner","banner":{"w":300,"h":250}}],
  "device":{"os":"android","ip":"112.198.254.159","ua":"Mozilla/5.0 (Linux; Android 12)","ifa":"550e8400-e29b-41d4-a716-446655440000","geo":{"country":"PHL","city":"Quezon City","region":"00"}}
}`

func gzipRequestBody(t *testing.T, value string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(value)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return compressed.Bytes()
}
