package main

import (
	"sync"

	"github.com/mattermost/mattermost-server/v5/plugin"

	"github.com/mattermost/mattermost-plugin-email-reply/server/mailermost"
)

// Plugin is the object to run the plugin
type Plugin struct {
	plugin.MattermostPlugin

	Poller *mailermost.Poller

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}
