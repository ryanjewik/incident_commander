package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/ryanjewik/incident_commander/backend/models"
	"github.com/ryanjewik/incident_commander/backend/services"
	"google.golang.org/api/iterator"
)

type ChatHandler struct {
	userService     *services.UserService
	firebaseService *services.FirebaseService
	upgrader        websocket.Upgrader
	connections     map[string]map[*websocket.Conn]bool // organizationID -> connections
	userConnections map[string]*websocket.Conn          // userID -> connection (one per user)
	connMutex       sync.RWMutex
}

func NewChatHandler(userService *services.UserService, firebaseService *services.FirebaseService) *ChatHandler {
	return &ChatHandler{
		userService:     userService,
		firebaseService: firebaseService,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Allow connections from localhost during development
				origin := r.Header.Get("Origin")
				return origin == "http://localhost:5173" || origin == "http://localhost:3000"
			},
		},
		connections:     make(map[string]map[*websocket.Conn]bool),
		userConnections: make(map[string]*websocket.Conn),
	}
}

type SendMessageRequest struct {
	Text       string `json:"text" binding:"required"`
	IncidentID string `json:"incident_id"`
}

// SendMessage saves a chat message to Firestore
func (h *ChatHandler) SendMessage(c *gin.Context) {
	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	user, err := h.userService.GetUser(context.Background(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user details"})
		return
	}

	if user.OrganizationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User not part of an organization"})
		return
	}

	mentionsBot := strings.Contains(req.Text, "@assistant")

	message := models.Message{
		ID:             uuid.New().String(),
		OrganizationID: user.OrganizationID,
		IncidentID:     req.IncidentID,
		UserID:         user.ID,
		UserName:       user.FirstName + " " + user.LastName,
		Text:           req.Text,
		MentionsBot:    mentionsBot,
		CreatedAt:      time.Now(),
	}

	ctx := context.Background()
	_, err = h.firebaseService.Firestore.Collection("messages").Doc(message.ID).Set(ctx, message)
	if err != nil {
		log.Printf("[SendMessage] Failed to save message to Firestore: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save message"})
		return
	}

	c.JSON(http.StatusCreated, message)
}

// GetMessages retrieves all messages for the user's organization
func (h *ChatHandler) GetMessages(c *gin.Context) {
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	user, err := h.userService.GetUser(context.Background(), userID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user details"})
		return
	}

	if user.OrganizationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User not part of an organization"})
		return
	}

	incidentID := c.Query("incident_id")
	ctx := context.Background()

	// Fetch all messages for the organization and filter in memory
	query := h.firebaseService.Firestore.Collection("messages").
		Where("organization_id", "==", user.OrganizationID)

	iter := query.Documents(ctx)

	messages := []models.Message{}
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Printf("[GetMessages] Error iterating documents: %v", err)
			break
		}

		var msg models.Message
		if err := doc.DataTo(&msg); err != nil {
			log.Printf("[GetMessages] Error parsing document %s: %v", doc.Ref.ID, err)
			continue
		}
		msg.ID = doc.Ref.ID

		// Filter by incident_id in memory
		if incidentID != "" {
			if msg.IncidentID == incidentID {
				messages = append(messages, msg)
			}
		} else {
			if msg.IncidentID == "" {
				messages = append(messages, msg)
			}
		}
	}

	// Sort messages by created_at in memory since we removed OrderBy from query
	// This avoids needing a composite index
	for i := 0; i < len(messages)-1; i++ {
		for j := i + 1; j < len(messages); j++ {
			if messages[i].CreatedAt.After(messages[j].CreatedAt) {
				messages[i], messages[j] = messages[j], messages[i]
			}
		}
	}

	c.JSON(http.StatusOK, messages)
}

// HandleWebSocket handles WebSocket connections for real-time chat
func (h *ChatHandler) HandleWebSocket(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing token"})
		return
	}

	decodedToken, err := h.userService.VerifyToken(c.Request.Context(), token)
	if err != nil {
		log.Printf("[WebSocket] Token verification failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
		return
	}

	user, err := h.userService.GetUser(c.Request.Context(), decodedToken.UID)
	if err != nil {
		log.Printf("[WebSocket] Failed to get user: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
		return
	}

	if user.OrganizationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User not part of an organization"})
		return
	}

	h.closeExistingUserConnection(user.ID)

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WebSocket] Upgrade failed: %v", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	h.registerConnection(user.OrganizationID, user.ID, conn)

	log.Printf("[WebSocket] User %s connected to organization %s", user.Email, user.OrganizationID)

	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-pingTicker.C:
				if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
					log.Printf("[WebSocket] Ping failed for user %s: %v", user.Email, err)
					conn.Close()
					return
				}
			case <-done:
				return
			}
		}
	}()

	defer func() {
		close(done)
		h.unregisterConnection(user.OrganizationID, user.ID, conn)
		conn.Close()
		log.Printf("[WebSocket] User %s disconnected from organization %s", user.Email, user.OrganizationID)
	}()

	for {
		var req SendMessageRequest
		err := conn.ReadJSON(&req)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WebSocket] Error reading message: %v", err)
			}
			break
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		mentionsBot := strings.Contains(req.Text, "@assistant")

		message := models.Message{
			ID:             uuid.New().String(),
			OrganizationID: user.OrganizationID,
			IncidentID:     req.IncidentID,
			UserID:         user.ID,
			UserName:       user.FirstName + " " + user.LastName,
			Text:           req.Text,
			MentionsBot:    mentionsBot,
			CreatedAt:      time.Now(),
		}

		ctx := context.Background()
		_, err = h.firebaseService.Firestore.Collection("messages").Doc(message.ID).Set(ctx, message)
		if err != nil {
			log.Printf("[WebSocket] Failed to save message: %v", err)
			continue
		}

		h.broadcastMessage(user.OrganizationID, message)
	}
}

func (h *ChatHandler) closeExistingUserConnection(userID string) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if existingConn, exists := h.userConnections[userID]; exists {
		log.Printf("[WebSocket] Closing existing connection for user %s", userID)
		existingConn.Close()
		delete(h.userConnections, userID)
	}
}

func (h *ChatHandler) registerConnection(organizationID string, userID string, conn *websocket.Conn) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if h.connections[organizationID] == nil {
		h.connections[organizationID] = make(map[*websocket.Conn]bool)
	}
	h.connections[organizationID][conn] = true
	h.userConnections[userID] = conn
}

func (h *ChatHandler) unregisterConnection(organizationID string, userID string, conn *websocket.Conn) {
	h.connMutex.Lock()
	defer h.connMutex.Unlock()

	if h.connections[organizationID] != nil {
		delete(h.connections[organizationID], conn)
		if len(h.connections[organizationID]) == 0 {
			delete(h.connections, organizationID)
		}
	}

	if h.userConnections[userID] == conn {
		delete(h.userConnections, userID)
	}
}

func (h *ChatHandler) broadcastMessage(organizationID string, message models.Message) {
	h.connMutex.RLock()
	defer h.connMutex.RUnlock()

	connections := h.connections[organizationID]
	if connections == nil {
		return
	}

	for conn := range connections {
		err := conn.WriteJSON(message)
		if err != nil {
			log.Printf("[WebSocket] Error broadcasting to connection: %v", err)
		}
	}
}
