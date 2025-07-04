package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	mautrix "github.com/iKonoTelecomunicaciones/go"
	"github.com/iKonoTelecomunicaciones/go/event"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/iKonoTelecomunicaciones/go/bridgev2"
	"github.com/iKonoTelecomunicaciones/go/bridgev2/matrix"
	"github.com/iKonoTelecomunicaciones/go/bridgev2/status"
	"github.com/iKonoTelecomunicaciones/go/id"
	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exhttp"
	"go.mau.fi/whatsmeow/types"

	"github.com/iKonoTelecomunicaciones/whatsapp/pkg/connector"
	"github.com/iKonoTelecomunicaciones/whatsapp/pkg/waid"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"net.maunium.whatsapp.login"},
}

func legacyProvAuth(r *http.Request) string {
	if !strings.HasSuffix(r.URL.Path, "/v1/login") {
		return ""
	}
	authParts := strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",")
	for _, part := range authParts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "net.maunium.whatsapp.auth-") {
			return strings.TrimPrefix(part, "net.maunium.whatsapp.auth-")
		}
	}
	return ""
}

type ConnInfo struct {
	IsConnected bool `json:"is_connected"`
	IsLoggedIn  bool `json:"is_logged_in"`
}

type ConnectionInfo struct {
	HasSession     bool      `json:"has_session"`
	ManagementRoom id.RoomID `json:"management_room"`
	Conn           ConnInfo  `json:"conn"`
	JID            string    `json:"jid"`
	Phone          string    `json:"phone"`
	Platform       string    `json:"platform"`
}

type PingInfo struct {
	WhatsappConnectionInfo ConnectionInfo `json:"whatsapp"`
	Mxid                   id.UserID      `json:"mxid"`
}

type OtherUserInfo struct {
	MXID   id.UserID           `json:"mxid"`
	JID    types.JID           `json:"jid"`
	Name   string              `json:"displayname"`
	Avatar id.ContentURIString `json:"avatar_url"`
}

type PortalInfo struct {
	RoomID      id.RoomID        `json:"room_id"`
	OtherUser   *OtherUserInfo   `json:"other_user,omitempty"`
	GroupInfo   *types.GroupInfo `json:"group_info,omitempty"`
	JustCreated bool             `json:"just_created"`
}

type Error struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
	ErrCode string `json:"errcode"`
}

type Response struct {
	Success bool   `json:"success"`
	Status  string `json:"status"`
}

type SetEventBody struct {
	RoomID     string `json:"room_id"`
	PowerLevel int    `json:"power_level"`
	UserID     string `json:"user_id"`
}

func respondWebsocketWithError(conn *websocket.Conn, err error, message string) {
	var mautrixRespErr mautrix.RespError
	var bv2RespErr bridgev2.RespError
	if errors.As(err, &bv2RespErr) {
		mautrixRespErr = mautrix.RespError(bv2RespErr)
	} else if !errors.As(err, &mautrixRespErr) {
		mautrixRespErr = mautrix.RespError{
			Err:        message,
			ErrCode:    "M_UNKNOWN",
			StatusCode: http.StatusInternalServerError,
		}
	}
	_ = conn.WriteJSON(&mautrixRespErr)
}

var notNumbers = regexp.MustCompile("[^0-9]")

