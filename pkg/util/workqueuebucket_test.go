package util

import (
	"reflect"
	"testing"
)

func TestAddWithDataBatchesItemsForSameKey(t *testing.T) {
	q := NewBucketingWorkQueue("test-batch")
	t.Cleanup(q.ShutDown)

	q.AddWithData("quota-a", "ns-a")
	q.AddWithData("quota-a", "ns-b")

	key, data, quit := q.GetWithData()
	if quit {
		t.Fatalf("expected queue item, got shutdown")
	}
	if key != "quota-a" {
		t.Fatalf("expected key quota-a, got %v", key)
	}
	if !reflect.DeepEqual(data, []interface{}{"ns-a", "ns-b"}) {
		t.Fatalf("expected batched data [ns-a ns-b], got %#v", data)
	}

	q.Done(key)
}

func TestAddWhileInProgressIsReturnedAfterDone(t *testing.T) {
	q := NewBucketingWorkQueue("test-in-progress")
	t.Cleanup(q.ShutDown)

	q.AddWithData("quota-a", "ns-a")

	key, data, quit := q.GetWithData()
	if quit {
		t.Fatalf("expected queue item, got shutdown")
	}
	if key != "quota-a" {
		t.Fatalf("expected key quota-a, got %v", key)
	}
	if !reflect.DeepEqual(data, []interface{}{"ns-a"}) {
		t.Fatalf("expected first batch [ns-a], got %#v", data)
	}

	q.AddWithData("quota-a", "ns-b")
	q.Done(key)

	nextKey, nextData, nextQuit := q.GetWithData()
	if nextQuit {
		t.Fatalf("expected re-queued item, got shutdown")
	}
	if nextKey != "quota-a" {
		t.Fatalf("expected re-queued key quota-a, got %v", nextKey)
	}
	if !reflect.DeepEqual(nextData, []interface{}{"ns-b"}) {
		t.Fatalf("expected deferred batch [ns-b], got %#v", nextData)
	}

	q.Done(nextKey)
}

func TestAddWithDataRateLimitedPreservesData(t *testing.T) {
	q := NewBucketingWorkQueue("test-rate-limited")
	t.Cleanup(q.ShutDown)

	q.AddWithDataRateLimited("quota-a", "ns-a", "ns-b")

	key, data, quit := q.GetWithData()
	if quit {
		t.Fatalf("expected queue item, got shutdown")
	}
	if key != "quota-a" {
		t.Fatalf("expected key quota-a, got %v", key)
	}
	if !reflect.DeepEqual(data, []interface{}{"ns-a", "ns-b"}) {
		t.Fatalf("expected rate-limited data [ns-a ns-b], got %#v", data)
	}

	q.Done(key)
	q.Forget(key)
}

func TestAddWithNoPayloadReturnsEmptySlice(t *testing.T) {
	q := NewBucketingWorkQueue("test-empty")
	t.Cleanup(q.ShutDown)

	q.AddWithData("quota-a")

	key, data, quit := q.GetWithData()
	if quit {
		t.Fatalf("expected queue item, got shutdown")
	}
	if key != "quota-a" {
		t.Fatalf("expected key quota-a, got %v", key)
	}
	if len(data) != 0 {
		t.Fatalf("expected no payload, got %#v", data)
	}

	q.Done(key)
}

func TestShutDownMakesGetWithDataQuit(t *testing.T) {
	q := NewBucketingWorkQueue("test-shutdown")
	q.ShutDown()

	key, data, quit := q.GetWithData()
	if !quit {
		t.Fatalf("expected shutdown signal, got key=%v data=%#v", key, data)
	}
	if key != nil {
		t.Fatalf("expected nil key on shutdown, got %v", key)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty data on shutdown, got %#v", data)
	}
}
