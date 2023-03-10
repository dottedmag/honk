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
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	notrand "math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ridge/must/v2"
	"github.com/ridge/tj"
	"humungus.tedunangst.com/r/webs/cache"
	"humungus.tedunangst.com/r/webs/gate"
	"humungus.tedunangst.com/r/webs/httpsig"
	"humungus.tedunangst.com/r/webs/junk"
	"humungus.tedunangst.com/r/webs/templates"
)

// ++
var ldjsonContentType = `application/ld+json; profile="https://www.w3.org/ns/activitystreams"`

// ++
var activityJsonContentType = `application/activity+json`

// ++
var atContextString = "https://www.w3.org/ns/activitystreams"

// ++
var activitystreamsPublicString = "https://www.w3.org/ns/activitystreams#Public"

// ++
var fastTimeout time.Duration = 5

// ++
var slowTimeout time.Duration = 30

// ++
var activityStreamsMediaTypes = []string{
	`application/ld+json`,
	`application/activity+json`,
}

// ++
func isActivityStreamsMediaType(ct string) bool {
	ct = strings.ToLower(ct)
	for _, at := range activityStreamsMediaTypes {
		if strings.HasPrefix(ct, at) {
			return true
		}
	}
	return false
}

// ++
var develClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

// ++
func PostJSON(keyname string, key httpsig.PrivateKey, url string, j tj.O) error {
	return PostMsg(keyname, key, url, must.OK1(json.Marshal(j)))
}

// ++
func PostMsg(keyname string, key httpsig.PrivateKey, url string, msg []byte) error {
	client := http.DefaultClient
	if develMode {
		client = develClient
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "honksnonk/5.0; "+serverName)
	req.Header.Set("Content-Type", ldjsonContentType)
	httpsig.SignRequest(keyname, key, req, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*slowTimeout*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 201:
	case 202:
	default:
		return fmt.Errorf("http post status: %d", resp.StatusCode)
	}
	ilog.Printf("successful post: %s %d", url, resp.StatusCode)
	return nil
}

// -- junk
func getAndParseLongTimeout(userid int64, url string) (junk.Junk, error) {
	return getAndParseWithTimeout(userid, url, slowTimeout*time.Second)
}

// -- junk
func getAndParseShortTimeout(userid int64, url string) (junk.Junk, error) {
	return getAndParseWithTimeout(userid, url, fastTimeout*time.Second)
}

// -- junk
func getAndParseWithRetry(userid int64, url string) (junk.Junk, error) {
	j, err := getAndParseLongTimeout(userid, url)
	if err != nil {
		emsg := err.Error()
		if emsg == "http get status: 502" || strings.Contains(emsg, "timeout") {
			ilog.Printf("trying again after error: %s", emsg)
			time.Sleep(time.Duration(60+notrand.Int63n(60)) * time.Second)
			j, err = getAndParseLongTimeout(userid, url)
			if err != nil {
				ilog.Printf("still couldn't get it")
			} else {
				ilog.Printf("retry success!")
			}
		}
	}
	return j, err
}

var flightdeck = gate.NewSerializer()

// ++
var signGets = true

// -- junk ziggies
func getAndParse(userid int64, url string, accept string, agent string, timeout time.Duration, client *http.Client) (junk.Junk, error) {
	log.Printf("Outbound (getAndParse) Request: %v", url)
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if agent != "" {
		req.Header.Set("User-Agent", agent)
	}
	if signGets {
		var ki *KeyInfo
		ok := ziggies.Get(userid, &ki)
		if ok {
			httpsig.SignRequest(ki.keyname, ki.seckey, req, nil)
		}
	}
	if timeout != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errorSample := make([]byte, 100)
		io.ReadFull(resp.Body, errorSample)
		return nil, fmt.Errorf("http get status: %d [%s]", resp.StatusCode, errorSample)
	}
	return junk.Read(resp.Body)
}

// -- junk
func getAndParseWithTimeout(userid int64, url string, timeout time.Duration) (junk.Junk, error) {
	log.Printf("Outbound (getAndParseWithTimeout) Request: %v", url)
	client := http.DefaultClient
	if develMode {
		client = develClient
	}
	ji, err := flightdeck.Call(url, func() (interface{}, error) {
		at := activityJsonContentType
		if strings.Contains(url, ".well-known/webfinger?resource") {
			at = "application/jrd+json"
		}
		j, err := getAndParse(userid, url, at, "honksnonk/5.0; "+serverName, timeout, client)
		// log.Printf("debug junk %#v", j)
		if err != nil {
			log.Printf("Outbound (getAndParseWithTimeout) Request: %v Failed! %v", url, err)
		}
		return j, err
	})
	if err != nil {
		return nil, err
	}
	j := ji.(junk.Junk)
	return j, nil
}

func fetchsome(url string) ([]byte, error) {
	log.Printf("Outbound (fetchsome) Request: %v", url)
	client := http.DefaultClient
	if develMode {
		client = develClient
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		ilog.Printf("error fetching %s: %s", url, err)
		return nil, err
	}
	req.Header.Set("User-Agent", "honksnonk/5.0; "+serverName)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		ilog.Printf("error fetching %s: %s", url, err)
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200:
	case 201:
	case 202:
	default:
		return nil, fmt.Errorf("http get not 200: %d %s", resp.StatusCode, url)
	}
	var buf bytes.Buffer
	limiter := io.LimitReader(resp.Body, 10*1024*1024)
	io.Copy(&buf, limiter)
	return buf.Bytes(), nil
}

func saveAttachment(url string, name, desc, media string, localize bool) *Attachment {
	if url == "" {
		return nil
	}
	if attachment := findAttachment(url); attachment != nil {
		return attachment
	}
	ilog.Printf("saving attachment: %s", url)
	data := []byte{}
	if localize {
		ii, err := flightdeck.Call(url, func() (interface{}, error) {
			return fetchsome(url)
		})
		if err != nil {
			ilog.Printf("error fetching attachment: %s", err)
			localize = false
			goto saveit
		}
		data = ii.([]byte)

		if len(data) == 10*1024*1024 {
			ilog.Printf("truncation likely")
		}
		if strings.HasPrefix(media, "image") {
			img, err := shrinkit(data)
			if err != nil {
				ilog.Printf("unable to decode image: %s", err)
				localize = false
				data = []byte{}
				goto saveit
			}
			data = img.Data
			media = "image/" + img.Format
		} else if media == "application/pdf" {
			if len(data) > 1000000 {
				ilog.Printf("not saving large pdf")
				localize = false
				data = []byte{}
			}
		} else if len(data) > 100000 {
			ilog.Printf("not saving large attachment")
			localize = false
			data = []byte{}
		}
	}
saveit:
	var xid string
	if localize {
		var err error
		xid, err = saveFileBody(media, data)
		if err != nil {
			elog.Printf("error saving file %s body: %s", url, err)
			return nil
		}
	}
	fileID, err := saveFileMetadata(xid, name, desc, url, media)
	if err != nil {
		elog.Printf("error saving file %s: %s", url, err)
		return nil
	}
	attachment := new(Attachment)
	attachment.FileID = fileID
	return attachment
}

