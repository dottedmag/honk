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
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"regexp"

	"golang.org/x/crypto/bcrypt"
	_ "humungus.tedunangst.com/r/go-sqlite3"
	"humungus.tedunangst.com/r/webs/httpsig"
	"humungus.tedunangst.com/r/webs/login"
)

var savedassetparams = make(map[string]string)

var re_plainname = regexp.MustCompile("^[[:alnum:]_-]+$")

func getassetparam(file string) string {
	if p, ok := savedassetparams[file]; ok {
		return p
	}
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return ""
	}
	hasher := sha512.New()
	hasher.Write(data)

	return fmt.Sprintf("?v=%.8x", hasher.Sum(nil))
}

var dbtimeformat = "2006-01-02 15:04:05"

var alreadyopendb *sql.DB
var stmtConfig *sql.Stmt

func initdb() {
	db := opendatabase()
	_ = openblobdb()

	prepareStatements(db)

	r := bufio.NewReader(os.Stdin)
	err := createuser(db, r)
	if err != nil {
		elog.Print(err)
		return
	}
	// must came later or user above will have negative id
	err = createserveruser(db)
	if err != nil {
		elog.Print(err)
		return
	}

	fmt.Printf("listen address: ")
	addr, err := r.ReadString('\n')
	if err != nil {
		elog.Print(err)
		return
	}
	addr = addr[:len(addr)-1]
	if len(addr) < 1 {
		elog.Print("that's way too short")
		return
	}
	setConfigValue("listenaddr", addr)
	fmt.Printf("server name: ")
	addr, err = r.ReadString('\n')
	if err != nil {
		elog.Print(err)
		return
	}
	addr = addr[:len(addr)-1]
	if len(addr) < 1 {
		elog.Print("that's way too short")
		return
	}
	setConfigValue("servername", addr)
	var randbytes [16]byte
	rand.Read(randbytes[:])
	key := fmt.Sprintf("%x", randbytes)
	setConfigValue("csrfkey", key)

	setConfigValue("servermsg", "<h2>Things happen.</h2>")
	setConfigValue("aboutmsg", "<h3>What is honk?</h3><p>Honk is amazing!")
	setConfigValue("loginmsg", "<h2>login</h2>")
	setConfigValue("devel", 0)

	db.Close()
	fmt.Printf("done.\n")
	os.Exit(0)
}

func adduser() {
	db := opendatabase()
	defer func() {
		os.Exit(1)
	}()
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Printf("\n")
		os.Exit(1)
	}()

	r := bufio.NewReader(os.Stdin)

	err := createuser(db, r)
	if err != nil {
		elog.Print(err)
		return
	}

	os.Exit(0)
}

func deluser(username string) {
	user, _ := getUserBio(username)
	if user == nil {
		elog.Printf("no userfound")
		return
	}
	userid := user.ID
	db := opendatabase()

	where := " where honkid in (select honkid from honks where userid = ?)"
	sqlMustQuery(db, "delete from attachments"+where, userid)
	sqlMustQuery(db, "delete from hashtags"+where, userid)
	sqlMustQuery(db, "delete from honkmeta"+where, userid)
	where = " where chatMessageId in (select chatMessageId from chatMessages where userid = ?)"
	sqlMustQuery(db, "delete from attachments"+where, userid)

	sqlMustQuery(db, "delete from honks where userid = ?", userid)
	sqlMustQuery(db, "delete from chatMessages where userid = ?", userid)
	sqlMustQuery(db, "delete from authors where userid = ?", userid)
	sqlMustQuery(db, "delete from actions where userid = ?", userid)
	sqlMustQuery(db, "delete from resubmissions where userid = ?", userid)
	sqlMustQuery(db, "delete from hfcs where userid = ?", userid)
	sqlMustQuery(db, "delete from auth where userid = ?", userid)
	sqlMustQuery(db, "delete from users where userid = ?", userid)
}

func chpass() {
	if len(os.Args) < 3 {
		fmt.Printf("need a username\n")
		os.Exit(1)
	}
	user, err := getUserBio(os.Args[2])
	if err != nil {
		elog.Fatal(err)
	}
	defer func() {
		os.Exit(1)
	}()
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		fmt.Printf("\n")
		os.Exit(1)
	}()

	db := opendatabase()
	login.Init(login.InitArgs{Db: db, Logger: ilog})

	r := bufio.NewReader(os.Stdin)

	pass, err := askpassword(r)
	if err != nil {
		elog.Print(err)
		return
	}
	err = login.SetPassword(user.ID, pass)
	if err != nil {
		elog.Print(err)
		return
	}
	fmt.Printf("done\n")
	os.Exit(0)
}

