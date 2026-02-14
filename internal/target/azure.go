package target

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// azureTarget implements Target for Azure Blob Storage.
type azureTarget struct {
	client          *azblob.Client
	containerName   string
	prefix          string
	encryptionScope string
	name            string
}

// newAzureTarget constructs an Azure Blob Storage-backed Target.
func newAzureTarget(cfg Config) (Target, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net", cfg.StorageAccount)
	client, err := azblob.NewClient(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure blob client: %w", err)
	}

	prefix := cfg.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	return &azureTarget{
		client:          client,
		containerName:   cfg.ContainerName,
		prefix:          prefix,
		encryptionScope: cfg.EncryptionScope,
		name:            cfg.Name,
	}, nil
}

func (t *azureTarget) Name() string {
	return t.name
}

// fullKey prepends the configured prefix to the given key.
func (t *azureTarget) fullKey(key string) string {
	return t.prefix + key
}

func (t *azureTarget) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) error {
	blobName := t.fullKey(key)

	uploadOpts := &blockblob.UploadStreamOptions{}

	if opts.ContentType != "" {
		uploadOpts.HTTPHeaders = &blob.HTTPHeaders{
			BlobContentType: &opts.ContentType,
		}
	}

	if len(opts.Metadata) > 0 {
		m := make(map[string]*string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			v := v
			m[k] = &v
		}
		uploadOpts.Metadata = m
	}

	_, err := t.client.UploadStream(ctx, t.containerName, blobName, body, uploadOpts)
	if err != nil {
		return fmt.Errorf("azure UploadStream %q: %w", key, err)
	}
	return nil
}

func (t *azureTarget) Get(ctx context.Context, key string) (io.ReadCloser, ObjectMeta, error) {
	blobName := t.fullKey(key)

	resp, err := t.client.DownloadStream(ctx, t.containerName, blobName, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil, ObjectMeta{}, ErrNotFound
		}
		return nil, ObjectMeta{}, fmt.Errorf("azure DownloadStream %q: %w", key, err)
	}

	meta := ObjectMeta{}
	if resp.ETag != nil {
		meta.ETag = string(*resp.ETag)
	}
	if resp.ContentLength != nil {
		meta.Size = *resp.ContentLength
	}

	return resp.Body, meta, nil
}

func (t *azureTarget) Head(ctx context.Context, key string) (ObjectMeta, error) {
	blobName := t.fullKey(key)

	blobClient := t.client.ServiceClient().NewContainerClient(t.containerName).NewBlobClient(blobName)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return ObjectMeta{}, ErrNotFound
		}
		return ObjectMeta{}, fmt.Errorf("azure GetProperties %q: %w", key, err)
	}

	meta := ObjectMeta{}
	if props.ETag != nil {
		meta.ETag = string(*props.ETag)
	}
	if props.ContentLength != nil {
		meta.Size = *props.ContentLength
	}

	return meta, nil
}

func (t *azureTarget) Delete(ctx context.Context, key string) error {
	blobName := t.fullKey(key)

	_, err := t.client.DeleteBlob(ctx, t.containerName, blobName, nil)
	if err != nil {
		if isAzureNotFound(err) {
			return nil // Delete is idempotent.
		}
		return fmt.Errorf("azure DeleteBlob %q: %w", key, err)
	}
	return nil
}

func (t *azureTarget) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	fullPrefix := t.fullKey(prefix)
	var results []ObjectInfo

	pager := t.client.NewListBlobsFlatPager(t.containerName, &container.ListBlobsFlatOptions{
		Prefix: &fullPrefix,
	})

	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("azure ListBlobsFlat prefix %q: %w", prefix, err)
		}
		for _, item := range page.Segment.BlobItems {
			if item.Name == nil {
				continue
			}
			logicalKey := strings.TrimPrefix(*item.Name, t.prefix)
			info := ObjectInfo{
				Key: logicalKey,
			}
			if item.Properties != nil {
				if item.Properties.ContentLength != nil {
					info.Size = *item.Properties.ContentLength
				}
				if item.Properties.ETag != nil {
					info.ETag = string(*item.Properties.ETag)
				}
			}
			results = append(results, info)
		}
	}

	return results, nil
}

func (t *azureTarget) ConditionalPut(ctx context.Context, key string, body io.Reader, condition WriteCondition, opts PutOptions) error {
	blobName := t.fullKey(key)

	uploadOpts := &blockblob.UploadStreamOptions{}

	if opts.ContentType != "" {
		uploadOpts.HTTPHeaders = &blob.HTTPHeaders{
			BlobContentType: &opts.ContentType,
		}
	}

	if len(opts.Metadata) > 0 {
		m := make(map[string]*string, len(opts.Metadata))
		for k, v := range opts.Metadata {
			v := v
			m[k] = &v
		}
		uploadOpts.Metadata = m
	}

	// For Azure conditional writes, use lease-based access conditions.
	if condition.LeaseID != "" {
		uploadOpts.AccessConditions = &blob.AccessConditions{
			LeaseAccessConditions: &blob.LeaseAccessConditions{
				LeaseID: &condition.LeaseID,
			},
		}
	}

	// If an ETag condition is provided, use If-Match.
	if condition.IfMatch != "" && condition.IfMatch != "*" {
		etag := azcore.ETag(condition.IfMatch)
		if uploadOpts.AccessConditions == nil {
			uploadOpts.AccessConditions = &blob.AccessConditions{}
		}
		uploadOpts.AccessConditions.ModifiedAccessConditions = &blob.ModifiedAccessConditions{
			IfMatch: &etag,
		}
	} else if condition.IfMatch == "*" {
		// If-None-Match: * means the blob must not exist.
		star := azcore.ETag("*")
		if uploadOpts.AccessConditions == nil {
			uploadOpts.AccessConditions = &blob.AccessConditions{}
		}
		uploadOpts.AccessConditions.ModifiedAccessConditions = &blob.ModifiedAccessConditions{
			IfNoneMatch: &star,
		}
	}

	_, err := t.client.UploadStream(ctx, t.containerName, blobName, body, uploadOpts)
	if err != nil {
		if isAzurePreconditionFailed(err) {
			return ErrPreconditionFailed
		}
		if isAzureLeaseConflict(err) {
			return ErrLeaseConflict
		}
		return fmt.Errorf("azure ConditionalPut %q: %w", key, err)
	}
	return nil
}

// isAzureNotFound returns true if the Azure error indicates a 404.
func isAzureNotFound(err error) bool {
	return bloberror.HasCode(err, bloberror.BlobNotFound, bloberror.ContainerNotFound)
}

// isAzurePreconditionFailed returns true if the Azure error indicates a 412.
func isAzurePreconditionFailed(err error) bool {
	if bloberror.HasCode(err, bloberror.ConditionNotMet) {
		return true
	}
	var respErr interface{ StatusCode() int }
	if errors.As(err, &respErr) && respErr.StatusCode() == 412 {
		return true
	}
	return false
}

// isAzureLeaseConflict returns true if the error is a lease conflict.
func isAzureLeaseConflict(err error) bool {
	return bloberror.HasCode(err, bloberror.LeaseIDMismatchWithBlobOperation, bloberror.LeaseIDMissing)
}