func iszonked(userid int64, xid string) bool {
	var id int64
	row := stmtFindZonk.QueryRow(userid, xid)
	err := row.Scan(&id)
	if err == nil {
		return true
	}
	if err != sql.ErrNoRows {
		ilog.Printf("error querying zonk: %s", err)
	}
	return false
}

func needActivityPubActivity(user *UserProfile, x *ActivityPubActivity) bool {
	if rejectxonk(x) {
		return false
	}
	return needActivityPubActivityID(user, x.XID)
}
func needShareID(user *UserProfile, xid string) bool {
	return needxonkidX(user, xid, true)
}
func needActivityPubActivityID(user *UserProfile, xid string) bool {
	return needxonkidX(user, xid, false)
}
func needxonkidX(user *UserProfile, xid string, isannounce bool) bool {
	if !strings.HasPrefix(xid, "https://") {
		return false
	}
	if strings.HasPrefix(xid, user.URL+"/") {
		return false
	}
	if rejectorigin(user.ID, xid, isannounce) {
		ilog.Printf("rejecting origin: %s", xid)
		return false
	}
	if iszonked(user.ID, xid) {
		ilog.Printf("already zonked: %s", xid)
		return false
	}
	var id int64
	row := stmtFindXonk.QueryRow(user.ID, xid)
	err := row.Scan(&id)
	if err == nil {
		return false
	}
	if err != sql.ErrNoRows {
		ilog.Printf("error querying xonk: %s", err)
	}
	return true
}

func deleteActivityPubActivity(userid int64, xid string) {
	xonk := getActivityPubActivity(userid, xid)
	if xonk != nil {
		deleteHonk(xonk.ID)
	}
	_, err := stmtSaveAction.Exec(userid, xid, "zonk")
	if err != nil {
		elog.Printf("error eradicating: %s", err)
	}
}

func saveActivityPubActivity(x *ActivityPubActivity) {
	ilog.Printf("saving xonk: %s", x.XID)
	go handles(x.Author)
	go handles(x.Oonker)
	savehonk(x)
}

type Box struct {
	In     string
	Out    string
	Shared string
}

var boxofboxes = cache.New(cache.Options{Filler: func(ident string) (*Box, bool) {
	box := &Box{}
	err := stmtActorGetBoxes.QueryRow(ident).Scan(&box.In, &box.Out, &box.Shared)
	if err != nil {
		dlog.Printf("need to get boxes for %s", ident)
		var j junk.Junk
		j, err = getAndParseLongTimeout(serverUID, ident)
		if err != nil {
			dlog.Printf("error getting boxes: %s", err)
			return nil, false
		}
		allinjest(originate(ident), j)
		err = stmtActorGetBoxes.QueryRow(ident).Scan(&box.In, &box.Out, &box.Shared)
	}
	if err == nil {
		return box, true
	}
	return nil, false
}})

func gimmexonks(user *UserProfile, outbox string) {
	dlog.Printf("getting outbox: %s", outbox)
	j, err := getAndParseLongTimeout(user.ID, outbox)
	if err != nil {
		ilog.Printf("error getting outbox: %s", err)
		return
	}
	t, _ := j.GetString("type")
	origin := originate(outbox)
	if t == "OrderedCollection" {
		items, _ := j.GetArray("orderedItems")
		if items == nil {
			items, _ = j.GetArray("items")
		}
		if items == nil {
			obj, ok := j.GetMap("first")
			if ok {
				items, _ = obj.GetArray("orderedItems")
			} else {
				page1, ok := j.GetString("first")
				if ok {
					j, err = getAndParseLongTimeout(user.ID, page1)
					if err != nil {
						ilog.Printf("error gettings page1: %s", err)
						return
					}
					items, _ = j.GetArray("orderedItems")
				}
			}
		}
		if len(items) > 20 {
			items = items[0:20]
		}
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
		for _, item := range items {
			obj, ok := item.(junk.Junk)
			if ok {
				xonksaver(user, obj, origin)
				continue
			}
			xid, ok := item.(string)
			if ok {
				if !needActivityPubActivityID(user, xid) {
					continue
				}
				obj, err = getAndParseLongTimeout(user.ID, xid)
				if err != nil {
					ilog.Printf("error getting item: %s", err)
					continue
				}
				xonksaver(user, obj, originate(xid))
			}
		}
	}
}

func newphone(a []string, obj junk.Junk) []string {
	for _, addr := range []string{"to", "cc", "attributedTo"} {
		who, _ := obj.GetString(addr)
		if who != "" {
			a = append(a, who)
		}
		whos, _ := obj.GetArray(addr)
		for _, w := range whos {
			who, _ := w.(string)
			if who != "" {
				a = append(a, who)
			}
		}
	}
	return a
}

func extractattrto(obj junk.Junk) string {
	who, _ := obj.GetString("attributedTo")
	if who != "" {
		return who
	}
	o, ok := obj.GetMap("attributedTo")
	if ok {
		id, ok := o.GetString("id")
		if ok {
			return id
		}
	}
	arr, _ := obj.GetArray("attributedTo")
	for _, a := range arr {
		o, ok := a.(junk.Junk)
		if ok {
			t, _ := o.GetString("type")
			id, _ := o.GetString("id")
			if t == "Person" || t == "" {
				return id
			}
		}
		s, ok := a.(string)
		if ok {
			return s
		}
	}
	return ""
}

func firstofmany(obj junk.Junk, key string) string {
	if val, _ := obj.GetString(key); val != "" {
		return val
	}
	if arr, _ := obj.GetArray(key); len(arr) > 0 {
		val, ok := arr[0].(string)
		if ok {
			return val
		}
	}
	return ""
}

