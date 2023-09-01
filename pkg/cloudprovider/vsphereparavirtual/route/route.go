package route

import (
	"github.com/pkg/errors"
	"time"
)

// A list of possible RouteSet operation error messages
var (
	ErrGetRouteSet    = errors.New("failed to get RouteSet")
	ErrCreateRouteSet = errors.New("failed to create RouteSet")
	ErrListRouteSet   = errors.New("failed to list RouteSet")
	ErrDeleteRouteSet = errors.New("failed to delete RouteSet")
)

const (
	// LabelKeyClusterName is the label key to specify GC name for RouteSet CR
	LabelKeyClusterName = "clusterName"
	// RealizedStateTimeout is the timeout duration for realized state check
	RealizedStateTimeout = 10 * time.Second
	// RealizedStateSleepTime is the interval between realized state check
	RealizedStateSleepTime = 1 * time.Second
)
