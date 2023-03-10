//
// Copyright (c) 2019 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package main

import (
	"crypto/rand"
	"crypto/sha512"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"humungus.tedunangst.com/r/webs/cache"
	"humungus.tedunangst.com/r/webs/htfilter"
	"humungus.tedunangst.com/r/webs/httpsig"
	"humungus.tedunangst.com/r/webs/mz"
	"humungus.tedunangst.com/r/webs/templates"
)

var allowedclasses = make(map[string]bool)

func init() {
	allowedclasses["kw"] = true
	allowedclasses["bi"] = true
	allowedclasses["st"] = true
	allowedclasses["nm"] = true
	allowedclasses["tp"] = true
	allowedclasses["op"] = true
	allowedclasses["cm"] = true
	allowedclasses["al"] = true
	allowedclasses["dl"] = true
}

var relingo = make(map[string]string)

func loadLingo() {
	for _, l := range []string{"honked", "shared", "honked back", "qonked", "evented"} {
		v := l
		k := "lingo-" + strings.ReplaceAll(l, " ", "")
		getConfigValue(k, &v)
		relingo[l] = v
	}
}

func reverbolate(userid int64, honks []*ActivityPubActivity) {
	var user *UserProfile
	usersCacheByID.Get(userid, &user)
	for _, h := range honks {
		h.What += "ed"
		if h.What == "tonked" {
			h.What = "honked back"
			h.Style += " subtle"
		}
		if !h.Public {
			h.Style += " limited"
		}
		if h.Whofore == 1 {
			h.Style += " atme"
		}
		translate(h)
		local := false
		if h.Whofore == 2 || h.Whofore == 3 {
			local = true
		}
		if local && h.What != "shared" {
			h.Text = re_memes.ReplaceAllString(h.Text, "")
		}
		h.Username, h.Handle = handles(h.Author)
		if !local {
			short := shortname(userid, h.Author)
			if short != "" {
				h.Username = short
			} else {
				h.Username = h.Handle
				if len(h.Username) > 20 {
					h.Username = h.Username[:20] + ".."
				}
			}
		}
		if user != nil {
			if user.Options.MentionAll {
				hset := []string{"@" + h.Handle}
				for _, a := range h.Audience {
					if a == h.Author || a == user.URL {
						continue
					}
					_, hand := handles(a)
					if hand != "" {
						hand = "@" + hand
						hset = append(hset, hand)
					}
				}
				h.Handles = strings.Join(hset, " ")
			} else if h.Author != user.URL {
				h.Handles = "@" + h.Handle
			}
		}
		if h.URL == "" {
			h.URL = h.XID
		}
		if h.Oonker != "" {
			_, h.Oondle = handles(h.Oonker)
		}
		h.Precis = demoji(h.Precis)
		h.Text = demoji(h.Text)
		h.Open = "open"
		for _, m := range h.Mentions {
			if !m.IsPresent(h.Text) {
				h.Text = "(" + m.Who + ")" + h.Text
			}
		}

		zap := make(map[string]bool)
		{
			var htf htfilter.Filter
			htf.Imager = replaceimgsand(zap, false)
			htf.SpanClasses = allowedclasses
			htf.BaseURL, _ = url.Parse(h.XID)
			emuxifier := func(e string) string {
				for _, d := range h.Attachments {
					if d.Name == e {
						zap[d.XID] = true
						if d.Local {
							return fmt.Sprintf(`<img class="emu" title="%s" src="/d/%s">`, d.Name, d.XID)
						}
					}
				}
				if local && h.What != "shared" {
					var emu Emu
					emucache.Get(e, &emu)
					if emu.ID != "" {
						return fmt.Sprintf(`<img class="emu" title="%s" src="%s">`, emu.Name, emu.ID)
					}
				}
				return e
			}
			htf.FilterText = func(w io.Writer, data string) {
				data = htfilter.EscapeText(data)
				data = re_emus.ReplaceAllStringFunc(data, emuxifier)
				io.WriteString(w, data)
			}
			p, _ := htf.String(h.Precis)
			n, _ := htf.String(h.Text)
			h.Precis = string(p)
			h.Text = string(n)
		}
		j := 0
		for i := 0; i < len(h.Attachments); i++ {
			if !zap[h.Attachments[i].XID] {
				h.Attachments[j] = h.Attachments[i]
				j++
			}
		}
		h.Attachments = h.Attachments[:j]
	}

	unsee(honks, userid)

	for _, h := range honks {
		renderflags(h)

		h.HTPrecis = template.HTML(h.Precis)
		h.HTML = template.HTML(h.Text)
		if redo := relingo[h.What]; redo != "" {
			h.What = redo
		}
	}
}