func xonksaver(user *UserProfile, item junk.Junk, origin string) *ActivityPubActivity {
	depth := 0
	maxdepth := 10
	currenttid := ""
	goingup := 0
	var xonkxonkfn func(item junk.Junk, origin string, isUpdate bool) *ActivityPubActivity

	saveonemore := func(xid string) {
		dlog.Printf("getting onemore: %s", xid)
		if depth >= maxdepth {
			ilog.Printf("in too deep")
			return
		}
		obj, err := getAndParseWithRetry(user.ID, xid)
		if err != nil {
			ilog.Printf("error getting onemore: %s: %s", xid, err)
			return
		}
		depth++
		xonkxonkfn(obj, originate(xid), false)
		depth--
	}

	xonkxonkfn = func(item junk.Junk, origin string, isUpdate bool) *ActivityPubActivity {
		id, _ := item.GetString("id")
		what := firstofmany(item, "type")
		dt, ok := item.GetString("published")
		if !ok {
			dt = time.Now().Format(time.RFC3339)
		}

		var err error
		var xid, inReplyToID, url, thread string
		var replies []string
		var obj junk.Junk
		switch what {
		case "Delete":
			obj, ok = item.GetMap("object")
			if ok {
				xid, _ = obj.GetString("id")
			} else {
				xid, _ = item.GetString("object")
			}
			if xid == "" {
				return nil
			}
			if originate(xid) != origin {
				ilog.Printf("forged delete: %s", xid)
				return nil
			}
			ilog.Printf("eradicating %s", xid)
			deleteActivityPubActivity(user.ID, xid)
			return nil
		case "Remove":
			xid, _ = item.GetString("object")
			targ, _ := obj.GetString("target")
			ilog.Printf("remove %s from %s", obj, targ)
			return nil
		case "Tombstone":
			xid, _ = item.GetString("id")
			if xid == "" {
				return nil
			}
			if originate(xid) != origin {
				ilog.Printf("forged delete: %s", xid)
				return nil
			}
			ilog.Printf("eradicating %s", xid)
			deleteActivityPubActivity(user.ID, xid)
			return nil
		case "Announce":
			obj, ok = item.GetMap("object")
			if ok {
				xid, _ = obj.GetString("id")
			} else {
				xid, _ = item.GetString("object")
			}
			if !needShareID(user, xid) {
				return nil
			}
			dlog.Printf("getting share: %s", xid)
			obj, err = getAndParseWithRetry(user.ID, xid)
			if err != nil {
				ilog.Printf("error getting share: %s: %s", xid, err)
			}
			origin = originate(xid)
			what = "share"
		case "Update", "Create":
			isUpdate = what == "Update"
			obj, ok = item.GetMap("object")
			if !ok {
				xid, _ = item.GetString("object")
				dlog.Printf("getting created honk: %s", xid)
				if originate(xid) != origin {
					ilog.Printf("out of bounds %s not from %s", xid, origin)
					return nil
				}
				obj, err = getAndParseWithRetry(user.ID, xid)
				if err != nil {
					ilog.Printf("error getting creation: %s", err)
				}
			}
			if obj == nil {
				ilog.Printf("no object for creation %s", id)
				return nil
			}
			return xonkxonkfn(obj, origin, isUpdate)
		case "Read":
			xid, ok = item.GetString("object")
			if ok {
				if !needActivityPubActivityID(user, xid) {
					dlog.Printf("don't need read obj: %s", xid)
					return nil
				}
				obj, err = getAndParseWithRetry(user.ID, xid)
				if err != nil {
					ilog.Printf("error getting read: %s", err)
					return nil
				}
				return xonkxonkfn(obj, originate(xid), false)
			}
			return nil
		case "Add":
			xid, ok = item.GetString("object")
			if ok {
				// check target...
				if !needActivityPubActivityID(user, xid) {
					dlog.Printf("don't need added obj: %s", xid)
					return nil
				}
				obj, err = getAndParseWithRetry(user.ID, xid)
				if err != nil {
					ilog.Printf("error getting add: %s", err)
					return nil
				}
				return xonkxonkfn(obj, originate(xid), false)
			}
			return nil
		case "Move":
			obj = item
			what = "move"
		case "Audio", "Image", "Video", "Question", "Note", "Article", "Page":
			obj = item
			what = "honk"
		case "Event":
			obj = item
			what = "event"
		case "ChatMessage":
			obj = item
			what = "chatMessage"
		default:
			ilog.Printf("unknown activity: %s", what)
			dumpactivity(item)
			return nil
		}

		if obj != nil {
			xid, _ = obj.GetString("id")
		}

		if xid == "" {
			ilog.Printf("don't know what xid is")
			item.Write(ilog.Writer())
			return nil
		}
		if originate(xid) != origin {
			ilog.Printf("original sin: %s not from %s", xid, origin)
			item.Write(ilog.Writer())
			return nil
		}

		var xonk ActivityPubActivity
		// early init
		xonk.XID = xid
		xonk.UserID = user.ID
		xonk.Author, _ = item.GetString("actor")
		if xonk.Author == "" {
			xonk.Author, _ = item.GetString("attributedTo")
		}
		if obj != nil {
			if xonk.Author == "" {
				xonk.Author = extractattrto(obj)
			}
			xonk.Oonker = extractattrto(obj)
			if xonk.Oonker == xonk.Author {
				xonk.Oonker = ""
			}
			xonk.Audience = newphone(nil, obj)
		}
		xonk.Audience = append(xonk.Audience, xonk.Author)
		xonk.Audience = stringArrayTrimUntilDupe(xonk.Audience)
		xonk.Public = publicAudience(xonk.Audience)

		var mentions []Mention
		if obj != nil {
			ot, _ := obj.GetString("type")
			url, _ = obj.GetString("url")
			if dt2, ok := obj.GetString("published"); ok {
				dt = dt2
			}
			content, _ := obj.GetString("content")
			if !strings.HasPrefix(content, "<p>") {
				content = "<p>" + content
			}
			precis, _ := obj.GetString("summary")
			if name, ok := obj.GetString("name"); ok {
				if precis != "" {
					content = precis + "<p>" + content
				}
				precis = html.EscapeString(name)
			}
			if sens, _ := obj["sensitive"].(bool); sens && precis == "" {
				precis = "unspecified horror"
			}
			inReplyToID, ok = obj.GetString("inReplyTo")
			if !ok {
				if robj, ok := obj.GetMap("inReplyTo"); ok {
					inReplyToID, _ = robj.GetString("id")
				}
			}
			thread, _ = obj.GetString("context")
			if thread == "" {
				thread, _ = obj.GetString("conversation")
			}
			if ot == "Question" {
				if what == "honk" {
					what = "qonk"
				}
				content += "<ul>"
				ans, _ := obj.GetArray("oneOf")
				for _, ai := range ans {
					a, ok := ai.(junk.Junk)
					if !ok {
						continue
					}
					as, _ := a.GetString("name")
					content += "<li>" + as
				}
				ans, _ = obj.GetArray("anyOf")
				for _, ai := range ans {
					a, ok := ai.(junk.Junk)
					if !ok {
						continue
					}
					as, _ := a.GetString("name")
					content += "<li>" + as
				}
				content += "</ul>"
			}
			if ot == "Move" {
				targ, _ := obj.GetString("target")
				content += string(templates.Sprintf(`<p>Moved to <a href="%s">%s</a>`, targ, targ))
			}
			if what == "honk" && inReplyToID != "" {
				what = "tonk"
			}
			if len(content) > 90001 {
				ilog.Printf("content too long. truncating")
				content = content[:90001]
			}

			xonk.Text = content
			xonk.Precis = precis
			if rejectxonk(&xonk) {
				dlog.Printf("fast reject: %s", xid)
				return nil
			}

			numatts := 0
			procatt := func(att junk.Junk) {
				at, _ := att.GetString("type")
				mt, _ := att.GetString("mediaType")
				u, ok := att.GetString("url")
				if !ok {
					if ua, ok := att.GetArray("url"); ok && len(ua) > 0 {
						u, ok = ua[0].(string)
						if !ok {
							if uu, ok := ua[0].(junk.Junk); ok {
								u, _ = uu.GetString("href")
								if mt == "" {
									mt, _ = uu.GetString("mediaType")
								}
							}
						}
					} else if uu, ok := att.GetMap("url"); ok {
						u, _ = uu.GetString("href")
						if mt == "" {
							mt, _ = uu.GetString("mediaType")
						}
					}
				}
				name, _ := att.GetString("name")
				desc, _ := att.GetString("summary")
				desc = html.UnescapeString(desc)
				if desc == "" {
					desc = name
				}
				localize := false
				if numatts > 4 {
					ilog.Printf("excessive attachment: %s", at)
				} else if at == "Document" || at == "Image" {
					mt = strings.ToLower(mt)
					dlog.Printf("attachment: %s %s", mt, u)
					if mt == "text/plain" || mt == "application/pdf" ||
						strings.HasPrefix(mt, "image") {
						localize = true
					}
				} else {
					ilog.Printf("unknown attachment: %s", at)
				}
				if skipMedia(&xonk) {
					localize = false
				}
				attachment := saveAttachment(u, name, desc, mt, localize)
				if attachment != nil {
					xonk.Attachments = append(xonk.Attachments, attachment)
				}
				numatts++
			}
			atts, _ := obj.GetArray("attachment")
			for _, atti := range atts {
				att, ok := atti.(junk.Junk)
				if !ok {
					ilog.Printf("attachment that wasn't map?")
					continue
				}
				procatt(att)
			}
			if att, ok := obj.GetMap("attachment"); ok {
				procatt(att)
			}
			tags, _ := obj.GetArray("tag")
			for _, tagi := range tags {
				tag, ok := tagi.(junk.Junk)
				if !ok {
					continue
				}
				tt, _ := tag.GetString("type")
				name, _ := tag.GetString("name")
				desc, _ := tag.GetString("summary")
				desc = html.UnescapeString(desc)
				if desc == "" {
					desc = name
				}
				if tt == "Emoji" {
					icon, _ := tag.GetMap("icon")
					mt, _ := icon.GetString("mediaType")
					if mt == "" {
						mt = "image/png"
					}
					u, _ := icon.GetString("url")
					attachment := saveAttachment(u, name, desc, mt, true)
					if attachment != nil {
						xonk.Attachments = append(xonk.Attachments, attachment)
					}
				}
				if tt == "Hashtag" {
					if name == "" || name == "#" {
						// skip it
					} else {
						if name[0] != '#' {
							name = "#" + name
						}
						xonk.Hashtags = append(xonk.Hashtags, name)
					}
				}
				if tt == "Place" {
					p := new(Place)
					p.Name = name
					p.Latitude, _ = tag.GetNumber("latitude")
					p.Longitude, _ = tag.GetNumber("longitude")
					p.Url, _ = tag.GetString("url")
					xonk.Place = p
				}
				if tt == "Mention" {
					var m Mention
					m.Who, _ = tag.GetString("name")
					m.Where, _ = tag.GetString("href")
					mentions = append(mentions, m)
				}
			}
			if starttime, ok := obj.GetString("startTime"); ok {
				if start, err := time.Parse(time.RFC3339, starttime); err == nil {
					t := new(Time)
					t.StartTime = start
					endtime, _ := obj.GetString("endTime")
					t.EndTime, _ = time.Parse(time.RFC3339, endtime)
					dura, _ := obj.GetString("duration")
					if strings.HasPrefix(dura, "PT") {
						dura = strings.ToLower(dura[2:])
						d, _ := time.ParseDuration(dura)
						t.Duration = Duration(d)
					}
					xonk.Time = t
				}
			}
			if loca, ok := obj.GetMap("location"); ok {
				if tt, _ := loca.GetString("type"); tt == "Place" {
					p := new(Place)
					p.Name, _ = loca.GetString("name")
					p.Latitude, _ = loca.GetNumber("latitude")
					p.Longitude, _ = loca.GetNumber("longitude")
					p.Url, _ = loca.GetString("url")
					xonk.Place = p
				}
			}

			xonk.Hashtags = stringArrayTrimUntilDupe(xonk.Hashtags)
			replyobj, ok := obj.GetMap("replies")
			if ok {
				items, ok := replyobj.GetArray("items")
				if !ok {
					first, ok := replyobj.GetMap("first")
					if ok {
						items, _ = first.GetArray("items")
					}
				}
				for _, repl := range items {
					s, ok := repl.(string)
					if ok {
						replies = append(replies, s)
					}
				}
			}

		}

		if currenttid == "" {
			currenttid = thread
		}

		// init xonk
		xonk.What = what
		xonk.InReplyToID = inReplyToID
		xonk.Date, _ = time.Parse(time.RFC3339, dt)
		xonk.URL = url
		xonk.Format = "html"
		xonk.Thread = thread
		xonk.Mentions = mentions
		for _, m := range mentions {
			if m.Where == user.URL {
				xonk.Whofore = 1
			}
		}
		imaginate(&xonk)

		if what == "chatMessage" {
			ch := ChatMessage{
				UserID:      xonk.UserID,
				XID:         xid,
				Who:         xonk.Author,
				Target:      xonk.Author,
				Date:        xonk.Date,
				Text:        xonk.Text,
				Format:      xonk.Format,
				Attachments: xonk.Attachments,
			}
			saveChatMessage(&ch)
			return nil
		}

		if isUpdate {
			dlog.Printf("something has changed! %s", xonk.XID)
			prev := getActivityPubActivity(user.ID, xonk.XID)
			if prev == nil {
				ilog.Printf("didn't find old version for update: %s", xonk.XID)
				isUpdate = false
			} else {
				xonk.ID = prev.ID
				updateHonk(&xonk)
			}
		}
		if !isUpdate && needActivityPubActivity(user, &xonk) {
			if inReplyToID != "" && xonk.Public {
				if needActivityPubActivityID(user, inReplyToID) {
					goingup++
					saveonemore(inReplyToID)
					goingup--
				}
				if thread == "" {
					xx := getActivityPubActivity(user.ID, inReplyToID)
					if xx != nil {
						thread = xx.Thread
					}
				}
			}
			if thread == "" {
				thread = currenttid
			}
			if thread == "" {
				thread = "data:,missing-" + make18CharRandomString()
				currenttid = thread
			}
			xonk.Thread = thread
			saveActivityPubActivity(&xonk)
		}
		if goingup == 0 {
			for _, replid := range replies {
				if needActivityPubActivityID(user, replid) {
					dlog.Printf("missing a reply: %s", replid)
					saveonemore(replid)
				}
			}
		}
		return &xonk
	}

	return xonkxonkfn(item, origin, false)
}

