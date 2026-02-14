package provider

import "github.com/agentctx/terraform-provider-agentctx/internal/providerdata"

// ProviderData is an alias for the shared ProviderData type. This alias
// allows existing code inside the provider package to continue using the
// unqualified name while the canonical definition lives in the providerdata
// package (breaking the import cycle with resource packages).
type ProviderData = providerdata.ProviderData

// TargetConfigModel is an alias for the shared TargetConfigModel type.
type TargetConfigModel = providerdata.TargetConfigModel
