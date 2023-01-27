package chatwootapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strings"

	log "github.com/sirupsen/logrus"
	mid "maunium.net/go/mautrix/id"
)

type MessageType int

const (
	IncomingMessage MessageType = iota
	OutgoingMessage
)

func MessageTypeString(messageType MessageType) string {
	switch messageType {
	case IncomingMessage:
		return "incoming"
	case OutgoingMessage:
		return "outgoing"
	}
	return ""
}

type ChatwootAPI struct {
	BaseURL     string
	AccountID   AccountID
	InboxID     InboxID
	AccessToken string

	Client *http.Client
}

func CreateChatwootAPI(baseURL string, accountID AccountID, inboxID InboxID, accessToken string) *ChatwootAPI {
	return &ChatwootAPI{
		BaseURL:     baseURL,
		AccountID:   accountID,
		InboxID:     inboxID,
		AccessToken: accessToken,
		Client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("Too many (>=10) redirects! Cancelling request.")
				}
				return nil
			},
		},
	}
}

func (api *ChatwootAPI) DoRequest(req *http.Request) (*http.Response, error) {
	req.Header.Add("API_ACCESS_TOKEN", api.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	return api.Client.Do(req)
}

func (api *ChatwootAPI) MakeUri(endpoint string) string {
	url, err := url.Parse(api.BaseURL)
	if err != nil {
		panic(err)
	}
	url.Path = path.Join(url.Path, fmt.Sprintf("api/v1/accounts/%d", api.AccountID), endpoint)
	return url.String()
}

func (api *ChatwootAPI) CreateContact(userID mid.UserID) (ContactID, error) {
	log.Info("Creating contact for ", userID)
	payload := CreateContactPayload{
		InboxID:    api.InboxID,
		Name:       userID.String(),
		Email:      userID.String(),
		Identifier: userID.String(),
	}
	jsonValue, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, api.MakeUri("contacts"), bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error(err)
		return 0, err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return 0, err
	}
	if resp.StatusCode != 200 {
		data, err := io.ReadAll(resp.Body)
		if err == nil {
			log.Error(string(data))
		}
		return 0, errors.New(fmt.Sprintf("POST contacts returned non-200 status code: %d", resp.StatusCode))
	}

	decoder := json.NewDecoder(resp.Body)
	var contactPayload ContactPayload
	err = decoder.Decode(&contactPayload)
	if err != nil {
		return 0, err
	}

	log.Debug(contactPayload)
	return contactPayload.Payload.Contact.ID, nil
}

func (api *ChatwootAPI) ContactIDForMxid(userID mid.UserID) (ContactID, error) {
	req, err := http.NewRequest(http.MethodGet, api.MakeUri("contacts/search"), nil)
	if err != nil {
		log.Error(err)
		return 0, err
	}

	q := req.URL.Query()
	q.Add("q", userID.String())
	req.URL.RawQuery = q.Encode()

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return 0, err
	}
	if resp.StatusCode != 200 {
		return 0, errors.New(fmt.Sprintf("GET contacts/search returned non-200 status code: %d", resp.StatusCode))
	}

	decoder := json.NewDecoder(resp.Body)
	var contactsPayload ContactsPayload
	err = decoder.Decode(&contactsPayload)
	if err != nil {
		return 0, err
	}
	for _, contact := range contactsPayload.Payload {
		if contact.Identifier == userID.String() {
			return contact.ID, nil
		}
	}

	return 0, errors.New(fmt.Sprintf("Couldn't find user with user ID %s!", userID))
}

func (api *ChatwootAPI) GetChatwootConversation(conversationID ConversationID) (*Conversation, error) {
	req, err := http.NewRequest(http.MethodGet, api.MakeUri(fmt.Sprintf("conversations/%d", conversationID)), nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("GET conversations/%d returned non-200 status code: %d", conversationID, resp.StatusCode))
	}

	decoder := json.NewDecoder(resp.Body)
	var conversation Conversation
	err = decoder.Decode(&conversation)
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (api *ChatwootAPI) CreateConversation(sourceID string, contactID ContactID, additionalAttrs map[string]string) (*Conversation, error) {
	values := map[string]any{
		"source_id":             sourceID,
		"inbox_id":              api.InboxID,
		"contact_id":            contactID,
		"status":                "open",
		"additional_attributes": additionalAttrs,
	}
	jsonValue, _ := json.Marshal(values)
	req, err := http.NewRequest(http.MethodPost, api.MakeUri("conversations"), bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error(err)
		return nil, err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			content = []byte{}
		}
		return nil, errors.New(fmt.Sprintf("POST conversations returned non-200 status code: %d: %s", resp.StatusCode, string(content)))
	}

	decoder := json.NewDecoder(resp.Body)
	var conversation Conversation
	err = decoder.Decode(&conversation)
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (api *ChatwootAPI) AddConversationLabel(conversationID ConversationID, labels []string) error {
	jsonValue, err := json.Marshal(map[string]any{"labels": labels})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, api.MakeUri(fmt.Sprintf("conversations/%d/labels", conversationID)), bytes.NewBuffer(jsonValue))
	if err != nil {
		return err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			content = []byte{}
		}
		return errors.New(fmt.Sprintf("POST conversations returned non-200 status code: %d: %s", resp.StatusCode, string(content)))
	}
	return nil
}

