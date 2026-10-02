package prbuild

import (
	"github.com/brudnak/ha-rancher-rke2/internal/imagelookup"
	"time"
)

type Options struct {
	ImageLookup *imagelookup.Service
	RunCommand  imagelookup.CommandRunner
	Now         func() time.Time
	Inspect     ImageInspector
	FetchPull   PullFetcher
	Compare     CommitComparator
}

func NewWithOptions(o Options) *Service {
	return &Service{imageLookup: o.ImageLookup, runCommand: o.RunCommand, now: o.Now, inspect: o.Inspect, fetchPull: o.FetchPull, compare: o.Compare}
}

// Target accessors expose the validated identity to injected GitHub clients.
func (t Target) Owner() string      { return t.owner }
func (t Target) Repository() string { return t.repository }
func (t Target) Number() int        { return t.number }
func (t Target) URL() string        { return t.url }
