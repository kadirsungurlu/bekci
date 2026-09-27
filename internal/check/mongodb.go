package check

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// MongoDBConfig MongoDB kontrol ayarları. Bağlantı URI'si kullanıcı adı ve
// şifreyi içerebileceğinden gizli alan olarak saklanır.
type MongoDBConfig struct {
	URI      string `json:"uri"`      // gizli; mongodb:// veya mongodb+srv://
	Database string `json:"database"` // boşsa "admin" üzerinde ping çalıştırılır
}

type mongodbChecker struct{}

func init() { Register("mongodb", mongodbChecker{}) }

func (mongodbChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c MongoDBConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URI = strings.TrimSpace(c.URI)
	if c.URI == "" {
		return nil, invalid("Bağlantı URI'si gerekli")
	}
	if !strings.HasPrefix(c.URI, "mongodb://") && !strings.HasPrefix(c.URI, "mongodb+srv://") {
		return nil, invalid("URI mongodb:// veya mongodb+srv:// ile başlamalı")
	}
	c.Database = strings.TrimSpace(c.Database)
	return encode(c), nil
}

func (mongodbChecker) Target(raw json.RawMessage) string {
	var c MongoDBConfig
	json.Unmarshal(raw, &c)
	return redactMongoURI(c.URI) + targetDBSuffix(c.Database)
}

func targetDBSuffix(db string) string {
	if db == "" {
		return ""
	}
	return "/" + db
}

// redactMongoURI URI'deki kullanıcı adı/şifreyi kaldırır.
func redactMongoURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return "mongodb://…" // ayrıştırılamıyorsa hiçbir şey sızdırma
	}
	u.User = nil
	return u.String()
}

func (mongodbChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c MongoDBConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}

	timeout := remainingTimeout(ctx)
	opts := options.Client().ApplyURI(c.URI).SetConnectTimeout(timeout).SetServerSelectionTimeout(timeout).SetTimeout(timeout)
	client, err := mongo.Connect(opts)
	if err != nil {
		return down("Bağlantı ayarı geçersiz: " + describeErr(ctx, err))
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.Disconnect(dctx)
	}()

	start := time.Now()
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return down(describeErr(ctx, err))
	}

	dbName := c.Database
	if dbName == "" {
		dbName = "admin"
	}
	if err := client.Database(dbName).RunCommand(ctx, bson.D{{Key: "ping", Value: 1}}).Err(); err != nil {
		return down(describeErr(ctx, err))
	}
	return Result{Up: true, PingMs: msSince(start), Message: "Ping başarılı"}
}
