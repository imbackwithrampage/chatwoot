package store

import (
	"database/sql"

	"github.com/beeper/chatwoot/chatwootapi"
	log "github.com/sirupsen/logrus"
	mid "maunium.net/go/mautrix/id"
)

func (store *StateStore) SetChatwootMessageIdForMatrixEvent(eventID mid.EventID, messageID chatwootapi.MessageID) error {
	log.Debug("Inserting row into chatwoot_message_to_matrix_event. ", eventID, " / ", messageID)
	tx, err := store.DB.Begin()
	if err != nil {
		tx.Rollback()
		return err
	}

	insert := `
		INSERT INTO chatwoot_message_to_matrix_event (matrix_event_id, chatwoot_message_id)
			VALUES ($1, $2)
	`
	if _, err := tx.Exec(insert, eventID, messageID); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (store *StateStore) GetMatrixEventIDsForChatwootMessage(messageID chatwootapi.MessageID) []mid.EventID {
	log.Debug("Getting matrix event IDs for chatwoot message ID ", messageID)
	rows, err := store.DB.Query(`
		SELECT matrix_event_id
		  FROM chatwoot_message_to_matrix_event
		 WHERE chatwoot_message_id = $1`, messageID)
	eventIDs := make([]mid.EventID, 0)
	if err != nil {
		log.Error(err)
		return eventIDs
	}
	defer rows.Close()

	var eventID mid.EventID
	for rows.Next() {
		if err := rows.Scan(&eventID); err == nil {
			eventIDs = append(eventIDs, eventID)
		}
	}
	return eventIDs
}

func (store *StateStore) GetChatwootMessageIDsForMatrixEventID(matrixEventID mid.EventID) (messageIDs []chatwootapi.MessageID, err error) {
	log.Debug("Getting chatwoot message IDs for matrix event ID ", matrixEventID)
	var rows *sql.Rows
	rows, err = store.DB.Query(`
		SELECT chatwoot_message_id
		  FROM chatwoot_message_to_matrix_event
		 WHERE matrix_event_id = $1`, matrixEventID)
	if err != nil {
		log.Error(err)
		return
	}
	defer rows.Close()

	var messageID chatwootapi.MessageID
	for rows.Next() {
		if err := rows.Scan(&messageID); err == nil {
			messageIDs = append(messageIDs, messageID)
		}
	}
	log.Debugf("Found %v chatwoot message IDs for matrix event ID %s", messageIDs, matrixEventID)
	return messageIDs, rows.Err()
}
