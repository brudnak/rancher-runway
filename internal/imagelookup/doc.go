// Package imagelookup searches registries and inspects immutable image evidence.
// Services own transport, authentication, limits, and command dependencies; they
// know nothing about panel state, workspace storage, or HTTP handlers. Construct
// and configure a service before sharing it between callers.
package imagelookup
