package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	_ "github.com/jackc/pgx/v4/stdlib"
	"github.com/rs/zerolog"
	globallog "github.com/rs/zerolog/log"
	"gopkg.in/yaml.v2"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/util/dbutil"

	"github.com/beeper/chatwoot/chatwootapi"
	"github.com/beeper/chatwoot/database"
)

var client *mautrix.Client
var configuration Configuration
var stateStore *database.Database

var chatwootApi *chatwootapi.ChatwootAPI
var botHomeserver string

var roomSendlocks map[id.RoomID]*sync.Mutex

var VERSION = "0.2.1"

func main() {
	// Arg parsing
	configPath := flag.String("config", "./config.yaml", "config file location")
	flag.Parse()

	// Load configuration
	globallog.Info().Str("config_path", *configPath).Msg("Reading config")
	configYaml, err := os.ReadFile(*configPath)
	if err != nil {
		globallog.Fatal().Err(err).Str("config_path", *configPath).Msg("Failed reading the config")
	}

	// Default configuration values
	configuration = Configuration{
		AllowMessagesFromUsersOnOtherHomeservers: false,
		ChatwootBaseUrl:                          "https://app.chatwoot.com/",
		ListenPort:                               8080,
		BridgeIfMembersLessThan:                  -1,
		RenderMarkdown:                           false,
	}

	err = yaml.Unmarshal(configYaml, &configuration)
	if err != nil {
		globallog.Fatal().Err(err).Msg("Failed to parse configuration YAML")
	}

	// Setup logging
	log, err := configuration.Logging.Compile()
	if err != nil {
		globallog.Fatal().Err(err).Msg("Failed to compile logging configuration")
	}

	log.Info().Interface("configuration", configuration).Msg("Config loaded")
	botHomeserver = configuration.Username.Homeserver()

	log.Info().Msg("Chatwoot service starting...")

	// Open the chatwoot database
	db, err := dbutil.NewFromConfig("chatwoot", configuration.Database, dbutil.ZeroLogger(*log))
	if err != nil {
		log.Fatal().Err(err).Msg("couldn't open database")
	}

	// Make sure to exit cleanly
	c := make(chan os.Signal, 1)
	signal.Notify(c,
		syscall.SIGABRT,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)
	go func() {
		for range c { // when the process is killed
			log.Info().Msg("Cleaning up")
			db.RawDB.Close()
			os.Exit(0)
		}
	}()

	// Initialize the send lock map
	roomSendlocks = map[id.RoomID]*sync.Mutex{}

	stateStore = database.NewDatabase(db)
	if err := stateStore.DB.Upgrade(); err != nil {
		log.Fatal().Err(err).Msg("failed to upgrade the Chatwoot database")
	}

	client, err = mautrix.NewClient(configuration.Homeserver, "", "")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create matrix client")
	}
	client.Log = *log

	accessToken, err := configuration.GetChatwootAccessToken(log)
	if err != nil {
		log.Fatal().Err(err).Str("access_token_file", configuration.ChatwootAccessTokenFile).Msg("Could not read access token")
	}
	chatwootApi = chatwootapi.CreateChatwootAPI(
		configuration.ChatwootBaseUrl,
		configuration.ChatwootAccountID,
		configuration.ChatwootInboxID,
		accessToken,
	)

	getLogger := func(evt *event.Event) zerolog.Logger {
		return log.With().
			Str("event_type", evt.Type.String()).
			Str("sender", evt.Sender.String()).
			Str("room_id", string(evt.RoomID)).
			Str("event_id", string(evt.ID)).
			Logger()
	}

	cryptoHelper, err := cryptohelper.NewCryptoHelper(client, []byte("chatwoot_cryptostore_key"), db)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create crypto helper")
	}
	password, err := configuration.GetPassword(log)
	if err != nil {
		log.Fatal().Err(err).Str("password_file", configuration.PasswordFile).Msg("Could not read password from ")
	}
	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type:       mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: configuration.Username.String()},
		Password:   password,
	}
	cryptoHelper.DBAccountID = configuration.Username.String()
	cryptoHelper.DecryptErrorCallback = func(evt *event.Event, err error) {
		log := getLogger(evt)
		ctx := log.WithContext(context.TODO())
		log.Error().Err(err).Msg("Failed to decrypt message")

		stateStore.UpdateMostRecentEventIdForRoom(ctx, evt.RoomID, evt.ID)
		if !VerifyFromAuthorizedUser(evt.Sender) {
			return
		}

		conversationID, err := stateStore.GetChatwootConversationIDFromMatrixRoom(ctx, evt.RoomID)

		if err != nil {
			log.Warn().Msg("no Chatwoot conversation associated with this room")
			return
		}

		DoRetry(ctx, fmt.Sprintf("send private error message to %d for %+v", conversationID, err), func(ctx context.Context) (*chatwootapi.Message, error) {
			return chatwootApi.SendPrivateMessage(
				ctx,
				conversationID,
				fmt.Sprintf("**Failed to decrypt Matrix event (%s). You probably missed a message!**\n\nError: %+v", evt.ID, err))
		})
	}

	err = cryptoHelper.Init()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize crypto helper")
	}
	cryptoHelper.Machine().AllowKeyShare = AllowKeyShare
	client.Crypto = cryptoHelper

	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.EventMessage, func(source mautrix.EventSource, evt *event.Event) {
		log := getLogger(evt)
		ctx := log.WithContext(context.TODO())

		stateStore.UpdateMostRecentEventIdForRoom(ctx, evt.RoomID, evt.ID)
		if VerifyFromAuthorizedUser(evt.Sender) {
			go HandleBeeperClientInfo(ctx, evt)
			go HandleMessage(ctx, source, evt)
		}
	})
	syncer.OnEventType(event.EventReaction, func(source mautrix.EventSource, evt *event.Event) {
		log := getLogger(evt)
		ctx := log.WithContext(context.TODO())

		stateStore.UpdateMostRecentEventIdForRoom(ctx, evt.RoomID, evt.ID)
		if VerifyFromAuthorizedUser(evt.Sender) {
			go HandleBeeperClientInfo(ctx, evt)
			go HandleReaction(ctx, source, evt)
		}
	})
	syncer.OnEventType(event.EventRedaction, func(source mautrix.EventSource, evt *event.Event) {
		log := getLogger(evt)
		ctx := log.WithContext(context.TODO())

		stateStore.UpdateMostRecentEventIdForRoom(ctx, evt.RoomID, evt.ID)
		if VerifyFromAuthorizedUser(evt.Sender) {
			go HandleBeeperClientInfo(ctx, evt)
			go HandleRedaction(ctx, source, evt)
		}
	})

	syncCtx, cancelSync := context.WithCancel(context.Background())
	var syncStopWait sync.WaitGroup
	syncStopWait.Add(1)

	// Start the sync loop
	go func() {
		log.Debug().Msg("starting sync loop")
		err = client.SyncWithContext(syncCtx)
		defer syncStopWait.Done()
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal().Err(err).Msg("Sync error")
		}
	}()

	// Make sure that there are conversations for all of the rooms that the bot
	// is in.
	go func() {
		joined, err := client.JoinedRooms()
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to get joined rooms")
		}

		log := log.With().Str("component", "conversation_creation_backfill").Logger()
		ctx := log.WithContext(context.Background())
	joinedRoomsLoop:
		for _, roomID := range joined.JoinedRooms {
			_, err := stateStore.GetChatwootConversationIDFromMatrixRoom(ctx, roomID)
			if err == nil {
				// This room already has a Chatwoot conversation associtaed with it.
				continue
			}

			log := log.With().Str("room_id", roomID.String()).Logger()

			log.Info().Msg("Creating conversation for room")

			messages, err := client.Messages(roomID, "", "", mautrix.DirectionBackward, nil, 50)
			if err != nil {
				log.Err(err).Msg("Failed to get messages for room")
				continue
			}

			// Iterating through the messages will go in reverse order, so find
			// the most recent message event and use that to create the
			// conversation.
			for _, evt := range messages.Chunk {
				if evt.Type != event.EventMessage && evt.Type != event.EventEncrypted {
					continue
				}

				chatwootConversationID, err := GetOrCreateChatwootConversation(ctx, roomID, evt)
				if err != nil {
					log.Err(err).Msg("failed to get or create Chatwoot conversation")
					continue
				}

				log.Info().
					Int("chatwoot_conversation_id", chatwootConversationID).
					Msg("created Chatwoot conversation")
				continue joinedRoomsLoop
			}

			log.Warn().Msg("No messages found for room suitable for creating conversation")
		}

		log.Info().Msg("finished creating conversations for rooms that don't have a conversation yet")
	}()

	// Listen to the webhook
	http.HandleFunc("/", HandleWebhook)
	http.HandleFunc("/webhook", HandleWebhook)
	log.Info().Int("listen_port", configuration.ListenPort).Msg("starting webhook listener")
	err = http.ListenAndServe(fmt.Sprintf(":%d", configuration.ListenPort), nil)
	if err != nil {
		log.Error().Err(err).Msg("creating the webhook listener failed")
	}

	cancelSync()
	syncStopWait.Wait()
	err = cryptoHelper.Close()
	if err != nil {
		log.Error().Err(err).Msg("Error closing database")
	}
}

