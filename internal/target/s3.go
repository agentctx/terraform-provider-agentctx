package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// s3Target implements Target for Amazon S3.
type s3Target struct {
	client   *s3.Client
	bucket   string
	prefix   string
	kmsKeyID string
	name     string
}

// newS3Target constructs an S3-backed Target from the provided Config.
func newS3Target(cfg Config) (Target, error) {
	ctx := context.Background()

	var optFns []func(*awsconfig.LoadOptions) error
	if cfg.Region != "" {
		optFns = append(optFns, awsconfig.WithRegion(cfg.Region))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg)

	prefix := cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &s3Target{
		client:   client,
		bucket:   cfg.Bucket,
		prefix:   prefix,
		kmsKeyID: cfg.KMSKeyID,
		name:     cfg.Name,
	}, nil
}

func (t *s3Target) Name() string {
	return t.name
}

// fullKey prepends the configured prefix to the given key.
func (t *s3Target) fullKey(key string) string {
	return t.prefix + key
}

func (t *s3Target) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(t.fullKey(key)),
		Body:   body,
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}

	// Use KMS encryption if configured (opts overrides target-level config).
	kmsKey := t.kmsKeyID
	if opts.KMSKeyID != "" {
		kmsKey = opts.KMSKeyID
	}
	if kmsKey != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(kmsKey)
	}

	_, err := t.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 PutObject %q: %w", key, err)
	}
	return nil
}

func (t *s3Target) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	input := &s3.GetObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(t.fullKey(key)),
	}

	output, err := t.client.GetObject(ctx, input)
	if err != nil {
		if isS3NotFound(err) {
			return nil, ObjectMeta{}, ErrNotFound
		}
		return nil, ObjectMeta{}, fmt.Errorf("s3 GetObject %q: %w", key, err)
	}

	meta := ObjectMeta{
		Size: aws.ToInt64(output.ContentLength),
	}
	if output.ETag != nil {
		meta.ETag = *output.ETag
	}

	return output.Body, meta, nil
}

func (t *s3Target) Head(ctx context.Context, key string) (ObjectMeta, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(t.fullKey(key)),
	}

	output, err := t.client.HeadObject(ctx, input)
	if err != nil {
		if isS3NotFound(err) {
			return ObjectMeta{}, ErrNotFound
		}
		return ObjectMeta{}, fmt.Errorf("s3 HeadObject %q: %w", key, err)
	}

	meta := ObjectMeta{
		Size: aws.ToInt64(output.ContentLength),
	}
	if output.ETag != nil {
		meta.ETag = *output.ETag
	}

	return meta, nil
}

func (t *s3Target) Delete(ctx context.Context, key string) error {
	input := &s3.DeleteObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(t.fullKey(key)),
	}

	_, err := t.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 DeleteObject %q: %w", key, err)
	}
	return nil
}

func (t *s3Target) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	fullPrefix := t.fullKey(prefix)
	var results []ObjectInfo

	paginator := s3.NewListObjectsV2Paginator(t.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(t.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 ListObjectsV2 prefix %q: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			objKey := aws.ToString(obj.Key)
			// Strip the internal prefix so callers see the logical key.
			logicalKey := strings.TrimPrefix(objKey, t.prefix)

			info := ObjectInfo{
				Key:  logicalKey,
				Size: aws.ToInt64(obj.Size),
			}
			if obj.ETag != nil {
				info.ETag = *obj.ETag
			}
			results = append(results, info)
		}
	}

	return results, nil
}

func (t *s3Target) ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error {
	input := &s3.PutObjectInput{
		Bucket: aws.String(t.bucket),
		Key:    aws.String(t.fullKey(key)),
		Body:   body,
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	if len(opts.Metadata) > 0 {
		input.Metadata = opts.Metadata
	}

	kmsKey := t.kmsKeyID
	if opts.KMSKeyID != "" {
		kmsKey = opts.KMSKeyID
	}
	if kmsKey != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(kmsKey)
	}

	// Set the conditional header.
	if condition.IfMatch == "*" {
		// If-None-Match: * means "create only if the object does not exist".
		input.IfNoneMatch = aws.String("*")
	} else if condition.IfMatch != "" {
		// If-Match: <etag> means "update only if the current ETag matches".
		input.IfMatch = aws.String(condition.IfMatch)
	}

	_, err := t.client.PutObject(ctx, input)
	if err != nil {
		if isS3PreconditionFailed(err) {
			return ErrPreconditionFailed
		}
		return fmt.Errorf("s3 ConditionalPut %q: %w", key, err)
	}
	return nil
}

// isS3NotFound returns true if the error indicates the object was not found.
func isS3NotFound(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	// HeadObject returns a generic error with status 404.
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
		return true
	}
	return false
}

// isS3PreconditionFailed returns true if the error is an HTTP 412.
func isS3PreconditionFailed(err error) bool {
	var respErr interface{ HTTPStatusCode() int }
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 412 {
		return true
	}
	return false
}
