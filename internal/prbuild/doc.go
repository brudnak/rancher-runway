// Package prbuild verifies PR inclusion in images and collects related issue
// readiness evidence. Service dependencies are supplied by Options. A daily
// scan can use NewIssueScan to share evidence and image observations within that
// scan, while scheduling and persistence remain the application's responsibility.
package prbuild
