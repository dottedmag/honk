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
	"crypto/sha512"
	"database/sql"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"humungus.tedunangst.com/r/webs/cache"
	"humungus.tedunangst.com/r/webs/httpsig"
	"humungus.tedunangst.com/r/webs/login"
	"humungus.tedunangst.com/r/webs/mz"
)

func userfromrow(row *sql.Row) (*UserProfile, error) {
	user := new(UserProfile)
	var seckey, options string
	err := row.Scan(&user.ID, &user.Name, &user.Display, &user.About, &user.Key, &seckey, &options)
	if err == nil {
		user.SecKey, _, err = httpsig.DecodeKey(seckey)
	}
	if err != nil {
		return nil, err
	}
	if user.ID > 0 {
		user.URL = fmt.Sprintf("https://%s/%s/%s", serverName, userSep, user.Name)
		err = json.Unmarshal([]byte(options), &user.Options)
		if err != nil {
			elog.Printf("error processing user options: %s", err)
		}
	} else {
		user.URL = fmt.Sprintf("https://%s/%s", serverName, user.Name)
	}
	if user.Options.Reaction == "" {
		user.Options.Reaction = "none"
	}

	return user, nil
}

var somenamedusers = cache.New(cache.Options{Filler: func(name string) (*UserProfile, bool) {
	row := stmtUserByName.QueryRow(name)
	user, err := userfromrow(row)
	if err != nil {
		return nil, false
	}
	var marker mz.Marker
	marker.HashLinker = ontoreplacer
	marker.AtLinker = attoreplacer
	user.HTAbout = template.HTML(marker.Mark(user.About))
	user.Onts = marker.HashTags
	return user, true
}})

var somenumberedusers = cache.New(cache.Options{Filler: func(userid int64) (*UserProfile, bool) {
	row := stmtUserByNumber.QueryRow(userid)
	user, err := userfromrow(row)
	if err != nil {
		return nil, false
	}
	// don't touch attoreplacer, which introduces a loop
	// finger -> getjunk -> keys -> users
	return user, true
}})

func getserveruser() *UserProfile {
	var user *UserProfile
	ok := somenumberedusers.Get(serverUID, &user)
	if !ok {
		elog.Panicf("lost server user")
	}
	return user
}

func getUserBio(name string) (*UserProfile, error) {
	var user *UserProfile
	ok := somenamedusers.Get(name, &user)
	if !ok {
		return nil, fmt.Errorf("no user: %s", name)
	}
	return user, nil
}

var honkerinvalidator cache.Invalidator

func gethonkers(userid int64) []*Honker {
	rows, err := stmtHonkers.Query(userid)
	if err != nil {
		elog.Printf("error querying honkers: %s", err)
		return nil
	}
	defer rows.Close()
	var honkers []*Honker
	for rows.Next() {
		h := new(Honker)
		var combos, meta string
		err = rows.Scan(&h.ID, &h.UserID, &h.Name, &h.XID, &h.Flavor, &combos, &meta)
		if err == nil {
			err = json.Unmarshal([]byte(meta), &h.Meta)
		}
		if err != nil {
			elog.Printf("error scanning honker: %s", err)
			continue
		}
		h.Combos = strings.Split(strings.TrimSpace(combos), " ")
		honkers = append(honkers, h)
	}
	return honkers
}

func getdubs(userid int64) []*Honker {
	rows, err := stmtDubbers.Query(userid)
	return dubsfromrows(rows, err)
}

func getnameddubs(userid int64, name string) []*Honker {
	rows, err := stmtNamedDubbers.Query(userid, name)
	return dubsfromrows(rows, err)
}

func dubsfromrows(rows *sql.Rows, err error) []*Honker {
	if err != nil {
		elog.Printf("error querying dubs: %s", err)
		return nil
	}
	defer rows.Close()
	var honkers []*Honker
	for rows.Next() {
		h := new(Honker)
		err = rows.Scan(&h.ID, &h.UserID, &h.Name, &h.XID, &h.Flavor)
		if err != nil {
			elog.Printf("error scanning honker: %s", err)
			return nil
		}
		honkers = append(honkers, h)
	}
	return honkers
}

func allusers() []login.UserInfo {
	var users []login.UserInfo
	rows, _ := opendatabase().Query("select userid, username from users where userid > 0")
	defer rows.Close()
	for rows.Next() {
		var u login.UserInfo
		rows.Scan(&u.UserID, &u.Username)
		users = append(users, u)
	}
	return users
}

func getActivityPubActivity(userid int64, xid string) *ActivityPubActivity {
	row := stmtOneActivityPubActivity.QueryRow(userid, xid)
	return scanhonk(row)
}

func getShare(userid int64, xid string) *ActivityPubActivity {
	row := stmtOneShare.QueryRow(userid, xid)
	return scanhonk(row)
}

func getpublichonks() []*ActivityPubActivity {
	dt := getRetentionTimeForDB()
	rows, err := stmtPublicHonks.Query(dt, 100)
	return getsomehonks(rows, err)
}
func geteventhonks(userid int64) []*ActivityPubActivity {
	rows, err := stmtEventHonks.Query(userid, 25)
	honks := getsomehonks(rows, err)
	sort.Slice(honks, func(i, j int) bool {
		var t1, t2 time.Time
		if honks[i].Time == nil {
			t1 = honks[i].Date
		} else {
			t1 = honks[i].Time.StartTime
		}
		if honks[j].Time == nil {
			t2 = honks[j].Date
		} else {
			t2 = honks[j].Time.StartTime
		}
		return t1.After(t2)
	})
	now := time.Now().Add(-24 * time.Hour)
	for i, h := range honks {
		t := h.Date
		if tm := h.Time; tm != nil {
			t = tm.StartTime
		}
		if t.Before(now) {
			honks = honks[:i]
			break
		}
	}
	reverseSlice(honks)
	return honks
}