func legacyProvLogin(w http.ResponseWriter, r *http.Request) {
	log := hlog.FromRequest(r)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Err(err).Msg("Failed to upgrade connection to websocket")
		return
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Debug().Err(err).Msg("Error closing websocket")
		}
	}()

	go func() {
		// Read everything so SetCloseHandler() works
		for {
			_, _, err = conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	conn.SetCloseHandler(func(code int, text string) error {
		log.Debug().Int("close_code", code).Msg("Login websocket closed, cancelling login")
		cancel()
		return nil
	})

	user := m.Matrix.Provisioning.GetUser(r)
	loginFlowID := connector.LoginFlowIDQR
	phoneNum := r.URL.Query().Get("phone_number")
	if phoneNum != "" {
		phoneNum = notNumbers.ReplaceAllString(phoneNum, "")
		if len(phoneNum) < 7 || strings.HasPrefix(phoneNum, "0") {
			errorMsg := "Invalid phone number"
			if len(phoneNum) > 6 {
				errorMsg = "Please enter the phone number in international format"
			}
			_ = conn.WriteJSON(Error{
				Error:   errorMsg,
				ErrCode: "invalid phone number",
			})
			return
		}
		loginFlowID = connector.LoginFlowIDPhone
	}
	login, err := c.CreateLogin(ctx, user, loginFlowID)
	if err != nil {
		log.Err(err).Msg("Failed to create login")
		respondWebsocketWithError(conn, err, "Failed to create login")
		return
	}
	waLogin := login.(*connector.WALogin)
	waLogin.Timezone = r.URL.Query().Get("tz")
	step, err := waLogin.Start(ctx)
	if err != nil {
		log.Err(err).Msg("Failed to start login")
		respondWebsocketWithError(conn, err, "Failed to start login")
		return
	}
	if phoneNum != "" {
		if step.StepID != connector.LoginStepIDPhoneNumber {
			respondWebsocketWithError(conn, errors.New("unexpected step"), "Unexpected step while starting phone number login")
			waLogin.Cancel()
			return
		}
		step, err = waLogin.SubmitUserInput(ctx, map[string]string{"phone_number": phoneNum})
		if err != nil {
			log.Err(err).Msg("Failed to submit phone number")
			respondWebsocketWithError(conn, err, "Failed to start phone code login")
			return
		} else if step.StepID != connector.LoginStepIDCode {
			respondWebsocketWithError(conn, errors.New("unexpected step"), "Unexpected step after submitting phone number")
			waLogin.Cancel()
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"pairing_code": step.DisplayAndWaitParams.Data,
			"timeout":      180,
		})
	} else if step.StepID != connector.LoginStepIDQR {
		respondWebsocketWithError(conn, errors.New("unexpected step"), "Unexpected step while starting QR login")
		waLogin.Cancel()
		return
	} else {
		_ = conn.WriteJSON(map[string]any{
			"code":    step.DisplayAndWaitParams.Data,
			"timeout": 60,
		})
	}
	for {
		step, err = waLogin.Wait(ctx)
		if err != nil {
			log.Err(err).Msg("Failed to wait for login")
			respondWebsocketWithError(conn, err, "Failed to wait for login")
		} else if step.StepID == connector.LoginStepIDQR {
			_ = conn.WriteJSON(map[string]any{
				"code":    step.DisplayAndWaitParams.Data,
				"timeout": 20,
			})
			continue
		} else if step.StepID != connector.LoginStepIDComplete {
			respondWebsocketWithError(conn, errors.New("unexpected step"), "Unexpected step while waiting for login")
			waLogin.Cancel()
		} else {
			// TODO delete old logins
			_ = conn.WriteJSON(map[string]any{
				"success":  true,
				"jid":      waid.ParseUserLoginID(step.CompleteParams.UserLoginID, step.CompleteParams.UserLogin.Metadata.(*waid.UserLoginMetadata).WADeviceID).String(),
				"platform": step.CompleteParams.UserLogin.Client.(*connector.WhatsAppClient).Device.Platform,
				"phone":    step.CompleteParams.UserLogin.RemoteProfile.Phone,
			})
			go handleLoginComplete(context.WithoutCancel(ctx), user, step.CompleteParams.UserLogin)
		}
		break
	}
}
func handleLoginComplete(ctx context.Context, user *bridgev2.User, newLogin *bridgev2.UserLogin) {
	allLogins := user.GetUserLogins()
	for _, login := range allLogins {
		if login.ID != newLogin.ID {
			login.Delete(ctx, status.BridgeState{StateEvent: status.StateLoggedOut, Reason: "LOGIN_OVERRIDDEN"}, bridgev2.DeleteOpts{})
		}
	}
}

func legacyProvLogout(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	allLogins := user.GetUserLogins()
	if len(allLogins) == 0 {
		exhttp.WriteJSONResponse(w, http.StatusOK, Error{
			Error:   "You're not logged in",
			ErrCode: "not logged in",
		})
		return
	}
	for _, login := range allLogins {
		// Intentionally don't delete the user login, only logout remote
		login.Client.(*connector.WhatsAppClient).LogoutRemote(r.Context())
	}
	exhttp.WriteJSONResponse(w, http.StatusOK, Response{true, "Logged out successfully"})
}

func legacyProvContacts(w http.ResponseWriter, r *http.Request) {
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	if userLogin == nil {
		return
	}
	if contacts, err := userLogin.Client.(*connector.WhatsAppClient).Device.Contacts.GetAllContacts(r.Context()); err != nil {
		hlog.FromRequest(r).Err(err).Msg("Failed to fetch all contacts")
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Internal server error while fetching contact list",
			ErrCode: "failed to get contacts",
		})
	} else {
		augmentedContacts := map[types.JID]any{}
		for jid, contact := range contacts {
			var avatarURL id.ContentURIString
			if puppet, _ := m.Bridge.GetExistingGhostByID(r.Context(), waid.MakeUserID(jid)); puppet != nil {
				avatarURL = puppet.AvatarMXC
			}
			augmentedContacts[jid] = map[string]interface{}{
				"Found":        contact.Found,
				"FirstName":    contact.FirstName,
				"FullName":     contact.FullName,
				"PushName":     contact.PushName,
				"BusinessName": contact.BusinessName,
				"AvatarURL":    avatarURL,
			}
		}
		exhttp.WriteJSONResponse(w, http.StatusOK, augmentedContacts)
	}
}

