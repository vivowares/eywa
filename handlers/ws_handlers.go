package handlers

import (
	"encoding/base64"
	"github.com/gorilla/websocket"
	"github.com/vivowares/octopus/Godeps/_workspace/src/github.com/zenazn/goji/web"
	. "github.com/vivowares/octopus/configs"
	"github.com/vivowares/octopus/connections"
	. "github.com/vivowares/octopus/handlers/message_handlers"
	. "github.com/vivowares/octopus/models"
	. "github.com/vivowares/octopus/utils"
	"net/http"
	"strconv"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  Config.Connections.BufferSizes.Read,
	WriteBufferSize: Config.Connections.BufferSizes.Write,
}

func WsHandler(c web.C, w http.ResponseWriter, r *http.Request) {
	asBytes, err := base64.URLEncoding.DecodeString(c.URLParams["channel_id"])
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	id, err := strconv.Atoi(string(asBytes))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ch := &Channel{}
	found := ch.FindById(id)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	t := r.Header.Get("AccessToken")
	if len(t) == 0 || !StringSliceContains(ch.AccessTokens, t) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	deviceId := c.URLParams["device_id"]
	if len(deviceId) == 0 {
		Render.JSON(w, http.StatusBadRequest, map[string]string{"error": "empty device id"})
		return
	}

	md := connections.NewMiddlewareStack()
	for _, hStr := range ch.MessageHandlers {
		if m, found := SupportedMessageHandlers[hStr]; found {
			md.Use(m)
		} else {
			Render.JSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported message handler: " + hStr})
			return
		}
	}
	h := md.Chain(nil)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		Render.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	_, err = connections.CM.NewConnection(deviceId, ws, h, map[string]interface{}{"channel": ch})
	if err != nil {
		Render.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
}