func AllowKeyShare(ctx context.Context, device *id.Device, info event.RequestedKeyInfo) *crypto.KeyShareRejection {
	log := *zerolog.Ctx(ctx)

	// Always allow key requests from @help
	if device.UserID == configuration.Username {
		log.Info().Msg("allowing key share because it's another login of the help account")
		return nil
	}

	conversationID, err := stateStore.GetChatwootConversationIDFromMatrixRoom(ctx, info.RoomID)
	if err != nil {
		log.Info().Msg("no Chatwoot conversation found")
		return &crypto.KeyShareRejectNoResponse
	}
	log = log.With().Int("conversation_id", conversationID).Logger()

	conversation, err := chatwootApi.GetChatwootConversation(conversationID)
	if err != nil {
		log.Info().Err(err).Msg("couldn't get Chatwoot conversation")
		return &crypto.KeyShareRejectNoResponse
	}
	log = log.With().Int("sender_identifier", conversation.Meta.Sender.ID).Logger()

	// This is the user that we expected for this Chatwoot conversation.
	if conversation.Meta.Sender.Identifier == device.UserID.String() {
		log.Info().Msg("Chatwoot conversation contact identifier matched device that was requesting the key. Allowing.")
		return nil
	} else {
		log.Info().Msg("rejecting key share request")
		return &crypto.KeyShareRejectNoResponse
	}
}

func VerifyFromAuthorizedUser(sender id.UserID) bool {
	if configuration.AllowMessagesFromUsersOnOtherHomeservers {
		return true
	}
	_, homeserver, err := sender.Parse()
	if err != nil {
		return false
	}

	return botHomeserver == homeserver
}
