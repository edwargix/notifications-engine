package services

import (
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

type MatrixOptions struct {
	AccessToken   string      `json:"accessToken"`
	DeviceID      id.DeviceID `json:"deviceID"`
	HomeserverURL string      `json:"homeserverURL"`
	UserID        id.UserID   `json:"userID"`
}

func NewMatrixService(opts MatrixOptions) (NotificationService, error) {
	client, err := mautrix.NewClient(opts.HomeserverURL, opts.UserID, opts.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create matrix client: %w", err)
	}
	// normally gets set during client.Login
	client.DeviceID = opts.DeviceID
	return &matrixService{client, opts}, nil
}

type matrixService struct {
	client *mautrix.Client
	opts   MatrixOptions
}

func (s *matrixService) Send(notification Notification, dest Destination) error {
	markdownContent := format.RenderMarkdown(notification.Message, true, true)

	_, err := s.client.SendMessageEvent(id.RoomID(dest.Recipient), event.EventMessage, &event.MessageEventContent{
		MsgType: event.MsgNotice,
		Body:    markdownContent.Body,

		Format:        markdownContent.Format,
		FormattedBody: markdownContent.FormattedBody,
	})
	if err != nil {
		return fmt.Errorf("couldn't send matrix message: %w", err)
	}
	return nil
}
