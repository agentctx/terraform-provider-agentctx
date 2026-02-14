package target

import "fmt"

// NewTarget creates a Target based on the provided Config.
// It dispatches to the appropriate backend constructor (S3, Azure, or GCS)
// and wraps the result in a RetryTarget if MaxRetries > 0.
func NewTarget(cfg Config) (Target, error) {
	var (
		t   Target
		err error
	)

	switch cfg.Type {
	case "s3":
		t, err = newS3Target(cfg)
	case "azure":
		t, err = newAzureTarget(cfg)
	case "gcs":
		t, err = newGCSTarget(cfg)
	case "memory":
		return GetOrCreateMemoryTarget(cfg.Name), nil
	default:
		return nil, fmt.Errorf("unsupported target type: %q (must be s3, azure, gcs, or memory)", cfg.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("creating %s target %q: %w", cfg.Type, cfg.Name, err)
	}

	if cfg.MaxRetries > 0 {
		backoff := cfg.RetryBackoff
		if backoff == "" {
			backoff = "exponential"
		}
		t = NewRetryTarget(t, cfg.MaxRetries, backoff)
	}

	return t, nil
}