func dumpactivity(item junk.Junk) {
	fd, err := os.OpenFile("savedinbox.json", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		elog.Printf("error opening inbox! %s", err)
		return
	}
	defer fd.Close()
	item.Write(fd)
	io.WriteString(fd, "\n")
}

func rubadubdub(user *UserProfile, req junk.Junk) {
	actor, _ := req.GetString("actor")
	j := tj.O{
		"@context":  atContextString,
		"id":        user.URL + "/dub/" + make18CharRandomString(),
		"type":      "Accept",
		"actor":     user.URL,
		"to":        actor,
		"published": time.Now().UTC().Format(time.RFC3339),
		"object":    req,
	}

	deliverate(0, user.ID, actor, must.OK1(json.Marshal(j)), true)
}

func sendUndo(user *UserProfile, xid string, owner string, folxid string) {
	j := tj.O{
		"@context": atContextString,
		"id":       user.URL + "/unsub/" + folxid,
		"type":     "Undo",
		"actor":    user.URL,
		"to":       owner,
		"object": tj.O{
			"id":     user.URL + "/sub/" + folxid,
			"type":   "Follow",
			"actor":  user.URL,
			"to":     owner,
			"object": xid,
		},
		"published": time.Now().UTC().Format(time.RFC3339),
	}

	deliverate(0, user.ID, owner, must.OK1(json.Marshal(j)), true)
}

