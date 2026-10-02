package cachelab

type Request struct {
	ClusterID      string       `json:"clusterId,omitempty"`
	Action         string       `json:"action"`
	Workspace      string       `json:"workspace"`
	Snapshot       string       `json:"snapshot"`
	Other          string       `json:"other"`
	Name           string       `json:"name"`
	Notes          string       `json:"notes"`
	Folder         string       `json:"folder"`
	Favorite       bool         `json:"favorite"`
	Confirm        string       `json:"confirm"`
	Token          string       `json:"token"`
	Profile        Workspace    `json:"profile"`
	View           cacheLabView `json:"view"`
	SQL            string       `json:"sql"`
	Table          string       `json:"table"`
	Search         string       `json:"search"`
	Sort           string       `json:"sort"`
	Desc           bool         `json:"desc"`
	Offset         int          `json:"offset"`
	Limit          int          `json:"limit"`
	IgnoreVolatile bool         `json:"ignoreVolatile"`
	RunID          string       `json:"runId"`
}
