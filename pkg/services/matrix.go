package services

import (
	"fmt"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/id"
)

type MatrixOptions struct {
	AccessToken string      `json:"access_token"`
	DeviceID    id.DeviceID `json:"device_id"`
	Homeserver  string      `json:"homeserver"`
	UserID      id.UserID   `json:"user_id"`
}

func NewMatrixService(opts MatrixOptions) NotificationService {
	return &matrixService{opts: opts}
}

type matrixService struct {
	opts MatrixOptions
}

func (s *matrixService) Send(notification Notification, dest Destination) error {
	client, err := mautrix.NewClient(s.opts.Homeserver, s.opts.UserID, s.opts.AccessToken)
	if err != nil {
		return fmt.Errorf("couldn't create matrix client: %w", err)
	}
	// normally gets set during client.Login
	client.DeviceID = s.opts.DeviceID

	markdownContent := format.RenderMarkdown(notification.Message, true, true)

	_, err = client.SendMessageEvent(id.RoomID(dest.Recipient), event.EventMessage, &event.MessageEventContent{
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