var publicRetention = flag.Int("display.days", 7, "how many days you want to show in the outbox/user/etc")

func gethonksbyuser(name string, includeprivate bool, wanted int64) []*ActivityPubActivity {
	dt := getRetentionTimeForDB()
	limit := 50
	whofore := 2
	if includeprivate {
		whofore = 3
	}
	rows, err := stmtUserHonks.Query(wanted, whofore, name, dt, limit)
	return getsomehonks(rows, err)
}

func getRetentionTimeForDB() string {
	dt := time.Now().Add(time.Duration(*publicRetention*-1) * 24 * time.Hour).UTC().Format(dbtimeformat)
	return dt
}

func gethonksforuser(userid int64, wanted int64) []*ActivityPubActivity {
	dt := getRetentionTimeForDB()
	rows, err := stmtHonksForUser.Query(wanted, userid, dt, userid, userid)
	return getsomehonks(rows, err)
}
func gethonksforuserfirstclass(userid int64, wanted int64) []*ActivityPubActivity {
	dt := getRetentionTimeForDB()
	rows, err := stmtHonksForUserFirstClass.Query(wanted, userid, dt, userid, userid)
	return getsomehonks(rows, err)
}

func gethonksforme(userid int64, wanted int64) []*ActivityPubActivity {
	dt := getRetentionTimeForDB()
	rows, err := stmtHonksForMe.Query(wanted, userid, dt, userid)
	return getsomehonks(rows, err)
}
func gethonksfromlongago(userid int64, wanted int64) []*ActivityPubActivity {
	now := time.Now()
	var honks []*ActivityPubActivity
	for i := 1; i <= 3; i++ {
		dt := time.Date(now.Year()-i, now.Month(), now.Day(), now.Hour(), now.Minute(),
			now.Second(), 0, now.Location())
		dt1 := dt.Add(-36 * time.Hour).UTC().Format(dbtimeformat)
		dt2 := dt.Add(12 * time.Hour).UTC().Format(dbtimeformat)
		rows, err := stmtHonksFromLongAgo.Query(wanted, userid, dt1, dt2, userid)
		honks = append(honks, getsomehonks(rows, err)...)
	}
	return honks
}
func getsavedhonks(userid int64, wanted int64) []*ActivityPubActivity {
	rows, err := stmtHonksISaved.Query(wanted, userid)
	return getsomehonks(rows, err)
}
func gethonksbyhonker(userid int64, honker string, wanted int64) []*ActivityPubActivity {
	rows, err := stmtHonksByHonker.Query(wanted, userid, honker, userid)
	return getsomehonks(rows, err)
}
func gethonksbyxonker(userid int64, xonker string, wanted int64) []*ActivityPubActivity {
	rows, err := stmtHonksByXonker.Query(wanted, userid, xonker, xonker, userid)
	return getsomehonks(rows, err)
}
func gethonksbycombo(userid int64, combo string, wanted int64) []*ActivityPubActivity {
	combo = "% " + combo + " %"
	rows, err := stmtHonksByCombo.Query(wanted, userid, userid, combo, userid, wanted, userid, combo, userid)
	return getsomehonks(rows, err)
}
func gethonksbyThread(userid int64, thread string, wanted int64) []*ActivityPubActivity {
	rows, err := stmtHonksByThread.Query(wanted, userid, userid, thread)
	honks := getsomehonks(rows, err)
	return honks
}
func gethonksbysearch(userid int64, q string, wanted int64) []*ActivityPubActivity {
	var queries []string
	var params []interface{}
	queries = append(queries, "honks.honkid > ?")
	params = append(params, wanted)
	queries = append(queries, "honks.userid = ?")
	params = append(params, userid)

	terms := strings.Split(q, " ")
	for _, t := range terms {
		if t == "" {
			continue
		}
		negate := " "
		if t[0] == '-' {
			t = t[1:]
			negate = " not "
		}
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "site:") {
			site := t[5:]
			site = "%" + site + "%"
			queries = append(queries, "xid"+negate+"like ?")
			params = append(params, site)
			continue
		}
		if strings.HasPrefix(t, "honker:") {
			honker := t[7:]
			xid := fullname(honker, userid)
			if xid != "" {
				honker = xid
			}
			queries = append(queries, negate+"(honks.honker = ? or honks.oonker = ?)")
			params = append(params, honker)
			params = append(params, honker)
			continue
		}
		t = "%" + t + "%"
		queries = append(queries, "noise"+negate+"like ?")
		params = append(params, t)
	}

	selecthonks := "select honks.honkid, honks.userid, username, what, honker, oonker, honks.xid, rid, dt, url, audience, noise, precis, format, thread, whofore, flags from honks join users on honks.userid = users.userid "
	where := "where " + strings.Join(queries, " and ")
	butnotthose := " and thread not in (select name from zonkers where userid = ? and wherefore = 'mute-thread' order by zonkerid desc limit 100)"
	limit := " order by honks.honkid desc limit 250"
	params = append(params, userid)
	rows, err := opendatabase().Query(selecthonks+where+butnotthose+limit, params...)
	honks := getsomehonks(rows, err)
	return honks
}
func gethonksbyontology(userid int64, name string, wanted int64) []*ActivityPubActivity {
	rows, err := stmtHonksByOntology.Query(wanted, name, userid, userid)
	honks := getsomehonks(rows, err)
	return honks
}

func reverseSlice[T any](slice []T) {
	for i, j := 0, len(slice)-1; i < j; i, j = i+1, j-1 {
		slice[i], slice[j] = slice[j], slice[i]
	}
}

