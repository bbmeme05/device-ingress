package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bbmeme05/device-ingress/internal/batcher"
)

type discardPublisher struct{}

func (discardPublisher) PushBatch(context.Context, [][]byte) error { return nil }

func BenchmarkIngestOpenRTB(b *testing.B) {
	batch := batcher.New(discardPublisher{}, batcher.Config{
		Workers: 4, QueueDepth: 1 << 16, BatchSize: 500, BatchWait: 2 * time.Millisecond, PushTimeout: time.Second,
	})
	defer batch.Close()

	server := New(Config{MaxCompressedBodyBytes: 1 << 20, MaxDecodedBodyBytes: 1 << 20}, batch)
	handler := server.Handler()
	body := []byte(validAndroidRequest)
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			request := httptest.NewRequest(http.MethodPost, "/v1/openrtb/device", bytes.NewReader(body))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				b.Fatalf("response = %d", response.Code)
			}
		}
	})
}