func (api *ChatwootAPI) SetConversationCustomAttributes(conversationID ConversationID, customAttrs map[string]string) error {
	jsonValue, _ := json.Marshal(map[string]any{
		"custom_attributes": customAttrs,
	})
	req, err := http.NewRequest(http.MethodPost, api.MakeUri(fmt.Sprintf("conversations/%d/custom_attributes", conversationID)), bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error(err)
		return err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("POST conversations/%d/custom_attributes returned non-200 status code: %d", conversationID, resp.StatusCode))
	}
	return nil
}

func (api *ChatwootAPI) doSendTextMessage(conversationID ConversationID, jsonValues map[string]any) (*Message, error) {
	jsonValue, _ := json.Marshal(jsonValues)
	req, err := http.NewRequest(http.MethodPost, api.MakeUri(fmt.Sprintf("conversations/%d/messages", conversationID)), bytes.NewBuffer(jsonValue))
	if err != nil {
		log.Error(err)
		return nil, err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			content = []byte{}
		}
		return nil, errors.New(fmt.Sprintf("POST conversations/%d/messages returned non-200 status code: %d: %s", conversationID, resp.StatusCode, string(content)))
	}

	decoder := json.NewDecoder(resp.Body)
	var message Message
	err = decoder.Decode(&message)
	if err != nil {
		return nil, err
	}
	return &message, nil

}

func (api *ChatwootAPI) SendTextMessage(conversationID ConversationID, content string, messageType MessageType) (*Message, error) {
	values := map[string]any{"content": content, "message_type": MessageTypeString(messageType), "private": false}
	return api.doSendTextMessage(conversationID, values)
}

func (api *ChatwootAPI) SendPrivateMessage(conversationID ConversationID, content string) (*Message, error) {
	values := map[string]any{"content": content, "message_type": MessageTypeString(OutgoingMessage), "private": true}
	return api.doSendTextMessage(conversationID, values)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func (api *ChatwootAPI) SendAttachmentMessage(conversationID ConversationID, filename string, mimeType string, fileData io.Reader, messageType MessageType) (*Message, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	contentFieldWriter, err := bodyWriter.CreateFormField("content")
	if err != nil {
		log.Error(err)
		return nil, err
	}
	contentFieldWriter.Write([]byte{})

	privateFieldWriter, err := bodyWriter.CreateFormField("private")
	if err != nil {
		log.Error(err)
		return nil, err
	}
	privateFieldWriter.Write([]byte("false"))

	messageTypeFieldWriter, err := bodyWriter.CreateFormField("message_type")
	if err != nil {
		log.Error(err)
		return nil, err
	}
	messageTypeFieldWriter.Write([]byte(MessageTypeString(messageType)))

	h := make(textproto.MIMEHeader)
	h.Set(
		"Content-Disposition",
		fmt.Sprintf(`form-data; name="attachments[]"; filename="%s"`, quoteEscaper.Replace(filename)))
	if mimeType != "" {
		h.Set("Content-Type", mimeType)
	} else {
		h.Set("Content-Type", "application/octet-stream")
	}
	fileWriter, err := bodyWriter.CreatePart(h)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// Copy the file data into the form.
	io.Copy(fileWriter, fileData)

	bodyWriter.Close()

	req, err := http.NewRequest(http.MethodPost, api.MakeUri(fmt.Sprintf("conversations/%d/messages", conversationID)), bodyBuf)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	req.Header.Add("API_ACCESS_TOKEN", api.AccessToken)
	req.Header.Set("Content-Type", bodyWriter.FormDataContentType())

	resp, err := api.Client.Do(req)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		content, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			content = []byte{}
		}
		return nil, errors.New(fmt.Sprintf("POST conversations/%d/messages returned non-200 status code: %d: %s", conversationID, resp.StatusCode, string(content)))
	}

	decoder := json.NewDecoder(resp.Body)
	var message Message
	err = decoder.Decode(&message)
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func (api *ChatwootAPI) DownloadAttachment(url string) (*[]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, errors.New(fmt.Sprintf("GET attachment returned non-200 status code: %d", resp.StatusCode))
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	return &data, err
}

func (api *ChatwootAPI) DeleteMessage(conversationID ConversationID, messageID MessageID) error {
	req, err := http.NewRequest(http.MethodDelete, api.MakeUri(fmt.Sprintf("conversations/%d/messages/%d", conversationID, messageID)), nil)
	if err != nil {
		log.Error(err)
		return err
	}

	resp, err := api.DoRequest(req)
	if err != nil {
		log.Error(err)
		return err
	}
	if resp.StatusCode != 200 {
		return errors.New(fmt.Sprintf("GET attachment returned non-200 status code: %d", resp.StatusCode))
	}

	return nil
}