func legacyProvResolveIdentifier(w http.ResponseWriter, r *http.Request) {
	number := mux.Vars(r)["number"]
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	if userLogin == nil {
		return
	}
	startChat := strings.Contains(r.URL.Path, "/v1/pm/")
	resp, err := userLogin.Client.(*connector.WhatsAppClient).ResolveIdentifier(r.Context(), number, startChat)
	if err != nil {
		hlog.FromRequest(r).Warn().Err(err).Str("identifier", number).Msg("Failed to resolve identifier")
		matrix.RespondWithError(w, err, "Internal error resolving identifier")
		return
	}
	var portal *bridgev2.Portal
	if startChat {
		portal, err = m.Bridge.GetPortalByKey(r.Context(), resp.Chat.PortalKey)
		if err != nil {
			hlog.FromRequest(r).Warn().Err(err).Stringer("portal_key", resp.Chat.PortalKey).Msg("Failed to get portal by key")
			matrix.RespondWithError(w, err, "Internal error getting portal by key")
			return
		}
		err = portal.CreateMatrixRoom(r.Context(), userLogin, nil)
		if err != nil {
			hlog.FromRequest(r).Warn().Err(err).Stringer("portal_key", resp.Chat.PortalKey).Msg("Failed to create matrix room for portal")
			matrix.RespondWithError(w, err, "Internal error creating matrix room for portal")
			return
		}
	} else {
		portal, _ = m.Bridge.GetExistingPortalByKey(r.Context(), resp.Chat.PortalKey)
	}
	var roomID id.RoomID
	if portal != nil {
		roomID = portal.MXID
	}
	exhttp.WriteJSONResponse(w, http.StatusOK, PortalInfo{
		RoomID: roomID,
		OtherUser: &OtherUserInfo{
			JID:    waid.ParseUserID(resp.UserID),
			MXID:   resp.Ghost.Intent.GetMXID(),
			Name:   resp.Ghost.Name,
			Avatar: resp.Ghost.AvatarMXC,
		},
	})
}

func legacyProvPing(w http.ResponseWriter, r *http.Request) {
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)

	if userLogin == nil {
		return
	}

	whatsappClient := userLogin.Client.(*connector.WhatsAppClient)
	managementRoom, err := userLogin.User.GetManagementRoom(r.Context())

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching management room",
			ErrCode: "failed to get management room",
		})
		return
	}

	whatsappConnectionInfo := ConnectionInfo{
		HasSession:     whatsappClient.IsLoggedIn(),
		ManagementRoom: managementRoom,
	}

	if !whatsappClient.JID.IsEmpty() {
		whatsappConnectionInfo.JID = whatsappClient.JID.String()
		whatsappConnectionInfo.Phone = "+" + whatsappClient.JID.User
		if whatsappClient.Device != nil && whatsappClient.Device.Platform != "" {
			whatsappConnectionInfo.Platform = whatsappClient.Device.Platform
		}
	}

	if whatsappClient.Client != nil {
		whatsappConnectionInfo.Conn = ConnInfo{
			IsConnected: whatsappClient.Client.IsConnected(),
			IsLoggedIn:  whatsappClient.Client.IsLoggedIn(),
		}
	}

	resp := PingInfo{
		WhatsappConnectionInfo: whatsappConnectionInfo,
		Mxid:                   whatsappClient.UserLogin.User.MXID,
	}

	w.Header().Set("Content-Type", "application/json")
	exhttp.WriteJSONResponse(w, http.StatusOK, resp)
}

func legacyProvRoomInfo(w http.ResponseWriter, r *http.Request) {
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)

	if userLogin == nil {
		return
	}

	room_id := r.URL.Query().Get("room_id")

	if room_id == "" {
		exhttp.WriteJSONResponse(w, http.StatusBadRequest, Error{
			Error:   "Missing room_id",
			ErrCode: "missing room_id",
		})
		return
	}

	portal, err := m.Bridge.GetPortalByMXID(r.Context(), id.RoomID(room_id))

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching portal",
			ErrCode: "failed to get portal",
		})
		return
	}

	if portal == nil {
		exhttp.WriteJSONResponse(w, http.StatusNotFound, Error{
			Error:   "Portal not found",
			ErrCode: "portal not found",
		})
		return
	}

	whatsappClient := userLogin.Client.(*connector.WhatsAppClient)
	chatInfo, err := whatsappClient.GetChatInfo(r.Context(), portal)

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching chat info",
			ErrCode: "failed to get chat info",
		})
		return
	}

	portalInfo := map[string]interface{}{
		"room_id":    portal.MXID,
		"name":       chatInfo.Name,
		"topic":      *chatInfo.Topic,
		"avatar":     chatInfo.Avatar,
		"members":    *chatInfo.Members,
		"join_rule":  chatInfo.JoinRule,
		"type":       *chatInfo.Type,
		"disappear":  chatInfo.Disappear,
		"parent_id":  chatInfo.ParentID,
		"user_local": *chatInfo.UserLocal,
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, portalInfo)
}

