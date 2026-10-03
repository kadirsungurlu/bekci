package api

// Etiket tabanlı kurallar (E-12): bildirim kanalı, kısıtlı kullanıcı ve durum
// sayfası grubu bir etikete (= değere) bağlanabilir. Açık seçimlerle birleşim:
//   - kanal: monitörün açık bağlantıları ∪ monitörün etiketlerine uyan kanallar;
//   - kullanıcı: seçilen monitörler ∪ etikete uyan monitörler;
//   - sayfa grubu: açıkça eklenenler + etiketi taşıyanlar (Auto, ada göre).
// Etiket silinince kural da silinir; açık seçimler etkilenmez.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/store"
)

const maxTagRules = 20

// normalizeTagRules kuralları doğrular: etiket var olmalı, değer sınırlı,
// aynı kural bir kez. nil girdi boş liste döner (eski istemci).
func (s *Server) normalizeTagRules(ctx context.Context, in []store.TagRule) ([]store.TagRule, error) {
	out := make([]store.TagRule, 0, len(in))
	if len(in) > maxTagRules {
		return nil, fmt.Errorf("En fazla %d etiket kuralı eklenebilir", maxTagRules)
	}
	seen := map[string]bool{}
	for _, r := range in {
		if r.TagID <= 0 {
			return nil, errors.New(tagNotFoundMessage)
		}
		if _, err := s.store.GetTag(ctx, r.TagID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, errors.New(tagNotFoundMessage)
			}
			return nil, err
		}
		v, err := normalizeTagValue(r.Value)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%d\x00%s", r.TagID, strings.ToLower(v))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, store.TagRule{TagID: r.TagID, Value: v})
	}
	return out, nil
}

// joinDetail boş olmayan ayrıntı parçalarını "; " ile birleştirir.
func joinDetail(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "; ")
}

// tagRulesDetail işlem kaydı için "etiket: ad=değer, ad" metni (Türkçe saklanır).
func tagRulesDetail(rules []store.TagRule) string {
	if len(rules) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		if r.Value != "" {
			parts = append(parts, r.Name+"="+r.Value)
		} else {
			parts = append(parts, r.Name)
		}
	}
	return "etiket kuralı: " + strings.Join(parts, " | ")
}
