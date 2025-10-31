package main

import (
	"github.com/iKonoTelecomunicaciones/go/bridgev2/matrix/mxmain"

	"github.com/iKonoTelecomunicaciones/whatsapp/pkg/connector"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "mautrix-whatsapp",
	URL:         "https://github.com/mautrix/whatsapp",
	Description: "A Matrix-WhatsApp puppeting bridge.",
	Version:     "0.12.5",
	Connector:   &connector.WhatsAppConnector{},
}

func main() {
	m.PostStart = func() {
		if m.Matrix.Provisioning != nil {
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/contacts", legacyProvContacts)
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/resolve_identifier/{number}", legacyProvResolveIdentifier)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/pm/{number}", legacyProvResolveIdentifier)
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/ping", legacyProvPing)
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/room_info", legacyProvRoomInfo)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/set_power_level", legacyProvSetPowerlevels)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/set_relay", legacyProvSetRelay)
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/set_relay/{roomID}", legacyProvValidateSetRelay)
			m.Matrix.Provisioning.GetAuthFromRequest = legacyProvAuth
		}
	}
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