func subsub(user *UserProfile, xid string, owner string, folxid string) {
	if xid == "" {
		ilog.Printf("can't subscribe to empty")
		return
	}
	j := junk.New()
	j["@context"] = atContextString
	j["id"] = user.URL + "/sub/" + folxid
	j["type"] = "Follow"
	j["actor"] = user.URL
	j["to"] = owner
	j["object"] = xid
	j["published"] = time.Now().UTC().Format(time.RFC3339)

	deliverate(0, user.ID, owner, j.ToBytes(), true)
}

func activateAttachments(attachments []*Attachment) []tj.O {
	var atts []tj.O
	for _, d := range attachments {
		if re_emus.MatchString(d.Name) {
			continue
		}
		atts = append(atts, tj.O{
			"mediaType": d.Media,
			"name":      d.Name,
			"summary":   html.EscapeString(d.Desc),
			"type":      "Document",
			"url":       d.URL,
		})
	}
	return atts
}

// returns activity, object
func jonkjonk(user *UserProfile, h *ActivityPubActivity) (tj.O, tj.O) {
	dt := h.Date.Format(time.RFC3339)
	jo := tj.O{}
	j := tj.O{
		"id":        user.URL + "/" + h.What + "/" + shortxid(h.XID),
		"actor":     user.URL,
		"published": dt,
		"to":        h.Audience[0],
	}
	if len(h.Audience) > 1 {
		j["cc"] = h.Audience[1:]
	}

	switch h.What {
	case "update", "tonk", "event", "honk":
		j["type"] = "Create"
		jo = tj.O{
			"id":           h.XID,
			"type":         "Note",
			"published":    dt,
			"url":          h.XID,
			"attributedTo": user.URL,
		}
		if h.What == "event" {
			jo["type"] = "Event"
		}
		if h.What == "update" {
			j["type"] = "Update"
			jo["updated"] = dt
		}
		if h.InReplyToID != "" {
			jo["inReplyTo"] = h.InReplyToID
		}
		if h.Thread != "" {
			jo["context"] = h.Thread
			jo["conversation"] = h.Thread
		}
		jo["to"] = h.Audience[0]
		if len(h.Audience) > 1 {
			jo["cc"] = h.Audience[1:]
		}
		if !h.Public {
			jo["directMessage"] = true
		}
		translate(h)
		redoimages(h)
		if h.Precis != "" {
			jo["sensitive"] = true
		}

		var replies []string
		for _, reply := range h.Replies {
			replies = append(replies, reply.XID)
		}
		if len(replies) > 0 {
			jo["replies"] = tj.O{
				"type":       "Collection",
				"totalItems": len(replies),
				"items":      replies,
			}
		}

		var tags []tj.O
		for _, m := range h.Mentions {
			tags = append(tags, tj.O{
				"type": "Mention",
				"name": m.Who,
				"href": m.Where,
			})
		}
		for _, h := range h.Hashtags {
			h = strings.ToLower(h)
			tags = append(tags, tj.O{
				"type": "Hashtag",
				"name": h,
				"href": fmt.Sprintf("https://%s/o/%s", serverName, h[1:]),
			})
		}
		for _, e := range herdofemus(h.Text) {
			tags = append(tags, tj.O{
				"id":   e.ID,
				"type": "Emoji",
				"name": e.Name,
				"icon": tj.O{
					"type":      "Image",
					"mediaType": e.Type,
					"url":       e.ID,
				},
			})
		}
		for _, e := range fixupflags(h) {
			tags = append(tags, tj.O{
				"id":   e.ID,
				"type": "Emoji",
				"name": e.Name,
				"icon": tj.O{
					"type":      "Image",
					"mediaType": "image/png",
					"url":       e.ID,
				},
			})
		}
		if len(tags) > 0 {
			jo["tag"] = tags
		}
		if p := h.Place; p != nil {
			t := tj.O{
				"type": "Place",
			}
			if p.Name != "" {
				t["name"] = p.Name
			}
			if p.Latitude != 0 {
				t["latitude"] = p.Latitude
			}
			if p.Longitude != 0 {
				t["longitude"] = p.Longitude
			}
			if p.Url != "" {
				t["url"] = p.Url
			}
			jo["location"] = t
		}
		if t := h.Time; t != nil {
			jo["startTime"] = t.StartTime.Format(time.RFC3339)
			if t.Duration != 0 {
				jo["duration"] = "PT" + strings.ToUpper(t.Duration.String())
			}
		}
		atts := activateAttachments(h.Attachments)
		if len(atts) > 0 {
			jo["attachment"] = atts
		}
		jo["summary"] = html.EscapeString(h.Precis)
		jo["content"] = h.Text
		j["object"] = jo
	case "share":
		j["type"] = "Announce"
		if h.Thread != "" {
			j["context"] = h.Thread
		}
		j["object"] = h.XID
	case "unshare":
		b := tj.O{
			"id":     user.URL + "/" + "share" + "/" + shortxid(h.XID),
			"type":   "Announce",
			"actor":  user.URL,
			"object": h.XID,
		}
		if h.Thread != "" {
			b["context"] = h.Thread
		}
		j["type"] = "Undo"
		j["object"] = b
	case "zonk":
		j["type"] = "Delete"
		j["object"] = h.XID
	case "ack":
		j["type"] = "Read"
		j["object"] = h.XID
		if h.Thread != "" {
			j["context"] = h.Thread
		}
	case "react":
		j["type"] = "EmojiReact"
		j["object"] = h.XID
		if h.Thread != "" {
			j["context"] = h.Thread
		}
		j["content"] = h.Text
	case "deack":
		b := tj.O{
			"id":     user.URL + "/" + "ack" + "/" + shortxid(h.XID),
			"type":   "Read",
			"actor":  user.URL,
			"object": h.XID,
		}
		if h.Thread != "" {
			b["context"] = h.Thread
		}
		j["type"] = "Undo"
		j["object"] = b
	}

	return j, jo
}

