package logger

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubscriptionSnapshotBoundary(t *testing.T) {
	store := openTestStore(t, 1<<20, 2)
	appendTest(t, store, "info", "history")
	subscription, snapshot, err := store.Subscribe(true)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	defer snapshot.Close()
	appendTest(t, store, "info", "live")
	after := uint64(0)
	page, err := snapshot.Query(context.Background(), QueryOptions{After: &after, Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Message != "history" {
		t.Fatalf("snapshot: %+v %v", page, err)
	}
	select {
	case entry := <-subscription.Entries():
		if entry.ID != 2 || entry.Message != "live" {
			t.Fatal(entry)
		}
	case <-time.After(time.Second):
		t.Fatal("no live entry")
	}
}

func TestSlowSubscriptionDoesNotBlockLogger(t *testing.T) {
	store := openTestStore(t, 1<<20, 2)
	subscription, snapshot, err := store.Subscribe(false)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	snapshot.Close()
	for i := 0; i <= SubscriptionQueue; i++ {
		appendTest(t, store, "info", "queue full")
	}
	select {
	case <-subscription.Done():
	default:
		t.Fatal("slow subscription not cancelled")
	}
	if len(store.subscribers) != 0 {
		t.Fatal("subscription leaked")
	}
	appendTest(t, store, "info", "logger still works")
}

func TestSubscriptionLimitAndClose(t *testing.T) {
	store := openTestStore(t, 1<<20, 2)
	var subscriptions []*Subscription
	for i := 0; i < MaxStreams; i++ {
		subscription, snapshot, err := store.Subscribe(false)
		if err != nil {
			t.Fatal(err)
		}
		snapshot.Close()
		subscriptions = append(subscriptions, subscription)
	}
	if _, _, err := store.Subscribe(false); !errors.Is(err, ErrStreamLimit) {
		t.Fatal(err)
	}
	subscriptions[0].Close()
	if subscription, snapshot, err := store.Subscribe(false); err != nil {
		t.Fatal(err)
	} else {
		snapshot.Close()
		defer subscription.Close()
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, subscription := range subscriptions {
		select {
		case <-subscription.Done():
		default:
			t.Fatal("close left live subscription")
		}
	}
	if _, _, err := store.Subscribe(false); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestLiveSubscriptionDuringDiskFault(t *testing.T) {
	store := openTestStore(t, mib, 2)
	injectWriterFailure(t, store)
	_, _ = store.Append("error", "disk fault")
	if _, _, err := store.Subscribe(true); err == nil {
		t.Fatal("replay succeeded during disk fault")
	}
	subscription, snapshot, err := store.Subscribe(false)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	snapshot.Close()
	if _, err := store.Append("error", "still live"); err == nil {
		t.Fatal("expected disk error")
	}
	select {
	case entry := <-subscription.Entries():
		if entry.Message != "still live" {
			t.Fatal(entry)
		}
	case <-time.After(time.Second):
		t.Fatal("live output stopped")
	}
}
