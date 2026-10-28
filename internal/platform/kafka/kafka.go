// Package kafka wraps franz-go with the small surface the services need:
// publish a JSON value, and consume a topic with at-least-once handling.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	maxAttempts = 5
	retryDelay  = 2 * time.Second
)

// Producer publishes JSON values.
type Producer struct {
	cl *kgo.Client
}

func NewProducer(brokers []string) (*Producer, error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.AllowAutoTopicCreation(),
		kgo.RecordPartitioner(kgo.UniformBytesPartitioner(64<<10, true, true, nil)),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	return &Producer{cl: cl}, nil
}

// Publish marshals v and waits for the broker to acknowledge it. Records with
// the same key land on the same partition and keep their order.
func (p *Producer) Publish(ctx context.Context, topic, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal for %s: %w", topic, err)
	}
	rec := &kgo.Record{Topic: topic, Key: []byte(key), Value: b}
	if err := p.cl.ProduceSync(ctx, rec).FirstErr(); err != nil {
		return fmt.Errorf("publish to %s: %w", topic, err)
	}
	return nil
}

func (p *Producer) Close() { p.cl.Close() }

// Consume reads topic as part of group and calls handle for every record,
// committing the offset only after handle succeeds (at-least-once). A failing
// handler is retried a few times, then the record is logged and skipped so one
// poison message cannot block the partition; handlers must be idempotent.
// Undecodable payloads are skipped immediately. Returns when ctx is done.
func Consume[T any](ctx context.Context, brokers []string, group, topic string, handle func(context.Context, T) error) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.AllowAutoTopicCreation(),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("kafka consumer: %w", err)
	}
	defer cl.Close()

	for {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, e := range errs {
				slog.Error("kafka fetch", "topic", e.Topic, "partition", e.Partition, "err", e.Err)
			}
			if errors.Is(fetches.Err0(), kgo.ErrClientClosed) {
				return fetches.Err0()
			}
			sleep(ctx, retryDelay)
			continue
		}

		var done bool
		fetches.EachRecord(func(rec *kgo.Record) {
			if done {
				return
			}
			var v T
			if err := json.Unmarshal(rec.Value, &v); err != nil {
				slog.Error("undecodable record, skipping", "topic", topic, "offset", rec.Offset, "err", err)
			} else if err := retry(ctx, func() error { return handle(ctx, v) }); err != nil {
				if ctx.Err() != nil {
					done = true // shutting down: leave the record uncommitted
					return
				}
				slog.Error("handler failed, skipping record", "topic", topic, "offset", rec.Offset, "err", err)
			}
			if err := cl.CommitRecords(ctx, rec); err != nil && ctx.Err() == nil {
				slog.Error("commit failed", "topic", topic, "offset", rec.Offset, "err", err)
			}
		})
		if done {
			return nil
		}
	}
}

func retry(ctx context.Context, fn func() error) error {
	var err error
	for i := 0; i < maxAttempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		slog.Warn("handler error, retrying", "attempt", i+1, "err", err)
		if !sleep(ctx, retryDelay*time.Duration(i+1)) {
			return ctx.Err()
		}
	}
	return err
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
