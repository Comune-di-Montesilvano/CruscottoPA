package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Chat informale tra colleghi: conversazioni 1:1, messaggi conservati 30
// giorni. Il testo, l'IP e il PC dei messaggi non devono mai finire nei log.

// ChatAudienceID: content_id della chat in content_audience (un solo «contenuto»).
const ChatAudienceID int64 = 1

// ChatMessage è un messaggio. Hidden = scritto da un mittente bloccato dal
// destinatario: lo vede solo chi l'ha scritto. IP e PC servono solo in caso
// di abuso, non vanno mai mostrati.
type ChatMessage struct {
	ID, ConversationID int64
	Sender, Body       string
	Verified, Hidden   bool
	IP, PC             string
	CreatedAt          time.Time
}

// ChatConversation vista da uno dei due partecipanti: Other è l'altro.
type ChatConversation struct {
	ID     int64
	Other  string
	LastAt time.Time
	Unread int
}

func chatUser(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

func chatPair(a, b string) (string, string) {
	a, b = chatUser(a), chatUser(b)
	if b < a {
		return b, a
	}
	return a, b
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ChatConversationID: conversazione tra a e b. Con create la crea se manca
// (created = appena creata); senza, ErrNotFound se non c'è.
func (db *DB) ChatConversationID(a, b string, create bool, now time.Time) (id int64, created bool, err error) {
	ua, ub := chatPair(a, b)
	err = db.QueryRow(`SELECT id FROM chat_conversations WHERE user_a = ? AND user_b = ?`, ua, ub).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if !create {
		return 0, false, ErrNotFound
	}
	t := formatTime(now)
	res, err := db.Exec(`INSERT INTO chat_conversations (user_a, user_b, created_at, last_at) VALUES (?, ?, ?, ?)
ON CONFLICT(user_a, user_b) DO NOTHING`, ua, ub, t, t)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 { // creata da una richiesta concorrente
		err = db.QueryRow(`SELECT id FROM chat_conversations WHERE user_a = ? AND user_b = ?`, ua, ub).Scan(&id)
		return id, false, err
	}
	id, err = res.LastInsertId()
	return id, true, err
}

// ChatConversationOther: l'altro partecipante; ErrNotFound se user non partecipa.
func (db *DB) ChatConversationOther(convID int64, user string) (string, error) {
	u := chatUser(user)
	var a, b string
	err := db.QueryRow(`SELECT user_a, user_b FROM chat_conversations WHERE id = ?`, convID).Scan(&a, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	switch u {
	case a:
		return b, nil
	case b:
		return a, nil
	}
	return "", ErrNotFound
}

// AddChatMessage salva il messaggio e aggiorna la conversazione; un messaggio
// non nascosto conta come non letto per il destinatario.
func (db *DB) AddChatMessage(m ChatMessage) (int64, error) {
	sender := chatUser(m.Sender)
	var id int64
	err := db.inTx(func(tx *sql.Tx) error {
		var a, b string
		if err := tx.QueryRow(`SELECT user_a, user_b FROM chat_conversations WHERE id = ?`, m.ConversationID).Scan(&a, &b); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		to := a
		switch sender {
		case a:
			to = b
		case b:
		default:
			return ErrNotFound
		}
		t := formatTime(m.CreatedAt)
		res, err := tx.Exec(`INSERT INTO chat_messages (conversation_id, sender, body, verified, hidden, ip, pc, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, m.ConversationID, sender, m.Body, boolInt(m.Verified), boolInt(m.Hidden), m.IP, m.PC, t)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE chat_conversations SET last_at = ? WHERE id = ? AND last_at < ?`, t, m.ConversationID, t); err != nil {
			return err
		}
		if m.Hidden {
			return nil
		}
		_, err = tx.Exec(`INSERT INTO chat_unread (username, conversation_id, count) VALUES (?, ?, 1)
ON CONFLICT(username, conversation_id) DO UPDATE SET count = count + 1`, to, m.ConversationID)
		return err
	})
	return id, err
}

// visibleTo: condizione SQL sui messaggi (alias m) che user può vedere.
const chatVisibleTo = `(m.hidden = 0 OR m.sender = ?)`

// ListChatConversations: conversazioni di user con almeno un messaggio che
// può vedere, dalla più recente.
func (db *DB) ListChatConversations(user string, limit int) ([]ChatConversation, error) {
	u := chatUser(user)
	rows, err := db.Query(`
SELECT c.id, CASE WHEN c.user_a = ? THEN c.user_b ELSE c.user_a END,
	(SELECT MAX(m.created_at) FROM chat_messages m WHERE m.conversation_id = c.id AND `+chatVisibleTo+`),
	COALESCE((SELECT count FROM chat_unread r WHERE r.username = ? AND r.conversation_id = c.id), 0)
FROM chat_conversations c
WHERE (c.user_a = ? OR c.user_b = ?)
	AND EXISTS (SELECT 1 FROM chat_messages m WHERE m.conversation_id = c.id AND `+chatVisibleTo+`)
ORDER BY 3 DESC, c.id DESC LIMIT ?`, u, u, u, u, u, u, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatConversation{}
	for rows.Next() {
		var c ChatConversation
		var last string
		if err := rows.Scan(&c.ID, &c.Other, &last, &c.Unread); err != nil {
			return nil, err
		}
		if c.LastAt, err = parseTime(last); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatMessages: fino a limit messaggi che viewer può vedere, prima di before
// (0 = i più recenti), in ordine crescente. ErrNotFound se viewer non partecipa.
func (db *DB) ChatMessages(convID int64, viewer string, before int64, limit int) ([]ChatMessage, error) {
	v := chatUser(viewer)
	if _, err := db.ChatConversationOther(convID, v); err != nil {
		return nil, err
	}
	if before <= 0 {
		before = 1<<63 - 1
	}
	rows, err := db.Query(`SELECT id, conversation_id, sender, body, verified, hidden, ip, pc, created_at FROM (
	SELECT m.* FROM chat_messages m WHERE m.conversation_id = ? AND m.id < ? AND `+chatVisibleTo+`
	ORDER BY m.id DESC LIMIT ?) ORDER BY id`, convID, before, v, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var verified, hidden int
		var at string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Sender, &m.Body, &verified, &hidden, &m.IP, &m.PC, &at); err != nil {
			return nil, err
		}
		m.Verified, m.Hidden = verified == 1, hidden == 1
		if m.CreatedAt, err = parseTime(at); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) MarkChatRead(user string, convID int64) error {
	_, err := db.Exec(`DELETE FROM chat_unread WHERE username = ? AND conversation_id = ?`, chatUser(user), convID)
	return err
}

func (db *DB) ChatUnreadTotal(user string) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COALESCE(SUM(count), 0) FROM chat_unread WHERE username = ?`, chatUser(user)).Scan(&n)
	return n, err
}

// SetChatBlock: blocker non riceve più i messaggi di blocked (on=false sblocca).
func (db *DB) SetChatBlock(blocker, blocked string, on bool, now time.Time) error {
	if !on {
		_, err := db.Exec(`DELETE FROM chat_blocks WHERE blocker = ? AND blocked = ?`, chatUser(blocker), chatUser(blocked))
		return err
	}
	_, err := db.Exec(`INSERT INTO chat_blocks (blocker, blocked, created_at) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		chatUser(blocker), chatUser(blocked), formatTime(now))
	return err
}

func (db *DB) ChatBlocked(blocker, blocked string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM chat_blocks WHERE blocker = ? AND blocked = ?`, chatUser(blocker), chatUser(blocked)).Scan(&n)
	return n > 0, err
}

func (db *DB) ChatBlockedList(blocker string) ([]string, error) {
	return db.stringList(`SELECT blocked FROM chat_blocks WHERE blocker = ? ORDER BY blocked`, chatUser(blocker))
}

// CleanupChat: via i messaggi più vecchi di before e le conversazioni rimaste
// vuote; i non letti si ricalcolano sui messaggi rimasti.
func (db *DB) CleanupChat(before time.Time) error {
	return db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM chat_messages WHERE created_at < ?`, formatTime(before)); err != nil {
			return err
		}
		// Senza messaggi: scaduti, oppure mai scritti (creazione fallita a metà).
		// Le conversazioni recenti restano: un invio potrebbe essere in corso.
		if _, err := tx.Exec(`DELETE FROM chat_conversations WHERE created_at < ?
AND NOT EXISTS (SELECT 1 FROM chat_messages m WHERE m.conversation_id = chat_conversations.id)`, formatTime(before)); err != nil {
			return err
		}
		// Non letti: al massimo i messaggi visibili rimasti dell'altro partecipante.
		if _, err := tx.Exec(`UPDATE chat_unread SET count = MIN(count, (SELECT COUNT(*) FROM chat_messages m
WHERE m.conversation_id = chat_unread.conversation_id AND m.sender <> chat_unread.username AND m.hidden = 0))`); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM chat_unread WHERE count <= 0`)
		return err
	})
}

// ChatUsers: chi compare in conversazioni o blocchi (per la pulizia degli
// utenti che AD non conosce più).
func (db *DB) ChatUsers() ([]string, error) {
	return db.stringList(`SELECT user_a FROM chat_conversations UNION SELECT user_b FROM chat_conversations
UNION SELECT blocker FROM chat_blocks UNION SELECT blocked FROM chat_blocks ORDER BY 1`)
}

// DeleteChatUser: conversazioni (con messaggi e non letti) e blocchi dell'utente.
func (db *DB) DeleteChatUser(user string) error {
	u := chatUser(user)
	return db.inTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM chat_conversations WHERE user_a = ? OR user_b = ?`, u, u); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM chat_unread WHERE username = ?`, u); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM chat_today WHERE username = ?`, u); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM chat_settings WHERE username = ?`, u); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM chat_blocks WHERE blocker = ? OR blocked = ?`, u, u)
		return err
	})
}