var oldjonks = cache.New(cache.Options{Filler: func(xid string) ([]byte, bool) {
	row := stmtAnyXonk.QueryRow(xid)
	honk := scanhonk(row)
	if honk == nil || !honk.Public {
		return nil, true
	}
	user, _ := getUserBio(honk.Username)
	rawhonks := gethonksbyThread(honk.UserID, honk.Thread, 0)
	reverseSlice(rawhonks)
	for _, h := range rawhonks {
		if h.InReplyToID == honk.XID && h.Public && (h.Whofore == 2 || h.IsAcked()) {
			honk.Replies = append(honk.Replies, h)
		}
	}
	attachmentsForHonks([]*ActivityPubActivity{honk})
	_, j := jonkjonk(user, honk)
	j["@context"] = atContextString

	return must.OK1(json.Marshal(j)), true
}, Limit: 128})

func gimmejonk(xid string) ([]byte, bool) {
	var j []byte
	ok := oldjonks.Get(xid, &j)
	return j, ok
}

func boxuprcpts(user *UserProfile, addresses []string, useshared bool) map[string]bool {
	rcpts := make(map[string]bool)
	for _, a := range addresses {
		if a == "" || a == activitystreamsPublicString || a == user.URL || strings.HasSuffix(a, "/followers") {
			continue
		}
		if a[0] == '%' {
			rcpts[a] = true
			continue
		}
		var box *Box
		ok := boxofboxes.Get(a, &box)
		if ok && useshared && box.Shared != "" {
			rcpts["%"+box.Shared] = true
		} else {
			rcpts[a] = true
		}
	}
	return rcpts
}

func serializeChatMessage(user *UserProfile, ch *ChatMessage) []byte {
	dt := ch.Date.Format(time.RFC3339)
	aud := []string{ch.Target}

	jo := junk.New()
	jo["id"] = ch.XID
	jo["type"] = "ChatMessage"
	jo["published"] = dt
	jo["attributedTo"] = user.URL
	jo["to"] = aud
	jo["content"] = ch.HTML
	atts := activateAttachments(ch.Attachments)
	if len(atts) > 0 {
		jo["attachment"] = atts
	}
	var tags []junk.Junk
	for _, e := range herdofemus(ch.Text) {
		t := junk.New()
		t["id"] = e.ID
		t["type"] = "Emoji"
		t["name"] = e.Name
		i := junk.New()
		i["type"] = "Image"
		i["mediaType"] = e.Type
		i["url"] = e.ID
		t["icon"] = i
		tags = append(tags, t)
	}
	if len(tags) > 0 {
		jo["tag"] = tags
	}

	j := junk.New()
	j["@context"] = atContextString
	j["id"] = user.URL + "/" + "honk" + "/" + shortxid(ch.XID)
	j["type"] = "Create"
	j["actor"] = user.URL
	j["published"] = dt
	j["to"] = aud
	j["object"] = jo

	return j.ToBytes()
}

func sendChatMessage(user *UserProfile, ch *ChatMessage) {
	msg := serializeChatMessage(user, ch)

	rcpts := make(map[string]bool)
	rcpts[ch.Target] = true
	for a := range rcpts {
		go deliverate(0, user.ID, a, msg, true)
	}
}

func honkworldwide(user *UserProfile, honk *ActivityPubActivity) {
	jonk, _ := jonkjonk(user, honk)
	jonk["@context"] = atContextString
	msg := must.OK1(json.Marshal(jonk))

	rcpts := boxuprcpts(user, honk.Audience, honk.Public)

	if honk.Public {
		for _, h := range getdubs(user.ID) {
			if h.XID == user.URL {
				continue
			}
			var box *Box
			ok := boxofboxes.Get(h.XID, &box)
			if ok && box.Shared != "" {
				rcpts["%"+box.Shared] = true
			} else {
				rcpts[h.XID] = true
			}
		}
		for _, f := range getbacktracks(honk.XID) {
			if f[0] == '%' {
				rcpts[f] = true
			} else {
				var box *Box
				ok := boxofboxes.Get(f, &box)
				if ok && box.Shared != "" {
					rcpts["%"+box.Shared] = true
				} else {
					rcpts[f] = true
				}
			}
		}
	}
	for a := range rcpts {
		go deliverate(0, user.ID, a, msg, doesitmatter(honk.What))
	}
	if honk.Public && len(honk.Hashtags) > 0 {
		collectiveaction(honk)
	}
}

func doesitmatter(what string) bool {
	switch what {
	case "ack":
		return false
	case "react":
		return false
	case "deack":
		return false
	}
	return true
}

func collectiveaction(honk *ActivityPubActivity) {
	user := getserveruser()
	for _, hashtag := range honk.Hashtags {
		dubs := getnameddubs(serverUID, hashtag)
		if len(dubs) == 0 {
			continue
		}
		j := junk.New()
		j["@context"] = atContextString
		j["type"] = "Add"
		j["id"] = user.URL + "/add/" + shortxid(hashtag+honk.XID)
		j["actor"] = user.URL
		j["object"] = honk.XID
		j["target"] = fmt.Sprintf("https://%s/o/%s", serverName, hashtag[1:])
		rcpts := make(map[string]bool)
		for _, dub := range dubs {
			var box *Box
			ok := boxofboxes.Get(dub.XID, &box)
			if ok && box.Shared != "" {
				rcpts["%"+box.Shared] = true
			} else {
				rcpts[dub.XID] = true
			}
		}
		msg := j.ToBytes()
		for a := range rcpts {
			go deliverate(0, user.ID, a, msg, false)
		}
	}
}

