package main

import (
	"fmt"

	"github.com/blang/semver"
	"github.com/pkg/errors"

	"github.com/mattermost/mattermost-plugin-email-reply/server/mailermost"
)

const minimumServerVersion = "5.4.0"

func (p *Plugin) checkServerVersion() error {
	serverVersion, err := semver.Parse(p.API.GetServerVersion())
	if err != nil {
		return errors.Wrap(err, "failed to parse server version")
	}

	r := semver.MustParseRange(">=" + minimumServerVersion)
	if !r(serverVersion) {
		return fmt.Errorf("this plugin requires Mattermost v%s or later", minimumServerVersion)
	}

	return nil
}

// OnActivate is invoked when the plugin is activated.
//
// This demo implementation logs a message to the demo channel whenever the plugin is activated.
func (p *Plugin) OnActivate() error {
	if err := p.checkServerVersion(); err != nil {
		return err
	}

	configuration := p.getConfiguration()

	poller, err := mailermost.NewPoller(p.API, configuration.Server, configuration.Security, configuration.Password, configuration.PollingInterval)
	if err != nil {
		return errors.Wrap(err, "failed to create poller")
	}
	p.Poller = poller

	go p.Poller.Poll()

	return nil
}
