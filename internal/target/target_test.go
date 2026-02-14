package target

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// MemoryTarget basic operations
// ---------------------------------------------------------------------------

func TestMemoryTarget_PutGet(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	content := "hello, world"
	err := m.Put(ctx, "key1", strings.NewReader(content), PutOptions{})
	if err != nil {
		t.Fatalf("Put: unexpected error: %v", err)
	}

	rc, meta, err := m.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(got) != content {
		t.Errorf("Get returned %q, want %q", string(got), content)
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("meta.Size = %d, want %d", meta.Size, len(content))
	}
	if meta.ETag == "" {
		t.Error("meta.ETag is empty, expected non-empty")
	}
	if meta.Generation == 0 {
		t.Error("meta.Generation is 0, expected non-zero")
	}
}

func TestMemoryTarget_Head(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	content := "some data for head"
	if err := m.Put(ctx, "headkey", strings.NewReader(content), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	meta, err := m.Head(ctx, "headkey")
	if err != nil {
		t.Fatalf("Head: unexpected error: %v", err)
	}

	if meta.Size != int64(len(content)) {
		t.Errorf("Head Size = %d, want %d", meta.Size, len(content))
	}
	if meta.ETag == "" {
		t.Error("Head ETag is empty")
	}
	if meta.Generation == 0 {
		t.Error("Head Generation is 0")
	}
}

func TestMemoryTarget_Delete(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	if err := m.Put(ctx, "delkey", strings.NewReader("data"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if err := m.Delete(ctx, "delkey"); err != nil {
		t.Fatalf("Delete: unexpected error: %v", err)
	}

	_, _, err := m.Get(ctx, "delkey")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: got err = %v, want ErrNotFound", err)
	}
}

func TestMemoryTarget_DeleteNonExistent(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	err := m.Delete(ctx, "no-such-key")
	if err != nil {
		t.Errorf("Delete non-existent key: got err = %v, want nil (idempotent)", err)
	}
}

func TestMemoryTarget_List(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	keys := []string{
		"prefix/aaa",
		"prefix/bbb",
		"prefix/ccc",
		"other/ddd",
	}
	for _, k := range keys {
		if err := m.Put(ctx, k, strings.NewReader("v"), PutOptions{}); err != nil {
			t.Fatalf("Put(%s): %v", k, err)
		}
	}

	// List with prefix filter.
	results, err := m.List(ctx, "prefix/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("List returned %d results, want 3", len(results))
	}

	// Verify sorted order.
	for i := 1; i < len(results); i++ {
		if results[i].Key < results[i-1].Key {
			t.Errorf("results not sorted: %q comes after %q", results[i-1].Key, results[i].Key)
		}
	}

	// Verify the correct keys are present.
	wantKeys := []string{"prefix/aaa", "prefix/bbb", "prefix/ccc"}
	for i, want := range wantKeys {
		if results[i].Key != want {
			t.Errorf("results[%d].Key = %q, want %q", i, results[i].Key, want)
		}
	}

	// List with different prefix returns only matching keys.
	otherResults, err := m.List(ctx, "other/")
	if err != nil {
		t.Fatalf("List(other/): %v", err)
	}
	if len(otherResults) != 1 {
		t.Fatalf("List(other/) returned %d results, want 1", len(otherResults))
	}
	if otherResults[0].Key != "other/ddd" {
		t.Errorf("otherResults[0].Key = %q, want %q", otherResults[0].Key, "other/ddd")
	}
}

func TestMemoryTarget_GetNotFound(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	_, _, err := m.Get(ctx, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get nonexistent: got err = %v, want ErrNotFound", err)
	}
}

func TestMemoryTarget_Name(t *testing.T) {
	m := NewMemoryTarget("my-target-name")
	if got := m.Name(); got != "my-target-name" {
		t.Errorf("Name() = %q, want %q", got, "my-target-name")
	}
}

// ---------------------------------------------------------------------------
// MemoryTarget conditional writes
// ---------------------------------------------------------------------------

func TestMemoryTarget_ConditionalPut_ETag(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	// Put initial object.
	if err := m.Put(ctx, "cond", strings.NewReader("v1"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Read the etag.
	meta, err := m.Head(ctx, "cond")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Conditional put with correct etag should succeed.
	err = m.ConditionalPut(ctx, "cond", strings.NewReader("v2"), WriteCondition{IfMatch: meta.ETag}, PutOptions{})
	if err != nil {
		t.Fatalf("ConditionalPut with correct etag: unexpected error: %v", err)
	}

	// Verify the content was updated.
	rc, _, err := m.Get(ctx, "cond")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "v2" {
		t.Errorf("content = %q, want %q", string(got), "v2")
	}
}

func TestMemoryTarget_ConditionalPut_ETagMismatch(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	if err := m.Put(ctx, "cond", strings.NewReader("v1"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Conditional put with wrong etag should fail.
	err := m.ConditionalPut(ctx, "cond", strings.NewReader("v2"), WriteCondition{IfMatch: `"wrong-etag"`}, PutOptions{})
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("ConditionalPut with wrong etag: got err = %v, want ErrPreconditionFailed", err)
	}
}

func TestMemoryTarget_ConditionalPut_IfNoneMatch(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	// IfMatch="*" on a non-existent key should succeed (If-None-Match: * semantics).
	err := m.ConditionalPut(ctx, "new-key", strings.NewReader("v1"), WriteCondition{IfMatch: "*"}, PutOptions{})
	if err != nil {
		t.Fatalf("ConditionalPut IfMatch=* on new key: unexpected error: %v", err)
	}

	// IfMatch="*" on an existing key should fail.
	err = m.ConditionalPut(ctx, "new-key", strings.NewReader("v2"), WriteCondition{IfMatch: "*"}, PutOptions{})
	if err == nil {
		t.Fatal("ConditionalPut IfMatch=* on existing key: expected error, got nil")
	}

	// Should be a ConcurrentModificationError.
	var cme *ConcurrentModificationError
	if !errors.As(err, &cme) {
		t.Errorf("expected ConcurrentModificationError, got %T: %v", err, err)
	}
}

func TestMemoryTarget_ConditionalPut_Generation(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	if err := m.Put(ctx, "gen-key", strings.NewReader("v1"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	meta, err := m.Head(ctx, "gen-key")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Conditional put with correct generation should succeed.
	err = m.ConditionalPut(ctx, "gen-key", strings.NewReader("v2"), WriteCondition{Generation: meta.Generation}, PutOptions{})
	if err != nil {
		t.Fatalf("ConditionalPut with correct generation: unexpected error: %v", err)
	}

	// Verify content updated.
	rc, _, err := m.Get(ctx, "gen-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "v2" {
		t.Errorf("content = %q, want %q", string(got), "v2")
	}
}

func TestMemoryTarget_ConditionalPut_GenerationMismatch(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	if err := m.Put(ctx, "gen-key", strings.NewReader("v1"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Use a wrong generation number.
	err := m.ConditionalPut(ctx, "gen-key", strings.NewReader("v2"), WriteCondition{Generation: 999}, PutOptions{})
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("ConditionalPut with wrong generation: got err = %v, want ErrPreconditionFailed", err)
	}
}

func TestMemoryTarget_ConditionalPut_LeaseID(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryTarget("test")

	// Put an object first so it exists.
	if err := m.Put(ctx, "lease-key", strings.NewReader("v1"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Conditional put with lease ID on existing object should succeed.
	err := m.ConditionalPut(ctx, "lease-key", strings.NewReader("v2"), WriteCondition{LeaseID: "some-lease-id"}, PutOptions{})
	if err != nil {
		t.Fatalf("ConditionalPut with lease on existing object: unexpected error: %v", err)
	}

	// Conditional put with lease ID on non-existent key should fail with ErrNotFound.
	err = m.ConditionalPut(ctx, "no-such-key", strings.NewReader("v1"), WriteCondition{LeaseID: "some-lease-id"}, PutOptions{})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("ConditionalPut with lease on missing key: got err = %v, want ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// RetryTarget tests
// ---------------------------------------------------------------------------

// faultyTarget wraps a Target and injects errors for the first N calls to Get.
type faultyTarget struct {
	Target
	mu        sync.Mutex
	callCount int
	failUntil int
	err       error
}

func (f *faultyTarget) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	f.mu.Lock()
	f.callCount++
	count := f.callCount
	f.mu.Unlock()

	if count <= f.failUntil {
		return nil, ObjectMeta{}, f.err
	}
	return f.Target.Get(ctx, key)
}

func (f *faultyTarget) getCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func TestRetryTarget_NoRetryOnSuccess(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryTarget("test")

	if err := mem.Put(ctx, "ok", strings.NewReader("data"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	faulty := &faultyTarget{
		Target:    mem,
		failUntil: 0, // never fails
		err:       fmt.Errorf("transient error"),
	}

	rt := NewRetryTarget(faulty, 3, "exponential")

	rc, _, err := rt.Get(ctx, "ok")
	if err != nil {
		t.Fatalf("Get: unexpected error: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "data" {
		t.Errorf("content = %q, want %q", string(got), "data")
	}

	if faulty.getCallCount() != 1 {
		t.Errorf("callCount = %d, want 1 (no retries on success)", faulty.getCallCount())
	}
}

func TestRetryTarget_RetriesTransientErrors(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryTarget("test")

	if err := mem.Put(ctx, "retry-key", strings.NewReader("retry-data"), PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Fail the first 2 calls with a transient error, then succeed.
	faulty := &faultyTarget{
		Target:    mem,
		failUntil: 2,
		err:       fmt.Errorf("transient network error"),
	}

	// Use linear backoff for faster test execution (base delay is 100ms).
	rt := NewRetryTarget(faulty, 5, "linear")

	rc, _, err := rt.Get(ctx, "retry-key")
	if err != nil {
		t.Fatalf("Get: unexpected error after retries: %v", err)
	}
	defer rc.Close()

	got, _ := io.ReadAll(rc)
	if string(got) != "retry-data" {
		t.Errorf("content = %q, want %q", string(got), "retry-data")
	}

	// Should have been called 3 times: 2 failures + 1 success.
	if faulty.getCallCount() != 3 {
		t.Errorf("callCount = %d, want 3", faulty.getCallCount())
	}
}

func TestRetryTarget_NoRetryOnNotFound(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryTarget("test")

	// Return ErrNotFound -- this is non-retryable.
	faulty := &faultyTarget{
		Target:    mem,
		failUntil: 100, // always fail
		err:       ErrNotFound,
	}

	rt := NewRetryTarget(faulty, 5, "exponential")

	_, _, err := rt.Get(ctx, "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: got err = %v, want ErrNotFound", err)
	}

	// Should have been called exactly once -- no retries for ErrNotFound.
	if faulty.getCallCount() != 1 {
		t.Errorf("callCount = %d, want 1 (no retry on ErrNotFound)", faulty.getCallCount())
	}
}

func TestRetryTarget_NoRetryOnPreconditionFailed(t *testing.T) {
	ctx := context.Background()
	mem := NewMemoryTarget("test")

	faulty := &faultyTarget{
		Target:    mem,
		failUntil: 100,
		err:       ErrPreconditionFailed,
	}

	rt := NewRetryTarget(faulty, 5, "exponential")

	_, _, err := rt.Get(ctx, "any-key")
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("Get: got err = %v, want ErrPreconditionFailed", err)
	}

	if faulty.getCallCount() != 1 {
		t.Errorf("callCount = %d, want 1 (no retry on ErrPreconditionFailed)", faulty.getCallCount())
	}
}

// ---------------------------------------------------------------------------
// Factory tests
// ---------------------------------------------------------------------------

func TestNewTarget_UnsupportedType(t *testing.T) {
	_, err := NewTarget(Config{
		Name: "bad",
		Type: "unsupported",
	})
	if err == nil {
		t.Fatal("NewTarget with unsupported type: expected error, got nil")
	}

	if !strings.Contains(err.Error(), "unsupported target type") {
		t.Errorf("error message = %q, want it to contain 'unsupported target type'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Helper: verify MemoryTarget implements Target interface at compile time.
// ---------------------------------------------------------------------------
var _ Target = (*MemoryTarget)(nil)

// Verify faultyTarget satisfies Target via embedding (compile-time check).
var _ interface {
	Get(context.Context, string) (io.ReadCloser, ObjectMeta, error)
} = (*faultyTarget)(nil)

// Verify the unused import suppressor.
var _ = bytes.NewReader
