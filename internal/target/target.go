package target

import (
	"context"
	"errors"
	"io"
)

// Sentinel errors for target operations.
var (
	ErrNotFound           = errors.New("object not found")
	ErrPreconditionFailed = errors.New("precondition failed: object was modified by another process")
	ErrLeaseConflict      = errors.New("lease conflict: another process holds a lease")
)

// ConcurrentModificationError represents a conflict when updating the ACTIVE pointer.
type ConcurrentModificationError struct {
	Key     string
	Message string
}

func (e *ConcurrentModificationError) Error() string { return e.Message }

// PutOptions controls optional behavior for Put and ConditionalPut operations.
type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	KMSKeyID    string
}

// WriteCondition specifies the precondition for a conditional write.
// Only the field relevant to the backend should be set.
type WriteCondition struct {
	IfMatch    string // S3 ETag
	Generation int64  // GCS generation; 0 means "object must not exist"
	LeaseID    string // Azure lease ID
}

// ObjectMeta is returned from Get and Head with version information.
type ObjectMeta struct {
	ETag       string
	Generation int64
	Size       int64
}

// ObjectInfo is a single entry returned from List.
type ObjectInfo struct {
	Key  string
	Size int64
	ETag string
}

// Target is the storage abstraction layer for Terraform provider state objects.
// Implementations exist for S3, Azure Blob Storage, and GCS.
type Target interface {
	// Put writes an object unconditionally.
	Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error
	// Get retrieves an object. Returns ErrNotFound if the key does not exist.
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error)
	// Head retrieves object metadata without the body.
	Head(ctx context.Context, key string) (ObjectMeta, error)
	// Delete removes an object.
	Delete(ctx context.Context, key string) error
	// List returns all objects under the given prefix.
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	// ConditionalPut writes an object only if the write condition is satisfied.
	// Returns ErrPreconditionFailed if the condition is not met.
	ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error
	// Name returns the target name for logging.
	Name() string
}

// Config holds the configuration used by NewTarget to construct a Target.
type Config struct {
	Name            string
	Type            string // "s3", "azure", "gcs"
	Bucket          string
	Region          string
	Prefix          string
	StorageAccount  string
	ContainerName   string
	KMSKeyID        string
	KMSKeyName      string
	EncryptionScope string
	MaxConcurrency  int
	MaxRetries      int
	TimeoutSeconds  int
	RetryBackoff    string // "exponential" | "linear"
}