func getsomehonks(rows *sql.Rows, err error) []*ActivityPubActivity {
	if err != nil {
		elog.Printf("error querying honks: %s", err)
		return nil
	}
	defer rows.Close()
	var honks []*ActivityPubActivity
	for rows.Next() {
		h := scanhonk(rows)
		if h != nil {
			honks = append(honks, h)
		}
	}
	rows.Close()
	attachmentsForHonks(honks)
	return honks
}

type RowLike interface {
	Scan(dest ...interface{}) error
}

func scanhonk(row RowLike) *ActivityPubActivity {
	h := new(ActivityPubActivity)
	var dt, aud string
	err := row.Scan(&h.ID, &h.UserID, &h.Username, &h.What, &h.Honker, &h.Oonker, &h.XID, &h.RID,
		&dt, &h.URL, &aud, &h.Noise, &h.Precis, &h.Format, &h.Thread, &h.Whofore, &h.Flags)
	if err != nil {
		if err != sql.ErrNoRows {
			elog.Printf("error scanning honk: %s", err)
		}
		return nil
	}
	h.Date, _ = time.Parse(dbtimeformat, dt)
	h.Audience = strings.Split(aud, " ")
	h.Public = loudandproud(h.Audience)
	return h
}

func attachmentsForHonks(honks []*ActivityPubActivity) {
	db := opendatabase()
	var ids []string
	hmap := make(map[int64]*ActivityPubActivity)
	for _, h := range honks {
		ids = append(ids, fmt.Sprintf("%d", h.ID))
		hmap[h.ID] = h
	}
	idset := strings.Join(ids, ",")
	// grab attachments
	q := fmt.Sprintf("select honkid, attachments.fileid, xid, name, description, url, media, local from attachments join filemeta on attachments.fileid = filemeta.fileid where honkid in (%s)", idset)
	rows, err := db.Query(q)
	if err != nil {
		elog.Printf("error querying attachments: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var hid int64
		d := new(Attachment)
		err = rows.Scan(&hid, &d.FileID, &d.XID, &d.Name, &d.Desc, &d.URL, &d.Media, &d.Local)
		if err != nil {
			elog.Printf("error scanning attachment: %s", err)
			continue
		}
		d.External = !strings.HasPrefix(d.URL, serverPrefix)
		h := hmap[hid]
		h.Attachments = append(h.Attachments, d)
	}
	rows.Close()

	// grab onts
	q = fmt.Sprintf("select honkid, ontology from onts where honkid in (%s)", idset)
	rows, err = db.Query(q)
	if err != nil {
		elog.Printf("error querying onts: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var hid int64
		var o string
		err = rows.Scan(&hid, &o)
		if err != nil {
			elog.Printf("error scanning attachment: %s", err)
			continue
		}
		h := hmap[hid]
		h.Onts = append(h.Onts, o)
	}
	rows.Close()

	// grab meta
	q = fmt.Sprintf("select honkid, genus, json from honkmeta where honkid in (%s)", idset)
	rows, err = db.Query(q)
	if err != nil {
		elog.Printf("error querying honkmeta: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var hid int64
		var genus, j string
		err = rows.Scan(&hid, &genus, &j)
		if err != nil {
			elog.Printf("error scanning honkmeta: %s", err)
			continue
		}
		h := hmap[hid]
		switch genus {
		case "place":
			p := new(Place)
			err = json.Unmarshal([]byte(j), p)
			if err != nil {
				elog.Printf("error parsing place: %s", err)
				continue
			}
			h.Place = p
		case "time":
			t := new(Time)
			err = json.Unmarshal([]byte(j), t)
			if err != nil {
				elog.Printf("error parsing time: %s", err)
				continue
			}
			h.Time = t
		case "mentions":
			err = json.Unmarshal([]byte(j), &h.Mentions)
			if err != nil {
				elog.Printf("error parsing mentions: %s", err)
				continue
			}
		case "reactions":
			err = json.Unmarshal([]byte(j), &h.Reactions)
			if err != nil {
				elog.Printf("error parsing reactions: %s", err)
				continue
			}
		case "guesses":
			h.Guesses = template.HTML(j)
		case "oldrev":
		default:
			elog.Printf("unknown meta genus: %s", genus)
		}
	}
	rows.Close()
}

func attachmentsForChonks(chonks []*Chonk) {
	db := opendatabase()
	var ids []string
	chmap := make(map[int64]*Chonk)
	for _, ch := range chonks {
		ids = append(ids, fmt.Sprintf("%d", ch.ID))
		chmap[ch.ID] = ch
	}
	idset := strings.Join(ids, ",")
	// grab attachments
	q := fmt.Sprintf("select chonkid, attachments.fileid, xid, name, description, url, media, local from attachments join filemeta on attachments.fileid = filemeta.fileid where chonkid in (%s)", idset)
	rows, err := db.Query(q)
	if err != nil {
		elog.Printf("error querying attachments: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var chid int64
		d := new(Attachment)
		err = rows.Scan(&chid, &d.FileID, &d.XID, &d.Name, &d.Desc, &d.URL, &d.Media, &d.Local)
		if err != nil {
			elog.Printf("error scanning attachment: %s", err)
			continue
		}
		ch := chmap[chid]
		ch.Attachments = append(ch.Attachments, d)
	}
}

func savefile(name string, desc string, url string, media string, local bool, data []byte) (int64, error) {
	fileid, _, err := savefileandxid(name, desc, url, media, local, data)
	return fileid, err
}

func hashfiledata(data []byte) string {
	h := sha512.New512_256()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func savefileandxid(name string, desc string, url string, media string, local bool, data []byte) (int64, string, error) {
	var xid string
	if local {
		hash := hashfiledata(data)
		row := stmtCheckFileData.QueryRow(hash)
		err := row.Scan(&xid)
		if err == sql.ErrNoRows {
			xid = make18CharRandomString()
			switch media {
			case "image/png":
				xid += ".png"
			case "image/jpeg":
				xid += ".jpg"
			case "application/pdf":
				xid += ".pdf"
			case "text/plain":
				xid += ".txt"
			}
			_, err = stmtSaveFileData.Exec(xid, media, hash, data)
			if err != nil {
				return 0, "", err
			}
		} else if err != nil {
			elog.Printf("error checking file hash: %s", err)
			return 0, "", err
		}
		if url == "" {
			url = fmt.Sprintf("https://%s/d/%s", serverName, xid)
		}
	}

	res, err := stmtSaveFile.Exec(xid, name, desc, url, media, local)
	if err != nil {
		return 0, "", err
	}
	fileid, _ := res.LastInsertId()
	return fileid, xid, nil
}

func findAttachment(url string) *Attachment {
	attachment := new(Attachment)
	row := stmtFindFile.QueryRow(url)
	err := row.Scan(&attachment.FileID, &attachment.XID)
	if err == nil {
		return attachment
	}
	if err != sql.ErrNoRows {
		elog.Printf("error finding file: %s", err)
	}
	return nil
}

func savechonk(ch *Chonk) error {
	dt := ch.Date.UTC().Format(dbtimeformat)
	db := opendatabase()
	tx, err := db.Begin()
	if err != nil {
		elog.Printf("can't begin tx: %s", err)
		return err
	}

	res, err := tx.Stmt(stmtSaveChonk).Exec(ch.UserID, ch.XID, ch.Who, ch.Target, dt, ch.Noise, ch.Format)
	if err == nil {
		ch.ID, _ = res.LastInsertId()
		for _, d := range ch.Attachments {
			_, err := tx.Stmt(stmtSaveAttachment).Exec(-1, ch.ID, d.FileID)
			if err != nil {
				elog.Printf("error saving attachment: %s", err)
				break
			}
		}
		chatplusone(tx, ch.UserID)
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	return err
}

func chatplusone(tx *sql.Tx, userid int64) {
	var user *UserProfile
	ok := somenumberedusers.Get(userid, &user)
	if !ok {
		return
	}
	options := user.Options
	options.ChatCount += 1
	j, err := encodeJson(options)
	if err == nil {
		_, err = tx.Exec("update users set options = ? where username = ?", j, user.Name)
	}
	if err != nil {
		elog.Printf("error plussing chat: %s", err)
	}
	somenamedusers.Clear(user.Name)
	somenumberedusers.Clear(user.ID)
}

func chatnewnone(userid int64) {
	var user *UserProfile
	ok := somenumberedusers.Get(userid, &user)
	if !ok || user.Options.ChatCount == 0 {
		return
	}
	options := user.Options
	options.ChatCount = 0
	j, err := encodeJson(options)
	if err == nil {
		db := opendatabase()
		_, err = db.Exec("update users set options = ? where username = ?", j, user.Name)
	}
	if err != nil {
		elog.Printf("error noneing chat: %s", err)
	}
	somenamedusers.Clear(user.Name)
	somenumberedusers.Clear(user.ID)
}

func meplusone(tx *sql.Tx, userid int64) {
	var user *UserProfile
	ok := somenumberedusers.Get(userid, &user)
	if !ok {
		return
	}
	options := user.Options
	options.MeCount += 1
	j, err := encodeJson(options)
	if err == nil {
		_, err = tx.Exec("update users set options = ? where username = ?", j, user.Name)
	}
	if err != nil {
		elog.Printf("error plussing me: %s", err)
	}
	somenamedusers.Clear(user.Name)
	somenumberedusers.Clear(user.ID)
}

func menewnone(userid int64) {
	var user *UserProfile
	ok := somenumberedusers.Get(userid, &user)
	if !ok || user.Options.MeCount == 0 {
		return
	}
	options := user.Options
	options.MeCount = 0
	j, err := encodeJson(options)
	if err == nil {
		db := opendatabase()
		_, err = db.Exec("update users set options = ? where username = ?", j, user.Name)
	}
	if err != nil {
		elog.Printf("error noneing me: %s", err)
	}
	somenamedusers.Clear(user.Name)
	somenumberedusers.Clear(user.ID)
}

func loadchatter(userid int64) []*Chatter {
	duedt := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(dbtimeformat)
	rows, err := stmtLoadChonks.Query(userid, duedt)
	if err != nil {
		elog.Printf("error loading chonks: %s", err)
		return nil
	}
	defer rows.Close()
	chonks := make(map[string][]*Chonk)
	var allchonks []*Chonk
	for rows.Next() {
		ch := new(Chonk)
		var dt string
		err = rows.Scan(&ch.ID, &ch.UserID, &ch.XID, &ch.Who, &ch.Target, &dt, &ch.Noise, &ch.Format)
		if err != nil {
			elog.Printf("error scanning chonk: %s", err)
			continue
		}
		ch.Date, _ = time.Parse(dbtimeformat, dt)
		chonks[ch.Target] = append(chonks[ch.Target], ch)
		allchonks = append(allchonks, ch)
	}
	attachmentsForChonks(allchonks)
	rows.Close()
	rows, err = stmtGetChatters.Query(userid)
	if err != nil {
		elog.Printf("error getting chatters: %s", err)
		return nil
	}
	for rows.Next() {
		var target string
		err = rows.Scan(&target)
		if err != nil {
			elog.Printf("error scanning chatter: %s", target)
			continue
		}
		if _, ok := chonks[target]; !ok {
			chonks[target] = []*Chonk{}

		}
	}
	var chatter []*Chatter
	for target, chonks := range chonks {
		chatter = append(chatter, &Chatter{
			Target: target,
			Chonks: chonks,
		})
	}
	sort.Slice(chatter, func(i, j int) bool {
		a, b := chatter[i], chatter[j]
		if len(a.Chonks) == 0 || len(b.Chonks) == 0 {
			if len(a.Chonks) == len(b.Chonks) {
				return a.Target < b.Target
			}
			return len(a.Chonks) > len(b.Chonks)
		}
		return a.Chonks[len(a.Chonks)-1].Date.After(b.Chonks[len(b.Chonks)-1].Date)
	})

	return chatter
}

func savehonk(h *ActivityPubActivity) error {
	dt := h.Date.UTC().Format(dbtimeformat)
	aud := strings.Join(h.Audience, " ")

	db := opendatabase()
	tx, err := db.Begin()
	if err != nil {
		elog.Printf("can't begin tx: %s", err)
		return err
	}

	res, err := tx.Stmt(stmtSaveHonk).Exec(h.UserID, h.What, h.Honker, h.XID, h.RID, dt, h.URL,
		aud, h.Noise, h.Thread, h.Whofore, h.Format, h.Precis,
		h.Oonker, h.Flags)
	if err == nil {
		h.ID, _ = res.LastInsertId()
		err = saveextras(tx, h)
	}
	if err == nil {
		if h.Whofore == 1 {
			meplusone(tx, h.UserID)
		}
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		elog.Printf("error saving honk: %s", err)
	}
	honkhonkline()
	return err
}

func updateHonk(h *ActivityPubActivity) error {
	old := getActivityPubActivity(h.UserID, h.XID)
	oldrev := OldRevision{Precis: old.Precis, Noise: old.Noise}
	dt := h.Date.UTC().Format(dbtimeformat)

	db := opendatabase()
	tx, err := db.Begin()
	if err != nil {
		elog.Printf("can't begin tx: %s", err)
		return err
	}

	err = deleteextras(tx, h.ID, false)
	if err == nil {
		_, err = tx.Stmt(stmtUpdateHonk).Exec(h.Precis, h.Noise, h.Format, h.Whofore, dt, h.ID)
	}
	if err == nil {
		err = saveextras(tx, h)
	}
	if err == nil {
		var j string
		j, err = encodeJson(&oldrev)
		if err == nil {
			_, err = tx.Stmt(stmtSaveMeta).Exec(old.ID, "oldrev", j)
		}
		if err != nil {
			elog.Printf("error saving oldrev: %s", err)
		}
	}
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		elog.Printf("error updating honk %d: %s", h.ID, err)
	}
	return err
}

func deleteHonk(honkid int64) error {
	db := opendatabase()
	tx, err := db.Begin()
	if err != nil {
		elog.Printf("can't begin tx: %s", err)
		return err
	}

	err = deleteextras(tx, honkid, true)
	if err == nil {
		_, err = tx.Stmt(stmtDeleteHonk).Exec(honkid)
	}
	if err == nil {
		err = tx.Commit()
	} else {
		tx.Rollback()
	}
	if err != nil {
		elog.Printf("error deleting honk %d: %s", honkid, err)
	}
	return err
}

func saveextras(tx *sql.Tx, h *ActivityPubActivity) error {
	for _, d := range h.Attachments {
		_, err := tx.Stmt(stmtSaveAttachment).Exec(h.ID, -1, d.FileID)
		if err != nil {
			elog.Printf("error saving attachment: %s", err)
			return err
		}
	}
	for _, o := range h.Onts {
		_, err := tx.Stmt(stmtSaveOnt).Exec(strings.ToLower(o), h.ID)
		if err != nil {
			elog.Printf("error saving ont: %s", err)
			return err
		}
	}
	if p := h.Place; p != nil {
		j, err := encodeJson(p)
		if err == nil {
			_, err = tx.Stmt(stmtSaveMeta).Exec(h.ID, "place", j)
		}
		if err != nil {
			elog.Printf("error saving place: %s", err)
			return err
		}
	}
	if t := h.Time; t != nil {
		j, err := encodeJson(t)
		if err == nil {
			_, err = tx.Stmt(stmtSaveMeta).Exec(h.ID, "time", j)
		}
		if err != nil {
			elog.Printf("error saving time: %s", err)
			return err
		}
	}
	if m := h.Mentions; len(m) > 0 {
		j, err := encodeJson(m)
		if err == nil {
			_, err = tx.Stmt(stmtSaveMeta).Exec(h.ID, "mentions", j)
		}
		if err != nil {
			elog.Printf("error saving mentions: %s", err)
			return err
		}
	}
	if g := h.Guesses; g != "" {
		_, err := tx.Stmt(stmtSaveMeta).Exec(h.ID, "guesses", g)
		if err != nil {
			elog.Printf("error saving guesses: %s", err)
			return err
		}
	}
	return nil
}

var reactionLock sync.Mutex

func addReaction(user *UserProfile, xid string, who, react string) {
	reactionLock.Lock()
	defer reactionLock.Unlock()
	h := getActivityPubActivity(user.ID, xid)
	if h == nil {
		return
	}
	h.Reactions = append(h.Reactions, Reaction{Who: who, What: react})
	j, _ := encodeJson(h.Reactions)
	db := opendatabase()
	tx, _ := db.Begin()
	_, _ = tx.Stmt(stmtDeleteOneMeta).Exec(h.ID, "reactions")
	_, _ = tx.Stmt(stmtSaveMeta).Exec(h.ID, "reactions", j)
	tx.Commit()
}

func deleteextras(tx *sql.Tx, honkid int64, everything bool) error {
	_, err := tx.Stmt(stmtDeleteAttachments).Exec(honkid)
	if err != nil {
		return err
	}
	_, err = tx.Stmt(stmtDeleteOnts).Exec(honkid)
	if err != nil {
		return err
	}
	if everything {
		_, err = tx.Stmt(stmtDeleteAllMeta).Exec(honkid)
	} else {
		_, err = tx.Stmt(stmtDeleteSomeMeta).Exec(honkid)
	}
	if err != nil {
		return err
	}
	return nil
}

func encodeJson(what interface{}) (string, error) {
	var buf bytes.Buffer
	e := json.NewEncoder(&buf)
	e.SetEscapeHTML(false)
	e.SetIndent("", "")
	err := e.Encode(what)
	return buf.String(), err
}

func getxonker(what, flav string) string {
	var res string
	row := stmtGetXonker.QueryRow(what, flav)
	row.Scan(&res)
	return res
}

func savexonker(what, value, flav, when string) {
	stmtSaveXonker.Exec(what, value, flav, when)
}

func savehonker(user *UserProfile, url, name, flavor, combos, mj string) error {
	var owner string
	if url[0] == '#' {
		flavor = "peep"
		if name == "" {
			name = url[1:]
		}
		owner = url
	} else {
		info, err := investigate(url)
		if err != nil {
			ilog.Printf("failed to investigate honker: %s", err)
			return err
		}
		url = info.XID
		if name == "" {
			name = info.Name
		}
		owner = info.Owner
	}

	var x string
	db := opendatabase()
	row := db.QueryRow("select xid from honkers where xid = ? and userid = ? and flavor in ('sub', 'unsub', 'peep')", url, user.ID)
	err := row.Scan(&x)
	if err != sql.ErrNoRows {
		if err != nil {
			elog.Printf("honker scan err: %s", err)
		} else {
			err = fmt.Errorf("it seems you are already subscribed to them")
		}
		return err
	}

	res, err := stmtSaveHonker.Exec(user.ID, name, url, flavor, combos, owner, mj)
	if err != nil {
		elog.Print(err)
		return err
	}
	honkerid, _ := res.LastInsertId()
	if flavor == "presub" {
		followyou(user, honkerid)
	}
	return nil
}

func cleanupdb(arg string) {
	db := opendatabase()
	days, err := strconv.Atoi(arg)
	var sqlargs []interface{}
	var where string
	if err != nil {
		honker := arg
		expdate := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(dbtimeformat)
		where = "dt < ? and honker = ?"
		sqlargs = append(sqlargs, expdate)
		sqlargs = append(sqlargs, honker)
	} else {
		expdate := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC().Format(dbtimeformat)
		where = "dt < ? and thread not in (select thread from honks where flags & 4 or whofore = 2 or whofore = 3)"
		sqlargs = append(sqlargs, expdate)
	}
	sqlMustQuery(db, "delete from honks where flags & 4 = 0 and whofore = 0 and "+where, sqlargs...)
	sqlMustQuery(db, "delete from attachments where honkid > 0 and honkid not in (select honkid from honks)")
	sqlMustQuery(db, "delete from onts where honkid not in (select honkid from honks)")
	sqlMustQuery(db, "delete from honkmeta where honkid not in (select honkid from honks)")

	sqlMustQuery(db, "delete from filemeta where fileid not in (select fileid from attachments)")
	for _, u := range allusers() {
		sqlMustQuery(db, "delete from zonkers where userid = ? and wherefore = 'mute-thread' and zonkerid < (select zonkerid from zonkers where userid = ? and wherefore = 'mute-thread' order by zonkerid desc limit 1 offset 200)", u.UserID, u.UserID)
	}

	filexids := make(map[string]bool)
	blobdb := openblobdb()
	rows, err := blobdb.Query("select xid from filedata")
	if err != nil {
		elog.Fatal(err)
	}
	for rows.Next() {
		var xid string
		err = rows.Scan(&xid)
		if err != nil {
			elog.Fatal(err)
		}
		filexids[xid] = true
	}
	rows.Close()
	rows, err = db.Query("select xid from filemeta")
	for rows.Next() {
		var xid string
		err = rows.Scan(&xid)
		if err != nil {
			elog.Fatal(err)
		}
		delete(filexids, xid)
	}
	rows.Close()
	tx, err := blobdb.Begin()
	if err != nil {
		elog.Fatal(err)
	}
	for xid, _ := range filexids {
		_, err = tx.Exec("delete from filedata where xid = ?", xid)
		if err != nil {
			elog.Fatal(err)
		}
	}
	err = tx.Commit()
	if err != nil {
		elog.Fatal(err)
	}
}

var stmtHonkers, stmtDubbers, stmtNamedDubbers, stmtSaveHonker, stmtUpdateFlavor, stmtUpdateHonker *sql.Stmt
var stmtDeleteHonker *sql.Stmt
var stmtAnyXonk, stmtOneActivityPubActivity, stmtPublicHonks, stmtUserHonks, stmtHonksByCombo, stmtHonksByThread *sql.Stmt
var stmtHonksByOntology, stmtHonksForUser, stmtHonksForMe, stmtSaveDub, stmtHonksByXonker *sql.Stmt
var stmtHonksFromLongAgo *sql.Stmt
var stmtHonksByHonker, stmtSaveHonk, stmtUserByName, stmtUserByNumber *sql.Stmt
var stmtEventHonks, stmtOneShare, stmtFindZonk, stmtFindXonk, stmtSaveAttachment *sql.Stmt
var stmtFindFile, stmtGetFileData, stmtSaveFileData, stmtSaveFile *sql.Stmt
var stmtCheckFileData *sql.Stmt
var stmtAddResubmission, stmtGetResubmissions, stmtLoadResubmission, stmtDeleteResubmission, stmtOneHonker *sql.Stmt
var stmtUntagged, stmtDeleteHonk, stmtDeleteAttachments, stmtDeleteOnts, stmtSaveZonker *sql.Stmt
var stmtGetZonkers, stmtRecentHonkers, stmtGetXonker, stmtSaveXonker, stmtDeleteXonker, stmtDeleteOldXonkers *sql.Stmt
var stmtAllOnts, stmtSaveOnt, stmtUpdateFlags, stmtClearFlags *sql.Stmt
var stmtHonksForUserFirstClass *sql.Stmt
var stmtSaveMeta, stmtDeleteAllMeta, stmtDeleteOneMeta, stmtDeleteSomeMeta, stmtUpdateHonk *sql.Stmt
var stmtHonksISaved, stmtGetFilters, stmtSaveFilter, stmtDeleteFilter *sql.Stmt
var stmtGetTracks *sql.Stmt
var stmtSaveChonk, stmtLoadChonks, stmtGetChatters *sql.Stmt
var stmtGetTopDubbed *sql.Stmt

func sqlMustPrepare(db *sql.DB, s string) *sql.Stmt {
	stmt, err := db.Prepare(s)
	if err != nil {
		elog.Fatalf("error %s: %s", err, s)
	}
	return stmt
}

func prepareStatements(db *sql.DB) {
	stmtHonkers = sqlMustPrepare(db, "select honkerid, userid, name, xid, flavor, combos, meta from honkers where userid = ? and (flavor = 'presub' or flavor = 'sub' or flavor = 'peep' or flavor = 'unsub') order by name")
	stmtSaveHonker = sqlMustPrepare(db, "insert into honkers (userid, name, xid, flavor, combos, owner, meta, folxid) values (?, ?, ?, ?, ?, ?, ?, '')")
	stmtUpdateFlavor = sqlMustPrepare(db, "update honkers set flavor = ?, folxid = ? where userid = ? and name = ? and xid = ? and flavor = ?")
	stmtUpdateHonker = sqlMustPrepare(db, "update honkers set name = ?, combos = ?, meta = ? where honkerid = ? and userid = ?")
	stmtDeleteHonker = sqlMustPrepare(db, "delete from honkers where honkerid = ?")
	stmtOneHonker = sqlMustPrepare(db, "select xid from honkers where name = ? and userid = ?")
	stmtDubbers = sqlMustPrepare(db, "select honkerid, userid, name, xid, flavor from honkers where userid = ? and flavor = 'dub'")
	stmtNamedDubbers = sqlMustPrepare(db, "select honkerid, userid, name, xid, flavor from honkers where userid = ? and name = ? and flavor = 'dub'")

	selecthonks := "select honks.honkid, honks.userid, username, what, honker, oonker, honks.xid, rid, dt, url, audience, noise, precis, format, thread, whofore, flags from honks join users on honks.userid = users.userid "
	limit := " order by honks.honkid desc limit 250"
	smalllimit := " order by honks.honkid desc limit ?"
	butnotthose := " and thread not in (select name from zonkers where userid = ? and wherefore = 'mute-thread' order by zonkerid desc limit 100)"
	stmtOneActivityPubActivity = sqlMustPrepare(db, selecthonks+"where honks.userid = ? and xid = ?")
	stmtAnyXonk = sqlMustPrepare(db, selecthonks+"where xid = ? order by honks.honkid asc")
	stmtOneShare = sqlMustPrepare(db, selecthonks+"where honks.userid = ? and xid = ? and what = 'share' and whofore = 2")
	stmtPublicHonks = sqlMustPrepare(db, selecthonks+"where whofore = 2 and dt > ?"+smalllimit)
	stmtEventHonks = sqlMustPrepare(db, selecthonks+"where (whofore = 2 or honks.userid = ?) and what = 'event'"+smalllimit)
	stmtUserHonks = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and (whofore = 2 or whofore = ?) and username = ? and dt > ?"+smalllimit)
	myhonkers := " and honker in (select xid from honkers where userid = ? and (flavor = 'sub' or flavor = 'peep' or flavor = 'presub') and combos not like '% - %')"
	stmtHonksForUser = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and honks.userid = ? and dt > ?"+myhonkers+butnotthose+limit)
	stmtHonksForUserFirstClass = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and honks.userid = ? and dt > ? and (what <> 'tonk')"+myhonkers+butnotthose+limit)
	stmtHonksForMe = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and honks.userid = ? and dt > ? and whofore = 1"+butnotthose+limit)
	stmtHonksFromLongAgo = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and honks.userid = ? and dt > ? and dt < ? and whofore = 2"+butnotthose+limit)
	stmtHonksISaved = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and honks.userid = ? and flags & 4 order by honks.honkid desc")
	stmtHonksByHonker = sqlMustPrepare(db, selecthonks+"join honkers on (honkers.xid = honks.honker or honkers.xid = honks.oonker) where honks.honkid > ? and honks.userid = ? and honkers.name = ?"+butnotthose+limit)
	stmtHonksByXonker = sqlMustPrepare(db, selecthonks+" where honks.honkid > ? and honks.userid = ? and (honker = ? or oonker = ?)"+butnotthose+limit)
	stmtHonksByCombo = sqlMustPrepare(db, selecthonks+" where honks.honkid > ? and honks.userid = ? and honks.honker in (select xid from honkers where honkers.userid = ? and honkers.combos like ?) "+butnotthose+" union "+selecthonks+"join onts on honks.honkid = onts.honkid where honks.honkid > ? and honks.userid = ? and onts.ontology in (select xid from honkers where combos like ?)"+butnotthose+limit)
	stmtHonksByThread = sqlMustPrepare(db, selecthonks+"where honks.honkid > ? and (honks.userid = ? or (? = -1 and whofore = 2)) and thread = ?"+limit)
	stmtHonksByOntology = sqlMustPrepare(db, selecthonks+"join onts on honks.honkid = onts.honkid where honks.honkid > ? and onts.ontology = ? and (honks.userid = ? or (? = -1 and honks.whofore = 2))"+limit)

	stmtSaveMeta = sqlMustPrepare(db, "insert into honkmeta (honkid, genus, json) values (?, ?, ?)")
	stmtDeleteAllMeta = sqlMustPrepare(db, "delete from honkmeta where honkid = ?")
	stmtDeleteSomeMeta = sqlMustPrepare(db, "delete from honkmeta where honkid = ? and genus not in ('oldrev')")
	stmtDeleteOneMeta = sqlMustPrepare(db, "delete from honkmeta where honkid = ? and genus = ?")
	stmtSaveHonk = sqlMustPrepare(db, "insert into honks (userid, what, honker, xid, rid, dt, url, audience, noise, thread, whofore, format, precis, oonker, flags) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	stmtDeleteHonk = sqlMustPrepare(db, "delete from honks where honkid = ?")
	stmtUpdateHonk = sqlMustPrepare(db, "update honks set precis = ?, noise = ?, format = ?, whofore = ?, dt = ? where honkid = ?")
	stmtSaveOnt = sqlMustPrepare(db, "insert into onts (ontology, honkid) values (?, ?)")
	stmtDeleteOnts = sqlMustPrepare(db, "delete from onts where honkid = ?")
	stmtSaveAttachment = sqlMustPrepare(db, "insert into attachments (honkid, chonkid, fileid) values (?, ?, ?)")
	stmtDeleteAttachments = sqlMustPrepare(db, "delete from attachments where honkid = ?")
	stmtSaveFile = sqlMustPrepare(db, "insert into filemeta (xid, name, description, url, media, local) values (?, ?, ?, ?, ?, ?)")
	blobdb := openblobdb()
	stmtSaveFileData = sqlMustPrepare(blobdb, "insert into filedata (xid, media, hash, content) values (?, ?, ?, ?)")
	stmtCheckFileData = sqlMustPrepare(blobdb, "select xid from filedata where hash = ?")
	stmtGetFileData = sqlMustPrepare(blobdb, "select media, content from filedata where xid = ?")
	stmtFindXonk = sqlMustPrepare(db, "select honkid from honks where userid = ? and xid = ?")
	stmtFindFile = sqlMustPrepare(db, "select fileid, xid from filemeta where url = ? and local = 1")
	stmtUserByName = sqlMustPrepare(db, "select userid, username, displayname, about, pubkey, seckey, options from users where username = ?")
	stmtUserByNumber = sqlMustPrepare(db, "select userid, username, displayname, about, pubkey, seckey, options from users where userid = ?")
	stmtSaveDub = sqlMustPrepare(db, "insert into honkers (userid, name, xid, flavor, combos, owner, meta, folxid) values (?, ?, ?, ?, '', '', '', ?)")
	stmtAddResubmission = sqlMustPrepare(db, "insert into resubmissions (dt, tries, userid, rcpt, msg) values (?, ?, ?, ?, ?)")
	stmtGetResubmissions = sqlMustPrepare(db, "select resubmissionid, dt from resubmissions")
	stmtLoadResubmission = sqlMustPrepare(db, "select tries, userid, rcpt, msg from resubmissions where resubmissionid = ?")
	stmtDeleteResubmission = sqlMustPrepare(db, "delete resubmissions resubmissions where resubmissionid = ?")
	stmtUntagged = sqlMustPrepare(db, "select xid, rid, flags from (select honkid, xid, rid, flags from honks where userid = ? order by honkid desc limit 10000) order by honkid asc")
	stmtFindZonk = sqlMustPrepare(db, "select zonkerid from zonkers where userid = ? and name = ? and wherefore = 'zonk'")
	stmtGetZonkers = sqlMustPrepare(db, "select zonkerid, name, wherefore from zonkers where userid = ? and wherefore <> 'zonk'")
	stmtSaveZonker = sqlMustPrepare(db, "insert into zonkers (userid, name, wherefore) values (?, ?, ?)")
	stmtGetXonker = sqlMustPrepare(db, "select info from xonkers where name = ? and flavor = ?")
	stmtSaveXonker = sqlMustPrepare(db, "insert into xonkers (name, info, flavor, dt) values (?, ?, ?, ?)")
	stmtDeleteXonker = sqlMustPrepare(db, "delete from xonkers where name = ? and flavor = ? and dt < ?")
	stmtDeleteOldXonkers = sqlMustPrepare(db, "delete from xonkers where flavor = ? and dt < ?")
	stmtRecentHonkers = sqlMustPrepare(db, "select distinct(honker) from honks where userid = ? and honker not in (select xid from honkers where userid = ? and flavor = 'sub') order by honkid desc limit 100")
	stmtUpdateFlags = sqlMustPrepare(db, "update honks set flags = flags | ? where honkid = ?")
	stmtClearFlags = sqlMustPrepare(db, "update honks set flags = flags & ~ ? where honkid = ?")
	stmtAllOnts = sqlMustPrepare(db, "select ontology, count(ontology) from onts join honks on onts.honkid = honks.honkid where (honks.userid = ? or honks.whofore = 2) group by ontology")
	stmtGetFilters = sqlMustPrepare(db, "select hfcsid, json from hfcs where userid = ?")
	stmtSaveFilter = sqlMustPrepare(db, "insert into hfcs (userid, json) values (?, ?)")
	stmtDeleteFilter = sqlMustPrepare(db, "delete from hfcs where userid = ? and hfcsid = ?")
	stmtGetTracks = sqlMustPrepare(db, "select fetches from tracks where xid = ?")
	stmtSaveChonk = sqlMustPrepare(db, "insert into chonks (userid, xid, who, target, dt, noise, format) values (?, ?, ?, ?, ?, ?, ?)")
	stmtLoadChonks = sqlMustPrepare(db, "select chonkid, userid, xid, who, target, dt, noise, format from chonks where userid = ? and dt > ? order by chonkid asc")
	stmtGetChatters = sqlMustPrepare(db, "select distinct(target) from chonks where userid = ?")
	stmtGetTopDubbed = sqlMustPrepare(db, `SELECT COUNT(*) as c,userid FROM honkers WHERE flavor = "dub" GROUP BY userid`)
}