func serializeUser(user *UserProfile) tj.O {
	j := tj.O{
		"@context":          atContextString,
		"id":                user.URL,
		"inbox":             user.URL + "/inbox",
		"outbox":            user.URL + "/outbox",
		"name":              user.Display,
		"preferredUsername": user.Name,
		"summary":           user.HTAbout,
	}
	var tags []tj.O
	for _, h := range user.Hashtags {
		h = strings.ToLower(h)
		tags = append(tags, tj.O{
			"type": "Hashtag",
			"href": fmt.Sprintf("https://%s/o/%s", serverName, h[1:]),
			"name": h,
		})
	}
	if len(tags) > 0 {
		j["tag"] = tags
	}

	if user.ID > 0 {
		j["type"] = "Person"
		j["url"] = user.URL
		j["followers"] = user.URL + "/followers"
		j["following"] = user.URL + "/following"
		a := tj.O{
			"type":      "Image",
			"mediaType": "image/png",
		}
		if ava := user.Options.Avatar; ava != "" {
			a["url"] = ava
		} else {
			u := fmt.Sprintf("https://%s/a?a=%s", serverName, url.QueryEscape(user.URL))
			if user.Options.Avahex {
				u += "&hex=1"
			}
			a["url"] = u
		}
		j["icon"] = a
		if ban := user.Options.Banner; ban != "" {
			j["image"] = tj.O{
				"type":      "Image",
				"mediaType": "image/jpg",
				"url":       ban,
			}

		}
	} else {
		j["type"] = "Service"
	}
	j["publicKey"] = tj.O{
		"id":           user.URL + "#key",
		"owner":        user.URL,
		"publicKeyPem": user.Key,
	}

	return j
}

var userBioAsJSONCache = cache.New(cache.Options{Filler: func(name string) ([]byte, bool) {
	user, err := getUserBio(name)
	if err != nil {
		return nil, false
	}
	j := serializeUser(user)
	return must.OK1(json.Marshal(j)), true
}, Duration: 1 * time.Minute})

func userBioAsJSON(name string) ([]byte, bool) {
	var j []byte
	ok := userBioAsJSONCache.Get(name, &j)
	return j, ok
}

var handfull = cache.New(cache.Options{Filler: func(name string) (string, bool) {
	m := strings.Split(name, "@")
	if len(m) != 2 {
		dlog.Printf("bad fish name: %s", name)
		return "", true
	}
	var href string
	if err := stmtFriendlyNameGetHref.QueryRow(m[0], m[1]).Scan(&href); err == nil {
		return href, true
	}
	dlog.Printf("fishing for %s", name)
	j, err := getAndParseShortTimeout(serverUID, fmt.Sprintf("https://%s/.well-known/webfinger?resource=acct:%s", m[1], name))
	if err != nil {
		ilog.Printf("failed to go fish %s: %s", name, err)
		return "", true
	}
	links, _ := j.GetArray("links")
	for _, li := range links {
		l, ok := li.(junk.Junk)
		if !ok {
			continue
		}
		href, _ := l.GetString("href")
		rel, _ := l.GetString("rel")
		t, _ := l.GetString("type")
		if rel == "self" && isActivityStreamsMediaType(t) {
			if _, err := stmtFriendlyNameSetHref.Exec(name, href); err != nil {
				elog.Printf("error saving fishname: %s", err)
			}
			return href, true
		}
	}
	return href, true
}, Duration: 1 * time.Minute})

func gofish(name string) string {
	if name[0] == '@' {
		name = name[1:]
	}
	var href string
	handfull.Get(name, &href)
	return href
}

func investigate(name string) (*SomeThing, error) {
	if name == "" {
		return nil, fmt.Errorf("no name")
	}
	if name[0] == '@' {
		name = gofish(name)
	}
	if name == "" {
		return nil, fmt.Errorf("no name")
	}
	obj, err := getAndParseShortTimeout(serverUID, name)
	if err != nil {
		return nil, err
	}
	allinjest(originate(name), obj)
	return somethingabout(obj)
}

func somethingabout(obj junk.Junk) (*SomeThing, error) {
	info := new(SomeThing)
	t, _ := obj.GetString("type")
	switch t {
	case "Person", "Organization", "Application", "Service":
		info.What = SomeActor
	case "OrderedCollection", "Collection":
		info.What = SomeCollection
	default:
		return nil, fmt.Errorf("unknown object type")
	}
	info.XID, _ = obj.GetString("id")
	info.Name, _ = obj.GetString("preferredUsername")
	if info.Name == "" {
		info.Name, _ = obj.GetString("name")
	}
	info.Owner, _ = obj.GetString("attributedTo")
	if info.Owner == "" {
		info.Owner = info.XID
	}

	iconInfo, ok := obj.GetMap("icon")
	if ok {
		mType, _ := iconInfo.GetString("mediaType")
		if strings.HasPrefix(mType, "image/") {
			AvatarUrl, ok := iconInfo.GetString("url")
			if ok {
				info.AvatarURL = AvatarUrl
			}
		}
	}

	return info, nil
}

func allinjest(origin string, obj junk.Junk) {
	keyobj, ok := obj.GetMap("publicKey")
	if ok {
		ingestpubkey(origin, keyobj)
	}
	ingestboxes(origin, obj)
	ingestPreferredUsername(origin, obj)
}

func ingestpubkey(origin string, obj junk.Junk) {
	keyobj, ok := obj.GetMap("publicKey")
	if ok {
		obj = keyobj
	}
	keyname, ok := obj.GetString("id")

	var pubkey string
	if err := stmtActorGetPubkey.QueryRow(keyname).Scan(&pubkey); err == nil {
		return
	}
	if !ok || origin != originate(keyname) {
		ilog.Printf("bad key origin %s <> %s", origin, keyname)
		return
	}
	dlog.Printf("ingesting a needed pubkey: %s", keyname)
	owner, ok := obj.GetString("owner")
	if !ok {
		ilog.Printf("error finding %s pubkey owner", keyname)
		return
	}
	data, ok := obj.GetString("publicKeyPem")
	if !ok {
		ilog.Printf("error finding %s pubkey", keyname)
		return
	}
	if originate(owner) != origin {
		ilog.Printf("bad key owner: %s <> %s", owner, origin)
		return
	}
	_, _, err := httpsig.DecodeKey(data)
	if err != nil {
		ilog.Printf("error decoding %s pubkey: %s", keyname, err)
		return
	}
	when := time.Now().UTC().Format(dbtimeformat)
	if _, err := stmtActorSetPubkey.Exec(keyname, data, when); err != nil {
		elog.Printf("error saving key: %s", err)
	}
}