func replaceimgsand(zap map[string]bool, absolute bool) func(node *html.Node) string {
	return func(node *html.Node) string {
		src := htfilter.GetAttr(node, "src")
		alt := htfilter.GetAttr(node, "alt")
		//title := GetAttr(node, "title")
		if htfilter.HasClass(node, "Emoji") && alt != "" {
			return alt
		}
		d := findAttachment(src)
		if d != nil {
			zap[d.XID] = true
			base := ""
			if absolute {
				base = "https://" + serverName
			}
			return string(templates.Sprintf(`<img alt="%s" title="%s" src="%s/d/%s">`, alt, alt, base, d.XID))
		}
		return string(templates.Sprintf(`&lt;img alt="%s" src="<a href="%s">%s</a>"&gt;`, alt, src, src))
	}
}

func translateChatMessage(ch *ChatMessage) {
	text := ch.Text
	if ch.Format == "markdown" {
		text = markitzero(text)
	}
	var htf htfilter.Filter
	htf.SpanClasses = allowedclasses
	htf.BaseURL, _ = url.Parse(ch.XID)
	ch.HTML, _ = htf.String(text)
}

func filterChatMessage(ch *ChatMessage) {
	translateChatMessage(ch)

	text := string(ch.HTML)

	local := originate(ch.XID) == serverName

	zap := make(map[string]bool)
	emuxifier := func(e string) string {
		for _, d := range ch.Attachments {
			if d.Name == e {
				zap[d.XID] = true
				if d.Local {
					return fmt.Sprintf(`<img class="emu" title="%s" src="/d/%s">`, d.Name, d.XID)
				}
			}
		}
		if local {
			var emu Emu
			emucache.Get(e, &emu)
			if emu.ID != "" {
				return fmt.Sprintf(`<img class="emu" title="%s" src="%s">`, emu.Name, emu.ID)
			}
		}
		return e
	}
	text = re_emus.ReplaceAllStringFunc(text, emuxifier)
	j := 0
	for i := 0; i < len(ch.Attachments); i++ {
		if !zap[ch.Attachments[i].XID] {
			ch.Attachments[j] = ch.Attachments[i]
			j++
		}
	}
	ch.Attachments = ch.Attachments[:j]

	text = strings.TrimPrefix(text, "<p>")
	ch.HTML = template.HTML(text)
	if short := shortname(ch.UserID, ch.Who); short != "" {
		ch.Handle = short
	} else {
		ch.Handle, _ = handles(ch.Who)
	}

}

func inlineimgsfor(honk *ActivityPubActivity) func(node *html.Node) string {
	return func(node *html.Node) string {
		src := htfilter.GetAttr(node, "src")
		alt := htfilter.GetAttr(node, "alt")
		d := saveAttachment(src, "image", alt, "image", true)
		if d != nil {
			honk.Attachments = append(honk.Attachments, d)
		}
		dlog.Printf("inline img with src: %s", src)
		return ""
	}
}

func imaginate(honk *ActivityPubActivity) {
	var htf htfilter.Filter
	htf.Imager = inlineimgsfor(honk)
	htf.BaseURL, _ = url.Parse(honk.XID)
	htf.String(honk.Text)
}

