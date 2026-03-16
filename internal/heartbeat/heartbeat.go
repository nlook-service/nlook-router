package heartbeat

import (
	"context"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// Registrar sends register and periodic heartbeats to the server.
type Registrar struct {
	Client   apiclient.Interface
	Interval time.Duration
	Payload  *apiclient.RegisterPayload
	stopCh   chan struct{}
}

// NewRegistrar creates a heartbeat registrar.
func NewRegistrar(client apiclient.Interface, interval time.Duration, payload *apiclient.RegisterPayload) *Registrar {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Registrar{
		Client:   client,
		Interval: interval,
		Payload:  payload,
		stopCh:   make(chan struct{}),
	}
}

// Start registers once then runs heartbeats until Stop is called.
func (r *Registrar) Start(ctx context.Context) error {
	if err := r.Client.RegisterRouter(ctx, r.Payload); err != nil {
		return err
	}
	go r.loop(ctx)
	return nil
}

// Stop stops the heartbeat loop.
func (r *Registrar) Stop() error {
	close(r.stopCh)
	return nil
}

func (r *Registrar) loop(ctx context.Context) {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Client.Heartbeat(ctx, r.Payload); err != nil {
				log.Printf("heartbeat error: %v", err)
			}
		}
	}
}