func ingestboxes(origin string, obj junk.Junk) {
	ident, _ := obj.GetString("id")
	if ident == "" {
		return
	}
	if originate(ident) != origin {
		return
	}
	var countBoxes int
	if err := stmtActorHasBoxes.QueryRow(ident, "boxes").Scan(&countBoxes); err == nil && countBoxes == 1 {
		return
	}
	dlog.Printf("ingesting boxes: %s", ident)
	inbox, _ := obj.GetString("inbox")
	outbox, _ := obj.GetString("outbox")
	sbox, _ := obj.GetString("endpoints", "sharedInbox")
	if inbox != "" {
		when := time.Now().UTC().Format(dbtimeformat)
		if _, err := stmtActorSetBoxes.Exec(ident, when, inbox, outbox, sbox); err != nil {
			elog.Printf("error saving boxes: %s", err)
		}
	}
}

func ingestPreferredUsername(origin string, obj junk.Junk) {
	xid, _ := obj.GetString("id")
	if xid == "" {
		return
	}
	if originate(xid) != origin {
		return
	}
	var preferredUsername string
	if err := stmtPreferredUsernameGet.QueryRow(xid).Scan(&preferredUsername); err == nil {
		return
	}
	preferredUsername, _ = obj.GetString("preferredUsername")
	if preferredUsername != "" {
		if _, err := stmtPreferredUsernameSet.Exec(xid, preferredUsername); err != nil {
			elog.Printf("error saving preferred username: %s", err)
		}
	}
}

func updateMe(username string) {
	var user *UserProfile
	usersCacheByName.Get(username, &user)
	dt := time.Now().UTC().Format(time.RFC3339)
	j := tj.O{
		"@context":  atContextString,
		"id":        fmt.Sprintf("%s/upme/%s/%d", user.URL, user.Name, time.Now().Unix()),
		"actor":     user.URL,
		"published": dt,
		"to":        activitystreamsPublicString,
		"type":      "Update",
		"object":    serializeUser(user),
	}

	msg := must.OK1(json.Marshal(j))

	rcpts := make(map[string]bool)
	for _, f := range getdubs(user.ID) {
		if f.XID == user.URL {
			continue
		}
		var box *Box
		boxofboxes.Get(f.XID, &box)
		if box != nil && box.Shared != "" {
			rcpts["%"+box.Shared] = true
		} else {
			rcpts[f.XID] = true
		}
	}
	for a := range rcpts {
		go deliverate(0, user.ID, a, msg, false)
	}
}

func followme(user *UserProfile, who string, name string, j junk.Junk) {
	folxid, _ := j.GetString("id")

	log.Printf("updating author follow: %s %s", who, folxid)

	var x string
	db := opendatabase()
	row := db.QueryRow("select xid from authors where name = ? and xid = ? and userid = ? and flavor in ('dub', 'undub')", name, who, user.ID)
	err := row.Scan(&x)
	if err != sql.ErrNoRows {
		ilog.Printf("duplicate follow request: %s", who)
		_, err = stmtUpdateFlavor.Exec("dub", folxid, user.ID, name, who, "undub")
		if err != nil {
			elog.Printf("error updating author: %s", err)
		}
	} else {
		stmtSaveDub.Exec(user.ID, name, who, "dub", folxid)
	}
	go rubadubdub(user, j)
}

func unfollowme(user *UserProfile, who string, name string, j junk.Junk) {
	var folxid string
	if who == "" {
		folxid, _ = j.GetString("object")

		db := opendatabase()
		row := db.QueryRow("select xid, name from authors where userid = ? and folxid = ? and flavor in ('dub', 'undub')", user.ID, folxid)
		err := row.Scan(&who, &name)
		if err != nil {
			if err != sql.ErrNoRows {
				elog.Printf("error scanning authors: %s", err)
			}
			return
		}
	}

	ilog.Printf("updating author undo: %s %s", who, folxid)
	_, err := stmtUpdateFlavor.Exec("undub", folxid, user.ID, name, who, "dub")
	if err != nil {
		elog.Printf("error updating author: %s", err)
		return
	}
}

func followyou(user *UserProfile, authorID int64) {
	var url, owner string
	db := opendatabase()
	row := db.QueryRow("select xid, owner from authors where authorID = ? and userid = ? and flavor in ('unsub', 'peep', 'presub', 'sub')",
		authorID, user.ID)
	err := row.Scan(&url, &owner)
	if err != nil {
		elog.Printf("can't get author xid: %s", err)
		return
	}
	folxid := make18CharRandomString()
	ilog.Printf("subscribing to %s", url)
	_, err = db.Exec("update authors set flavor = ?, folxid = ? where authorID = ?", "presub", folxid, authorID)
	if err != nil {
		elog.Printf("error updating author: %s", err)
		return
	}
	go subsub(user, url, owner, folxid)

}
func unfollowyou(user *UserProfile, authorID int64) {
	db := opendatabase()
	row := db.QueryRow("select xid, owner, folxid from authors where authorID = ? and userid = ? and flavor in ('sub')",
		authorID, user.ID)
	var url, owner, folxid string
	err := row.Scan(&url, &owner, &folxid)
	if err != nil {
		elog.Printf("can't get author xid: %s", err)
		return
	}
	ilog.Printf("unsubscribing from %s", url)
	_, err = db.Exec("update authors set flavor = ? where authorID = ?", "unsub", authorID)
	if err != nil {
		elog.Printf("error updating author: %s", err)
		return
	}
	go sendUndo(user, url, owner, folxid)
}

func followyou2(user *UserProfile, j junk.Junk) {
	who, _ := j.GetString("actor")

	ilog.Printf("updating author accept: %s", who)
	db := opendatabase()
	row := db.QueryRow("select name, folxid from authors where userid = ? and xid = ? and flavor in ('presub')",
		user.ID, who)
	var name, folxid string
	err := row.Scan(&name, &folxid)
	if err != nil {
		elog.Printf("can't get author name: %s", err)
		return
	}
	_, err = stmtUpdateFlavor.Exec("sub", folxid, user.ID, name, who, "presub")
	if err != nil {
		elog.Printf("error updating author: %s", err)
		return
	}
}

func nofollowyou2(user *UserProfile, j junk.Junk) {
	who, _ := j.GetString("actor")

	ilog.Printf("updating author reject: %s", who)
	db := opendatabase()
	row := db.QueryRow("select name, folxid from authors where userid = ? and xid = ? and flavor in ('presub', 'sub')",
		user.ID, who)
	var name, folxid string
	err := row.Scan(&name, &folxid)
	if err != nil {
		elog.Printf("can't get author name: %s", err)
		return
	}
	_, err = stmtUpdateFlavor.Exec("unsub", folxid, user.ID, name, who, "presub")
	_, err = stmtUpdateFlavor.Exec("unsub", folxid, user.ID, name, who, "sub")
	if err != nil {
		elog.Printf("error updating author: %s", err)
		return
	}
}
