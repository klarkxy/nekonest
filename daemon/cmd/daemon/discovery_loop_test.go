package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSessionDiscoveryLoopWaitsAfterSlowScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var starts []time.Time
	done := make(chan struct{})
	go runSessionDiscoveryLoop(ctx, 30*time.Millisecond, make(chan struct{}), func(_ bool) {
		mu.Lock()
		starts = append(starts, time.Now())
		count := len(starts)
		mu.Unlock()
		time.Sleep(45 * time.Millisecond)
		if count == 2 {
			close(done)
		}
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("discovery loop did not run twice")
	}
	cancel()
	mu.Lock()
	gap := starts[1].Sub(starts[0])
	mu.Unlock()
	if gap < 65*time.Millisecond {
		t.Fatalf("slow scan caught up without a post-completion wait: gap=%v", gap)
	}
}

func TestSessionDiscoveryLoopRunsBufferedForceImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	force := make(chan struct{}, 1)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	forced := make(chan time.Time, 1)
	go runSessionDiscoveryLoop(ctx, time.Hour, force, func(isForce bool) {
		select {
		case <-firstStarted:
			if isForce {
				forced <- time.Now()
			}
		default:
			close(firstStarted)
			<-releaseFirst
		}
	})
	<-firstStarted
	force <- struct{}{}
	releasedAt := time.Now()
	close(releaseFirst)
	select {
	case startedAt := <-forced:
		if delay := startedAt.Sub(releasedAt); delay > 100*time.Millisecond {
			t.Fatalf("buffered force was delayed by %v", delay)
		}
	case <-time.After(time.Second):
		t.Fatal("buffered force did not trigger discovery")
	}
}
