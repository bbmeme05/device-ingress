package queuebridgepb

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Publisher struct {
	queueID string
	conn    *grpc.ClientConn
	client  QueueBridgeBalancerClient
}

func NewPublisher(address, queueID string) (*Publisher, error) {
	if address == "" {
		return nil, fmt.Errorf("queue bridge address is required")
	}
	if queueID == "" {
		return nil, fmt.Errorf("queue id is required")
	}

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create queue bridge client: %w", err)
	}
	return &Publisher{
		queueID: queueID,
		conn:    conn,
		client:  NewQueueBridgeBalancerClient(conn),
	}, nil
}

func (p *Publisher) PushBatch(ctx context.Context, payloads [][]byte) error {
	messages := make([]*QueueMessage, 0, len(payloads))
	for _, payload := range payloads {
		messages = append(messages, &QueueMessage{QueueId: p.queueID, Message: payload})
	}
	_, err := p.client.PushBatch(ctx, &PushBatchRequest{Messages: messages})
	return err
}

func (p *Publisher) Close() error {
	return p.conn.Close()
}
