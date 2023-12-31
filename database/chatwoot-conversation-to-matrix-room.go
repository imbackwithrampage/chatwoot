package database

import (
	"context"
	"database/sql"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

func (store *Database) GetChatwootConversationIDFromMatrixRoom(ctx context.Context, roomID id.RoomID) (int, error) {
	row := store.DB.QueryRowContext(ctx, `
		SELECT chatwoot_conversation_id
		  FROM chatwoot_conversation_to_matrix_room
		 WHERE matrix_room_id = $1`, roomID)
	var chatwootConversationId int
	if err := row.Scan(&chatwootConversationId); err != nil {
		return -1, err
	}
	return chatwootConversationId, nil
}

func (store *Database) GetMatrixRoomFromChatwootConversation(ctx context.Context, conversationID int) (id.RoomID, id.EventID, error) {
	row := store.DB.QueryRowContext(ctx, `
		SELECT matrix_room_id, most_recent_event_id
		  FROM chatwoot_conversation_to_matrix_room
		 WHERE chatwoot_conversation_id = $1`, conversationID)
	var roomID id.RoomID
	var mostRecentEventIdStr sql.NullString
	if err := row.Scan(&roomID, &mostRecentEventIdStr); err != nil {
		return "", "", err
	}
	if mostRecentEventIdStr.Valid {
		return roomID, id.EventID(mostRecentEventIdStr.String), nil
	} else {
		return roomID, id.EventID(""), nil
	}
}

func (store *Database) UpdateMostRecentEventIdForRoom(ctx context.Context, roomID id.RoomID, mostRecentEventID id.EventID) error {
	log := zerolog.Ctx(ctx).With().
		Str("component", "update_most_recent_event_id_for_room").
		Str("most_recent_event_id", mostRecentEventID.String()).
		Logger()
	ctx = log.WithContext(ctx)

	log.Debug().Msg("setting most recent event ID for room")
	tx, err := store.DB.Begin()
	if err != nil {
		tx.Rollback()
		return err
	}

	update := `
		UPDATE chatwoot_conversation_to_matrix_room
		SET most_recent_event_id = $2
		WHERE matrix_room_id = $1
	`
	if _, err := tx.ExecContext(ctx, update, roomID, mostRecentEventID); err != nil {
		tx.Rollback()
		log.Err(err).Msg("failed to update most recent event ID")
		return err
	}

	return tx.Commit()
}

func (store *Database) UpdateConversationIdForRoom(ctx context.Context, roomID id.RoomID, conversationID int) error {
	log := zerolog.Ctx(ctx).With().
		Str("component", "update_conversation_id_for_room").
		Int("conversation_id", conversationID).
		Logger()
	ctx = log.WithContext(ctx)

	log.Debug().Msg("setting conversation ID for room")
	tx, err := store.DB.Begin()
	if err != nil {
		tx.Rollback()
		return err
	}

	upsert := `
		INSERT INTO chatwoot_conversation_to_matrix_room (matrix_room_id, chatwoot_conversation_id)
			VALUES ($1, $2)
		ON CONFLICT (matrix_room_id) DO UPDATE
			SET chatwoot_conversation_id = $2
	`
	if _, err := tx.ExecContext(ctx, upsert, roomID, conversationID); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}