func legacyProvSetPowerlevels(w http.ResponseWriter, r *http.Request) {
	var body SetEventBody
	err := json.NewDecoder(r.Body).Decode(&body)

	if err != nil {
		http.Error(w, "Can't read body", http.StatusBadRequest)
		return
	}

	log := hlog.FromRequest(r)
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	if userLogin == nil {
		return
	}

	roomID := body.RoomID
	powerLevel := body.PowerLevel
	userID := body.UserID

	if roomID == "" {
		exhttp.WriteJSONResponse(w, http.StatusBadRequest, Error{
			Error:   "Missing room_id",
			ErrCode: "missing room_id",
		})
		return
	}

	if powerLevel < 0 {
		exhttp.WriteJSONResponse(w, http.StatusBadRequest, Error{
			Error:   "Invalid power level",
			ErrCode: "invalid power level",
		})
		return
	}

	if userID == "" {
		exhttp.WriteJSONResponse(w, http.StatusBadRequest, Error{
			Error:   "Missing user_id",
			ErrCode: "missing user_id",
		})
		return
	}

	// Get the portal by room ID
	portal, err := m.Bridge.GetPortalByMXID(r.Context(), id.RoomID(roomID))

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching portal",
			ErrCode: "failed to get portal",
		})
		return
	}

	if portal == nil {
		exhttp.WriteJSONResponse(w, http.StatusNotFound, Error{
			Error:   "Portal not found",
			ErrCode: "portal not found",
		})
		return
	}

	// Get members of the portal
	powerLevels, err := portal.Bridge.Matrix.GetPowerLevels(r.Context(), portal.MXID)

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching portal members",
			ErrCode: "failed to get portal members",
		})
		return
	}

	// Change the power level of the user
	powerLevels.Users[id.UserID(userID)] = powerLevel

	botIntent := m.Bridge.Matrix.BotIntent()

	content := event.Content{
		Parsed: &powerLevels,
	}

	// Send the state event to the portal
	event, err := botIntent.SendState(r.Context(), portal.MXID, event.StatePowerLevels, "", &content, time.Now())

	if err != nil {
		log.Error().Err(err).Msg("Error while changing power levels")
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while changing power levels",
			ErrCode: "failed to change power levels",
		})
		return
	}

	resp := Response{
		Success: true,
		Status: "Successfully updated power level for user " + userID +
			". Event ID: " + event.EventID.String() + " room ID: " + roomID,
	}

	w.Header().Set("Content-Type", "application/json")
	exhttp.WriteJSONResponse(w, http.StatusOK, resp)
}

func legacyProvSetRelay(w http.ResponseWriter, r *http.Request) {
	var body SetEventBody
	err := json.NewDecoder(r.Body).Decode(&body)

	if err != nil {
		http.Error(w, "Can't read body", http.StatusBadRequest)
		return
	}

	log := hlog.FromRequest(r)
	userLogin := m.Matrix.Provisioning.GetLoginForRequest(w, r)
	if userLogin == nil {
		return
	}

	roomID := body.RoomID
	if roomID == "" {
		exhttp.WriteJSONResponse(w, http.StatusBadRequest, Error{
			Error:   "Missing room_id",
			ErrCode: "missing room_id",
		})
		return
	}

	// Get the portal by room ID
	portal, err := m.Bridge.GetPortalByMXID(r.Context(), id.RoomID(roomID))

	if err != nil {
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while fetching portal",
			ErrCode: "failed to get portal",
		})
		return
	}
	if portal == nil {
		exhttp.WriteJSONResponse(w, http.StatusNotFound, Error{
			Error:   "Portal not found",
			ErrCode: "portal not found",
		})
		return
	}

	err = portal.SetRelay(r.Context(), userLogin)

	if err != nil {
		log.Error().Err(err).Msg("Error while setting relay")
		exhttp.WriteJSONResponse(w, http.StatusInternalServerError, Error{
			Error:   "Error while setting relay",
			ErrCode: "failed to set relay",
		})
		return
	}

	resp := Response{
		Success: true,
		Status:  "Successfully set relay for room " + roomID,
	}

	w.Header().Set("Content-Type", "application/json")
	exhttp.WriteJSONResponse(w, http.StatusOK, resp)
}
