package api

// Durum sayfasının RSS akışı (giriş gerektirmez).
//
//	GET /api/public/pages/{slug}/feed.xml
//	GET /durum/{slug}/feed.xml
//
// Akış, sayfadaki son 14 günün olaylarını (sayfada olaylar açıksa) ve
// yayına girmiş duyuruları (ileri tarihli olanlar hariç; süresi bitmişler
// dahil, geçmiş okunabilsin) sayfanın dilinde verir. Herkese açık sayfadaki
// gizlilik kuralları aynen geçerlidir: olay nedeni, monitör kimliği ve hedef
// yazılmaz; şifreli sayfanın akışı yalnızca şifre çerezi olan tarayıcıya
// verilir (RSS okuyucular için erişilebilir bir akış yoktur).

import (
	"encoding/xml"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/public/pages/{slug}/feed.xml", s.publicFeed)
		mux.HandleFunc("GET /durum/{slug}/feed.xml", s.publicFeed)
	})
}

// feedMaxItems akıştaki en fazla kayıt (olay + duyuru).
const feedMaxItems = 100

// rss RSS 2.0 belgesi (atom:link kendine bağlantı için).
type rss struct {
	XMLName xml.Name    `xml:"rss"`
	Version string      `xml:"version,attr"`
	Atom    string      `xml:"xmlns:atom,attr"`
	Channel feedChannel `xml:"channel"`
}

type feedChannel struct {
	Title         string     `xml:"title"`
	Link          string     `xml:"link"`
	Description   string     `xml:"description"`
	Language      string     `xml:"language"`
	LastBuildDate string     `xml:"lastBuildDate"`
	TTL           int        `xml:"ttl"`
	Self          feedSelf   `xml:"atom:link"`
	Items         []feedItem `xml:"item"`
}

type feedSelf struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type feedItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        feedGUID `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description"`
	Category    string   `xml:"category,omitempty"`
	at          int64    // sıralama için (en yeni önce)
}

type feedGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// publicBase akıştaki bağlantıların kökü: BASE_URL, yoksa isteğin şeması ve
// sunucu adı (özel alan adında ziyaretçinin gördüğü adres).
func (s *Server) publicBase(r *http.Request) string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) publicFeed(w http.ResponseWriter, r *http.Request) {
	e, err := s.publicEntry(r.Context(), strings.ToLower(r.PathValue("slug")))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil {
		http.Error(w, "Durum sayfası bulunamadı", http.StatusNotFound)
		return
	}
	p := e.page
	if p.HasPassword && !s.unlocked(r, p) {
		http.Error(w, "Bu sayfa şifre korumalı; akış yalnızca sayfayı açan tarayıcıya verilir", http.StatusUnauthorized)
		return
	}
	lang := i18n.Or(p.Lang)
	base := s.publicBase(r)
	pageURL := base + "/durum/" + p.Slug
	if p.CustomDomain != "" && strings.EqualFold(r.Host, p.CustomDomain) {
		pageURL = base + "/"
	}
	now := s.now()
	items, err := s.feedItems(r, p, lang, pageURL, now.Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	doc := rss{Version: "2.0", Atom: "http://www.w3.org/2005/Atom", Channel: feedChannel{
		Title: p.Title, Link: pageURL, Description: p.Description, Language: lang,
		LastBuildDate: now.UTC().Format(time.RFC1123Z), TTL: 5,
		Self:  feedSelf{Href: base + "/api/public/pages/" + p.Slug + "/feed.xml", Rel: "self", Type: "application/rss+xml"},
		Items: items,
	}}
	if doc.Channel.Description == "" {
		doc.Channel.Description = i18n.T(lang, "feed.description", p.Title)
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		s.dbError(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/rss+xml; charset=utf-8")
	if p.HasPassword {
		h.Set("Cache-Control", "private, no-store")
	} else {
		h.Set("Cache-Control", "public, max-age=60")
	}
	w.Write([]byte(xml.Header))
	w.Write(out)
}

// feedItems akışın kayıtları: duyurular ve (açıksa) olaylar, en yeni önce.
func (s *Server) feedItems(r *http.Request, p store.StatusPage, lang, pageURL string, now int64) ([]feedItem, error) {
	ctx := r.Context()
	var items []feedItem
	anns, err := s.store.ListAnnouncements(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range anns {
		if a.StartsAt > now {
			continue // ileri tarihli duyuru sayfada da yoktur
		}
		at := max(a.StartsAt, a.UpdatedAt)
		items = append(items, feedItem{
			Title: a.Title, Link: pageURL, GUID: feedGUID{Value: "announcement-" + strconv.FormatInt(a.ID, 10) + "@" + p.Slug},
			PubDate: rfc1123(at), Description: a.Body, Category: i18n.T(lang, "feed.category.announcement"), at: at,
		})
	}
	if p.ShowIncidents {
		names := map[int64]string{}
		for _, sec := range p.Sections {
			for _, pm := range sec.Monitors {
				names[pm.ID] = pm.Name
			}
		}
		incs, err := s.store.IncidentsFor(ctx, p.MonitorIDs(), now-publicIncidentWindow, publicIncidentLimit)
		if err != nil {
			return nil, err
		}
		for _, in := range incs {
			name := names[in.MonitorID]
			if name == "" {
				name = in.MonitorName
			}
			started := i18n.DateTime(lang, time.Unix(in.StartedAt, 0).In(s.store.Location()))
			it := feedItem{Link: pageURL, Category: i18n.T(lang, "feed.category.incident"), at: in.StartedAt}
			if in.ResolvedAt > 0 {
				it.at = in.ResolvedAt
				it.Title = i18n.T(lang, "feed.incident.resolved", name)
				it.Description = i18n.T(lang, "feed.incident.resolved_body", started,
					i18n.DateTime(lang, time.Unix(in.ResolvedAt, 0).In(s.store.Location())),
					i18n.Duration(lang, time.Duration(in.ResolvedAt-in.StartedAt)*time.Second))
			} else {
				it.Title = i18n.T(lang, "feed.incident.ongoing", name)
				it.Description = i18n.T(lang, "feed.incident.ongoing_body", started)
			}
			// Aynı olayın "sürüyor" ve "çözüldü" halleri ayrı kayıttır: okuyucu
			// çözülmeyi de yeni kayıt olarak gösterir.
			it.GUID = feedGUID{Value: "incident-" + strconv.FormatInt(in.ID, 10) + "-" + strconv.FormatInt(in.ResolvedAt, 10) + "@" + p.Slug}
			it.PubDate = rfc1123(it.at)
			items = append(items, it)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at > items[j].at })
	if len(items) > feedMaxItems {
		items = items[:feedMaxItems]
	}
	if items == nil {
		items = []feedItem{}
	}
	return items, nil
}

func rfc1123(t int64) string { return time.Unix(t, 0).UTC().Format(time.RFC1123Z) }
