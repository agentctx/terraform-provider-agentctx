package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	gcsstorage "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// gcsTarget implements Target for Google Cloud Storage.
type gcsTarget struct {
	client     *gcsstorage.Client
	bucket     string
	prefix     string
	kmsKeyName string
	name       string
}

// newGCSTarget constructs a GCS-backed Target using Application Default Credentials.
func newGCSTarget(cfg Config) (Target, error) {
	ctx := context.Background()

	client, err := gcsstorage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating GCS client: %w", err)
	}

	prefix := cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &gcsTarget{
		client:     client,
		bucket:     cfg.Bucket,
		prefix:     prefix,
		kmsKeyName: cfg.KMSKeyName,
		name:       cfg.Name,
	}, nil
}

func (t *gcsTarget) Name() string {
	return t.name
}

// fullKey prepends the configured prefix to the given key.
func (t *gcsTarget) fullKey(key string) string {
	return t.prefix + key
}

// obj returns a handle to the named object in the configured bucket.
func (t *gcsTarget) obj(key string) *gcsstorage.ObjectHandle {
	return t.client.Bucket(t.bucket).Object(t.fullKey(key))
}

func (t *gcsTarget) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	o := t.obj(key)

	w := o.NewWriter(ctx)

	if opts.ContentType != "" {
		w.ContentType = opts.ContentType
	}
	if len(opts.Metadata) > 0 {
		w.Metadata = opts.Metadata
	}
	// Apply KMS key if configured.
	if t.kmsKeyName != "" {
		w.KMSKeyName = t.kmsKeyName
	}

	if _, err := io.Copy(w, body); err != nil {
		w.Close()
		return fmt.Errorf("gcs write %q: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gcs close writer %q: %w", key, err)
	}
	return nil
}

func (t *gcsTarget) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	o := t.obj(key)

	// Get object attributes first for metadata.
	attrs, err := o.Attrs(ctx)
	if err != nil {
		if errors.Is(err, gcsstorage.ErrObjectNotExist) {
			return nil, ObjectMeta{}, ErrNotFound
		}
		return nil, ObjectMeta{}, fmt.Errorf("gcs Attrs %q: %w", key, err)
	}

	reader, err := o.NewReader(ctx)
	if err != nil {
		if errors.Is(err, gcsstorage.ErrObjectNotExist) {
			return nil, ObjectMeta{}, ErrNotFound
		}
		return nil, ObjectMeta{}, fmt.Errorf("gcs NewReader %q: %w", key, err)
	}

	meta := ObjectMeta{
		ETag:       attrs.Etag,
		Generation: attrs.Generation,
		Size:       attrs.Size,
	}

	return reader, meta, nil
}

func (t *gcsTarget) Head(ctx context.Context, key string) (ObjectMeta, error) {
	o := t.obj(key)

	attrs, err := o.Attrs(ctx)
	if err != nil {
		if errors.Is(err, gcsstorage.ErrObjectNotExist) {
			return ObjectMeta{}, ErrNotFound
		}
		return ObjectMeta{}, fmt.Errorf("gcs Attrs %q: %w", key, err)
	}

	return ObjectMeta{
		ETag:       attrs.Etag,
		Generation: attrs.Generation,
		Size:       attrs.Size,
	}, nil
}

func (t *gcsTarget) Delete(ctx context.Context, key string) error {
	o := t.obj(key)

	err := o.Delete(ctx)
	if err != nil {
		if errors.Is(err, gcsstorage.ErrObjectNotExist) {
			return nil // Delete is idempotent.
		}
		return fmt.Errorf("gcs Delete %q: %w", key, err)
	}
	return nil
}

func (t *gcsTarget) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	fullPrefix := t.fullKey(prefix)

	it := t.client.Bucket(t.bucket).Objects(ctx, &gcsstorage.Query{
		Prefix: fullPrefix,
	})

	var results []ObjectInfo
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs List prefix %q: %w", prefix, err)
		}

		logicalKey := strings.TrimPrefix(attrs.Name, t.prefix)
		results = append(results, ObjectInfo{
			Key:  logicalKey,
			Size: attrs.Size,
			ETag: attrs.Etag,
		})
	}

	return results, nil
}

func (t *gcsTarget) ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error {
	o := t.obj(key)

	// Apply generation-based precondition.
	if condition.Generation > 0 {
		// Update only if the current generation matches.
		o = o.If(gcsstorage.Conditions{
			GenerationMatch: condition.Generation,
		})
	} else if condition.Generation == 0 && condition.IfMatch == "" && condition.LeaseID == "" {
		// Generation 0 with no other conditions means the object must not exist.
		o = o.If(gcsstorage.Conditions{
			DoesNotExist: true,
		})
	}

	// If an ETag condition is provided (cross-platform compat), treat as "must not exist".
	if condition.IfMatch == "*" {
		o = o.If(gcsstorage.Conditions{
			DoesNotExist: true,
		})
	}

	w := o.NewWriter(ctx)
	if opts.ContentType != "" {
		w.ContentType = opts.ContentType
	}
	if len(opts.Metadata) > 0 {
		w.Metadata = opts.Metadata
	}
	// Apply KMS key if configured.
	if t.kmsKeyName != "" {
		w.KMSKeyName = t.kmsKeyName
	}

	if _, err := io.Copy(w, body); err != nil {
		w.Close()
		return fmt.Errorf("gcs conditional write %q: %w", key, err)
	}

	if err := w.Close(); err != nil {
		if isGCSPreconditionFailed(err) {
			return ErrPreconditionFailed
		}
		return fmt.Errorf("gcs conditional close %q: %w", key, err)
	}
	return nil
}

// isGCSPreconditionFailed checks if the error is a GCS 412 Precondition Failed.
func isGCSPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	// GCS returns googleapi.Error with Code 412 for precondition failures.
	// We avoid importing googleapi directly by checking the error string.
	return strings.Contains(err.Error(), "conditionNotMet") ||
		strings.Contains(err.Error(), "Precondition Failed") ||
		strings.Contains(err.Error(), "412")
}
