package main

import (
	"os"
	"strings"

	"github.com/beeper/chatwoot/chatwootapi"
	log "github.com/sirupsen/logrus"
)

type Configuration struct {
	// Authentication settings
	Homeserver   string
	Username     string
	PasswordFile string

	// Chatwoot Authentication
	ChatwootBaseUrl         string
	ChatwootAccessTokenFile string
	ChatwootAccountID       chatwootapi.AccountID
	ChatwootInboxID         chatwootapi.InboxID

	// Database settings
	DBConnectionString string

	// Bot settings
	AllowMessagesFromUsersOnOtherHomeservers bool
	CanonicalDMPrefix                        string
	BridgeIfMembersLessThan                  int
	RenderMarkdown                           bool

	// Webhook listener settings
	ListenPort int
}

func (c *Configuration) GetPassword() (string, error) {
	log.Debug("Reading password from ", c.PasswordFile)
	buf, err := os.ReadFile(c.PasswordFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func (c *Configuration) GetChatwootAccessToken() (string, error) {
	log.Debug("Reading access token from ", c.ChatwootAccessTokenFile)
	buf, err := os.ReadFile(c.ChatwootAccessTokenFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}