// TouchChatToday: user ha usato la chat nel giorno day (YYYY-MM-DD di Roma).
func (db *DB) TouchChatToday(user, day string) error {
	_, err := db.Exec(`INSERT INTO chat_today (username, day) VALUES (?, ?) ON CONFLICT DO NOTHING`, chatUser(user), day)
	return err
}

// ChatToday: tra users, chi ha usato la chat nel giorno day e non nasconde lo stato.
func (db *DB) ChatToday(users []string, day string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(users) == 0 {
		return out, nil
	}
	args := []any{day}
	ph := make([]string, len(users))
	for i, u := range users {
		ph[i] = "?"
		args = append(args, chatUser(u))
	}
	rows, err := db.Query(`SELECT t.username FROM chat_today t LEFT JOIN chat_settings s ON s.username = t.username
WHERE t.day = ? AND COALESCE(s.hide_status, 0) = 0 AND t.username IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out[u] = true
	}
	return out, rows.Err()
}

func (db *DB) SetChatHideStatus(user string, hide bool) error {
	_, err := db.Exec(`INSERT INTO chat_settings (username, hide_status) VALUES (?, ?)
ON CONFLICT (username) DO UPDATE SET hide_status = excluded.hide_status`, chatUser(user), hide)
	return err
}

func (db *DB) ChatHideStatus(user string) (bool, error) {
	var h bool
	err := db.QueryRow(`SELECT hide_status FROM chat_settings WHERE username = ?`, chatUser(user)).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return h, err
}

// ChatStatusEnabled: interruttore generale di «Oggi in chat» (Admin → Chat).
func (db *DB) ChatStatusEnabled() (bool, error) {
	var on bool
	err := db.QueryRow(`SELECT status_enabled FROM chat_options WHERE id = 1`).Scan(&on)
	return on, err
}

func (db *DB) SetChatStatusEnabled(on bool) error {
	_, err := db.Exec(`UPDATE chat_options SET status_enabled = ? WHERE id = 1`, on)
	return err
}

// CleanupChatToday: restano solo le righe del giorno today.
func (db *DB) CleanupChatToday(today string) error {
	_, err := db.Exec(`DELETE FROM chat_today WHERE day < ?`, today)
	return err
}

func (db *DB) stringList(q string, args ...any) ([]string, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ChatStats: numeri aggregati per l'admin, mai per persona.
type ChatStats struct {
	Messages7, Messages30 int // messaggi scritti (anche verso chi ha bloccato)
	Senders7, Senders30   int // colleghi diversi che hanno scritto
	Conversations30       int // conversazioni con almeno un messaggio
	Unverified30          int // messaggi scritti senza accesso
	Blocks                int // blocchi attivi
}

func (db *DB) ChatStats(now time.Time) (ChatStats, error) {
	var st ChatStats
	d7, d30 := formatTime(now.AddDate(0, 0, -7)), formatTime(now.AddDate(0, 0, -30))
	err := db.QueryRow(`SELECT
	(SELECT COUNT(*) FROM chat_messages WHERE created_at >= ?1),
	(SELECT COUNT(*) FROM chat_messages WHERE created_at >= ?2),
	(SELECT COUNT(DISTINCT sender) FROM chat_messages WHERE created_at >= ?1),
	(SELECT COUNT(DISTINCT sender) FROM chat_messages WHERE created_at >= ?2),
	(SELECT COUNT(DISTINCT conversation_id) FROM chat_messages WHERE created_at >= ?2),
	(SELECT COUNT(*) FROM chat_messages WHERE created_at >= ?2 AND verified = 0),
	(SELECT COUNT(*) FROM chat_blocks)`, d7, d30).Scan(&st.Messages7, &st.Messages30, &st.Senders7, &st.Senders30,
		&st.Conversations30, &st.Unverified30, &st.Blocks)
	return st, err
}
