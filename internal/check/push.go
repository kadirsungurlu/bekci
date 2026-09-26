package check

import (
	"context"
	"encoding/json"
)

// pushChecker pasif tiptir: hedef sistem /api/push/{token} adresine sinyal
// gönderir, motor belirtilen aralıkta sinyal gelmezse monitörü DOWN yapar.
type pushChecker struct{}

func init() { Register(TypePush, pushChecker{}) }

func (pushChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c struct{}
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	return json.RawMessage("{}"), nil
}

func (pushChecker) Target(json.RawMessage) string { return "Push" }

func (pushChecker) Check(context.Context, json.RawMessage) Result {
	return down("Belirtilen sürede push sinyali gelmedi")
}