func askpassword(r *bufio.Reader) (string, error) {
	fmt.Printf("password: ")
	pass, err := r.ReadString('\n')
	fmt.Printf("\n")
	if err != nil {
		return "", err
	}
	pass = pass[:len(pass)-1]
	if len(pass) < 6 {
		return "", fmt.Errorf("that's way too short")
	}
	return pass, nil
}

func createuser(db *sql.DB, r *bufio.Reader) error {
	fmt.Printf("username: ")
	name, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	name = name[:len(name)-1]
	if len(name) < 1 {
		return fmt.Errorf("that's way too short")
	}
	if !re_plainname.MatchString(name) {
		return fmt.Errorf("alphanumeric only please")
	}
	if _, err := getUserBio(name); err == nil {
		return fmt.Errorf("user already exists")
	}
	pass, err := askpassword(r)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), 12)
	if err != nil {
		return err
	}
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pubkey, err := httpsig.EncodeKey(&k.PublicKey)
	if err != nil {
		return err
	}
	seckey, err := httpsig.EncodeKey(k)
	if err != nil {
		return err
	}
	about := "what about me?"
	_, err = db.Exec("insert into users (username, displayname, about, hash, pubkey, seckey, options) values (?, ?, ?, ?, ?, ?, ?)", name, name, about, hash, pubkey, seckey, "{}")
	if err != nil {
		return err
	}
	return nil
}

func createserveruser(db *sql.DB) error {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	pubkey, err := httpsig.EncodeKey(&k.PublicKey)
	if err != nil {
		return err
	}
	seckey, err := httpsig.EncodeKey(k)
	if err != nil {
		return err
	}
	name := "server"
	about := "server"
	hash := "*"
	_, err = db.Exec("insert into users (userid, username, displayname, about, hash, pubkey, seckey, options) values (?, ?, ?, ?, ?, ?, ?, ?)", serverUID, name, name, about, hash, pubkey, seckey, "")
	if err != nil {
		return err
	}
	return nil
}

func opendatabase() *sql.DB {
	if alreadyopendb != nil {
		return alreadyopendb
	}
	dbname := dataDir + "/honk.db"
	db, err := sql.Open("sqlite3", dbname)
	if err != nil {
		elog.Fatalf("unable to open database: %s", err)
	}
	if _, err := db.Exec("pragma journal_mode = wal"); err != nil {
		elog.Fatal(err)
	}
	if err := upgradeDB(context.Background(), db); err != nil {
		elog.Fatal("Unable to upgrade database: ", err)
	}
	stmtConfig, err = db.Prepare("select value from config where key = ?")
	if err != nil {
		elog.Fatal(err)
	}
	alreadyopendb = db
	return db
}

func openblobdb() *sql.DB {
	blobdbname := dataDir + "/blob.db"
	db, err := sql.Open("sqlite3", blobdbname)
	if err != nil {
		elog.Fatalf("unable to open database: %s", err)
	}
	if _, err := db.Exec("pragma journal_mode = wal"); err != nil {
		elog.Fatal(err)
	}
	if err := upgradeBlobDB(context.Background(), db); err != nil {
		elog.Fatal("Unable to upgrade blob database: ", err)
	}
	return db
}

func getConfigValue(key string, value interface{}) error {
	m, ok := value.(*map[string]bool)
	if ok {
		rows, err := stmtConfig.Query(key)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			err = rows.Scan(&s)
			if err != nil {
				return err
			}
			(*m)[s] = true
		}
		return nil
	}
	row := stmtConfig.QueryRow(key)
	err := row.Scan(value)
	if err == sql.ErrNoRows {
		err = nil
	}
	return err
}

func setConfigValue(key string, val interface{}) error {
	db := opendatabase()
	db.Exec("delete from config where key = ?", key)
	_, err := db.Exec("insert into config (key, value) values (?, ?)", key, val)
	return err
}

func openListener() (net.Listener, error) {
	var listenAddr string
	err := getConfigValue("listenaddr", &listenAddr)
	if err != nil {
		return nil, err
	}
	if listenAddr == "" {
		return nil, fmt.Errorf("must have listenaddr")
	}
	proto := "tcp"
	if listenAddr[0] == '/' {
		proto = "unix"
		err := os.Remove(listenAddr)
		if err != nil && !os.IsNotExist(err) {
			elog.Printf("unable to unlink socket: %s", err)
		}
	}
	listener, err := net.Listen(proto, listenAddr)
	if err != nil {
		return nil, err
	}
	if proto == "unix" {
		os.Chmod(listenAddr, 0777)
	}
	return listener, nil
}

type dbexecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func sqlMustQuery(db dbexecer, s string, args ...interface{}) {
	_, err := db.Exec(s, args...)
	if err != nil {
		elog.Fatalf("can't run %s: %s", s, err)
	}
}
