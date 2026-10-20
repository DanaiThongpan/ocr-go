package handlers

import (
	"ocr-go/middlewares"

	"github.com/gin-gonic/gin"
)

func LiveLogs(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// Get a dedicated channel for this client
	clientChan := middlewares.Broker.AddClient()
	defer middlewares.Broker.RemoveClient(clientChan)

	// Listen for client disconnection
	notify := c.Request.Context().Done()

	for {
		select {
		case <-notify:
			// Client disconnected
			return
		case msg, ok := <-clientChan:
			if !ok {
				return
			}
			c.SSEvent("message", msg)
			c.Writer.Flush()
		}
	}
}