func translate(honk *ActivityPubActivity) {
	if honk.Format == "html" {
		return
	}
	text := honk.Text
	if strings.HasPrefix(text, "DZ:") {
		idx := strings.Index(text, "\n")
		if idx == -1 {
			honk.Precis = text
			text = ""
		} else {
			honk.Precis = text[:idx]
			text = text[idx+1:]
		}
	}
	honk.Precis = markitzero(strings.TrimSpace(honk.Precis))

	var marker mz.Marker
	marker.HashLinker = ontoreplacer
	marker.AtLinker = attoreplacer
	text = strings.TrimSpace(text)
	text = marker.Mark(text)
	honk.Text = text
	honk.Hashtags = stringArrayTrimUntilDupe(marker.HashTags)
	honk.Mentions = bunchofgrapes(marker.Mentions)
}

func redoimages(honk *ActivityPubActivity) {
	zap := make(map[string]bool)
	{
		var htf htfilter.Filter
		htf.Imager = replaceimgsand(zap, true)
		htf.SpanClasses = allowedclasses
		p, _ := htf.String(honk.Precis)
		t, _ := htf.String(honk.Text)
		honk.Precis = string(p)
		honk.Text = string(t)
	}
	j := 0
	for i := 0; i < len(honk.Attachments); i++ {
		if !zap[honk.Attachments[i].XID] {
			honk.Attachments[j] = honk.Attachments[i]
			j++
		}
	}
	honk.Attachments = honk.Attachments[:j]

	honk.Text = re_memes.ReplaceAllString(honk.Text, "")
	honk.Text = strings.Replace(honk.Text, "<a href=", "<a class=\"mention u-url\" href=", -1)
}

func randomString(b []byte) string {
	letters := "BCDFGHJKLMNPQRSTVWXYZbcdfghjklmnpqrstvwxyz1234567891234567891234"
	for i, c := range b {
		b[i] = letters[c&63]
	}
	s := string(b)
	return s
}

func shortxid(xid string) string {
	h := sha512.New512_256()
	io.WriteString(h, xid)
	return randomString(h.Sum(nil)[:20])
}

func make18CharRandomString() string {
	var b [18]byte
	rand.Read(b[:])
	return randomString(b[:])
}

func grapevine(mentions []Mention) []string {
	var s []string
	for _, m := range mentions {
		s = append(s, m.Where)
	}
	return s
}

func bunchofgrapes(m []string) []Mention {
	var mentions []Mention
	for i := range m {
		where := gofish(m[i])
		if where != "" {
			mentions = append(mentions, Mention{Who: m[i], Where: where})
		}
	}
	return mentions
}

type Emu struct {
	ID   string
	Name string
	Type string
}

var re_emus = regexp.MustCompile(`:[[:alnum:]_-]+:`)

var emucache = cache.New(cache.Options{Filler: func(ename string) (Emu, bool) {
	fname := ename[1 : len(ename)-1]
	exts := []string{".png", ".gif"}
	for _, ext := range exts {
		_, err := os.Stat(dataDir + "/emus/" + fname + ext)
		if err != nil {
			continue
		}
		url := fmt.Sprintf("https://%s/emu/%s%s", serverName, fname, ext)
		return Emu{ID: url, Name: ename, Type: "image/" + ext[1:]}, true
	}
	return Emu{Name: ename, ID: "", Type: "image/png"}, true
}, Duration: 10 * time.Second})

func herdofemus(text string) []Emu {
	m := re_emus.FindAllString(text, -1)
	m = stringArrayTrimUntilDupe(m)
	var emus []Emu
	for _, e := range m {
		var emu Emu
		emucache.Get(e, &emu)
		if emu.ID == "" {
			continue
		}
		emus = append(emus, emu)
	}
	return emus
}

var re_memes = regexp.MustCompile("meme: ?([^\n]+)")
var re_avatar = regexp.MustCompile("avatar: ?([^\n]+)")
var re_banner = regexp.MustCompile("banner: ?([^\n]+)")

