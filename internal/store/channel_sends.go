package store

import "context"

// 42: kanal başına son gönderim sonucu (canlı topoloji kanal kartı).
// Bellekte değil DB'de: panel yeniden başlayınca kaybolmaz, olay ayrıntısındaki
// notify kayıtlarıyla (karar kartı) aynı kaynağa dayanır.
func init() {
	RegisterMigration(42, `
CREATE TABLE channel_last_send (
	channel_id INTEGER PRIMARY KEY REFERENCES notifications(id) ON DELETE CASCADE,
	at INTEGER NOT NULL,
	ok INTEGER NOT NULL DEFAULT 0
);`)
}

// ChannelSend bir kanala yapılan son gönderim.
type ChannelSend struct {
	At int64 `json:"at"` // unix sn
	OK bool  `json:"ok"`
	// Error son gönderim başarısızsa hata metni (adressiz; migration 50).
	Error string `json:"error,omitempty"`
}

// SetChannelSend kanalın son gönderim sonucunu yazar (üzerine yazar) ve saatlik
// sayacı artırır; errText boş = başarılı.
func (s *Store) SetChannelSend(ctx context.Context, channelID, at int64, errText string) error {
	v, failed := 1, 0
	if errText != "" {
		v, failed = 0, 1
	}
	hour := at - at%3600
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO channel_last_send (channel_id, at, ok, error) VALUES (?, ?, ?, ?)
			ON CONFLICT (channel_id) DO UPDATE SET at = excluded.at, ok = excluded.ok, error = excluded.error`,
			channelID, at, v, errText); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO channel_send_hourly (channel_id, hour, sent, failed) VALUES (?, ?, 1, ?)
			ON CONFLICT (channel_id, hour) DO UPDATE SET sent = channel_send_hourly.sent + 1,
				failed = channel_send_hourly.failed + excluded.failed`, channelID, hour, failed); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM channel_send_hourly WHERE channel_id = ? AND hour < ?",
			channelID, at-int64(channelHourlyKeep.Seconds()))
		return err
	})
}

// ChannelSends kanal id -> son gönderim.
func (s *Store) ChannelSends(ctx context.Context) (map[int64]ChannelSend, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, at, ok, error FROM channel_last_send`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ChannelSend{}
	for rows.Next() {
		var id, at, ok int64
		var msg string
		if err := rows.Scan(&id, &at, &ok, &msg); err != nil {
			return nil, err
		}
		out[id] = ChannelSend{At: at, OK: ok != 0, Error: msg}
	}
	return out, rows.Err()
}
