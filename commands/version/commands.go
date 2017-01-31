// Package versioncommands implements the version command
package versioncommands

import (
	"github.com/gluster/glusterd2/servers/rest/route"
)

// Command is a holding struct used to implement the GlusterD Command interface
type Command struct {
}

// Routes returns command routes. Required for the Command interface.
func (c *Command) Routes() route.Routes {
	return route.Routes{
		route.Route{
			Name:        "GetVersion",
			Method:      "GET",
			Pattern:     "/version",
			HandlerFunc: getVersionHandler,
		},
	}
}

// RegisterStepFuncs implements a required function for the Command interface
func (c *Command) RegisterStepFuncs() {
	return
}
