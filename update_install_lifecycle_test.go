package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUpdateShutdownCancelsPreparationWithoutWaiting(t *testing.T) {
	app := NewApp()
	ctx, finish, err := app.beginUpdateInstall()
	if err != nil {
		t.Fatal(err)
	}

	stopped := make(chan struct{})
	go func() {
		app.stopUpdateInstall()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for cancellable update preparation")
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("update preparation context ended with %v", ctx.Err())
		}
	default:
		t.Fatal("shutdown did not cancel update preparation")
	}
	if err := app.withCriticalUpdateInstall(func() error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("update entered its critical section after shutdown: %v", err)
	}
	finish()
}

func TestUpdateShutdownWaitsForCriticalInstall(t *testing.T) {
	app := NewApp()
	_, finish, err := app.beginUpdateInstall()
	if err != nil {
		t.Fatal(err)
	}

	criticalStarted := make(chan struct{})
	releaseCritical := make(chan struct{})
	criticalDone := make(chan error, 1)
	go func() {
		criticalDone <- app.withCriticalUpdateInstall(func() error {
			close(criticalStarted)
			<-releaseCritical
			return nil
		})
	}()
	select {
	case <-criticalStarted:
	case <-time.After(time.Second):
		t.Fatal("critical update transaction did not start")
	}

	stopped := make(chan struct{})
	go func() {
		app.stopUpdateInstall()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("shutdown returned during the critical update transaction")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseCritical)
	select {
	case err := <-criticalDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("critical update transaction did not finish")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not resume after the critical transaction")
	}
	finish()
	if _, _, err := app.beginUpdateInstall(); err == nil {
		t.Fatal("a new update started after shutdown closed the lifecycle gate")
	}
}
