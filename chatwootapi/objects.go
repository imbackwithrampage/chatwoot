package chatwootapi

// Identifiers
type AccountID int
type AttachmentID int
type ContactID int
type ConversationID int
type InboxID int
type MessageID int
type SenderID int

// Contact
type Contact struct {
	ID         ContactID `json:"id"`
	Email      string    `json:"email"`
	Identifier string    `json:"identifier"`
}

type ContactsPayload struct {
	Payload []Contact `json:"payload"`
}

type ContactPayloadInner struct {
	Contact Contact `json:"contact"`
}

type ContactPayload struct {
	Payload ContactPayloadInner `json:"payload"`
}

type CreateContactPayload struct {
	InboxID     InboxID `json:"inbox_id"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	PhoneNumber string  `json:"phone_number"`
	Identifier  string  `json:"identifier"`
}

// Attachment

type Attachment struct {
	ID        AttachmentID `json:"id"`
	FileType  string       `json:"file_type"`
	AccountID AccountID    `json:"account_id"`
	DataURL   string       `json:"data_url"`
	ThumbURL  string       `json:"thumb_url"`
}

// Message

type Sender struct {
	ID            SenderID `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"user"`
	AvailableName string   `json:"available_name"`
}

type Message struct {
	ID          MessageID    `json:"id"`
	Content     *string      `json:"content"`
	Private     bool         `json:"private"`
	Attachments []Attachment `json:"attachments"`
	Sender      Sender       `json:"sender"`
}

// Conversation

type ConversationMeta struct {
	Sender Contact `json:"sender"`
}

type Conversation struct {
	ID               ConversationID    `json:"id"`
	AccountID        AccountID         `json:"account_id"`
	InboxID          InboxID           `json:"inbox_id"`
	Messages         []Message         `json:"messages"`
	Meta             ConversationMeta  `json:"meta"`
	CustomAttributes map[string]string `json:"custom_attributes"`
}

type ConversationsPayload struct {
	Payload []Conversation `json:"payload"`
}

// Content Attributes

type ContentAttributes struct {
	Deleted bool `json:"deleted"`
}

// Webhook

type MessageCreated struct {
	ID                MessageID          `json:"id"`
	Content           string             `json:"content"`
	CreatedAt         string             `json:"created_at"`
	MessageType       string             `json:"message_type"`
	ContentType       string             `json:"content_type"`
	ContentAttributes *ContentAttributes `json:"content_attributes"`
	Private           bool               `json:"private"`
	Conversation      Conversation       `json:"conversation"`
}

type ConversationStatusChanged struct {
	ID     ConversationID `json:"id"`
	Status string         `json:"status"`
}