func memetize(honk *ActivityPubActivity) {
	repl := func(x string) string {
		name := x[5:]
		if name[0] == ' ' {
			name = name[1:]
		}
		fd, err := os.Open(dataDir + "/memes/" + name)
		if err != nil {
			ilog.Printf("no meme for %s", name)
			return x
		}
		var peek [512]byte
		n, _ := fd.Read(peek[:])
		ct := http.DetectContentType(peek[:n])
		fd.Close()

		url := fmt.Sprintf("https://%s/meme/%s", serverName, name)
		fileID, err := saveFileMetadata("", name, name, url, ct)
		if err != nil {
			elog.Printf("error saving meme: %s", err)
			return x
		}
		d := &Attachment{
			FileID: fileID,
			Name:   name,
			Media:  ct,
			URL:    url,
			Local:  false,
		}
		honk.Attachments = append(honk.Attachments, d)
		return ""
	}
	honk.Text = re_memes.ReplaceAllStringFunc(honk.Text, repl)
}

var re_quickmention = regexp.MustCompile("(^|[ \n])@[[:alnum:]]+([ \n.]|$)")

func quickrename(s string, userid int64) string {
	nonstop := true
	for nonstop {
		nonstop = false
		s = re_quickmention.ReplaceAllStringFunc(s, func(m string) string {
			prefix := ""
			if m[0] == ' ' || m[0] == '\n' {
				prefix = m[:1]
				m = m[1:]
			}
			prefix += "@"
			m = m[1:]
			tail := ""
			if last := m[len(m)-1]; last == ' ' || last == '\n' || last == '.' {
				tail = m[len(m)-1:]
				m = m[:len(m)-1]
			}

			xid := fullname(m, userid)

			if xid != "" {
				_, name := handles(xid)
				if name != "" {
					nonstop = true
					m = name
				}
			}
			return prefix + m + tail
		})
	}
	return s
}

var shortnames = cache.New(cache.Options{Filler: func(userid int64) (map[string]string, bool) {
	authors := getAuthors(userid)
	m := make(map[string]string)
	for _, a := range authors {
		m[a.XID] = a.Name
	}
	return m, true
}, Invalidator: &authorInvalidator})

func shortname(userid int64, xid string) string {
	var m map[string]string
	ok := shortnames.Get(userid, &m)
	if ok {
		return m[xid]
	}
	return ""
}

var fullnames = cache.New(cache.Options{Filler: func(userid int64) (map[string]string, bool) {
	authors := getAuthors(userid)
	m := map[string]string{}
	for _, a := range authors {
		m[a.Name] = a.XID
	}
	return m, true
}, Invalidator: &authorInvalidator})

func fullname(name string, userid int64) string {
	var m map[string]string
	ok := fullnames.Get(userid, &m)
	if ok {
		return m[name]
	}
	return ""
}

func attoreplacer(m string) string {
	fill := `<span class="h-card"><a class="u-url mention" href="%s">%s</a></span>`
	where := gofish(m)
	if where == "" {
		return m
	}
	who := m[0 : 1+strings.IndexByte(m[1:], '@')]
	return fmt.Sprintf(fill, html.EscapeString(where), html.EscapeString(who))
}

func ontoreplacer(h string) string {
	return fmt.Sprintf(`<a href="https://%s/o/%s">%s</a>`, serverName,
		strings.ToLower(h[1:]), h)
}

var re_unurl = regexp.MustCompile("https://([^/]+).*/([^/]+)")
var re_urlhost = regexp.MustCompile("https://([^/ #)]+)")

