package target

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// memoryObject holds a single object in the in-memory store.
type memoryObject struct {
	data        []byte
	contentType string
	metadata    map[string]string
	generation  int64
	etag        string
}

// MemoryTarget is an in-memory implementation of Target, intended for testing.
type MemoryTarget struct {
	name       string
	mu         sync.RWMutex
	objects    map[string]*memoryObject
	genCounter atomic.Int64
}

// NewMemoryTarget creates a new in-memory Target with the given name.
func NewMemoryTarget(name string) *MemoryTarget {
	return &MemoryTarget{
		name:    name,
		objects: make(map[string]*memoryObject),
	}
}

func (m *MemoryTarget) Name() string {
	return m.name
}

func (m *MemoryTarget) Put(_ context.Context, key string, body io.Reader, opts PutOptions) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	gen := m.genCounter.Add(1)
	etag := fmt.Sprintf(`"%d"`, gen)

	meta := make(map[string]string)
	for k, v := range opts.Metadata {
		meta[k] = v
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[key] = &memoryObject{
		data:        data,
		contentType: opts.ContentType,
		metadata:    meta,
		generation:  gen,
		etag:        etag,
	}
	return nil
}

func (m *MemoryTarget) Get(_ context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[key]
	if !ok {
		return nil, ObjectMeta{}, ErrNotFound
	}

	// Return a copy of the data so the caller cannot mutate the store.
	buf := make([]byte, len(obj.data))
	copy(buf, obj.data)

	meta := ObjectMeta{
		ETag:       obj.etag,
		Generation: obj.generation,
		Size:       int64(len(obj.data)),
	}
	return io.NopCloser(bytes.NewReader(buf)), meta, nil
}

func (m *MemoryTarget) Head(_ context.Context, key string) (ObjectMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[key]
	if !ok {
		return ObjectMeta{}, ErrNotFound
	}

	return ObjectMeta{
		ETag:       obj.etag,
		Generation: obj.generation,
		Size:       int64(len(obj.data)),
	}, nil
}

func (m *MemoryTarget) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.objects, key)
	return nil
}

func (m *MemoryTarget) List(_ context.Context, prefix string) ([]ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []ObjectInfo
	for k, obj := range m.objects {
		if strings.HasPrefix(k, prefix) {
			results = append(results, ObjectInfo{
				Key:  k,
				Size: int64(len(obj.data)),
				ETag: obj.etag,
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Key < results[j].Key
	})

	return results, nil
}

func (m *MemoryTarget) ConditionalPut(_ context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.objects[key]

	// Check generation-based condition (GCS-style).
	if condition.Generation != 0 {
		if !exists {
			return ErrPreconditionFailed
		}
		if existing.generation != condition.Generation {
			return ErrPreconditionFailed
		}
	} else if condition.Generation == 0 && condition.IfMatch == "" && condition.LeaseID == "" {
		// Generation 0 with no other conditions means "object must not exist" (create-only).
		if exists {
			return &ConcurrentModificationError{
				Key:     key,
				Message: fmt.Sprintf("object %q already exists", key),
			}
		}
	}

	// Check ETag-based condition (S3-style).
	if condition.IfMatch != "" {
		if condition.IfMatch == "*" {
			// If-None-Match: * means object must not exist.
			if exists {
				return &ConcurrentModificationError{
					Key:     key,
					Message: fmt.Sprintf("object %q already exists (If-None-Match: *)", key),
				}
			}
		} else {
			if !exists {
				return ErrPreconditionFailed
			}
			if existing.etag != condition.IfMatch {
				return ErrPreconditionFailed
			}
		}
	}

	// Check lease-based condition (Azure-style) - in memory we just verify presence.
	if condition.LeaseID != "" {
		// For in-memory testing, we accept any non-empty lease ID if the object exists.
		// Real Azure implementation validates the lease.
		if !exists {
			return ErrNotFound
		}
	}

	gen := m.genCounter.Add(1)
	etag := fmt.Sprintf(`"%d"`, gen)

	meta := make(map[string]string)
	for k, v := range opts.Metadata {
		meta[k] = v
	}

	m.objects[key] = &memoryObject{
		data:        data,
		contentType: opts.ContentType,
		metadata:    meta,
		generation:  gen,
		etag:        etag,
	}
	return nil
}
