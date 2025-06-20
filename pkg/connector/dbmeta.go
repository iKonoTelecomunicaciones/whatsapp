package connector

import (
	"github.com/iKonoTelecomunicaciones/go/bridgev2/database"

	"github.com/iKonoTelecomunicaciones/whatsapp/pkg/waid"
)

func (wa *WhatsAppConnector) GetDBMetaTypes() database.MetaTypes {
	return database.MetaTypes{
		Ghost: func() any {
			return &waid.GhostMetadata{}
		},
		Message: func() any {
			return &waid.MessageMetadata{}
		},
		Reaction: func() any {
			return &waid.ReactionMetadata{}
		},
		Portal: func() any {
			return &waid.PortalMetadata{}
		},
		UserLogin: func() any {
			return &waid.UserLoginMetadata{}
		},
	}
}