func originate(u string) string {
	m := re_urlhost.FindStringSubmatch(u)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

var allhandles = cache.New(cache.Options{Filler: func(xid string) (string, bool) {
	var preferredUsername string
	// FIXME: error is ignored?
	stmtPreferredUsernameGet.QueryRow(xid).Scan(&preferredUsername)
	if preferredUsername != "" {
		return preferredUsername, true
	}

	dlog.Printf("need to get a handle: %s", xid)
	info, err := investigate(xid)
	if err != nil {
		m := re_unurl.FindStringSubmatch(xid)
		if len(m) > 2 {
			return m[2], true
		}
		return xid, true
	}
	return info.Name, true
}})

// handle, handle@host
func handles(xid string) (string, string) {
	if xid == "" || xid == activitystreamsPublicString || strings.HasSuffix(xid, "/followers") {
		return "", ""
	}
	var handle string
	allhandles.Get(xid, &handle)
	if handle == xid {
		return xid, xid
	}
	return handle, handle + "@" + originate(xid)
}

func butnottooloud(aud []string) {
	for i, a := range aud {
		if strings.HasSuffix(a, "/followers") {
			aud[i] = ""
		}
	}
}

func publicAudience(aud []string) bool {
	for _, a := range aud {
		if a == activitystreamsPublicString {
			return true
		}
	}
	return false
}

func firstclass(honk *ActivityPubActivity) bool {
	return honk.Audience[0] == activitystreamsPublicString
}

func stringArrayTrimUntilDupe(a []string) []string {
	seen := make(map[string]bool)
	seen[""] = true
	j := 0
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			a[j] = s
			j++
		}
	}
	return a[:j]
}

var ziggies = cache.New(cache.Options{Filler: func(userid int64) (*KeyInfo, bool) {
	var user *UserProfile
	ok := usersCacheByID.Get(userid, &user)
	if !ok {
		return nil, false
	}
	ki := new(KeyInfo)
	ki.keyname = user.URL + "#key"
	ki.seckey = user.SecKey
	return ki, true
}})

func getPrivateKey(userid int64) *KeyInfo {
	var ki *KeyInfo
	ziggies.Get(userid, &ki)
	return ki
}

var zaggies = cache.New(cache.Options{Filler: func(keyname string) (httpsig.PublicKey, bool) {
	var data string
	// FIXME: error is ignored?
	stmtActorGetPubkey.QueryRow(keyname).Scan(&data)
	if data == "" {
		dlog.Printf("hitting the webs for missing pubkey: %s", keyname)
		j, err := getAndParseLongTimeout(serverUID, keyname)
		if err != nil {
			ilog.Printf("error getting %s pubkey: %s", keyname, err)
			when := time.Now().UTC().Format(dbtimeformat)
			// FIXME: error is ignored?
			stmtActorSetPubkey.Exec(keyname, "failed", when)
			return httpsig.PublicKey{}, true
		}
		allinjest(originate(keyname), j)
		// FIXME: error is ignored?
		stmtActorGetPubkey.QueryRow(keyname).Scan(&data)
		if data == "" {
			ilog.Printf("key not found after ingesting")
			when := time.Now().UTC().Format(dbtimeformat)
			stmtActorSetPubkey.Exec(keyname, "failed", when)
			return httpsig.PublicKey{}, true
		}
	}
	if data == "failed" {
		ilog.Printf("lookup previously failed key %s", keyname)
		return httpsig.PublicKey{}, true
	}
	_, key, err := httpsig.DecodeKey(data)
	if err != nil {
		ilog.Printf("error decoding %s pubkey: %s", keyname, err)
		return key, true
	}
	return key, true
}, Limit: 512})

func getPubKey(keyname string) (httpsig.PublicKey, error) {
	var key httpsig.PublicKey
	zaggies.Get(keyname, &key)
	return key, nil
}

func removeOldPubkey(keyname string) {
	when := time.Now().Add(-30 * time.Minute).UTC().Format(dbtimeformat)
	// FIXME: error is ignored?
	stmtActorDeleteOldPubkey.Exec(keyname, when)
	zaggies.Clear(keyname)
}

func keymatch(keyname string, actor string) string {
	hash := strings.IndexByte(keyname, '#')
	if hash == -1 {
		hash = len(keyname)
	}
	owner := keyname[0:hash]
	if owner == actor {
		return originate(actor)
	}
	return ""
}
