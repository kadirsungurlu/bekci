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
}

// SetChannelSend kanalın son gönderim sonucunu yazar (üzerine yazar).
func (s *Store) SetChannelSend(ctx context.Context, channelID, at int64, ok bool) error {
	v := 0
	if ok {
		v = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO channel_last_send (channel_id, at, ok) VALUES (?, ?, ?)
		ON CONFLICT (channel_id) DO UPDATE SET at = excluded.at, ok = excluded.ok`, channelID, at, v)
	return err
}

// ChannelSends kanal id -> son gönderim.
func (s *Store) ChannelSends(ctx context.Context) (map[int64]ChannelSend, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, at, ok FROM channel_last_send`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ChannelSend{}
	for rows.Next() {
		var id, at, ok int64
		if err := rows.Scan(&id, &at, &ok); err != nil {
			return nil, err
		}
		out[id] = ChannelSend{At: at, OK: ok != 0}
	}
	return out, rows.Err()
}
