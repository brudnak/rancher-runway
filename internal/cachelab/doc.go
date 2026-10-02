// Package cachelab owns the SQLite snapshot library and capture lifecycle.
// New accepts an explicit storage root and optional worker, command, and cluster
// resolution dependencies. Service methods synchronize access; callers read
// detached Library snapshots or preserve evidence with ReadEvidence.
package cachelab
