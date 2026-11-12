package store

// 730 §D: inode doluluğu ve zombi süreç kuralları. Var olan sunuculara
// varsayılanlar eklenir (yeni sunucular servers.DefaultRules ile alır):
// inode 90 % 5 dk etkin, zombies 50 süreç 10 dk pasif.
func init() {
	RegisterMigration(56, `
INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
	SELECT DISTINCT probe_id, 'inode', 90, 5, 1, 0 FROM server_alerts a
	WHERE NOT EXISTS (SELECT 1 FROM server_alerts b WHERE b.probe_id = a.probe_id AND b.metric = 'inode');
INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
	SELECT DISTINCT probe_id, 'zombies', 50, 10, 0, 0 FROM server_alerts WHERE metric = 'inode';
`)
}
