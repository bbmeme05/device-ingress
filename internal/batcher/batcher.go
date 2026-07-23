package batcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueFull = errors.New("ingress queue full")

type Publisher interface {
	PushBatch(context.Context, [][]byte) error
}

type Metrics struct {
	Queued       atomic.Uint64
	DroppedFull  atomic.Uint64
	Pushed       atomic.Uint64
	PushFailures atomic.Uint64
}

type Config struct {
	Workers     int
	QueueDepth  int
	BatchSize   int
	BatchWait   time.Duration
	PushTimeout time.Duration
	Retries     int
}

type Batcher struct {
	publisher Publisher
	config    Config
	queues    []chan []byte
	next      atomic.Uint64
	metrics   Metrics
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

func New(publisher Publisher, config Config) *Batcher {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Batcher{
		publisher: publisher,
		config:    config,
		queues:    make([]chan []byte, config.Workers),
		ctx:       ctx,
		cancel:    cancel,
	}
	depth := max(1, config.QueueDepth/config.Workers)
	for index := range b.queues {
		b.queues[index] = make(chan []byte, depth)
		b.wg.Add(1)
		go b.run(b.queues[index])
	}
	return b
}

func (b *Batcher) Enqueue(payload []byte) error {
	index := int(b.next.Add(1) % uint64(len(b.queues)))
	select {
	case b.queues[index] <- payload:
		b.metrics.Queued.Add(1)
		return nil
	default:
		b.metrics.DroppedFull.Add(1)
		return ErrQueueFull
	}
}

func (b *Batcher) Metrics() *Metrics {
	return &b.metrics
}

func (b *Batcher) Close() {
	b.cancel()
	b.wg.Wait()
}

func (b *Batcher) run(queue <-chan []byte) {
	defer b.wg.Done()
	batch := make([][]byte, 0, b.config.BatchSize)
	timer := time.NewTimer(b.config.BatchWait)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if b.push(batch) {
			b.metrics.Pushed.Add(uint64(len(batch)))
		}
		batch = batch[:0]
	}

	for {
		var timerC <-chan time.Time
		if len(batch) > 0 {
			timerC = timer.C
		}

		select {
		case <-b.ctx.Done():
			flush()
			return
		case payload := <-queue:
			batch = append(batch, payload)
			if len(batch) == 1 {
				timer.Reset(b.config.BatchWait)
			}
			if len(batch) >= b.config.BatchSize {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				flush()
			}
		case <-timerC:
			flush()
		}
	}
}

func (b *Batcher) push(batch [][]byte) bool {
	for attempt := 0; attempt <= b.config.Retries; attempt++ {
		ctx, cancel := context.WithTimeout(b.ctx, b.config.PushTimeout)
		err := b.publisher.PushBatch(ctx, batch)
		cancel()
		if err == nil {
			return true
		}
		if attempt < b.config.Retries {
			time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
			continue
		}
		b.metrics.PushFailures.Add(uint64(len(batch)))
		slog.Error("queue-bridge push failed; batch dropped", "count", len(batch), "error", err)
	}
	return false
}
